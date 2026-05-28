package proxy

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

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

	rp1 := p.reverseproxies[0]

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

		if p.reverseproxies[0].Transport != rp1.Transport {
			t.Errorf("iteration %d: transport changed", i)
		}
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
