package static

import (
	"bytes"
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
	root          string
	prefix        string
	cacheTTL      string
	setHeaders    map[string]string
	spa           bool
	autoindex     bool
	errorPages    map[int]string
	secHeaders    bool
	precompressed bool
	cache         *fileCache
}

func New(route config.Route) *Handler {
	root := route.StaticDir
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = filepath.Clean(root)

	h := &Handler{
		root:          root,
		prefix:        route.Path,
		setHeaders:    route.SetHeaders,
		spa:           route.SPA,
		autoindex:     route.AutoIndex,
		errorPages:    route.ErrorPages,
		secHeaders:    route.SecurityHeadersDefault(),
		precompressed: route.PrecompressedEnabled(),
	}
	if route.BrowserCacheTTL != nil {
		h.cacheTTL = "public, max-age=" + strconv.FormatInt(int64(route.BrowserCacheTTL.Seconds()), 10)
	}
	if route.StaticCacheTTL != nil && route.StaticCacheTTL.Duration > 0 {
		h.cache = newFileCache(route.StaticCacheTTL.Duration, route.EffectiveStaticCacheMaxBytes())
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.prefix)
	if !validPath(path) {
		h.serveError(w, r, http.StatusForbidden)
		return
	}

	// h.root is already absolute and clean; filepath.Join cleans the result, so
	// no per-request filepath.Abs (and its os.Getwd syscall) is needed.
	target := filepath.Join(h.root, path)

	if target != h.root && !strings.HasPrefix(target, h.root+string(filepath.Separator)) {
		h.serveError(w, r, http.StatusForbidden)
		return
	}

	if h.cache != nil {
		if e, ok := h.cache.get(target); ok {
			h.serveContent(w, r, bytes.NewReader(e.data), int64(len(e.data)), e.modTime, e.etag, e.ctype)
			return
		}
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

	// When the in-memory cache is enabled it takes precedence over
	// pre-compressed siblings (it serves identity content for every client),
	// so the .gz probe is skipped entirely.
	if h.cache == nil {
		if h.precompressed {
			if gz, ok := precompressedGzip(r, target); ok {
				defer gz.f.Close()
				h.serveFile(w, r, gz.f, gz.stat, gz.orig)
				return
			}
		}
		h.serveFile(w, r, f, stat, target)
		return
	}

	h.serveAndMaybeCache(w, r, f, stat, target)
}

// maxCacheFileSize bounds which files are eligible to be held in memory by the
// optional static cache. Larger files are always streamed from disk.
const maxCacheFileSize = 1 << 20 // 1 MiB

// serveAndMaybeCache serves a regular file and, when small enough, reads it once
// into the in-memory cache so subsequent requests avoid the open/stat/read
// syscalls entirely.
func (h *Handler) serveAndMaybeCache(w http.ResponseWriter, r *http.Request, f *os.File, stat os.FileInfo, origName string) {
	if !stat.Mode().IsRegular() || stat.Size() > maxCacheFileSize {
		h.serveFile(w, r, f, stat, origName)
		return
	}

	data := make([]byte, stat.Size())
	if _, err := io.ReadFull(f, data); err != nil {
		// Fall back to a fresh streaming read from offset 0.
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr == nil {
			h.serveFile(w, r, f, stat, origName)
		} else {
			h.serveError(w, r, http.StatusInternalServerError)
		}
		return
	}

	ctype := mime.TypeByExtension(filepath.Ext(origName))
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	e := &cacheEntry{
		modTime: stat.ModTime(),
		etag:    etagFor(stat),
		ctype:   ctype,
		data:    data,
	}
	h.cache.put(origName, e)
	h.serveContent(w, r, bytes.NewReader(data), int64(len(data)), e.modTime, e.etag, e.ctype)
}

// etagFor builds the `"<hexmod>-<hexsize>"` validator without fmt/reflection.
func etagFor(stat os.FileInfo) string {
	buf := make([]byte, 0, 32)
	buf = append(buf, '"')
	buf = strconv.AppendInt(buf, stat.ModTime().Unix(), 16)
	buf = append(buf, '-')
	buf = strconv.AppendInt(buf, stat.Size(), 16)
	buf = append(buf, '"')
	return string(buf)
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, f *os.File, stat os.FileInfo, origName string) {
	h.serveContent(w, r, f, stat.Size(), stat.ModTime(), etagFor(stat), mime.TypeByExtension(filepath.Ext(origName)))
}

// serveContent writes a response from any seekable source (an *os.File for the
// streaming path, a *bytes.Reader for the in-memory cache), handling
// conditional requests, byte ranges and the standard header set. A *os.File
// source still benefits from sendfile via the ResponseWriter's ReaderFrom.
func (h *Handler) serveContent(w http.ResponseWriter, r *http.Request, src io.ReadSeeker, size int64, modTime time.Time, etag, ctype string) {
	lastMod := modTime.UTC().Format(http.TimeFormat)

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
		if err == nil && !modTime.After(t) {
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

	if ctype == "" {
		bufp := sniffPool.Get().(*[]byte)
		n, _ := src.Read(*bufp)
		ctype = http.DetectContentType((*bufp)[:n])
		src.Seek(0, io.SeekStart)
		sniffPool.Put(bufp)
	}

	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" && strings.HasPrefix(rangeHdr, "bytes=") {
		if h.serveRange(w, r, src, size, ctype, etag, lastMod) {
			return
		}
	}

	hdr := w.Header()
	hdr["Content-Type"] = []string{ctype}
	hdr["Content-Length"] = []string{strconv.FormatInt(size, 10)}
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
	writeBody(w, src)
}

func writeBody(w http.ResponseWriter, src io.ReadSeeker) {
	if f, ok := src.(*os.File); ok {
		if rf, ok := w.(io.ReaderFrom); ok {
			rf.ReadFrom(f)
			return
		}
	}
	if wt, ok := src.(io.WriterTo); ok {
		wt.WriteTo(w)
		return
	}
	io.Copy(w, src)
}

func (h *Handler) serveRange(w http.ResponseWriter, r *http.Request, src io.ReadSeeker, size int64, ctype, etag, lastMod string) bool {
	s := r.Header.Get("Range")
	s = strings.TrimPrefix(s, "bytes=")

	ranges, err := parseRange(s, size)
	if err != nil || len(ranges) == 0 {
		w.Header()["Content-Range"] = []string{unsatisfiedRange(size)}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	ra := ranges[0]
	if ra.end >= size {
		ra.end = size - 1
	}
	if ra.start >= size || ra.start > ra.end {
		w.Header()["Content-Range"] = []string{unsatisfiedRange(size)}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return true
	}

	hdr := w.Header()
	hdr["Content-Type"] = []string{ctype}
	hdr["Content-Range"] = []string{contentRange(ra.start, ra.end, size)}
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
	src.Seek(ra.start, io.SeekStart)
	io.CopyN(w, src, ra.length())
	return true
}

func unsatisfiedRange(size int64) string {
	buf := make([]byte, 0, 16)
	buf = append(buf, "bytes */"...)
	buf = strconv.AppendInt(buf, size, 10)
	return string(buf)
}

func contentRange(start, end, size int64) string {
	buf := make([]byte, 0, 32)
	buf = append(buf, "bytes "...)
	buf = strconv.AppendInt(buf, start, 10)
	buf = append(buf, '-')
	buf = strconv.AppendInt(buf, end, 10)
	buf = append(buf, '/')
	buf = strconv.AppendInt(buf, size, 10)
	return string(buf)
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

// cacheEntry is a fully resolved small file held in memory by the optional
// static cache: its bytes plus everything serveContent needs to emit headers.
type cacheEntry struct {
	modTime time.Time
	etag    string
	ctype   string
	data    []byte
}

// maxCacheEntries bounds the number of distinct files held in memory. It is a
// secondary guard; the total-byte budget (fileCache.maxBytes) is the primary
// bound on resident memory.
const maxCacheEntries = 4096

type cacheItem struct {
	entry   *cacheEntry
	expires time.Time
}

// fileCache is a bounded, TTL'd map of filesystem path -> resolved file. Within
// the TTL a hit serves entirely from memory, skipping the open/stat/read
// syscalls. It deliberately does not revalidate against disk before expiry, so
// the configured static_cache_ttl bounds how stale a served file can be —
// mirroring nginx's open_file_cache trade-off.
//
// Resident memory is bounded two ways: maxEntries caps the count and maxBytes
// caps the summed body size. Without the byte budget a cache of maxCacheEntries
// 1 MiB files could pin gigabytes; bounding bytes keeps the footprint inside
// the process memory limit regardless of file sizes.
type fileCache struct {
	mu       sync.RWMutex
	entries  map[string]cacheItem
	ttl      time.Duration
	maxBytes int64
	curBytes int64
}

func newFileCache(ttl time.Duration, maxBytes int64) *fileCache {
	return &fileCache{entries: make(map[string]cacheItem), ttl: ttl, maxBytes: maxBytes}
}

func (c *fileCache) get(key string) (*cacheEntry, bool) {
	c.mu.RLock()
	it, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(it.expires) {
		c.mu.Lock()
		if cur, ok := c.entries[key]; ok && time.Now().After(cur.expires) {
			c.removeLocked(key, cur)
		}
		c.mu.Unlock()
		return nil, false
	}
	return it.entry, true
}

func (c *fileCache) put(key string, e *cacheEntry) {
	size := int64(len(e.data))
	// A single file larger than the whole budget is never cacheable.
	if c.maxBytes > 0 && size > c.maxBytes {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if old, exists := c.entries[key]; exists {
		c.curBytes -= int64(len(old.entry.data))
		delete(c.entries, key)
	}

	// Evict until both the count and byte budgets admit the new entry.
	for len(c.entries) >= maxCacheEntries || (c.maxBytes > 0 && c.curBytes+size > c.maxBytes) {
		evicted := false
		for k, it := range c.entries {
			c.removeLocked(k, it)
			evicted = true
			break
		}
		if !evicted {
			break // map empty; nothing left to evict
		}
	}

	c.entries[key] = cacheItem{entry: e, expires: time.Now().Add(c.ttl)}
	c.curBytes += size
}

// removeLocked deletes an entry and adjusts the byte counter. Caller holds mu.
func (c *fileCache) removeLocked(key string, it cacheItem) {
	delete(c.entries, key)
	c.curBytes -= int64(len(it.entry.data))
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
