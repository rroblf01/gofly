package proxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

type UpstreamSetter interface {
	SetUpstream(string)
}

// transportTuning holds the pool/buffer sizes applied to every upstream
// transport. It is set once at startup via Configure and then read-only, so the
// hot path needs no synchronisation.
type transportTuning struct {
	maxIdleConns        int
	maxIdleConnsPerHost int
	bufferSize          int
}

var tuning = transportTuning{
	maxIdleConns:        config.DefaultUpstreamMaxIdleConns,
	maxIdleConnsPerHost: config.DefaultUpstreamMaxIdleConnsPerHost,
	bufferSize:          config.DefaultUpstreamBufferSize,
}

// Configure sets the global upstream transport tuning. Call before New (i.e.
// before any proxy is built); it also resets the cached transports so a SIGHUP
// reload picks up new values.
func Configure(u config.Upstream) {
	tuning = transportTuning{
		maxIdleConns:        u.MaxIdleConns,
		maxIdleConnsPerHost: u.MaxIdleConnsPerHost,
		bufferSize:          u.BufferSize,
	}
	DefaultTransport = newTransport(0)
	timeoutTransports = sync.Map{}
}

// newTransport builds a transport with the current tuning and an optional
// response-header timeout (0 = none).
func newTransport(responseHeaderTimeout time.Duration) *http.Transport {
	return &http.Transport{
		MaxIdleConns:          tuning.maxIdleConns,
		MaxIdleConnsPerHost:   tuning.maxIdleConnsPerHost,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
		WriteBufferSize:       tuning.bufferSize,
		ReadBufferSize:        tuning.bufferSize,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		DisableCompression:    false,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}
}

// DefaultTransport is the shared transport for routes without a per-route
// upstream timeout. Rebuilt by Configure.
var DefaultTransport = newTransport(0)

// timeoutTransports caches one transport per distinct ResponseHeaderTimeout, so
// N routes sharing a timeout share one connection pool instead of allocating N.
var timeoutTransports sync.Map // time.Duration -> *http.Transport

// wsBufPool recycles the 32 KiB relay buffers used to shuttle bytes between the
// client and upstream on hijacked WebSocket connections, so long-lived
// connections don't each pin two fresh buffers.
var wsBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

type UpstreamState struct {
	url       *url.URL
	index     int
	failCount int32
	lastFail  int64
	disabled  int32
	inFlight  int32
}

// UpstreamStatus is a point-in-time snapshot of one upstream, used for metrics.
type UpstreamStatus struct {
	URL      string
	Healthy  bool
	InFlight int32
}

type Proxy struct {
	route          config.Route
	upstreams      []*UpstreamState
	counter        atomic.Uint64
	reverseproxies []*httputil.ReverseProxy
	strategy       string

	hcClient *http.Client
	stopHC   chan struct{}
	hcWG     sync.WaitGroup
}

func New(route config.Route) (*Proxy, error) {
	p := &Proxy{route: route, strategy: route.Strategy}

	if route.HealthCheckPath != "" {
		timeout := 5 * time.Second
		if route.HealthCheckInterval != nil && route.HealthCheckInterval.Duration < timeout {
			timeout = route.HealthCheckInterval.Duration
		}
		p.hcClient = &http.Client{Timeout: timeout}
	}

	for i, u := range route.Upstreams {
		parsed, err := url.Parse(u)
		if err != nil {
			return nil, err
		}

		state := &UpstreamState{url: parsed, index: i}
		p.upstreams = append(p.upstreams, state)

		rp := httputil.NewSingleHostReverseProxy(parsed)
		rp.Transport = transportForRoute(route)
		rp.ErrorHandler = p.errorHandler(state)

		baseDirector := rp.Director
		rp.Director = p.director(parsed, baseDirector)
		p.reverseproxies = append(p.reverseproxies, rp)
	}

	return p, nil
}

func transportForRoute(route config.Route) *http.Transport {
	if route.UpstreamTimeout == nil {
		return DefaultTransport
	}
	d := route.UpstreamTimeout.Duration
	if t, ok := timeoutTransports.Load(d); ok {
		return t.(*http.Transport)
	}
	t, _ := timeoutTransports.LoadOrStore(d, newTransport(d))
	return t.(*http.Transport)
}

func (p *Proxy) next() *UpstreamState {
	n := len(p.upstreams)
	if n == 0 {
		return nil
	}

	if p.strategy == "least_conn" {
		// Pick the healthy upstream with the fewest in-flight requests. The
		// rotating start makes ties round-robin instead of always favoring the
		// lowest index.
		start := p.counter.Add(1)
		var best *UpstreamState
		var bestLoad int32
		for k := 0; k < n; k++ {
			s := p.upstreams[(start+uint64(k))%uint64(n)]
			if atomic.LoadInt32(&s.disabled) != 0 {
				continue
			}
			load := atomic.LoadInt32(&s.inFlight)
			if best == nil || load < bestLoad {
				best, bestLoad = s, load
			}
		}
		return best
	}

	for range n {
		i := p.counter.Add(1) - 1
		state := p.upstreams[i%uint64(n)]
		if atomic.LoadInt32(&state.disabled) == 0 {
			return state
		}
	}

	return nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	up := p.next()
	if up == nil {
		logger.Error("no healthy upstreams available", logger.LogFields{"path": r.URL.Path})
		http.Error(w, "no healthy upstream", http.StatusServiceUnavailable)
		return
	}

	if us, ok := w.(UpstreamSetter); ok {
		us.SetUpstream(up.url.String())
	}

	atomic.AddInt32(&up.inFlight, 1)
	defer atomic.AddInt32(&up.inFlight, -1)

	if isWebSocket(r) {
		p.serveWebSocket(w, r, up.url)
		return
	}

	maxBodySize := p.route.EffectiveMaxBodySize(0)
	if maxBodySize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	}

	p.reverseproxies[up.index].ServeHTTP(w, r)
}

func (p *Proxy) applyDirector(req *http.Request) {
	// X-Forwarded-For is left to httputil.ReverseProxy, which appends the
	// parsed client IP (without port) to any existing chain. For the hijacked
	// WebSocket path we set it explicitly in serveWebSocket.
	req.Header.Set("X-Forwarded-Host", req.Host)
	req.Header.Set("X-Forwarded-Proto", scheme(req))

	for k, v := range p.route.SetHeaders {
		req.Header.Set(k, expandVars(v, req))
	}
	for _, h := range p.route.RemoveHeaders {
		req.Header.Del(h)
	}
	if p.route.Host != "" {
		req.Host = expandVars(p.route.Host, req)
	}
	if p.route.Rewrite != "" {
		req.URL.Path = expandVars(p.route.Rewrite, req)
	}
}

func (p *Proxy) director(up *url.URL, base func(*http.Request)) func(*http.Request) {
	return func(req *http.Request) {
		if base != nil {
			base(req)
		}
		p.applyDirector(req)
	}
}

func (p *Proxy) errorHandler(state *UpstreamState) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		p.recordFailure(state)

		logger.Error("proxy error", logger.LogFields{
			"upstream": state.url.String(),
			"path":     r.URL.Path,
			"error":    err.Error(),
		})

		if p.route.RetryOnError {
			up := p.next()
			if up != nil && up != state {
				logger.Info("retrying with next upstream", logger.LogFields{
					"upstream": up.url.String(),
					"path":     r.URL.Path,
				})
				p.reverseproxies[up.index].ServeHTTP(w, r)
				return
			}
		}

		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}

func (p *Proxy) recordFailure(state *UpstreamState) {
	maxFails := 1
	if p.route.MaxFails > 0 {
		maxFails = p.route.MaxFails
	}

	failTimeout := 30 * time.Second
	if p.route.FailTimeout != nil {
		failTimeout = p.route.FailTimeout.Duration
	}

	now := time.Now().UnixNano()
	lastFail := atomic.LoadInt64(&state.lastFail)

	if now-lastFail > failTimeout.Nanoseconds() {
		atomic.StoreInt32(&state.failCount, 1)
		atomic.StoreInt64(&state.lastFail, now)
		return
	}

	newFails := atomic.AddInt32(&state.failCount, 1)
	atomic.StoreInt64(&state.lastFail, now)

	if int(newFails) >= maxFails {
		atomic.StoreInt32(&state.disabled, 1)
		logger.Warn("upstream disabled due to failures", logger.LogFields{
			"upstream":  state.url.String(),
			"max_fails": maxFails,
		})

		time.AfterFunc(failTimeout, func() {
			atomic.StoreInt32(&state.disabled, 0)
			atomic.StoreInt32(&state.failCount, 0)
			logger.Info("upstream re-enabled after cooldown", logger.LogFields{
				"upstream": state.url.String(),
			})
		})
	}
}

func (p *Proxy) UpstreamURL() string {
	if len(p.upstreams) == 0 {
		return ""
	}
	return p.upstreams[0].url.String()
}

// Path returns the route path this proxy serves (used for metric labels).
func (p *Proxy) Path() string { return p.route.Path }

// Stats returns a snapshot of every upstream's health and in-flight count.
func (p *Proxy) Stats() []UpstreamStatus {
	out := make([]UpstreamStatus, 0, len(p.upstreams))
	for _, s := range p.upstreams {
		out = append(out, UpstreamStatus{
			URL:      s.url.String(),
			Healthy:  atomic.LoadInt32(&s.disabled) == 0,
			InFlight: atomic.LoadInt32(&s.inFlight),
		})
	}
	return out
}

// Start launches the active health-check loop if health_check_path is set.
// It is a no-op otherwise. Stop must be called to release the goroutine.
func (p *Proxy) Start() {
	if p.hcClient == nil {
		return
	}
	interval := 10 * time.Second
	if p.route.HealthCheckInterval != nil {
		interval = p.route.HealthCheckInterval.Duration
	}
	p.stopHC = make(chan struct{})
	p.hcWG.Add(1)
	go func() {
		defer p.hcWG.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		p.runHealthChecks() // probe immediately at startup
		for {
			select {
			case <-t.C:
				p.runHealthChecks()
			case <-p.stopHC:
				return
			}
		}
	}()
}

// Stop terminates the active health-check loop and waits for it to exit.
func (p *Proxy) Stop() {
	if p.stopHC != nil {
		close(p.stopHC)
		p.hcWG.Wait()
		p.stopHC = nil
	}
}

func (p *Proxy) runHealthChecks() {
	for _, s := range p.upstreams {
		target := strings.TrimRight(s.url.String(), "/") + p.route.HealthCheckPath
		healthy := false
		if req, err := http.NewRequest(http.MethodGet, target, nil); err == nil {
			if resp, err := p.hcClient.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				healthy = resp.StatusCode >= 200 && resp.StatusCode < 400
			}
		}

		if healthy {
			if atomic.SwapInt32(&s.disabled, 0) == 1 {
				logger.Info("upstream healthy (active check)", logger.LogFields{"upstream": s.url.String()})
			}
			atomic.StoreInt32(&s.failCount, 0)
		} else {
			if atomic.SwapInt32(&s.disabled, 1) == 0 {
				logger.Warn("upstream unhealthy (active check)", logger.LogFields{"upstream": s.url.String()})
			}
		}
	}
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "Upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (p *Proxy) serveWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL) {
	p.applyDirector(r)

	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}
	if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
		r.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		r.Header.Set("X-Forwarded-For", clientIP)
	}

	targetAddr := target.Host
	if !strings.Contains(targetAddr, ":") {
		if target.Scheme == "https" || target.Scheme == "wss" {
			targetAddr += ":443"
		} else {
			targetAddr += ":80"
		}
	}

	var dialer net.Dialer
	upstreamConn, err := dialer.DialContext(r.Context(), "tcp", targetAddr)
	if err != nil {
		logger.Error("websocket dial failed", logger.LogFields{
			"target": targetAddr,
			"error":  err.Error(),
		})
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	if err := r.Write(upstreamConn); err != nil {
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := wsBufPool.Get().(*[]byte)
		defer wsBufPool.Put(buf)
		io.CopyBuffer(upstreamConn, clientConn, *buf)
	}()
	go func() {
		defer wg.Done()
		buf := wsBufPool.Get().(*[]byte)
		defer wsBufPool.Put(buf)
		io.CopyBuffer(clientConn, upstreamConn, *buf)
	}()

	wg.Wait()
}

func expandVars(val string, r *http.Request) string {
	val = strings.ReplaceAll(val, "$remote_addr", r.RemoteAddr)
	val = strings.ReplaceAll(val, "$host", r.Host)
	val = strings.ReplaceAll(val, "$scheme", scheme(r))
	val = strings.ReplaceAll(val, "$request_uri", r.URL.RequestURI())
	val = strings.ReplaceAll(val, "$uri", r.URL.Path)
	return val
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
