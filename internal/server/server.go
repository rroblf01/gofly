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
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/metrics"
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
	rl         atomic.Pointer[rateLimiter]
	logCh      chan logEntry
	logWg      sync.WaitGroup
	stopLog    chan struct{}
	stopRL     chan struct{}
	listeners  []net.Listener
	sh         *swappableHandler
	proxies    []*proxy.Proxy
}

type swappableHandler struct {
	handler atomic.Value
}

func (sh *swappableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sh.handler.Load().(http.Handler).ServeHTTP(w, r)
}

func (sh *swappableHandler) Swap(h http.Handler) {
	sh.handler.Store(h)
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

// allow is called while the caller holds the shard lock, so no mutex needed.
func (tb *tokenBucket) allow() bool {
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

// rlShards is the number of independently-locked partitions of the per-IP
// bucket map. Sharding keeps the rate limiter from serialising every request
// through one mutex under load.
const rlShards = 256

// rlDefaultIdleTTL is how long a per-IP bucket may sit idle before the janitor
// evicts it, bounding memory against a wide or spoofed client base.
const rlDefaultIdleTTL = 10 * time.Minute

type rlShard struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type rateLimiter struct {
	shards [rlShards]rlShard
	rate   float64
	burst  int
	ttl    time.Duration
}

func newRateLimiter(rate float64, burst int, ttl time.Duration) *rateLimiter {
	rl := &rateLimiter{rate: rate, burst: burst, ttl: ttl}
	for i := range rl.shards {
		rl.shards[i].buckets = make(map[string]*tokenBucket)
	}
	return rl
}

func (rl *rateLimiter) shardFor(ip string) *rlShard {
	// FNV-1a over the IP string.
	var h uint32 = 2166136261
	for i := 0; i < len(ip); i++ {
		h ^= uint32(ip[i])
		h *= 16777619
	}
	return &rl.shards[h%rlShards]
}

func (rl *rateLimiter) allow(ip string) bool {
	s := rl.shardFor(ip)
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[ip]
	if !ok {
		b = newTokenBucket(rl.rate, rl.burst)
		s.buckets[ip] = b
	}
	return b.allow()
}

// sweep evicts buckets whose last activity predates the idle TTL.
func (rl *rateLimiter) sweep() {
	cutoff := time.Now().Add(-rl.ttl)
	for i := range rl.shards {
		s := &rl.shards[i]
		s.mu.Lock()
		for ip, b := range s.buckets {
			if b.lastRefill.Before(cutoff) {
				delete(s.buckets, ip)
			}
		}
		s.mu.Unlock()
	}
}

func New(cfg config.Config) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg:     cfg,
		mux:     mux,
		logCh:   make(chan logEntry, 16384),
		stopLog: make(chan struct{}),
		stopRL:  make(chan struct{}),
	}

	s.registerRoutes()
	s.startLogWorker()

	handler := s.buildMiddleware(mux)
	s.startRateLimitJanitor()
	s.startHealthChecks()

	s.http = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout.Duration,
		ReadHeaderTimeout: cfg.ReadTimeout.Duration,
		WriteTimeout:      cfg.WriteTimeout.Duration,
		IdleTimeout:       cfg.IdleTimeout.Duration,
		ErrorLog:          logger.StdLogger(),
	}

	return s
}

// CheckRoutes builds the routing table for cfg without starting any listeners
// or background goroutines, so `gofly -t` catches errors that would otherwise
// only surface (or panic) at startup. It recovers from any registration panic
// and returns it as an error.
func CheckRoutes(cfg config.Config) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("route registration failed: %v", r)
		}
	}()
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.registerRoutes()
	return nil
}

func (s *Server) newRateLimiterFromCfg() *rateLimiter {
	ttl := rlDefaultIdleTTL
	if s.cfg.RateLimit.IdleTTL != nil && s.cfg.RateLimit.IdleTTL.Duration > 0 {
		ttl = s.cfg.RateLimit.IdleTTL.Duration
	}
	return newRateLimiter(s.cfg.RateLimit.RequestsPerSecond, s.cfg.RateLimit.Burst, ttl)
}

func (s *Server) buildMiddleware(mux *http.ServeMux) http.Handler {
	sh := &swappableHandler{}
	s.sh = sh
	var handler http.Handler = mux
	handler = s.middleware(handler)
	if s.cfg.RateLimit != nil {
		rl := s.newRateLimiterFromCfg()
		s.rl.Store(rl)
		handler = s.rateLimitMiddleware(rl, handler)
	}
	sh.Swap(handler)
	return sh
}

func (s *Server) rebuildHandler() {
	s.stopHealthChecks() // tear down checkers bound to the previous proxies

	mux := http.NewServeMux()
	s.mux = mux
	s.registerRoutes()

	handler := http.Handler(mux)
	handler = s.middleware(handler)
	if s.cfg.RateLimit != nil {
		rl := s.newRateLimiterFromCfg()
		s.rl.Store(rl)
		handler = s.rateLimitMiddleware(rl, handler)
	} else {
		s.rl.Store(nil)
	}

	s.sh.Swap(handler)
	s.startHealthChecks()
}

// startHealthChecks starts active upstream probing for every proxy that
// configured it. No-op for proxies without health_check_path.
func (s *Server) startHealthChecks() {
	for _, p := range s.proxies {
		p.Start()
	}
}

// stopHealthChecks stops all active health-check loops and waits for them.
func (s *Server) stopHealthChecks() {
	for _, p := range s.proxies {
		p.Stop()
	}
}

// startRateLimitJanitor periodically evicts idle per-IP buckets so the rate
// limiter's memory stays bounded. It is a no-op when rate limiting is off.
func (s *Server) startRateLimitJanitor() {
	rl := s.rl.Load()
	if rl == nil {
		return
	}
	interval := rl.ttl / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	s.logWg.Add(1)
	go func() {
		defer s.logWg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if cur := s.rl.Load(); cur != nil {
					cur.sweep()
				}
			case <-s.stopRL:
				return
			}
		}
	}()
}

// vhostEntry binds a handler to an optional server_name (empty = catch-all)
// within a single ServeMux pattern.
type vhostEntry struct {
	serverName string
	handler    http.Handler
}

func (s *Server) registerRoutes() {
	s.proxies = nil

	// Collect handlers per ServeMux pattern so that multiple routes sharing a
	// pattern (e.g. several virtual hosts on "/") register exactly one handler
	// that dispatches by Host — registering the same pattern twice panics.
	patterns := map[string][]vhostEntry{}
	var order []string
	add := func(pattern, serverName string, h http.Handler) {
		if _, ok := patterns[pattern]; !ok {
			order = append(order, pattern)
		}
		patterns[pattern] = append(patterns[pattern], vhostEntry{serverName, h})
	}

	for _, route := range s.cfg.Routes {
		route := route
		var h http.Handler
		switch {
		case route.StaticDir != "":
			h = static.New(route)
			if gzipEnabled(route) {
				h = gzipMiddlewareWith(h, route.GzipCompressionLevel(), route.GzipMinLength)
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
			s.proxies = append(s.proxies, p)
			h = p
			if gzipEnabled(route) {
				h = gzipMiddlewareWith(h, route.GzipCompressionLevel(), route.GzipMinLength)
			}
			logger.Info("registered proxy route", logger.LogFields{
				"path":      route.Path,
				"upstreams": route.Upstreams,
				"host":      route.ServerName,
			})

		default:
			continue
		}

		for _, pat := range routePatterns(route.Path) {
			add(pat, route.ServerName, h)
		}
	}

	for _, pat := range order {
		s.mux.Handle(pat, vhostDispatcher(patterns[pat]))
	}

	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	if s.cfg.MetricsEnabled() {
		s.mux.HandleFunc("GET /metrics", s.metricsHandler)
	}
}

// metricsHandler exposes process metrics plus per-upstream gauges in the
// Prometheus text exposition format.
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.WriteTo(w)

	if len(s.proxies) == 0 {
		return
	}
	var b []byte
	b = append(b, "# HELP gofly_upstream_healthy Upstream health (1=healthy, 0=disabled).\n"...)
	b = append(b, "# TYPE gofly_upstream_healthy gauge\n"...)
	for _, p := range s.proxies {
		route := metrics.EscapeLabel(p.Path())
		for _, st := range p.Stats() {
			b = append(b, `gofly_upstream_healthy{route="`...)
			b = append(b, route...)
			b = append(b, `",upstream="`...)
			b = append(b, metrics.EscapeLabel(st.URL)...)
			b = append(b, `"} `...)
			if st.Healthy {
				b = append(b, '1')
			} else {
				b = append(b, '0')
			}
			b = append(b, '\n')
		}
	}
	b = append(b, "# HELP gofly_upstream_in_flight In-flight requests per upstream.\n"...)
	b = append(b, "# TYPE gofly_upstream_in_flight gauge\n"...)
	for _, p := range s.proxies {
		route := metrics.EscapeLabel(p.Path())
		for _, st := range p.Stats() {
			b = append(b, `gofly_upstream_in_flight{route="`...)
			b = append(b, route...)
			b = append(b, `",upstream="`...)
			b = append(b, metrics.EscapeLabel(st.URL)...)
			b = append(b, `"} `...)
			b = strconv.AppendInt(b, int64(st.InFlight), 10)
			b = append(b, '\n')
		}
	}
	w.Write(b)
}

// routePatterns returns the ServeMux patterns a route path maps to. A bare
// prefix like "/api" also registers the subtree "/api/"; "/" and paths that
// already end in "/" map to a single pattern.
func routePatterns(path string) []string {
	if path == "/" || path[len(path)-1] == '/' {
		return []string{path}
	}
	return []string{path, path + "/"}
}

// vhostDispatcher returns a handler for one ServeMux pattern. With a single
// catch-all entry it's the handler itself; otherwise it dispatches by Host,
// preferring an exact server_name match and falling back to a catch-all entry.
func vhostDispatcher(entries []vhostEntry) http.Handler {
	if len(entries) == 1 && entries[0].serverName == "" {
		return entries[0].handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range entries {
			if e.serverName != "" && hostMatches(e.serverName, r.Host) {
				e.handler.ServeHTTP(w, r)
				return
			}
		}
		for _, e := range entries {
			if e.serverName == "" {
				e.handler.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})
}

// hostMatches compares a configured server_name against a request Host header,
// ignoring any port and case.
func hostMatches(serverName, host string) bool {
	if strings.IndexByte(host, ':') >= 0 {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	return strings.EqualFold(host, serverName)
}

func (s *Server) rateLimitMiddleware(rl *rateLimiter, next http.Handler) http.Handler {
	trustForwarded := s.cfg.TrustForwardedFor != nil && *s.cfg.TrustForwardedFor
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(extractIP(r, trustForwarded)) {
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
	metricsOn := s.cfg.MetricsEnabled()
	logOn := s.cfg.AccessLogEnabled()

	// Zero-cost path: skip the wrapper entirely only when nothing observes it.
	if !metricsOn && !logOn {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := newResponseWriter(w)

		if metricsOn {
			metrics.IncInFlight()
		}

		// Finalize on the way out — runs on both normal return and panic, so a
		// recovered handler is still counted (as 5xx) and logged.
		defer func() {
			if metricsOn {
				metrics.DecInFlight()
			}
			if rec := recover(); rec != nil {
				if !lrw.written {
					lrw.WriteHeader(http.StatusInternalServerError)
				}
				logger.Error("panic recovered", logger.LogFields{
					"method": r.Method,
					"path":   r.URL.Path,
					"panic":  fmt.Sprint(rec),
				})
			}
			dur := time.Since(start)
			if metricsOn {
				metrics.Observe(lrw.status, lrw.bytes, dur.Nanoseconds())
			}
			if logOn {
				select {
				case s.logCh <- logEntry{start, r.Method, r.URL.Path, lrw.status, dur, lrw.upstream}:
				default:
				}
			}
			putResponseWriter(lrw)
		}()

		if id := r.Header.Get("X-Request-ID"); id != "" {
			r = r.WithContext(logger.WithRequestID(r.Context(), id))
		}

		next.ServeHTTP(lrw, r)
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
	s.stopHealthChecks()
	err := s.http.Shutdown(ctx)
	close(s.stopLog)
	close(s.stopRL)
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

	if cfg.GOGC > 0 {
		debug.SetGCPercent(cfg.GOGC)
		logger.Info("gc percent set", logger.LogFields{"gogc": cfg.GOGC})
	}

	if cfg.MaxProcs > 0 {
		prev := runtime.GOMAXPROCS(cfg.MaxProcs)
		logger.Info("gomaxprocs set", logger.LogFields{"gomaxprocs": cfg.MaxProcs, "previous": prev})
	}

	// Apply transport tuning before any proxy route is built in New.
	up := cfg.EffectiveUpstream()
	proxy.Configure(up)
	logger.Info("upstream transport tuned", logger.LogFields{
		"max_idle_conns":          up.MaxIdleConns,
		"max_idle_conns_per_host": up.MaxIdleConnsPerHost,
		"buffer_size":             up.BufferSize,
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
				proxy.Configure(newCfg.EffectiveUpstream())
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
	bytes    int64
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
	rw.bytes = 0
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
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

func (rw *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	var (
		n   int64
		err error
	)
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(rw.ResponseWriter, r)
	}
	rw.bytes += n
	return n, err
}

// extractIP returns the client IP used for rate limiting. It only honors
// X-Forwarded-For when trustForwarded is set (gofly is behind a trusted proxy);
// otherwise it uses the socket peer address, so a direct client cannot spoof
// its identity to evade per-IP limits.
func extractIP(r *http.Request, trustForwarded bool) string {
	if trustForwarded {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if idx := strings.IndexByte(fwd, ','); idx > 0 {
				return strings.TrimSpace(fwd[:idx])
			}
			return strings.TrimSpace(fwd)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func gzipEnabled(route config.Route) bool {
	if route.Gzip != nil {
		return *route.Gzip
	}
	return false
}

// gzipPools holds one sync.Pool of *gzip.Writer per compression level, since a
// writer's level is fixed once created. Reusing writers avoids reallocating the
// ~256 KiB compressor window on every gzipped response.
var gzipPools sync.Map // int -> *sync.Pool

func getGzipWriter(w io.Writer, level int) *gzip.Writer {
	p, _ := gzipPools.LoadOrStore(level, &sync.Pool{})
	if gw, ok := p.(*sync.Pool).Get().(*gzip.Writer); ok {
		gw.Reset(w)
		return gw
	}
	gw, err := gzip.NewWriterLevel(w, level)
	if err != nil {
		gw = gzip.NewWriter(w)
	}
	return gw
}

func putGzipWriter(level int, gw *gzip.Writer) {
	if p, ok := gzipPools.Load(level); ok {
		p.(*sync.Pool).Put(gw)
	}
}

// gzipBufPool recycles the threshold buffer used when minLength > 0, so small
// responses don't each allocate (and discard) a fresh backing array while
// waiting to learn whether they cross the gzip threshold.
var gzipBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 16<<10)
		return &b
	},
}

func getGzipBuf() []byte {
	return (*gzipBufPool.Get().(*[]byte))[:0]
}

func putGzipBuf(b []byte) {
	if cap(b) == 0 {
		return
	}
	b = b[:0]
	gzipBufPool.Put(&b)
}

// gzipResponseWriter compresses responses lazily. With minLength == 0 it commits
// to gzip on the first write (streaming, historical behaviour). With
// minLength > 0 it buffers until the body crosses the threshold, sending small
// responses uncompressed so tiny payloads aren't inflated.
type gzipResponseWriter struct {
	http.ResponseWriter
	level       int
	minLength   int
	status      int
	buf         []byte
	gw          *gzip.Writer
	gzipOn      bool
	wroteHeader bool
}

func (g *gzipResponseWriter) commitGzip() {
	if g.wroteHeader {
		return
	}
	g.gzipOn = true
	h := g.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Set("Vary", "Accept-Encoding")
	g.gw = getGzipWriter(g.ResponseWriter, g.level)
	g.wroteHeader = true
	g.ResponseWriter.WriteHeader(g.status)
}

func (g *gzipResponseWriter) commitPlain() {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	g.ResponseWriter.WriteHeader(g.status)
}

// bodyless reports whether a status code must not carry a response body, in
// which case gzip is skipped (compressing a 304/204 would emit a gzip stream as
// an illegal body).
func bodyless(code int) bool {
	return code == http.StatusNoContent || code == http.StatusNotModified || (code >= 100 && code < 200)
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.status = code
	if bodyless(code) {
		g.commitPlain()
		return
	}
	if g.minLength <= 0 {
		g.commitGzip()
	}
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.status == 0 {
		g.status = http.StatusOK
	}
	if g.wroteHeader {
		if g.gzipOn {
			return g.gw.Write(b)
		}
		return g.ResponseWriter.Write(b)
	}
	if g.minLength <= 0 {
		g.commitGzip()
		return g.gw.Write(b)
	}
	if g.buf == nil {
		g.buf = getGzipBuf()
	}
	g.buf = append(g.buf, b...)
	if len(g.buf) >= g.minLength {
		g.commitGzip()
		if _, err := g.gw.Write(g.buf); err != nil {
			putGzipBuf(g.buf)
			g.buf = nil
			return 0, err
		}
		putGzipBuf(g.buf)
		g.buf = nil
	}
	return len(b), nil
}

// finish flushes any buffered/compressed bytes and returns the writer to the
// pool. It must be called after the wrapped handler returns.
func (g *gzipResponseWriter) finish() {
	if !g.wroteHeader {
		g.commitPlain()
		if len(g.buf) > 0 {
			g.ResponseWriter.Write(g.buf)
		}
		putGzipBuf(g.buf)
		g.buf = nil
		return
	}
	if g.gzipOn && g.gw != nil {
		g.gw.Close()
		putGzipWriter(g.level, g.gw)
		g.gw = nil
	}
}

// gzipMiddleware keeps the historical single-argument signature (default level,
// no minimum length) for callers and tests that don't need tuning.
func gzipMiddleware(next http.Handler) http.Handler {
	return 	gzipMiddlewareWith(next, -1, 0)
}

var gzipRWPool = sync.Pool{
	New: func() any {
		return &gzipResponseWriter{}
	},
}

func gzipMiddlewareWith(next http.Handler, level, minLength int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		grw := gzipRWPool.Get().(*gzipResponseWriter)
		grw.ResponseWriter = w
		grw.level = level
		grw.minLength = minLength
		grw.status = 0
		grw.buf = nil
		grw.gw = nil
		grw.gzipOn = false
		grw.wroteHeader = false
		next.ServeHTTP(grw, r)
		grw.finish()
		grw.ResponseWriter = nil
		gzipRWPool.Put(grw)
	})
}
