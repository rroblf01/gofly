package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

func BenchmarkServer_StaticFile(b *testing.B) {
	logger.Init()

	dir := b.TempDir()
	www := filepath.Join(dir, "www")
	os.MkdirAll(www, 0755)
	os.WriteFile(www+"/index.html", []byte(benchPageHTML), 0644)
	os.WriteFile(www+"/style.css", []byte("body{}"), 0644)

	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{
			{Path: "/", StaticDir: www},
		},
	}

	srv := New(cfg)
	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Fatal(err)
	}
	go ts.Serve(listener)
	defer ts.Close()
	time.Sleep(10 * time.Millisecond)

	addr := "http://" + listener.Addr().String() + "/index.html"
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(addr)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkServer_HealthEndpoint(b *testing.B) {
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
		b.Fatal(err)
	}
	go ts.Serve(listener)
	defer ts.Close()
	time.Sleep(10 * time.Millisecond)

	addr := "http://" + listener.Addr().String() + "/health"
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(addr)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkServer_RateLimitEnabled(b *testing.B) {
	logger.Init()

	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{},
		RateLimit: &config.RateLimit{
			RequestsPerSecond: 100000,
			Burst:             100000,
		},
	}

	srv := New(cfg)

	handler := srv.rateLimitMiddleware(srv.middleware(srv.mux))
	ts := &http.Server{
		Addr:    ":0",
		Handler: handler,
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Fatal(err)
	}
	go ts.Serve(listener)
	defer ts.Close()
	time.Sleep(10 * time.Millisecond)

	addr := "http://" + listener.Addr().String() + "/health"
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(addr)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkServer_ConcurrentStatic(b *testing.B) {
	logger.Init()

	dir := b.TempDir()
	www := filepath.Join(dir, "www")
	os.MkdirAll(www, 0755)
	for i := range 100 {
		os.WriteFile(fmt.Sprintf("%s/file%d.html", www, i), []byte(fmt.Sprintf(benchPageHTML, i)), 0644)
	}

	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{
			{Path: "/", StaticDir: www},
		},
	}

	srv := New(cfg)
	ts := &http.Server{
		Addr:    ":0",
		Handler: srv.middleware(srv.mux),
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		b.Fatal(err)
	}
	go ts.Serve(listener)
	defer ts.Close()
	time.Sleep(10 * time.Millisecond)

	base := "http://" + listener.Addr().String()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			url := fmt.Sprintf("%s/file%d.html", base, idx%100)
			resp, err := http.Get(url)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			idx++
		}
	})
}

func BenchmarkMemory_ServerIdle(b *testing.B) {
	logger.Init()

	dir := b.TempDir()
	www := filepath.Join(dir, "www")
	os.MkdirAll(www, 0755)
	os.WriteFile(www+"/index.html", []byte(benchPageHTML), 0644)

	cfg := config.Config{
		Port:   0,
		Routes: []config.Route{
			{Path: "/", StaticDir: www},
		},
	}

	_ = New(cfg)

	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	b.ReportMetric(float64(memStats.HeapAlloc)/1024, "KB_heap_idle")
}

var benchPageHTML = `<!DOCTYPE html>
<html>
<head><title>gofly %d</title><link rel="stylesheet" href="style.css"></head>
<body><h1>Benchmark</h1><p>Static page served by gofly.</p></body>
</html>`
