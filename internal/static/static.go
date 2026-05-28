package static

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rroblf01/gofly/internal/config"
)

var sniffPool = sync.Pool{
	New: func() any {
		b := make([]byte, 512)
		return &b
	},
}

type Handler struct {
	root     string
	prefix   string
	cacheTTL string
}

func New(route config.Route) *Handler {
	h := &Handler{
		root:   route.StaticDir,
		prefix: route.Path,
	}
	if route.BrowserCacheTTL != nil {
		h.cacheTTL = "public, max-age=" + strconv.FormatInt(int64(route.BrowserCacheTTL.Seconds()), 10)
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.prefix)
	if !validPath(path) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	target, err := filepath.Abs(filepath.Join(h.root, path))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if target != h.root && !strings.HasPrefix(target, h.root+"/") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if stat.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		idx, err := os.Open(filepath.Join(target, "index.html"))
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer idx.Close()
		istat, err := idx.Stat()
		if err != nil || istat.IsDir() {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		stat = istat
		f.Close()
		f = idx
		target = filepath.Join(target, "index.html")
	}

	if gz, ok := precompressedGzip(r, target); ok {
		defer gz.f.Close()
		serveFile(w, gz.f, gz.stat, h.cacheTTL, gz.orig)
		return
	}

	serveFile(w, f, stat, h.cacheTTL, target)
}

type gzipFile struct {
	f    *os.File
	stat os.FileInfo
	orig string
}

func precompressedGzip(r *http.Request, target string) (*gzipFile, bool) {
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return nil, false
	}
	gzf, err := os.Open(target + ".gz")
	if err != nil {
		return nil, false
	}
	gzstat, err := gzf.Stat()
	if err != nil || gzstat.IsDir() {
		gzf.Close()
		return nil, false
	}
	return &gzipFile{f: gzf, stat: gzstat, orig: target}, true
}

func serveFile(w http.ResponseWriter, f *os.File, stat os.FileInfo, cacheTTL string, origName string) {
	ctype := mime.TypeByExtension(filepath.Ext(origName))
	if ctype == "" {
		bufp := sniffPool.Get().(*[]byte)
		n, _ := f.Read(*bufp)
		ctype = http.DetectContentType((*bufp)[:n])
		f.Seek(0, io.SeekStart)
		sniffPool.Put(bufp)
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

	if cacheTTL != "" {
		w.Header().Set("Cache-Control", cacheTTL)
	}

	w.WriteHeader(http.StatusOK)
	io.CopyN(w, f, stat.Size())
}

func validPath(path string) bool {
	return !strings.Contains(path, "..")
}
