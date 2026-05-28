package server

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/proxy"
	"github.com/rroblf01/gofly/internal/static"
)

type logEntry struct {
	start    time.Time
	method   string
	path     string
	status   int
	duration time.Duration
	upstream string
}

type Server struct {
	cfg        config.Config
	configPath string
	http       *http.Server
	mux        *http.ServeMux
	rateLimits map[string]*tokenBucket
	rlMu       sync.RWMutex
	logCh      chan logEntry
	logWg      sync.WaitGroup
	stopLog    chan struct{}
	listeners  []net.Listener
	sh         *swappableHandler
}

type swappableHandler struct {
	mu      sync.RWMutex
	handler atomic.Value
}

func (sh *swappableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sh.mu.RLock()
	h := sh.handler.Load().(http.Handler)
	sh.mu.RUnlock()
	h.ServeHTTP(w, r)
}

func (sh *swappableHandler) Swap(h http.Handler) {
	sh.mu.Lock()
	sh.handler.Store(h)
	sh.mu.Unlock()
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
		logCh:      make(chan logEntry, 16384),
		stopLog:    make(chan struct{}),
	}

	s.registerRoutes()
	s.startLogWorker()

	handler := s.buildMiddleware(mux)

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout.Duration,
		WriteTimeout: cfg.WriteTimeout.Duration,
		IdleTimeout:  cfg.IdleTimeout.Duration,
	}

	return s
}

func (s *Server) buildMiddleware(mux *http.ServeMux) http.Handler {
	sh := &swappableHandler{}
	s.sh = sh
	var handler http.Handler = mux
	handler = s.middleware(handler)
	if s.cfg.RateLimit != nil {
		handler = s.rateLimitMiddleware(handler)
	}
	sh.Swap(handler)
	return sh
}

func (s *Server) rebuildHandler() {
	mux := http.NewServeMux()
	s.mux = mux
	s.registerRoutes()

	handler := http.Handler(mux)
	handler = s.middleware(handler)
	if s.cfg.RateLimit != nil {
		s.rateLimits = make(map[string]*tokenBucket)
		handler = s.rateLimitMiddleware(handler)
	}

	s.sh.Swap(handler)
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

func (s *Server) startLogWorker() {
	s.logWg.Add(1)
	go func() {
		defer s.logWg.Done()
		for {
			select {
			case entry := <-s.logCh:
				logger.LogAccess(entry.start, entry.method, entry.path, entry.status, entry.duration, entry.upstream)
			case <-s.stopLog:
				for len(s.logCh) > 0 {
					entry := <-s.logCh
					logger.LogAccess(entry.start, entry.method, entry.path, entry.status, entry.duration, entry.upstream)
				}
				return
			}
		}
	}()
}

func (s *Server) middleware(next http.Handler) http.Handler {
	if !s.cfg.AccessLogEnabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := newResponseWriter(w)
		defer putResponseWriter(lrw)

		if id := r.Header.Get("X-Request-ID"); id != "" {
			r = r.WithContext(logger.WithRequestID(r.Context(), id))
		}

		next.ServeHTTP(lrw, r)

		select {
		case s.logCh <- logEntry{start, r.Method, r.URL.Path, lrw.status, time.Since(start), lrw.upstream}:
		default:
		}
	})
}

func (s *Server) ListenAndServe() error {
	workers := s.cfg.Workers
	if workers < 1 {
		workers = 1
	}

	errCh := make(chan error, workers*2)

	httpAddr := fmt.Sprintf(":%d", s.cfg.Port)
	for i := 0; i < workers; i++ {
		l, err := listen("tcp", httpAddr)
		if err != nil {
			return err
		}
		s.listeners = append(s.listeners, l)
		go func() {
			errCh <- s.http.Serve(l)
		}()
	}

	if s.cfg.TLS != nil && s.cfg.TLS.Enabled {
		tlsPort := s.cfg.TLS.TLSPort
		if tlsPort == 0 {
			tlsPort = 443
		}
		tlsAddr := fmt.Sprintf(":%d", tlsPort)
		for i := 0; i < workers; i++ {
			l, err := listen("tcp", tlsAddr)
			if err != nil {
				return err
			}
			s.listeners = append(s.listeners, l)
			go func() {
				errCh <- s.http.ServeTLS(l, s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
			}()
		}
	}

	err := <-errCh
	for _, l := range s.listeners {
		l.Close()
	}
	for range len(s.listeners) - 1 {
		<-errCh
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	err := s.http.Shutdown(ctx)
	close(s.stopLog)
	s.logWg.Wait()
	return err
}

func Run(cfg config.Config, configPath string) error {
	limit := cfg.EffectiveMemoryLimit()
	debug.SetMemoryLimit(limit)
	logger.Info("memory limit set", logger.LogFields{
		"limit_bytes": limit,
		"limit_mb":    limit >> 20,
	})

	srv := New(cfg)
	srv.configPath = configPath

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", logger.LogFields{
			"port":    cfg.Port,
			"tls":     cfg.TLS != nil && cfg.TLS.Enabled,
			"workers": cfg.Workers,
		})
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for {
		select {
		case err := <-errCh:
			return err
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				logger.Info("reloading config", logger.LogFields{"path": configPath})
				newCfg, err := config.Load(configPath)
				if err != nil {
					logger.Error("config reload failed", logger.LogFields{"error": err.Error()})
					continue
				}
				srv.cfg = newCfg
				srv.rebuildHandler()
				logger.Info("config reloaded", logger.LogFields{})
			default:
				logger.Info("shutting down", logger.LogFields{"signal": sig.String()})
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				return srv.Shutdown(ctx)
			}
		}
	}
}

type responseWriter struct {
	http.ResponseWriter
	status   int
	upstream string
	written  bool
}

var rwPool = sync.Pool{
	New: func() any {
		return &responseWriter{}
	},
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	rw := rwPool.Get().(*responseWriter)
	rw.ResponseWriter = w
	rw.status = http.StatusOK
	rw.upstream = ""
	rw.written = false
	return rw
}

func putResponseWriter(rw *responseWriter) {
	rw.ResponseWriter = nil
	rwPool.Put(rw)
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

func (rw *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(rw.ResponseWriter, r)
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
