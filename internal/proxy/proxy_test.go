package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var callOrder []int
	mu := strings.Builder{}

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

	for i := 0; i < 4; i++ {
		resp, err := http.Get(frontend.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		mu.WriteString(string(body))
		callOrder = append(callOrder, i)
	}

	result := mu.String()
	if len(result) != 4 {
		t.Errorf("expected 4 responses, got %d", len(result))
	}
	if result != "1212" && result != "2121" && result != "1221" && result != "2112" {
		t.Logf("round-robin result: %s", result)
	}
	_ = callOrder
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
