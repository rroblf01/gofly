package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

type Proxy struct {
	route     config.Route
	upstreams []*url.URL
	counter   atomic.Uint64
}

func New(route config.Route) (*Proxy, error) {
	p := &Proxy{route: route}
	for _, u := range route.Upstreams {
		parsed, err := url.Parse(u)
		if err != nil {
			return nil, err
		}
		p.upstreams = append(p.upstreams, parsed)
	}
	return p, nil
}

func (p *Proxy) next() *url.URL {
	n := len(p.upstreams)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return p.upstreams[0]
	}
	i := p.counter.Add(1) - 1
	return p.upstreams[i%uint64(n)]
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	up := p.next()
	if up == nil {
		logger.Error("no upstreams available", logger.LogFields{"path": r.URL.Path})
		http.Error(w, "no upstream available", http.StatusServiceUnavailable)
		return
	}

	rp := httputil.NewSingleHostReverseProxy(up)
	rp.Director = p.director(up, rp.Director)
	rp.ErrorHandler = p.errorHandler(up)
	rp.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
	}

	rp.ServeHTTP(w, r)
}

func (p *Proxy) director(up *url.URL, base func(*http.Request)) func(*http.Request) {
	return func(req *http.Request) {
		if base != nil {
			base(req)
		}
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Header.Set("X-Forwarded-Proto", scheme(req))

		for k, v := range p.route.SetHeaders {
			req.Header.Set(k, v)
		}
		for _, h := range p.route.RemoveHeaders {
			req.Header.Del(h)
		}
		if p.route.Host != "" {
			req.Host = p.route.Host
		}
	}
}

func (p *Proxy) errorHandler(up *url.URL) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("proxy error", logger.LogFields{
			"upstream": up.String(),
			"path":     r.URL.Path,
			"error":    err.Error(),
		})
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
