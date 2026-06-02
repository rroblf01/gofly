package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

func init() {
	logger.Init()
}

func TestProxy_SingleUpstream(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", string(body), "hello")
	}
}

func TestProxy_MultipleUpstreams(t *testing.T) {
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1"))
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("2"))
	}))
	defer backend2.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend1.URL, backend2.URL},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	var mu sync.Mutex
	var result strings.Builder
	var wg sync.WaitGroup

	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(frontend.URL + "/")
			if err != nil {
				t.Error(err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			mu.Lock()
			result.WriteString(string(body))
			mu.Unlock()
		}()
	}
	wg.Wait()

	s := result.String()
	if len(s) != 6 {
		t.Errorf("expected 6 responses, got %d", len(s))
	}
	count1 := strings.Count(s, "1")
	count2 := strings.Count(s, "2")
	if count1 != 3 || count2 != 3 {
		t.Errorf("expected 3 of each, got 1=%d 2=%d", count1, count2)
	}
}

func TestProxy_NoUpstreams(t *testing.T) {
	route := config.Route{
		Path:      "/",
		Upstreams: nil,
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestProxy_ForwardsHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
		SetHeaders: map[string]string{
			"X-Custom": "test",
		},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestProxy_RemovesHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Remove-Me") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.Route{
		Path:          "/",
		Upstreams:     []string{backend.URL},
		RemoveHeaders: []string{"X-Remove-Me"},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	req, _ := http.NewRequest("GET", frontend.URL+"/", nil)
	req.Header.Set("X-Remove-Me", "should-not-exist")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestProxy_BackendDown(t *testing.T) {
	route := config.Route{
		Path:      "/",
		Upstreams: []string{"http://127.0.0.1:1"},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestProxy_BackendDownWithRetry(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer healthy.Close()

	route := config.Route{
		Path:         "/",
		Upstreams:    []string{"http://127.0.0.1:1", healthy.URL},
		RetryOnError: true,
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", string(body), "ok")
	}
}

func TestNew_InvalidURL(t *testing.T) {
	route := config.Route{
		Path:      "/",
		Upstreams: []string{"://invalid-url"},
	}

	_, err := New(route)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestRoundRobin_Empty(t *testing.T) {
	p := &Proxy{}
	u := p.next()
	if u != nil {
		t.Errorf("expected nil, got %v", u)
	}
}

func TestProxy_LeastConnPicksLeastLoaded(t *testing.T) {
	route := config.Route{
		Path:      "/",
		Upstreams: []string{"http://a.local", "http://b.local"},
		Strategy:  "least_conn",
	}
	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	// upstream[0] is busy, upstream[1] is idle.
	atomic.StoreInt32(&p.upstreams[0].inFlight, 5)
	atomic.StoreInt32(&p.upstreams[1].inFlight, 0)

	got := p.next()
	if got != p.upstreams[1] {
		t.Errorf("least_conn should pick the idle upstream[1], got index %d", got.index)
	}
}

func TestProxy_LeastConnSkipsDisabled(t *testing.T) {
	route := config.Route{
		Path:      "/",
		Upstreams: []string{"http://a.local", "http://b.local"},
		Strategy:  "least_conn",
	}
	p, _ := New(route)

	// upstream[1] has fewer connections but is disabled.
	atomic.StoreInt32(&p.upstreams[0].inFlight, 9)
	atomic.StoreInt32(&p.upstreams[1].inFlight, 0)
	atomic.StoreInt32(&p.upstreams[1].disabled, 1)

	got := p.next()
	if got != p.upstreams[0] {
		t.Errorf("least_conn must skip disabled upstream, expected upstream[0]")
	}
}

func TestProxy_ActiveHealthCheck(t *testing.T) {
	logger.Init()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer good.Close()

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	interval := config.Duration{Duration: 20 * time.Millisecond}
	route := config.Route{
		Path:                "/",
		Upstreams:           []string{good.URL, bad.URL},
		HealthCheckPath:     "/healthz",
		HealthCheckInterval: &interval,
	}
	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}
	p.Start()
	defer p.Stop()

	time.Sleep(100 * time.Millisecond)

	status := map[string]bool{}
	for _, s := range p.Stats() {
		status[s.URL] = s.Healthy
	}
	if !status[good.URL] {
		t.Errorf("upstream returning 200 should be healthy")
	}
	if status[bad.URL] {
		t.Errorf("upstream returning 503 should be marked unhealthy")
	}
}

func TestProxy_HealthCheckNoopWithoutPath(t *testing.T) {
	route := config.Route{Path: "/", Upstreams: []string{"http://a.local"}}
	p, _ := New(route)
	// Start/Stop must be safe no-ops when no health_check_path is configured.
	p.Start()
	p.Stop()
}

func TestProxy_PassiveDisableAfterMaxFails(t *testing.T) {
	logger.Init()

	failTimeout := config.Duration{Duration: time.Minute}
	route := config.Route{
		Path:        "/",
		Upstreams:   []string{"http://127.0.0.1:1"}, // connection refused
		MaxFails:    2,
		FailTimeout: &failTimeout,
	}
	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}
	frontend := httptest.NewServer(p)
	defer frontend.Close()

	// First two requests fail with 502 and accumulate failures.
	for i := 0; i < 2; i++ {
		resp, err := http.Get(frontend.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("request %d: status = %d, want 502", i+1, resp.StatusCode)
		}
	}

	// The upstream is now disabled → no healthy upstream → 503.
	resp, err := http.Get(frontend.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("after max_fails: status = %d, want 503", resp.StatusCode)
	}
}

func TestProxy_WebSocketRelay(t *testing.T) {
	logger.Init()

	// Backend completes the upgrade handshake and echoes one line.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		buf.Flush()
		line, err := buf.ReadString('\n')
		if err != nil {
			return
		}
		buf.WriteString(line)
		buf.Flush()
	}))
	defer backend.Close()

	p, err := New(config.Route{Path: "/", Upstreams: []string{backend.URL}})
	if err != nil {
		t.Fatal(err)
	}
	frontend := httptest.NewServer(p)
	defer frontend.Close()

	addr := strings.TrimPrefix(frontend.URL, "http://")
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))

	fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	br := bufio.NewReader(c)

	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("expected 101 Switching Protocols, got %q", status)
	}
	// Drain headers until the blank line.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	fmt.Fprint(c, "ping\n")
	echo, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(echo) != "ping" {
		t.Errorf("echo = %q, want %q", strings.TrimSpace(echo), "ping")
	}
}

func TestScheme_HTTP(t *testing.T) {
	r := &http.Request{}
	if got := scheme(r); got != "http" {
		t.Errorf("scheme() = %q, want %q", got, "http")
	}
}

func TestScheme_HTTPS(t *testing.T) {
	r := &http.Request{TLS: &tls.ConnectionState{}}
	if got := scheme(r); got != "https" {
		t.Errorf("scheme() = %q, want %q", got, "https")
	}
}

func TestExpandVars(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "192.168.1.1:12345",
		Host:       "example.com",
		URL: &url.URL{
			Path:     "/foo/bar",
			RawQuery: "q=1",
		},
	}

	got := expandVars("$remote_addr $host $scheme $uri $request_uri", r)
	want := "192.168.1.1:12345 example.com http /foo/bar /foo/bar?q=1"
	if got != want {
		t.Errorf("expandVars = %q, want %q", got, want)
	}
}

func TestIsWebSocket(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{
			name: "websocket upgrade",
			header: http.Header{
				"Connection": {"Upgrade"},
				"Upgrade":    {"websocket"},
			},
			want: true,
		},
		{
			name: "case insensitive",
			header: http.Header{
				"Connection": {"upgrade"},
				"Upgrade":    {"WebSocket"},
			},
			want: true,
		},
		{
			name: "no upgrade header",
			header: http.Header{
				"Connection": {"close"},
			},
			want: false,
		},
		{
			name:   "empty headers",
			header: http.Header{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Header: tt.header}
			if got := isWebSocket(r); got != tt.want {
				t.Errorf("isWebSocket = %v, want %v", got, tt.want)
			}
		})
	}
}

type mockUpstreamSetter struct {
	url        string
	headers    http.Header
	statusCode int
	body       []byte
}

func (m *mockUpstreamSetter) SetUpstream(url string) {
	m.url = url
}

func (m *mockUpstreamSetter) Header() http.Header {
	if m.headers == nil {
		m.headers = http.Header{}
	}
	return m.headers
}

func (m *mockUpstreamSetter) Write(b []byte) (int, error) {
	m.body = append(m.body, b...)
	return len(b), nil
}

func (m *mockUpstreamSetter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

func TestUpstreamSetterInterface(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	setter := &mockUpstreamSetter{}
	p.ServeHTTP(setter, &http.Request{
		URL: &url.URL{Path: "/test"},
	})

	if setter.url != backend.URL {
		t.Errorf("SetUpstream called with %q, want %q", setter.url, backend.URL)
	}
}

func TestProxy_ConcurrentRequests(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL, backend.URL, backend.URL},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	var wg sync.WaitGroup
	workers := 20
	requests := 50

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range requests {
				resp, err := http.Get(frontend.URL + "/")
				if err != nil {
					t.Error(err)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
}

func TestProxy_TransportReuse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	tr := transportForRoute(route)

	for i := 0; i < 5; i++ {
		frontend := httptest.NewServer(p)
		resp, err := http.Get(frontend.URL + "/")
		if err != nil {
			frontend.Close()
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		frontend.Close()

		if transportForRoute(route) != tr {
			t.Errorf("iteration %d: transport changed", i)
		}
	}
}

func TestTransportForRoute_NoTimeoutSharesDefault(t *testing.T) {
	a := transportForRoute(config.Route{Path: "/a"})
	b := transportForRoute(config.Route{Path: "/b"})
	if a != DefaultTransport || b != DefaultTransport {
		t.Fatal("routes without upstream_timeout should share DefaultTransport")
	}
}

func TestTransportForRoute_SameTimeoutShared(t *testing.T) {
	d := config.Duration{Duration: 7 * time.Second}
	r1 := config.Route{Path: "/a", UpstreamTimeout: &d}
	r2 := config.Route{Path: "/b", UpstreamTimeout: &d}

	t1 := transportForRoute(r1)
	t2 := transportForRoute(r2)
	if t1 != t2 {
		t.Fatal("routes with the same upstream_timeout should share one transport")
	}
	if t1.ResponseHeaderTimeout != d.Duration {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", t1.ResponseHeaderTimeout, d.Duration)
	}

	other := config.Duration{Duration: 3 * time.Second}
	t3 := transportForRoute(config.Route{Path: "/c", UpstreamTimeout: &other})
	if t3 == t1 {
		t.Fatal("distinct timeouts must not share a transport")
	}
}

func TestConfigure_AppliesTuning(t *testing.T) {
	t.Cleanup(func() { c := config.Config{}; Configure(c.EffectiveUpstream()) })

	Configure(config.Upstream{MaxIdleConns: 8, MaxIdleConnsPerHost: 4, BufferSize: 4096})
	if DefaultTransport.MaxIdleConns != 8 ||
		DefaultTransport.MaxIdleConnsPerHost != 4 ||
		DefaultTransport.WriteBufferSize != 4096 ||
		DefaultTransport.ReadBufferSize != 4096 {
		t.Fatalf("Configure did not apply tuning: %+v", DefaultTransport)
	}
}

func TestProxy_ForwardsXForwardedHeaders(t *testing.T) {
	var forwardedFor, forwardedHost, forwardedProto string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardedFor = r.Header.Get("X-Forwarded-For")
		forwardedHost = r.Header.Get("X-Forwarded-Host")
		forwardedProto = r.Header.Get("X-Forwarded-Proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	http.Get(frontend.URL + "/test")

	if forwardedFor == "" {
		t.Error("X-Forwarded-For should not be empty")
	}
	if forwardedHost == "" {
		t.Error("X-Forwarded-Host should not be empty")
	}
	if forwardedProto != "http" {
		t.Errorf("X-Forwarded-Proto = %q, want %q", forwardedProto, "http")
	}
}

func TestProxy_HostRewrite(t *testing.T) {
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
		Host:      "rewritten.example.com",
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	http.Get(frontend.URL + "/")

	if receivedHost != "rewritten.example.com" {
		t.Errorf("r.Host = %q, want %q", receivedHost, "rewritten.example.com")
	}
}

func TestProxy_RewritePath(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	route := config.Route{
		Path:      "/",
		Upstreams: []string{backend.URL},
		Rewrite:   "/api",
	}

	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}

	frontend := httptest.NewServer(p)
	defer frontend.Close()

	http.Get(frontend.URL + "/original-path")

	if receivedPath != "/api" {
		t.Errorf("r.URL.Path = %q, want %q", receivedPath, "/api")
	}
}
