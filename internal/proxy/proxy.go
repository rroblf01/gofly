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

var DefaultTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
	TLSClientConfig: &tls.Config{
		InsecureSkipVerify: false,
	},
	DisableCompression: false,
}

type UpstreamState struct {
	url       *url.URL
	index     int
	failCount int32
	lastFail  int64
	disabled  int32
}

type Proxy struct {
	route          config.Route
	upstreams      []*UpstreamState
	counter        atomic.Uint64
	reverseproxies []*httputil.ReverseProxy
}

func New(route config.Route) (*Proxy, error) {
	p := &Proxy{route: route}

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
	t := DefaultTransport
	if route.UpstreamTimeout != nil {
		t = &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
			DisableCompression:    false,
			ResponseHeaderTimeout: route.UpstreamTimeout.Duration,
		}
	}
	return t
}

func (p *Proxy) next() *UpstreamState {
	n := len(p.upstreams)
	if n == 0 {
		return nil
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
	req.Header.Set("X-Forwarded-For", req.RemoteAddr)
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

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "Upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (p *Proxy) serveWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL) {
	p.applyDirector(r)

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
		io.Copy(upstreamConn, clientConn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, upstreamConn)
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


