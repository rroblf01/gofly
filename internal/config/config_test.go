package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 80 {
		t.Errorf("default port = %d, want 80", cfg.Port)
	}
	if cfg.ReadTimeout.Duration != 30*time.Second {
		t.Errorf("default read timeout = %v, want 30s", cfg.ReadTimeout.Duration)
	}
	if cfg.WriteTimeout.Duration != 30*time.Second {
		t.Errorf("default write timeout = %v, want 30s", cfg.WriteTimeout.Duration)
	}
	if cfg.IdleTimeout.Duration != 120*time.Second {
		t.Errorf("default idle timeout = %v, want 120s", cfg.IdleTimeout.Duration)
	}
}

func TestLoad_CustomPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"port":3000,"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Errorf("port = %d, want 3000", cfg.Port)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"port":99999,"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{invalid json}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_RouteWithoutUpstreamOrStatic(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/api"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for route without upstream or static_dir, got nil")
	}
}

func TestLoad_RouteWithStaticDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/static","static_dir":"/var/www/html"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Errorf("routes count = %d, want 1", len(cfg.Routes))
	}
	if cfg.Routes[0].StaticDir != "/var/www/html" {
		t.Errorf("static_dir = %q, want /var/www/html", cfg.Routes[0].StaticDir)
	}
}

func TestLoad_TLSWithoutCert(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"tls":{"enabled":true},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for TLS without cert_file, got nil")
	}
}

func TestLoad_MutualExclusionStaticAndProxy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for route with both static_dir and upstreams, got nil")
	}
}

func TestLoad_EmptyRoutes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"read_timeout":"1x","routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

func TestLoad_InvalidStrategy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"],"strategy":"least_connections"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for unsupported strategy, got nil")
	}
}

func TestLoad_NegativeMaxFails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"],"max_fails":-1}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for negative max_fails, got nil")
	}
}

func TestLoad_RateLimitConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"rate_limit":{"requests_per_second":10,"burst":20},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RateLimit.RequestsPerSecond != 10 {
		t.Errorf("rate = %f, want 10", cfg.RateLimit.RequestsPerSecond)
	}
	if cfg.RateLimit.Burst != 20 {
		t.Errorf("burst = %d, want 20", cfg.RateLimit.Burst)
	}
}

func TestLoad_InvalidRateLimit(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"rate_limit":{"requests_per_second":0,"burst":0},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid rate limit, got nil")
	}
}

func TestDurationRoundTrip(t *testing.T) {
	d := Duration{30 * time.Second}
	data, err := d.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"30s"` {
		t.Errorf("marshal = %s, want \"30s\"", string(data))
	}

	var d2 Duration
	if err := d2.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if d2.Duration != 30*time.Second {
		t.Errorf("unmarshal = %v, want 30s", d2.Duration)
	}
}

func TestEffectiveMaxBodySize(t *testing.T) {
	r := Route{Path: "/", Upstreams: []string{"http://localhost:8080"}}
	if s := r.EffectiveMaxBodySize(100); s != 100 {
		t.Errorf("effective = %d, want 100", s)
	}

	r.MaxBodySize = 500
	if s := r.EffectiveMaxBodySize(100); s != 500 {
		t.Errorf("effective = %d, want 500", s)
	}
}

func TestLoad_WithSetHeaders(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"],"set_headers":{"X-Custom":"value"}}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Routes[0].SetHeaders["X-Custom"] != "value" {
		t.Errorf("header = %q, want value", cfg.Routes[0].SetHeaders["X-Custom"])
	}
}

func TestLoad_DurationParsing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"read_timeout":"15s","routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReadTimeout.Duration != 15*time.Second {
		t.Errorf("read timeout = %v, want 15s", cfg.ReadTimeout.Duration)
	}
}

func TestDefault_Config(t *testing.T) {
	cfg := Default()
	if cfg.Port != 80 {
		t.Errorf("default port = %d, want 80", cfg.Port)
	}
}
