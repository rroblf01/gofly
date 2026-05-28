package static

import (
	"net/http"
	"strings"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

type Handler struct {
	dir      string
	cacheTTL int
	fs       http.Handler
}

func New(route config.Route) *Handler {
	cacheTTL := 0
	if route.BrowserCacheTTL != nil {
		cacheTTL = int(route.BrowserCacheTTL.Seconds())
	}

	fs := http.FileServer(http.Dir(route.StaticDir))

	return &Handler{
		dir:      route.StaticDir,
		cacheTTL: cacheTTL,
		fs:       fs,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if h.cacheTTL > 0 {
		w.Header().Set("Cache-Control", "public, max-age="+itoa(h.cacheTTL))
	}

	h.fs.ServeHTTP(w, r)

	logger.Info("static served", logger.LogFields{
		"path":   r.URL.Path,
		"method": r.Method,
	})
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
