package static

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rroblf01/gofly/internal/config"
)

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
	}

	serveFile(w, f, stat, h.cacheTTL)
}

func serveFile(w http.ResponseWriter, f *os.File, stat os.FileInfo, cacheTTL string) {
	ctype := mime.TypeByExtension(filepath.Ext(stat.Name()))
	if ctype == "" {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		ctype = http.DetectContentType(buf[:n])
		f.Seek(0, io.SeekStart)
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
