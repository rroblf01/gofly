package server

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/proxy"
	"github.com/rroblf01/gofly/internal/static"
)

type Server struct {
	cfg        config.Config
	http       *http.Server
	mux        *http.ServeMux
	rateLimits map[string]*tokenBucket
	rlMu       sync.RWMutex
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = min(tb.maxTokens, tb.tokens+elapsed*tb.refillRate)
	tb.lastRefill = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

func New(cfg config.Config) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg:        cfg,
		mux:        mux,
		rateLimits: make(map[string]*tokenBucket),
	}

	s.registerRoutes()

	handler := s.middleware(mux)
	if cfg.RateLimit != nil {
		handler = s.rateLimitMiddleware(handler)
	}

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout.Duration,
		WriteTimeout: cfg.WriteTimeout.Duration,
		IdleTimeout:  cfg.IdleTimeout.Duration,
	}

	return s
}

func (s *Server) registerRoutes() {
	for _, route := range s.cfg.Routes {
		route := route
		switch {
		case route.StaticDir != "":
			var h http.Handler = static.New(route)
			if gzipEnabled(route) {
				h = gzipMiddleware(h)
			}
			if route.ServerName != "" {
				s.mux.HandleFunc(route.Path, s.hostHandler(route.ServerName, h))
				s.mux.HandleFunc(route.Path+"/", s.hostHandler(route.ServerName, h))
			} else {
				s.registerPattern(route.Path, h)
			}
			logger.Info("registered static route", logger.LogFields{
				"path": route.Path,
				"dir":  route.StaticDir,
				"host": route.ServerName,
			})

		case len(route.Upstreams) > 0:
			p, err := proxy.New(route)
			if err != nil {
				logger.Error("failed to create proxy", logger.LogFields{
					"path":  route.Path,
					"error": err.Error(),
				})
				continue
			}
			var h http.Handler = p
			if gzipEnabled(route) {
				h = gzipMiddleware(h)
			}
			if route.ServerName != "" {
				s.mux.HandleFunc(route.Path, s.hostHandler(route.ServerName, h))
				s.mux.HandleFunc(route.Path+"/", s.hostHandler(route.ServerName, h))
			} else {
				s.registerPattern(route.Path, h)
			}
			logger.Info("registered proxy route", logger.LogFields{
				"path":      route.Path,
				"upstreams": route.Upstreams,
				"host":      route.ServerName,
			})
		}
	}

	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})
}

func (s *Server) hostHandler(serverName string, handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Host == serverName {
			handler.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (s *Server) registerPattern(path string, handler http.Handler) {
	if path == "/" {
		s.mux.Handle("/", handler)
		return
	}
	s.mux.Handle(path, handler)
	subtree := path
	if subtree[len(subtree)-1] != '/' {
		subtree += "/"
	}
	s.mux.Handle(subtree, handler)
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		s.rlMu.RLock()
		bucket, exists := s.rateLimits[ip]
		s.rlMu.RUnlock()

		if !exists {
			bucket = newTokenBucket(
				s.cfg.RateLimit.RequestsPerSecond,
				s.cfg.RateLimit.Burst,
			)
			s.rlMu.Lock()
			s.rateLimits[ip] = bucket
			s.rlMu.Unlock()
		}

		if !bucket.allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := newResponseWriter(w)

		if id := r.Header.Get("X-Request-ID"); id != "" {
			r = r.WithContext(logger.WithRequestID(r.Context(), id))
		}

		next.ServeHTTP(lrw, r)

		dur := time.Since(start)
		logger.LogAccess(start, r.Method, r.URL.Path, lrw.status, dur, lrw.upstream)
	})
}

func (s *Server) ListenAndServe() error {
	if s.cfg.TLS != nil && s.cfg.TLS.Enabled {
		return s.http.ListenAndServeTLS(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
	}
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func Run(cfg config.Config) error {
	srv := New(cfg)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", logger.LogFields{
			"port": cfg.Port,
			"tls":  cfg.TLS != nil && cfg.TLS.Enabled,
		})
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		logger.Info("shutting down", logger.LogFields{"signal": sig.String()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return srv.Shutdown(ctx)
}

type responseWriter struct {
	http.ResponseWriter
	status   int
	upstream string
	written  bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) SetUpstream(u string) {
	rw.upstream = u
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.status = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.IndexByte(fwd, ','); idx > 0 {
			return fwd[:idx]
		}
		return fwd
	}
	if idx := strings.LastIndexByte(r.RemoteAddr, ':'); idx > 0 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

func gzipEnabled(route config.Route) bool {
	if route.Gzip != nil {
		return *route.Gzip
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (grw *gzipResponseWriter) Write(b []byte) (int, error) {
	return grw.writer.Write(b)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gw := gzip.NewWriter(w)
		defer gw.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")

		grw := &gzipResponseWriter{ResponseWriter: w, writer: gw}
		next.ServeHTTP(grw, r)
	})
}
