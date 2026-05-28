package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

func writeFile(path, content string, mode os.FileMode) error {
	return os.WriteFile(path, []byte(content), mode)
}

func TestServer_HealthEndpoint(t *testing.T) {
	logger.Init()

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
	logger.Init()

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
	logger.Init()

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
	logger.Init()

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
}

func TestServer_RouteNotFound(t *testing.T) {
	logger.Init()

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
