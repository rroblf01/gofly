package static

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

func TestStatic_ServesFile(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "file content" {
		t.Errorf("body = %q, want %q", string(body), "file content")
	}
}

func TestStatic_ServesIndexHTML(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("index"), 0644); err != nil {
		t.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "index" {
		t.Errorf("body = %q, want %q", string(body), "index")
	}
}

func TestStatic_CacheControl(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	dur := config.Duration{Duration: 3600 * time.Second}
	route := config.Route{
		Path:            "/",
		StaticDir:       dir,
		BrowserCacheTTL: &dur,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	cc := resp.Header().Get("Cache-Control")
	if cc != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=3600")
	}
}

func TestStatic_NoCacheControl(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	cc := resp.Header().Get("Cache-Control")
	if cc != "" {
		t.Errorf("Cache-Control = %q, want empty", cc)
	}
}

func TestStatic_PathTraversalBasic(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/../etc/passwd", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestStatic_PathTraversalDoubleDot(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/../../etc/passwd", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestStatic_PathTraversalURLEncoded(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/%2e%2e/etc/passwd", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestStatic_PathTraversalDeeplyNested(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	os.MkdirAll(dir+"/sub/deep", 0755)
	if err := os.WriteFile(dir+"/sub/deep/file.txt", []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sub/deep/file.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "safe" {
		t.Errorf("body = %q, want %q", string(body), "safe")
	}
}

func TestStatic_FileNotFound(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nonexistent.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

func TestStatic_ContentType(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/style.css", []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}

	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/style.css", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	ct := resp.Header().Get("Content-Type")
	if ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/css; charset=utf-8")
	}
}

func TestStatic_SafePathInRoot(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	route := config.Route{
		Path:      "/",
		StaticDir: dir,
	}

	h := New(route)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK && resp.Code != http.StatusNotFound {
		t.Errorf("unexpected status: %d", resp.Code)
	}
}
