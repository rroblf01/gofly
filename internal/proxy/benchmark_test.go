package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/static"
)

func BenchmarkProxy_SingleUpstream(b *testing.B) {
	logger.Init()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		b.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(frontend.URL + "/test")
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkProxy_MultipleUpstreams(b *testing.B) {
	logger.Init()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL, backend.URL, backend.URL},
	}

	p, err := New(route)
	if err != nil {
		b.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(frontend.URL + "/")
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkStatic_SmallFile(b *testing.B) {
	logger.Init()

	dir := b.TempDir()
	if err := os.WriteFile(dir+"/small.txt", []byte("hello world"), 0644); err != nil {
		b.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}
	h := static.New(route)
	srv := httptest.NewServer(h)
	defer srv.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(srv.URL + "/small.txt")
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkStatic_LargeFile(b *testing.B) {
	logger.Init()

	dir := b.TempDir()
	data := make([]byte, 256*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(dir+"/large.bin", data, 0644); err != nil {
		b.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}
	h := static.New(route)
	srv := httptest.NewServer(h)
	defer srv.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(srv.URL + "/large.bin")
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkProxy_Throughput(b *testing.B) {
	logger.Init()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("benchmark response payload"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		b.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(frontend.URL + "/bench")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkRealWorldStatic(b *testing.B) {
	logger.Init()

	dir := b.TempDir()
	www := filepath.Join(dir, "www")
	os.MkdirAll(www, 0755)
	os.WriteFile(www+"/index.html", []byte(fmt.Sprintf(pageHTML, 0)), 0644)
	os.WriteFile(www+"/style.css", []byte(cssContent), 0644)
	os.WriteFile(www+"/script.js", []byte(jsContent), 0644)

	route := config.Route{
		Path:      "/",
		StaticDir: www,
	}
	h := static.New(route)
	srv := httptest.NewServer(h)
	defer srv.Close()

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(srv.URL + "/index.html")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkMemoryUsage(b *testing.B) {
	logger.Init()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		b.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	beforeHeap := memStats.HeapAlloc

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(frontend.URL + "/mem")
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	runtime.GC()
	runtime.ReadMemStats(&memStats)
	afterHeap := memStats.HeapAlloc
	b.ReportMetric(float64(afterHeap-beforeHeap)/float64(b.N), "B/req_heap")
}

var pageHTML = `<!DOCTYPE html>
<html>
<head><title>gofly benchmark</title><link rel="stylesheet" href="style.css"></head>
<body>
<h1>gofly benchmark page %d</h1>
<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.</p>
<ul><li>Item 1</li><li>Item 2</li><li>Item 3</li></ul>
<script src="script.js"></script>
</body>
</html>`

var cssContent = `body { font-family: sans-serif; color: #333; margin: 40px; }`
var jsContent = `console.log("benchmark");`
