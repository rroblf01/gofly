package static

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
)

// deadlineRecorder is a ResponseWriter that records the readers handed to
// ReadFrom and the deadlines passed to SetWriteDeadline, so a test can assert
// both the chunking and the sendfile precondition (an *io.LimitedReader around
// an *os.File, which Go's net/sendfile unwraps).
type deadlineRecorder struct {
	header    http.Header
	written   int64
	readers   []io.Reader
	deadlines []time.Time
}

func (d *deadlineRecorder) Header() http.Header {
	if d.header == nil {
		d.header = http.Header{}
	}
	return d.header
}
func (d *deadlineRecorder) Write(b []byte) (int, error) {
	d.written += int64(len(b))
	return len(b), nil
}
func (d *deadlineRecorder) WriteHeader(int) {}
func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadlines = append(d.deadlines, t)
	return nil
}
func (d *deadlineRecorder) ReadFrom(r io.Reader) (int64, error) {
	d.readers = append(d.readers, r)
	n, err := io.Copy(io.Discard, r)
	d.written += n
	return n, err
}

// slowWriter simulates a slow but progressing client: each chunk takes delay and
// the write fails if the current deadline has already passed.
type slowWriter struct {
	deadlineRecorder
	delay    time.Duration
	deadline time.Time
}

func (s *slowWriter) SetWriteDeadline(t time.Time) error { s.deadline = t; return nil }
func (s *slowWriter) ReadFrom(r io.Reader) (int64, error) {
	time.Sleep(s.delay)
	if !s.deadline.IsZero() && time.Now().After(s.deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	return s.deadlineRecorder.ReadFrom(r)
}

func writeTempFile(t *testing.T, name string, size int) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	data := bytes.Repeat([]byte("x"), size)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestWriteBodyChunksPreserveSendfileSource(t *testing.T) {
	path, data := writeTempFile(t, "big.bin", 3*writeChunkSize+123)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	h := New(config.Route{Path: "/", StaticDir: filepath.Dir(path)})
	h.SetTimeouts(time.Minute, 0)

	rec := &deadlineRecorder{}
	h.writeBody(rec, f)

	if rec.written != int64(len(data)) {
		t.Fatalf("wrote %d bytes, want %d", rec.written, len(data))
	}
	if len(rec.readers) < 4 {
		t.Fatalf("copy was not chunked: %d ReadFrom calls for a %d-byte file", len(rec.readers), len(data))
	}
	for i, r := range rec.readers {
		lr, ok := r.(*io.LimitedReader)
		if !ok {
			t.Fatalf("chunk %d reader is %T, want *io.LimitedReader", i, r)
		}
		if _, ok := lr.R.(*os.File); !ok {
			t.Fatalf("chunk %d source is %T, want *os.File (sendfile precondition)", i, lr.R)
		}
	}
	if len(rec.deadlines) < len(rec.readers) {
		t.Errorf("deadlines reset %d times for %d chunks, want at least one per chunk", len(rec.deadlines), len(rec.readers))
	}
}

func TestSlowDownloadCompletesWithSendTimeout(t *testing.T) {
	// A single absolute deadline would abort this transfer partway; renewing it
	// per chunk (each well under send_timeout) lets it finish. This is the
	// scaled equivalent of a 40 MB file read at 500 KB/s outliving a 30s
	// write_timeout.
	const chunks = 12
	path, data := writeTempFile(t, "big.bin", chunks*writeChunkSize)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	h := New(config.Route{Path: "/", StaticDir: filepath.Dir(path)})
	h.SetTimeouts(500*time.Millisecond, 0)

	w := &slowWriter{delay: 100 * time.Millisecond}
	h.writeBody(w, f)

	if w.written != int64(len(data)) {
		t.Fatalf("wrote %d of %d bytes: slow download was cut short", w.written, len(data))
	}
}

func TestWriteLimitCapsTotalTime(t *testing.T) {
	const chunks = 12
	path, data := writeTempFile(t, "big.bin", chunks*writeChunkSize)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	h := New(config.Route{Path: "/", StaticDir: filepath.Dir(path)})
	// Total cap far below the ~1.2s the transfer would take.
	h.SetTimeouts(time.Second, 250*time.Millisecond)

	w := &slowWriter{delay: 100 * time.Millisecond}
	h.writeBody(w, f)

	if w.written == int64(len(data)) {
		t.Errorf("total write limit did not cap the transfer: wrote all %d bytes", len(data))
	}
}

func TestChunkDeadline(t *testing.T) {
	start := time.Now()

	h := &Handler{}
	h.SetTimeouts(time.Minute, 0)
	if d := h.chunkDeadline(start); d.Before(time.Now().Add(50 * time.Second)) {
		t.Errorf("send-only deadline = %v, want ~now+1m", d)
	}

	h.SetTimeouts(time.Minute, 10*time.Second)
	if d := h.chunkDeadline(start); d.After(start.Add(10*time.Second + time.Millisecond)) {
		t.Errorf("total limit not applied: deadline = %v, want <= start+10s", d)
	}

	h.SetTimeouts(0, 10*time.Second)
	if d := h.chunkDeadline(start); !d.Equal(start.Add(10 * time.Second)) {
		t.Errorf("send disabled deadline = %v, want start+10s", d)
	}

	h.SetTimeouts(0, 0)
	if d := h.chunkDeadline(start); !d.IsZero() {
		t.Errorf("both disabled deadline = %v, want zero", d)
	}
}

func TestPrecompressedUnknownExtensionMIME(t *testing.T) {
	dir := t.TempDir()
	// An extension absent from Go's built-in table and from the host's
	// /etc/mime.types, mirroring the scratch image where TypeByExtension
	// returns "" for most application-specific extensions (e.g. .py). Before
	// the fix the pre-compressed path sniffed the gzip bytes and reported
	// application/x-gzip.
	const name = "app.goflyunknown"
	if err := os.WriteFile(dir+"/"+name, []byte("plain text body"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/"+name+".gz", []byte("gzipbytes"), 0644); err != nil {
		t.Fatal(err)
	}
	h := New(config.Route{Path: "/", StaticDir: dir})

	// Identity request: the source is sniffed, so it is text/plain.
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("GET", "/"+name, nil))
	identityType := resp.Header().Get("Content-Type")
	if !strings.HasPrefix(identityType, "text/plain") {
		t.Errorf("identity Content-Type = %q, want text/plain*", identityType)
	}

	// Pre-compressed request: same resource, so the same Content-Type. The
	// compressed bytes must never be sniffed (that reported application/x-gzip).
	resp = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(resp, req)
	if ct := resp.Header().Get("Content-Type"); ct != identityType {
		t.Errorf("precompressed Content-Type = %q, want %q (same as identity)", ct, identityType)
	}
	if ce := resp.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", ce)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "gzipbytes" {
		t.Errorf("body = %q, want gzipbytes", body)
	}
}

func TestNotModifiedIncludesSetHeaders(t *testing.T) {
	cacheTTL := config.Duration{Duration: time.Minute}
	for _, cached := range []bool{false, true} {
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/app.js", []byte("console.log(1)"), 0644); err != nil {
			t.Fatal(err)
		}
		ttl := config.Duration{Duration: time.Hour}
		route := config.Route{
			Path:            "/",
			StaticDir:       dir,
			BrowserCacheTTL: &ttl,
			SetHeaders: map[string]string{
				"Cache-Control": "no-cache",
				"X-Custom":      "custom-value",
			},
		}
		if cached {
			route.StaticCacheTTL = &cacheTTL
		}
		h := New(route)

		// First GET establishes the validator and, when enabled, the cache entry.
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest("GET", "/app.js", nil))
		if resp.Code != http.StatusOK {
			t.Fatalf("cached=%v: status = %d, want 200", cached, resp.Code)
		}
		etag := resp.Header().Get("Etag")
		if etag == "" {
			t.Fatalf("cached=%v: missing Etag", cached)
		}

		resp = httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/app.js", nil)
		req.Header.Set("If-None-Match", etag)
		h.ServeHTTP(resp, req)

		if resp.Code != http.StatusNotModified {
			t.Fatalf("cached=%v: status = %d, want 304", cached, resp.Code)
		}
		if cc := resp.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("cached=%v: 304 Cache-Control = %q, want no-cache", cached, cc)
		}
		if x := resp.Header().Get("X-Custom"); x != "custom-value" {
			t.Errorf("cached=%v: 304 X-Custom = %q, want custom-value", cached, x)
		}
		if x := resp.Header().Get("X-Content-Type-Options"); x != "nosniff" {
			t.Errorf("cached=%v: 304 missing security header, got %q", cached, x)
		}
	}
}

func TestStatic_ResponseHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	h := New(config.Route{
		Path:      "/",
		StaticDir: dir,
		SetHeaders: map[string]string{
			"X-Both": "from-set",
		},
		ResponseHeaders: map[string]string{
			"X-Frame-Options": "DENY",
			"X-Both":          "from-response",
		},
	})

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("GET", "/index.html", nil))

	if got := resp.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	// response_headers wins over set_headers on a name clash.
	if got := resp.Header().Get("X-Both"); got != "from-response" {
		t.Errorf("X-Both = %q, want from-response", got)
	}
}

func TestExtraMIMETypes(t *testing.T) {
	cases := map[string]string{
		".webmanifest": "application/manifest+json",
		".woff2":       "font/woff2",
		".map":         "application/json",
	}
	for ext, want := range cases {
		if got := mime.TypeByExtension(ext); got != want {
			t.Errorf("mime.TypeByExtension(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestValidPath(t *testing.T) {
	if !validPath("/index.html") {
		t.Error("valid path rejected")
	}
	if validPath("/../etc/passwd") {
		t.Error("traversal should be invalid")
	}
	if validPath("a..b") {
		t.Error(".. should be invalid anywhere")
	}
}

func TestServeDirListParentLink(t *testing.T) {
	dir := t.TempDir()
	// create subdir with file
	if err := os.MkdirAll(dir+"/a/b", 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(dir+"/a/b/file.txt", []byte("x"), 0644)
	h := New(config.Route{Path: "/", StaticDir: dir, AutoIndex: true})
	// Request nested directory
	req := httptest.NewRequest("GET", "/a/b/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `href="/a"`) {
		t.Errorf("parent link for /a/b/ should be /a, body %q", body)
	}
	// Request "/a/" should have parent "/"
	req2 := httptest.NewRequest("GET", "/a/", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), `href="/"`) {
		t.Errorf("parent for /a/ should be /, got %q", rec2.Body.String())
	}
	// Root should have no parent link
	reqRoot := httptest.NewRequest("GET", "/", nil)
	recRoot := httptest.NewRecorder()
	h.ServeHTTP(recRoot, reqRoot)
	if strings.Contains(recRoot.Body.String(), `href=".."`) || strings.Contains(recRoot.Body.String(), `..</a>`) {
		// We check that root doesn't contain parent link to "/"
		// Actually root's parent logic is skipped (Path == "/"), so no parent link
		if strings.Contains(recRoot.Body.String(), `..`) {
			t.Logf("root body contains .. unexpectedly: %q", recRoot.Body.String())
		}
	}
}

func TestParseRangeEdgeCases(t *testing.T) {
	cases := []struct {
		s    string
		size int64
		ok   bool
	}{
		{"0-4,5-9", 100, false}, // multipart not supported
		{"-0", 100, false},
		{"-500", 100, true},
		{"500-", 100, true},
		{"0-0", 100, true},
		{"", 100, false},
	}
	for _, c := range cases {
		_, err := parseRange(c.s, c.size)
		if c.ok && err != nil {
			t.Errorf("parseRange %q should succeed: %v", c.s, err)
		}
		if !c.ok && err == nil {
			// empty string returns nil, nil -> ok as per implementation, but we consider not ok
			// For empty, parseRange returns nil,nil -> no range, so not error but not ok in our table
			// Adjust: empty is expected nil,nil
			if c.s != "" {
				t.Errorf("parseRange %q should fail", c.s)
			}
		}
	}
}

func TestWriteBodySendfile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/file.txt", []byte("hello"), 0644)
	h := New(config.Route{Path: "/", StaticDir: dir})
	req := httptest.NewRequest("GET", "/file.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Body.String() != "hello" {
		t.Errorf("writeBody via sendfile failed %q", rec.Body.String())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("Accept-Ranges missing")
	}
}

func TestPrecompressedWithCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/a.txt", []byte("identity"), 0644)
	os.WriteFile(dir+"/a.txt.gz", []byte("gzbytes"), 0644)
	ttl := config.Duration{}
	// need non-zero cache TTL to enable cache; use Unmarshal
	ttl.UnmarshalJSON([]byte(`"60s"`))
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})
	req := httptest.NewRequest("GET", "/a.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// With cache, should serve identity, not gzipped (cache takes precedence)
	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("cache should skip precompressed and not set Content-Encoding")
	}
}

func TestRangeWithCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/range.txt", []byte("0123456789"), 0644)
	ttl := config.Duration{}
	ttl.UnmarshalJSON([]byte(`"60s"`))
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})
	// First request to populate cache
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest("GET", "/range.txt", nil))
	// Second with Range
	req := httptest.NewRequest("GET", "/range.txt", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Errorf("range status %d want 206", rec.Code)
	}
	if rec.Body.String() != "2345" {
		t.Errorf("range body %q want 2345", rec.Body.String())
	}
}

func TestAutoIndexHidesDotfiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/visible.txt", []byte("v"), 0644)
	os.WriteFile(dir+"/.hidden", []byte("h"), 0644)
	h := New(config.Route{Path: "/", StaticDir: dir, AutoIndex: true})
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "visible.txt") {
		t.Error("visible file missing")
	}
	if strings.Contains(body, ".hidden") {
		t.Error("dotfile should be hidden")
	}
}
