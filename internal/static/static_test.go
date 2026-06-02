package static

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	h := New(config.Route{Path: "/", StaticDir: dir})

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

	h := New(config.Route{Path: "/", StaticDir: dir})

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
	h := New(config.Route{
		Path: "/", StaticDir: dir,
		BrowserCacheTTL: &dur,
	})

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

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	cc := resp.Header().Get("Cache-Control")
	if cc != "" {
		t.Errorf("Cache-Control = %q, want empty", cc)
	}
}

func TestStatic_ContentType(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/style.css", []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

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

func TestStatic_FileNotFound(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nonexistent.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

func TestStatic_PathTraversal(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{Path: "/", StaticDir: dir})

	tests := []string{
		"/../etc/passwd",
		"/../../etc/passwd",
		"/%2e%2e/etc/passwd",
	}
	for _, p := range tests {
		t.Run(p, func(t *testing.T) {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest("GET", p, nil)
			h.ServeHTTP(resp, req)
			if resp.Code != http.StatusForbidden {
				t.Errorf("path %q: status = %d, want %d", p, resp.Code, http.StatusForbidden)
			}
		})
	}
}

func TestStatic_SafePath(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	os.MkdirAll(dir+"/sub/deep", 0755)
	if err := os.WriteFile(dir+"/sub/deep/file.txt", []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

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

func TestStatic_PrefixStripping(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/file.txt", []byte("prefix test"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/static", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/file.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "prefix test" {
		t.Errorf("body = %q, want %q", string(body), "prefix test")
	}
}

func TestStatic_TrailingSlashRedirect(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/subdir", 0755); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/subdir", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusMovedPermanently)
	}
	loc := resp.Header().Get("Location")
	if loc != "/subdir/" {
		t.Errorf("Location = %q, want %q", loc, "/subdir/")
	}
}

func TestStatic_SafePathInRoot(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK && resp.Code != http.StatusNotFound {
		t.Errorf("unexpected status: %d", resp.Code)
	}
}

func TestStatic_ETag(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("etag test"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	etag := resp.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag header not set")
	}
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("ETag %q should be quoted", etag)
	}
}

func TestStatic_IfNoneMatch304(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("304 test"), 0644); err != nil {
		t.Fatal(err)
	}

	stat, _ := os.Stat(dir + "/test.txt")
	expectedEtag := fmt.Sprintf(`"%x-%x"`, stat.ModTime().Unix(), stat.Size())

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("If-None-Match", expectedEtag)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotModified)
	}
	if resp.Body.Len() > 0 {
		t.Errorf("304 response should have empty body, got %d bytes", resp.Body.Len())
	}
}

func TestStatic_IfNoneMatchStar(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("star test"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("If-None-Match", "*")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotModified)
	}
}

func TestStatic_IfNoneMatch200(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("mismatch"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("If-None-Match", `"different-etag"`)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (non-matching etag should return 200)", resp.Code, http.StatusOK)
	}
}

func TestStatic_IfModifiedSince304(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	stat, _ := os.Stat(dir + "/test.txt")
	futureTime := stat.ModTime().Add(time.Hour).UTC().Format(http.TimeFormat)

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("If-Modified-Since", futureTime)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d (file not modified since future time)", resp.Code, http.StatusNotModified)
	}
}

func TestStatic_IfModifiedSince200(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	stat, _ := os.Stat(dir + "/test.txt")
	pastTime := stat.ModTime().Add(-time.Hour).UTC().Format(http.TimeFormat)

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("If-Modified-Since", pastTime)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (file modified after If-Modified-Since)", resp.Code, http.StatusOK)
	}
}

func TestStatic_LastModifiedHeader(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("lastmod"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	lm := resp.Header().Get("Last-Modified")
	if lm == "" {
		t.Fatal("Last-Modified header not set")
	}
	if _, err := time.Parse(http.TimeFormat, lm); err != nil {
		t.Errorf("Last-Modified %q is not a valid HTTP date: %v", lm, err)
	}

	resp.Header().Get("Accept-Ranges")
}

func TestStatic_AcceptRangesHeader(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("ranges"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	ar := resp.Header().Get("Accept-Ranges")
	if ar != "bytes" {
		t.Errorf("Accept-Ranges = %q, want %q", ar, "bytes")
	}
}

func TestStatic_Range206(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Range", "bytes=0-4")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusPartialContent)
	}

	cr := resp.Header().Get("Content-Range")
	if cr != "bytes 0-4/16" {
		t.Errorf("Content-Range = %q, want %q", cr, "bytes 0-4/16")
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "01234" {
		t.Errorf("body = %q, want %q", string(body), "01234")
	}
}

func TestStatic_RangeMiddle(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Range", "bytes=5-9")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusPartialContent)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "56789" {
		t.Errorf("body = %q, want %q", string(body), "56789")
	}
}

func TestStatic_RangeSuffix(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Range", "bytes=-5")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusPartialContent)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "bcdef" {
		t.Errorf("body = %q, want %q", string(body), "bcdef")
	}
}

func TestStatic_RangeOpenEnded(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Range", "bytes=10-")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusPartialContent)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "abcdef" {
		t.Errorf("body = %q, want %q", string(body), "abcdef")
	}
}

func TestStatic_RangeEndClamping(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("0123456789")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Range", "bytes=0-999")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want %d (clamped range should be 206)", resp.Code, http.StatusPartialContent)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "0123456789" {
		t.Errorf("body = %q, want %q (should serve entire file when end is clamped)", string(body), "0123456789")
	}
}

func TestStatic_Range416(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("0123456789")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Range", "bytes=100-200")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusRequestedRangeNotSatisfiable)
	}
	cr := resp.Header().Get("Content-Range")
	if cr != "bytes */10" {
		t.Errorf("Content-Range = %q, want %q", cr, "bytes */10")
	}
}

func TestStatic_RangeOnNormalGet(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	content := []byte("full content")
	if err := os.WriteFile(dir+"/test.txt", content, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "full content" {
		t.Errorf("body = %q, want %q", string(body), "full content")
	}
}

func TestStatic_SPA(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("spa shell"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir, SPA: true})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/some/deep/path", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (SPA should serve index.html)", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "spa shell" {
		t.Errorf("body = %q, want %q", string(body), "spa shell")
	}
}

func TestStatic_SPANoIndex(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{Path: "/", StaticDir: dir, SPA: true})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/some/path", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (SPA without index.html should 404)", resp.Code, http.StatusNotFound)
	}
}

func TestStatic_NoSPA(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (no SPA should 404)", resp.Code, http.StatusNotFound)
	}
}

func TestStatic_AutoIndex(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/a.txt", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/b.txt", []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir, AutoIndex: true})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	ct := resp.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}

	body := resp.Body.String()
	if !strings.Contains(body, "a.txt") || !strings.Contains(body, "b.txt") {
		t.Errorf("autoindex body should list files, got: %s", body)
	}
}

func TestStatic_AutoIndexWithIndexHTML(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("real index"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir, AutoIndex: true})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "real index" {
		t.Errorf("body = %q, want %q (index.html should take priority over autoindex)", string(body), "real index")
	}
}

func TestStatic_AutoIndexHidesDotFiles(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/visible.txt", []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/.hidden", []byte("h"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir, AutoIndex: true})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(resp, req)

	body := resp.Body.String()
	if !strings.Contains(body, "visible.txt") {
		t.Errorf("autoindex should list visible files")
	}
	if strings.Contains(body, ".hidden") {
		t.Errorf("autoindex should NOT list dotfiles")
	}
}

func TestStatic_ErrorPages(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/404.html", []byte("custom not found"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{
		Path:       "/",
		StaticDir:  dir,
		ErrorPages: map[int]string{404: "/404.html"},
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "custom not found" {
		t.Errorf("body = %q, want %q", string(body), "custom not found")
	}
}

func TestStatic_ErrorPagesWithoutFile(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{
		Path:       "/",
		StaticDir:  dir,
		ErrorPages: map[int]string{404: "/nonexistent_error.html"},
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

func TestStatic_ErrorPagesPathTraversal(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	h := New(config.Route{
		Path:       "/",
		StaticDir:  dir,
		ErrorPages: map[int]string{404: "/../../etc/passwd"},
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

func TestStatic_SecurityHeaders(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("sec"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", resp.Header().Get("X-Content-Type-Options"), "nosniff")
	}
	if resp.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", resp.Header().Get("X-Frame-Options"), "DENY")
	}
	if resp.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want %q", resp.Header().Get("Referrer-Policy"), "strict-origin-when-cross-origin")
	}
}

func TestStatic_NoSecurityHeaders(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("nosec"), 0644); err != nil {
		t.Fatal(err)
	}

	sec := false
	h := New(config.Route{
		Path: "/", StaticDir: dir,
		SecurityHeaders: &sec,
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Header().Get("X-Content-Type-Options") == "nosniff" {
		t.Errorf("X-Content-Type-Options should not be set when security_headers=false")
	}
}

func TestStatic_SetHeaders(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("headers"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{
		Path: "/", StaticDir: dir,
		SetHeaders: map[string]string{"X-Custom": "custom-value"},
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Header().Get("X-Custom") != "custom-value" {
		t.Errorf("X-Custom = %q, want %q", resp.Header().Get("X-Custom"), "custom-value")
	}
}

func TestStatic_GzipPrecompressed(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("original content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/test.txt.gz", []byte("gzipped content"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "gzipped content" {
		t.Errorf("body = %q, want %q (pre-compressed .gz should be served)", string(body), "gzipped content")
	}
}

func TestStatic_CacheServesFromMemory(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/cached.txt", []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	ttl := config.Duration{Duration: time.Hour}
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})

	// First request populates the cache.
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("GET", "/cached.txt", nil))
	if body, _ := io.ReadAll(resp.Body); string(body) != "v1" {
		t.Fatalf("first body = %q, want %q", body, "v1")
	}

	// Remove the file from disk; a cache hit must still serve the bytes.
	if err := os.Remove(dir + "/cached.txt"); err != nil {
		t.Fatal(err)
	}

	resp2 := httptest.NewRecorder()
	h.ServeHTTP(resp2, httptest.NewRequest("GET", "/cached.txt", nil))
	if resp2.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (should hit cache)", resp2.Code, http.StatusOK)
	}
	if body, _ := io.ReadAll(resp2.Body); string(body) != "v1" {
		t.Errorf("cached body = %q, want %q", body, "v1")
	}
}

func TestStatic_CacheHonorsConditional(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/c.txt", []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}

	ttl := config.Duration{Duration: time.Hour}
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})

	warm := httptest.NewRecorder()
	h.ServeHTTP(warm, httptest.NewRequest("GET", "/c.txt", nil))
	etag := warm.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag not set")
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/c.txt", nil)
	req.Header.Set("If-None-Match", etag)
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d (cached entry should honor If-None-Match)", resp.Code, http.StatusNotModified)
	}
}

func TestStatic_CacheRange(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/r.txt", []byte("0123456789abcdef"), 0644); err != nil {
		t.Fatal(err)
	}

	ttl := config.Duration{Duration: time.Hour}
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})

	// Warm the cache, then request a range from the in-memory copy.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/r.txt", nil))

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/r.txt", nil)
	req.Header.Set("Range", "bytes=5-9")
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusPartialContent)
	}
	if body, _ := io.ReadAll(resp.Body); string(body) != "56789" {
		t.Errorf("range body = %q, want %q", body, "56789")
	}
}

func TestStatic_CacheExpiresAndRefreshes(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/c.txt", []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	ttl := config.Duration{Duration: 30 * time.Millisecond}
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})

	// Warm the cache.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/c.txt", nil))

	// Change the file and wait for the TTL to lapse.
	if err := os.WriteFile(dir+"/c.txt", []byte("v2-updated"), 0644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("GET", "/c.txt", nil))
	if body, _ := io.ReadAll(resp.Body); string(body) != "v2-updated" {
		t.Errorf("after TTL expiry body = %q, want %q (should re-read from disk)", body, "v2-updated")
	}
}

func TestStatic_CacheBypassesLargeFiles(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	big := make([]byte, (1<<20)+1024) // > maxCacheFileSize (1 MiB)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(dir+"/big.bin", big, 0644); err != nil {
		t.Fatal(err)
	}

	ttl := config.Duration{Duration: time.Hour}
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest("GET", "/big.bin", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if body, _ := io.ReadAll(resp.Body); len(body) != len(big) {
		t.Errorf("served %d bytes, want %d", len(body), len(big))
	}

	// A large file must NOT be cached: removing it makes the next request 404.
	if err := os.Remove(dir + "/big.bin"); err != nil {
		t.Fatal(err)
	}
	resp2 := httptest.NewRecorder()
	h.ServeHTTP(resp2, httptest.NewRequest("GET", "/big.bin", nil))
	if resp2.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (large file should not be cached)", resp2.Code)
	}
}

func TestFileCache_ByteBudgetEvicts(t *testing.T) {
	c := newFileCache(time.Hour, 100) // 100-byte budget

	mk := func(n int) *cacheEntry { return &cacheEntry{data: make([]byte, n)} }

	c.put("a", mk(60))
	c.put("b", mk(60)) // pushes total to 120 > 100, must evict "a"

	if c.curBytes > c.maxBytes {
		t.Fatalf("curBytes=%d exceeds budget=%d", c.curBytes, c.maxBytes)
	}
	if len(c.entries) != 1 {
		t.Fatalf("entries=%d, want 1 after eviction", len(c.entries))
	}
	if _, ok := c.get("b"); !ok {
		t.Errorf("most recent entry %q should remain", "b")
	}
}

func TestFileCache_OversizedNotCached(t *testing.T) {
	c := newFileCache(time.Hour, 50)
	c.put("x", &cacheEntry{data: make([]byte, 100)}) // bigger than whole budget
	if len(c.entries) != 0 || c.curBytes != 0 {
		t.Fatalf("oversized entry was cached: entries=%d bytes=%d", len(c.entries), c.curBytes)
	}
}

func TestFileCache_OverwriteAdjustsBytes(t *testing.T) {
	c := newFileCache(time.Hour, 1000)
	c.put("k", &cacheEntry{data: make([]byte, 100)})
	c.put("k", &cacheEntry{data: make([]byte, 30)}) // overwrite, smaller
	if c.curBytes != 30 {
		t.Fatalf("curBytes=%d, want 30 after overwrite", c.curBytes)
	}
	if len(c.entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(c.entries))
	}
}

func TestStatic_PrecompressedDisabled(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/test.txt.gz", []byte("gzipped"), 0644); err != nil {
		t.Fatal(err)
	}

	off := false
	h := New(config.Route{Path: "/", StaticDir: dir, Precompressed: &off})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(resp, req)

	if body, _ := io.ReadAll(resp.Body); string(body) != "original" {
		t.Errorf("body = %q, want %q (precompressed disabled should serve identity)", body, "original")
	}
}

func TestStatic_GzipNotAccepted(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/test.txt.gz", []byte("gzipped"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", nil)
	h.ServeHTTP(resp, req)

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "original" {
		t.Errorf("body = %q, want %q (original file should be served without Accept-Encoding)", string(body), "original")
	}
}

func TestStatic_ContentTypeSniffing(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/unknown", []byte("<html>sniff</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/unknown", nil)
	h.ServeHTTP(resp, req)

	ct := resp.Header().Get("Content-Type")
	if ct == "" {
		t.Errorf("Content-Type should be detected by sniffing, got empty")
	}
}

func TestStatic_ZeroLengthFile(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/empty.txt", []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/empty.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	cl := resp.Header().Get("Content-Length")
	if cl != "0" {
		t.Errorf("Content-Length = %q, want %q", cl, "0")
	}
}

func TestStatic_ForeignDir(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	otherDir := t.TempDir()
	if err := os.WriteFile(otherDir+"/outside.txt", []byte("leak"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/../"+filepath.Base(otherDir)+"/outside.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (path traversal outside root)", resp.Code, http.StatusForbidden)
	}
}

func TestStatic_SymlinkInsideRoot(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0755)
	if err := os.WriteFile(sub+"/actual.txt", []byte("via symlink"), 0644); err != nil {
		t.Fatal(err)
	}
	err := os.Symlink("sub/actual.txt", filepath.Join(dir, "link.txt"))
	if err != nil {
		t.Skip("symlink not supported on this platform")
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/link.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "via symlink" {
		t.Errorf("body = %q, want %q", string(body), "via symlink")
	}
}

func TestStatic_DeeplyNestedDirectory(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	os.MkdirAll(dir+"/a/b/c/d/e", 0755)
	if err := os.WriteFile(dir+"/a/b/c/d/e/deep.txt", []byte("deep"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/a/b/c/d/e/deep.txt", nil)
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "deep" {
		t.Errorf("body = %q, want %q", string(body), "deep")
	}
}

func TestStatic_ConcurrentRequests(t *testing.T) {
	logger.Init()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/concurrent.txt", []byte("concurrent"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Route{Path: "/", StaticDir: dir})

	errCh := make(chan error, 20)
	for range 20 {
		go func() {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/concurrent.txt", nil)
			h.ServeHTTP(resp, req)
			if resp.Code != http.StatusOK {
				errCh <- fmt.Errorf("concurrent request status = %d", resp.Code)
				return
			}
			errCh <- nil
		}()
	}

	for range 20 {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}
