package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/nginx"
)

// slowBodyWriter simulates a slow but progressing client: each chunk takes
// delay, and a write fails once the current deadline has passed.
type slowBodyWriter struct {
	header   http.Header
	written  int64
	deadline time.Time
	delay    time.Duration
}

func (s *slowBodyWriter) Header() http.Header {
	if s.header == nil {
		s.header = http.Header{}
	}
	return s.header
}
func (s *slowBodyWriter) WriteHeader(int) {}
func (s *slowBodyWriter) Write(b []byte) (int, error) {
	s.written += int64(len(b))
	return len(b), nil
}
func (s *slowBodyWriter) SetWriteDeadline(t time.Time) error { s.deadline = t; return nil }
func (s *slowBodyWriter) ReadFrom(r io.Reader) (int64, error) {
	time.Sleep(s.delay)
	if !s.deadline.IsZero() && time.Now().After(s.deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	n, err := io.Copy(io.Discard, r)
	s.written += n
	return n, err
}

// TestConvertedConfigSlowDownloadCompletes reproduces the reported regression
// end to end: a minimal nginx.conf is converted, the JSON is loaded exactly as
// the binary loads it, and the resulting server serves a multi-chunk file to a
// slow client whose total transfer time exceeds the absolute write budget.
//
// The converted config must not carry an explicit write_timeout; if it did,
// config.Load would mark it explicit and StaticWriteLimit would cap the body,
// truncating the download.
func TestConvertedConfigSlowDownloadCompletes(t *testing.T) {
	cfg, _, err := nginx.Convert(strings.NewReader("server {\n  listen 80;\n  root /www;\n}\n"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, raw, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.StaticWriteLimit(); got != 0 {
		t.Fatalf("converted config StaticWriteLimit = %v, want 0 (no total cap)", got)
	}

	// Scale the absolute write budget down so the test stays fast. This changes
	// only the deadline, not the explicit flag set by Load.
	loaded.WriteTimeout = config.Duration{Duration: 300 * time.Millisecond}

	www := t.TempDir()
	data := bytes.Repeat([]byte("x"), 12*(1<<20)) // 12 chunks of 1 MiB
	if err := os.WriteFile(filepath.Join(www, "big.bin"), data, 0644); err != nil {
		t.Fatal(err)
	}
	loaded.Routes[0].StaticDir = www

	srv := New(loaded)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// ~600ms total, well past the 300ms absolute budget, but each 50ms chunk is
	// far below send_timeout, so per-chunk renewal must carry the transfer.
	w := &slowBodyWriter{delay: 50 * time.Millisecond}
	req := httptest.NewRequest("GET", "/big.bin", nil)
	req.Header.Set("Accept-Encoding", "identity")
	srv.http.Handler.ServeHTTP(w, req)

	if w.written != int64(len(data)) {
		t.Fatalf("wrote %d of %d bytes: converted-config slow download was cut short", w.written, len(data))
	}
}
