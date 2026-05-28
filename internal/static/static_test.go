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

	dur := config.Duration{Duration: 0}
	route := config.Route{
		Path:            "/",
		StaticDir:       dir,
		BrowserCacheTTL: &dur,
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

func TestStatic_PathTraversal(t *testing.T) {
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

func TestItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{999999, "999999"},
	}

	for _, tt := range tests {
		got := itoa(tt.input)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
