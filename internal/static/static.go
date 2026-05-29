package static

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rroblf01/gofly/internal/config"
)

var sniffPool = sync.Pool{
	New: func() any {
		b := make([]byte, 512)
		return &b
	},
}

type Handler struct {
	root       string
	prefix     string
	cacheTTL   string
	setHeaders map[string]string
	spa        bool
	autoindex  bool
	errorPages map[int]string
	secHeaders bool
}

func New(route config.Route) *Handler {
	h := &Handler{
		root:       route.StaticDir,
		prefix:     route.Path,
		setHeaders: route.SetHeaders,
		spa:        route.SPA,
		autoindex:  route.AutoIndex,
		errorPages: route.ErrorPages,
		secHeaders: route.SecurityHeadersDefault(),
	}
	if route.BrowserCacheTTL != nil {
		h.cacheTTL = "public, max-age=" + strconv.FormatInt(int64(route.BrowserCacheTTL.Seconds()), 10)
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.prefix)
	if !validPath(path) {
		h.serveError(w, r, http.StatusForbidden)
		return
	}

	target, err := filepath.Abs(filepath.Join(h.root, path))
	if err != nil {
		h.serveError(w, r, http.StatusInternalServerError)
		return
	}

	if target != h.root && !strings.HasPrefix(target, h.root+"/") {
		h.serveError(w, r, http.StatusForbidden)
		return
	}

	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			if h.spa {
				target = filepath.Join(h.root, "index.html")
				f2, err2 := os.Open(target)
				if err2 != nil {
					h.serveError(w, r, http.StatusNotFound)
					return
				}
				stat2, err2 := f2.Stat()
				if err2 != nil || stat2.IsDir() {
					f2.Close()
					h.serveError(w, r, http.StatusNotFound)
					return
				}
				h.serveFile(w, r, f2, stat2, target)
				return
			}
			h.serveError(w, r, http.StatusNotFound)
			return
		}
		h.serveError(w, r, http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		h.serveError(w, r, http.StatusInternalServerError)
		return
	}

	if stat.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		idx, err := os.Open(filepath.Join(target, "index.html"))
		if err != nil {
			if h.autoindex {
				h.serveDirList(w, r, target)
				return
			}
			h.serveError(w, r, http.StatusNotFound)
			return
		}
		defer idx.Close()
		istat, err := idx.Stat()
		if err != nil || istat.IsDir() {
			idx.Close()
			if h.autoindex {
				h.serveDirList(w, r, target)
				return
			}
			h.serveError(w, r, http.StatusNotFound)
			return
		}
		stat = istat
		f.Close()
		f = idx
		target = filepath.Join(target, "index.html")
	}

	if gz, ok := precompressedGzip(r, target); ok {
		defer gz.f.Close()
		h.serveFile(w, r, gz.f, gz.stat, gz.orig)
		return
	}

	h.serveFile(w, r, f, stat, target)
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, f *os.File, stat os.FileInfo, origName string) {
	etag := fmt.Sprintf(`"%x-%x"`, stat.ModTime().Unix(), stat.Size())
	lastMod := stat.ModTime().UTC().Format(http.TimeFormat)

	if match := r.Header.Get("If-None-Match"); match != "" {
		if match == etag || match == "*" {
			hdr := w.Header()
			hdr["Etag"] = []string{etag}
			hdr["Last-Modified"] = []string{lastMod}
			if h.cacheTTL != "" {
				hdr["Cache-Control"] = []string{h.cacheTTL}
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		t, err := time.Parse(http.TimeFormat, ims)
		if err == nil && !stat.ModTime().After(t) {
			hdr := w.Header()
			hdr["Etag"] = []string{etag}
			hdr["Last-Modified"] = []string{lastMod}
			if h.cacheTTL != "" {
				hdr["Cache-Control"] = []string{h.cacheTTL}
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	ctype := mime.TypeByExtension(filepath.Ext(origName))
	if ctype == "" {
		bufp := sniffPool.Get().(*[]byte)
		n, _ := f.Read(*bufp)
		ctype = http.DetectContentType((*bufp)[:n])
		f.Seek(0, io.SeekStart)
		sniffPool.Put(bufp)
	}

	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" && strings.HasPrefix(rangeHdr, "bytes=") {
		if h.serveRange(w, r, f, stat, ctype, etag, lastMod) {
			return
		}
	}

	hdr := w.Header()
	hdr["Content-Type"] = []string{ctype}
	hdr["Content-Length"] = []string{strconv.FormatInt(stat.Size(), 10)}
	hdr["Etag"] = []string{etag}
	hdr["Last-Modified"] = []string{lastMod}
	hdr["Accept-Ranges"] = []string{"bytes"}

	if h.cacheTTL != "" {
		hdr["Cache-Control"] = []string{h.cacheTTL}
	}
	if h.secHeaders {
		hdr["X-Content-Type-Options"] = []string{"nosniff"}
		hdr["X-Frame-Options"] = []string{"DENY"}
		hdr["Referrer-Policy"] = []string{"strict-origin-when-cross-origin"}
	}
	for k, v := range h.setHeaders {
		hdr[http.CanonicalHeaderKey(k)] = []string{v}
	}

	w.WriteHeader(http.StatusOK)
	if rf, ok := w.(io.ReaderFrom); ok {
		rf.ReadFrom(f)
	} else {
		f.WriteTo(w)
	}
}

func (h *Handler) serveRange(w http.ResponseWriter, r *http.Request, f *os.File, stat os.FileInfo, ctype, etag, lastMod string) bool {
	s := r.Header.Get("Range")
	s = strings.TrimPrefix(s, "bytes=")

	ranges, err := parseRange(s, stat.Size())
	if err != nil || len(ranges) == 0 {
		hdr := w.Header()
		hdr["Content-Range"] = []string{fmt.Sprintf("bytes */%d", stat.Size())}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	ra := ranges[0]
	if ra.end >= stat.Size() {
		ra.end = stat.Size() - 1
	}
	if ra.start >= stat.Size() || ra.start > ra.end {
		hdr := w.Header()
		hdr["Content-Range"] = []string{fmt.Sprintf("bytes */%d", stat.Size())}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	hdr := w.Header()
	hdr["Content-Type"] = []string{ctype}
	hdr["Content-Range"] = []string{fmt.Sprintf("bytes %d-%d/%d", ra.start, ra.end, stat.Size())}
	hdr["Content-Length"] = []string{strconv.FormatInt(ra.length(), 10)}
	hdr["Etag"] = []string{etag}
	hdr["Last-Modified"] = []string{lastMod}
	hdr["Accept-Ranges"] = []string{"bytes"}
	if h.secHeaders {
		hdr["X-Content-Type-Options"] = []string{"nosniff"}
	}
	for k, v := range h.setHeaders {
		hdr[http.CanonicalHeaderKey(k)] = []string{v}
	}

	w.WriteHeader(http.StatusPartialContent)
	f.Seek(ra.start, io.SeekStart)
	io.CopyN(w, f, ra.length())
	return true
}

type byteRange struct {
	start, end int64
}

func (br byteRange) length() int64 {
	return br.end - br.start + 1
}

func parseRange(s string, size int64) ([]byteRange, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.SplitN(s, ",", 2)
	if len(parts) > 1 {
		return nil, fmt.Errorf("multipart ranges not supported")
	}
	s = strings.TrimSpace(parts[0])

	if strings.HasPrefix(s, "-") {
		// suffix range: -500
		suffix, err := strconv.ParseInt(s[1:], 10, 64)
		if err != nil || suffix <= 0 {
			return nil, fmt.Errorf("invalid range")
		}
		if suffix > size {
			suffix = size
		}
		return []byteRange{{start: size - suffix, end: size - 1}}, nil
	}

	if strings.HasSuffix(s, "-") {
		// open-ended range: 500-
		start, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
		if err != nil || start < 0 {
			return nil, fmt.Errorf("invalid range")
		}
		return []byteRange{{start: start, end: size - 1}}, nil
	}

	parts = strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range")
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 {
		return nil, fmt.Errorf("invalid range")
	}
	end, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || end < 0 {
		return nil, fmt.Errorf("invalid range")
	}

	return []byteRange{{start: start, end: end}}, nil
}

func (h *Handler) serveDirList(w http.ResponseWriter, r *http.Request, dirPath string) {
	f, err := os.Open(dirPath)
	if err != nil {
		h.serveError(w, r, http.StatusNotFound)
		return
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		h.serveError(w, r, http.StatusInternalServerError)
		return
	}
	sort.Strings(names)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Index of %s</title>", r.URL.Path)
	fmt.Fprintf(w, "<style>body{font-family:sans-serif;margin:2em}a{display:block;padding:4px 8px}a:hover{background:#f0f0f0}</style>")
	fmt.Fprintf(w, "</head><body><h1>Index of %s</h1>", r.URL.Path)

	if r.URL.Path != "/" {
		parent := strings.TrimSuffix(r.URL.Path, "/")
		if idx := strings.LastIndex(parent, "/"); idx >= 0 {
			parent = parent[:idx]
		}
		if parent == "" {
			parent = "/"
		}
		fmt.Fprintf(w, "<a href=\"%s\">..</a>", parent)
	}

	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		subPath := strings.TrimSuffix(r.URL.Path, "/") + "/" + name
		fmt.Fprintf(w, "<a href=\"%s\">%s</a>", subPath, name)
	}

	fmt.Fprint(w, "</body></html>")
}

func (h *Handler) serveError(w http.ResponseWriter, r *http.Request, code int) {
	if path, ok := h.errorPages[code]; ok {
		errorPath := filepath.Join(h.root, path)
		if validPath(path) {
			absPath, err := filepath.Abs(errorPath)
			if err == nil && strings.HasPrefix(absPath, h.root+"/") {
				data, err := os.ReadFile(absPath)
				if err == nil {
					hdr := w.Header()
					hdr["Content-Type"] = []string{mime.TypeByExtension(filepath.Ext(path))}
					if h.secHeaders {
						hdr["X-Content-Type-Options"] = []string{"nosniff"}
					}
					w.WriteHeader(code)
					w.Write(data)
					return
				}
			}
		}
	}
	http.Error(w, http.StatusText(code), code)
}

func validPath(path string) bool {
	return !strings.Contains(path, "..")
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
