package static

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/rroblf01/gofly/internal/config"
)

type Handler struct {
	dir      string
	cacheTTL int
	fs       http.Handler
	prefix   string
}

func New(route config.Route) *Handler {
	cacheTTL := 0
	if route.BrowserCacheTTL != nil {
		cacheTTL = int(route.BrowserCacheTTL.Seconds())
	}

	absDir, _ := filepath.Abs(route.StaticDir)
	fs := http.StripPrefix(route.Path, http.FileServer(http.Dir(route.StaticDir)))

	return &Handler{
		dir:      absDir,
		cacheTTL: cacheTTL,
		fs:       fs,
		prefix:   route.Path,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.safePath(r.URL.Path) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if h.cacheTTL > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", h.cacheTTL))
	}

	h.fs.ServeHTTP(w, r)
}

func (h *Handler) safePath(path string) bool {
	if strings.Contains(path, "..") {
		return false
	}

	clean := filepath.Clean(strings.TrimPrefix(path, "/"))
	target := filepath.Join(h.dir, clean)

	root := filepath.Clean(h.dir)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) && target != root {
		return false
	}

	return true
}
