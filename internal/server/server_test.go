package server

import (
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

func init() {
	logger.Init()
}

func writeFile(path, content string, mode os.FileMode) error {
	return os.WriteFile(path, []byte(content), mode)
}

func TestServer_HealthEndpoint(t *testing.T) {
	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{},
	}

	srv := New(cfg)

	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go ts.Serve(listener)
	defer ts.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", string(body), `{"status":"ok"}`)
	}
}

func TestServer_StaticRoute(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/index.html", "hello world", 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Port: 0,
		Routes: []config.Route{
			{Path: "/", StaticDir: dir},
		},
	}

	srv := New(cfg)

	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go ts.Serve(listener)
	defer ts.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello world" {
		t.Errorf("body = %q, want %q", string(body), "hello world")
	}
}

func TestServer_ProxyRoute(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend"))
	}))
	defer backend.Close()

	cfg := config.Config{
		Port: 0,
		Routes: []config.Route{
			{Path: "/api", Upstreams: []string{backend.URL}},
		},
	}

	srv := New(cfg)

	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go ts.Serve(listener)
	defer ts.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/api/test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "backend" {
		t.Errorf("body = %q, want %q", string(body), "backend")
	}
}

func TestServer_GracefulShutdown(t *testing.T) {
	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{},
	}

	srv := New(cfg)

	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go ts.Serve(listener)

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ts.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestResponseWriter(t *testing.T) {
	rw := newResponseWriter(httptest.NewRecorder())

	if rw.status != http.StatusOK {
		t.Errorf("initial status = %d, want %d", rw.status, http.StatusOK)
	}

	rw.WriteHeader(http.StatusNotFound)
	if rw.status != http.StatusNotFound {
		t.Errorf("status after WriteHeader = %d, want %d", rw.status, http.StatusNotFound)
	}

	rw.WriteHeader(http.StatusForbidden)
	if rw.status != http.StatusNotFound {
		t.Errorf("status should not change after first WriteHeader, got %d", rw.status)
	}

	rw.SetUpstream("10.0.0.1:8080")
	if rw.upstream != "10.0.0.1:8080" {
		t.Errorf("upstream = %q, want %q", rw.upstream, "10.0.0.1:8080")
	}
}

func TestServer_RouteNotFound(t *testing.T) {
	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{},
	}

	srv := New(cfg)

	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go ts.Serve(listener)
	defer ts.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{"X-Forwarded-For single", "1.1.1.1:1234", map[string]string{"X-Forwarded-For": "2.2.2.2"}, "2.2.2.2"},
		{"X-Forwarded-For multi", "1.1.1.1:1234", map[string]string{"X-Forwarded-For": "2.2.2.2, 3.3.3.3"}, "2.2.2.2"},
		{"RemoteAddr IPv4", "4.4.4.4:5678", nil, "4.4.4.4"},
		{"RemoteAddr IPv6", "[::1]:8080", nil, "[::1]"},
		{"RemoteAddr no port", "5.5.5.5", nil, "5.5.5.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			got := extractIP(req)
			if got != tt.want {
				t.Errorf("extractIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTokenBucket(t *testing.T) {
	t.Run("allow and deny", func(t *testing.T) {
		tb := newTokenBucket(0, 1)
		if !tb.allow() {
			t.Error("expected allow on first call")
		}
		if tb.allow() {
			t.Error("expected deny after tokens exhausted")
		}
	})

	t.Run("burst capacity", func(t *testing.T) {
		tb := newTokenBucket(0, 3)
		for i := 0; i < 3; i++ {
			if !tb.allow() {
				t.Errorf("expected allow on call %d", i+1)
			}
		}
		if tb.allow() {
			t.Error("expected deny after burst exhausted")
		}
	})

	t.Run("refill over time", func(t *testing.T) {
		tb := newTokenBucket(100, 1)
		tb.allow()

		time.Sleep(50 * time.Millisecond)

		if !tb.allow() {
			t.Error("expected allow after refill")
		}
	})
}

func TestGzipMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	})
	handler := gzipMiddleware(inner)

	t.Run("with gzip", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Error("expected Content-Encoding: gzip")
		}
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Error("expected Vary: Accept-Encoding")
		}

		gr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer gr.Close()
		body, _ := io.ReadAll(gr)
		if string(body) != "hello world" {
			t.Errorf("body = %q, want %q", string(body), "hello world")
		}
	})

	t.Run("without gzip", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") == "gzip" {
			t.Error("did not expect gzip Content-Encoding")
		}
		if rec.Body.String() != "hello world" {
			t.Errorf("body = %q, want %q", rec.Body.String(), "hello world")
		}
	})
}

func TestServer_MetricsEndpoint(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	cfg := config.Config{
		Port: 0,
		Routes: []config.Route{
			{Path: "/api", Upstreams: []string{backend.URL}},
		},
	}
	srv := New(cfg)

	// serve a request so counters are non-zero
	srv.middleware(srv.mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"gofly_build_info",
		"gofly_requests_total",
		"gofly_requests_in_flight",
		"gofly_response_bytes_total",
		"gofly_goroutines",
		"gofly_upstream_healthy",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestServer_MetricsDisabled(t *testing.T) {
	off := false
	cfg := config.Config{
		Port:    0,
		Metrics: &off,
		Routes:  []config.Route{},
	}
	srv := New(cfg)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (/metrics should be absent when disabled)", rec.Code, http.StatusNotFound)
	}
}

func TestServer_TrailingSlashRouteNoPanic(t *testing.T) {
	// A proxy route whose path already ends in "/" must not double-register the
	// same ServeMux pattern (which would panic at startup).
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("api"))
	}))
	defer backend.Close()

	cfg := config.Config{
		Port: 0,
		Routes: []config.Route{
			{Path: "/api/", Upstreams: []string{backend.URL}},
		},
	}

	srv := New(cfg) // would panic before the registerPattern fix

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/things", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "api" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "api")
	}
}

func TestGzipMinLength(t *testing.T) {
	big := strings.Repeat("x", 500)
	inner := func(body string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		})
	}

	t.Run("below threshold stays plain", func(t *testing.T) {
		handler := gzipMiddlewareWith(inner("tiny"), -1, 100)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") == "gzip" {
			t.Error("small body should not be gzipped")
		}
		if rec.Body.String() != "tiny" {
			t.Errorf("body = %q, want %q", rec.Body.String(), "tiny")
		}
	})

	t.Run("above threshold compresses", func(t *testing.T) {
		handler := gzipMiddlewareWith(inner(big), -1, 100)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("large body should be gzipped")
		}
		gr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer gr.Close()
		got, _ := io.ReadAll(gr)
		if string(got) != big {
			t.Errorf("decompressed body mismatch")
		}
	})
}

func TestGzipWriterPoolReuse(t *testing.T) {
	handler := gzipMiddlewareWith(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world hello world"))
	}), -1, 0)

	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("iteration %d: missing gzip encoding", i)
		}
		gr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		got, _ := io.ReadAll(gr)
		gr.Close()
		if string(got) != "hello world hello world" {
			t.Fatalf("iteration %d: body mismatch %q", i, got)
		}
	}
}

func TestRateLimiterSweepEvictsIdle(t *testing.T) {
	rl := newRateLimiter(100, 10, 10*time.Millisecond)

	if !rl.allow("1.2.3.4") {
		t.Fatal("first request should be allowed")
	}
	if got := countBuckets(rl); got != 1 {
		t.Fatalf("buckets = %d, want 1", got)
	}

	time.Sleep(20 * time.Millisecond)
	rl.sweep()

	if got := countBuckets(rl); got != 0 {
		t.Errorf("buckets after sweep = %d, want 0 (idle bucket should be evicted)", got)
	}
}

func TestRateLimiterShardingDistributes(t *testing.T) {
	rl := newRateLimiter(100, 10, time.Minute)
	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"} {
		rl.allow(ip)
	}
	if got := countBuckets(rl); got != 4 {
		t.Errorf("distinct buckets = %d, want 4", got)
	}
}

func countBuckets(rl *rateLimiter) int {
	n := 0
	for i := range rl.shards {
		rl.shards[i].mu.Lock()
		n += len(rl.shards[i].buckets)
		rl.shards[i].mu.Unlock()
	}
	return n
}

func TestHostBasedRouting(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/index.html", "host content", 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Port: 0,
		Routes: []config.Route{
			{Path: "/", ServerName: "example.com", StaticDir: dir},
		},
	}

	srv := New(cfg)

	t.Run("matching host", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "example.com"
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if rec.Body.String() != "host content" {
			t.Errorf("body = %q, want %q", rec.Body.String(), "host content")
		}
	})

	t.Run("non-matching host", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "other.com"
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestSwappableHandler(t *testing.T) {
	var calls1, calls2 int
	h1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls1++
		w.WriteHeader(http.StatusOK)
	})
	h2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls2++
		w.WriteHeader(http.StatusTeapot)
	})

	sh := &swappableHandler{}
	sh.Swap(h1)

	rec := httptest.NewRecorder()
	sh.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("initial handler status = %d, want %d", rec.Code, http.StatusOK)
	}
	if calls1 != 1 {
		t.Errorf("h1 should have been called once, got %d calls", calls1)
	}

	sh.Swap(h2)
	rec2 := httptest.NewRecorder()
	sh.ServeHTTP(rec2, httptest.NewRequest("GET", "/", nil))
	if rec2.Code != http.StatusTeapot {
		t.Errorf("after swap status = %d, want %d", rec2.Code, http.StatusTeapot)
	}
	if calls2 != 1 {
		t.Errorf("h2 should have been called once, got %d calls", calls2)
	}
	if calls1 != 1 {
		t.Errorf("h1 should still have been called once after swap, got %d calls", calls1)
	}
}

func TestServer_StaticRouteWithSetHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/index.html", "cors test", 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Port: 0,
		Routes: []config.Route{
			{
				Path:       "/",
				StaticDir:  dir,
				SetHeaders: map[string]string{"Access-Control-Allow-Origin": "*"},
			},
		},
	}

	srv := New(cfg)
	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go ts.Serve(listener)
	defer ts.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	cors := resp.Header.Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", cors, "*")
	}
}

func TestServer_ConfiglessMode(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/index.html", "configless", 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Port:    0,
		Workers: 1,
		Routes: []config.Route{
			{Path: "/", StaticDir: dir},
		},
	}

	srv := New(cfg)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go srv.http.Serve(listener)
	defer srv.http.Close()

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + listener.Addr().String() + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "configless" {
		t.Errorf("body = %q, want %q", string(body), "configless")
	}
}

func TestServer_EmptyRoutes(t *testing.T) {
	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{},
	}

	srv := New(cfg)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go srv.http.Serve(listener)
	defer srv.http.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
