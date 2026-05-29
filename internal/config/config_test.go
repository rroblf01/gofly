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

func TestConfigDefaults(t *testing.T) {
	var c Config
	if !c.MetricsEnabled() {
		t.Error("MetricsEnabled should default to true")
	}
	if c.EffectiveMemoryLimit() != DefaultMemoryLimit {
		t.Errorf("EffectiveMemoryLimit = %d, want %d", c.EffectiveMemoryLimit(), DefaultMemoryLimit)
	}

	var r Route
	if !r.PrecompressedEnabled() {
		t.Error("PrecompressedEnabled should default to true")
	}
	if r.GzipCompressionLevel() != -1 {
		t.Errorf("GzipCompressionLevel default = %d, want -1", r.GzipCompressionLevel())
	}
	if !r.SecurityHeadersDefault() {
		t.Error("SecurityHeadersDefault should default to true")
	}

	enabled := true
	mem := Config{MemoryLimit: 50 << 20}
	if mem.EffectiveMemoryLimit() != 50<<20 {
		t.Error("EffectiveMemoryLimit should honor explicit value")
	}
	off := false
	c2 := Config{Metrics: &off}
	if c2.MetricsEnabled() {
		t.Error("MetricsEnabled should honor explicit false")
	}
	lvl := 9
	r2 := Route{GzipLevel: &lvl, Precompressed: &enabled}
	if r2.GzipCompressionLevel() != 9 {
		t.Error("GzipCompressionLevel should honor explicit value")
	}
}

func TestLoad_LeastConnStrategy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"],"strategy":"least_conn"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err != nil {
		t.Fatalf("least_conn should be a valid strategy, got: %v", err)
	}
}

func TestLoad_DuplicateRoutePath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[
		{"path":"/api","upstreams":["http://a:1"]},
		{"path":"/api","upstreams":["http://b:2"]}
	]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("expected error for duplicate route path, got nil")
	}
}

func TestLoad_DuplicatePathDifferentHostsOK(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[
		{"path":"/","server_name":"a.com","static_dir":"/tmp"},
		{"path":"/","server_name":"b.com","static_dir":"/tmp"}
	]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err != nil {
		t.Fatalf("same path with different server_name should be valid, got: %v", err)
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

func TestLoad_EnvVarExpansion(t *testing.T) {
	os.Setenv("GOFLY_TEST_DIR", "/var/www/html")
	defer os.Unsetenv("GOFLY_TEST_DIR")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"${GOFLY_TEST_DIR}"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].StaticDir != "/var/www/html" {
		t.Errorf("static_dir after expansion = %q, want %q", cfg.Routes[0].StaticDir, "/var/www/html")
	}
}

func TestLoad_EnvVarTLS(t *testing.T) {
	os.Setenv("GOFLY_CERT", "/tmp/cert.pem")
	os.Setenv("GOFLY_KEY", "/tmp/key.pem")
	defer os.Unsetenv("GOFLY_CERT")
	defer os.Unsetenv("GOFLY_KEY")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"tls":{"enabled":true,"cert_file":"${GOFLY_CERT}","key_file":"${GOFLY_KEY}"},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.TLS.CertFile != "/tmp/cert.pem" {
		t.Errorf("cert_file after expansion = %q, want %q", cfg.TLS.CertFile, "/tmp/cert.pem")
	}
	if cfg.TLS.KeyFile != "/tmp/key.pem" {
		t.Errorf("key_file after expansion = %q, want %q", cfg.TLS.KeyFile, "/tmp/key.pem")
	}
}

func TestLoad_EnvVarSetHeaders(t *testing.T) {
	os.Setenv("GOFLY_CORS", "https://example.com")
	defer os.Unsetenv("GOFLY_CORS")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:3000"],"set_headers":{"Access-Control-Allow-Origin":"${GOFLY_CORS}"}}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].SetHeaders["Access-Control-Allow-Origin"] != "https://example.com" {
		t.Errorf("after expansion = %q, want %q", cfg.Routes[0].SetHeaders["Access-Control-Allow-Origin"], "https://example.com")
	}
}

func TestLoad_MissingEnvVar(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"],"host":"${UNDEFINED_VAR}"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].Host != "" {
		t.Errorf("undefined env var should expand to empty, got %q", cfg.Routes[0].Host)
	}
}

func TestLoad_TLSPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"tls":{"enabled":true,"cert_file":"/tmp/cert.pem","key_file":"/tmp/key.pem"},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.TLS.TLSPort != 0 {
		t.Errorf("TLSPort should default to 0 (meaning 443 at runtime), got %d", cfg.TLS.TLSPort)
	}
}

func TestLoad_CustomTLSPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"tls":{"enabled":true,"cert_file":"/tmp/cert.pem","key_file":"/tmp/key.pem","tls_port":8443},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.TLS.TLSPort != 8443 {
		t.Errorf("TLSPort = %d, want %d", cfg.TLS.TLSPort, 8443)
	}
}

func TestLoad_SPAFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","spa":true}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Routes[0].SPA {
		t.Error("SPA should be true")
	}
}

func TestLoad_SPAFlagDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].SPA {
		t.Error("SPA should default to false")
	}
}

func TestLoad_AutoIndex(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","autoindex":true}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Routes[0].AutoIndex {
		t.Error("AutoIndex should be true")
	}
}

func TestLoad_ErrorPages(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","error_pages":{"404":"/404.html","500":"/500.html"}}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].ErrorPages[404] != "/404.html" {
		t.Errorf("error_pages[404] = %q, want %q", cfg.Routes[0].ErrorPages[404], "/404.html")
	}
	if cfg.Routes[0].ErrorPages[500] != "/500.html" {
		t.Errorf("error_pages[500] = %q, want %q", cfg.Routes[0].ErrorPages[500], "/500.html")
	}
}

func TestLoad_ErrorPagesEnvVar(t *testing.T) {
	os.Setenv("GOFLY_404", "/custom_404.html")
	defer os.Unsetenv("GOFLY_404")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","error_pages":{"404":"${GOFLY_404}"}}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].ErrorPages[404] != "/custom_404.html" {
		t.Errorf("error_pages[404] after expansion = %q, want %q", cfg.Routes[0].ErrorPages[404], "/custom_404.html")
	}
}

func TestLoad_SecurityHeadersTrue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","security_headers":true}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].SecurityHeaders == nil || !*cfg.Routes[0].SecurityHeaders {
		t.Error("SecurityHeaders should be true")
	}
}

func TestLoad_SecurityHeadersFalse(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","security_headers":false}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].SecurityHeaders == nil || *cfg.Routes[0].SecurityHeaders {
		t.Error("SecurityHeaders should be false")
	}
}

func TestAccessLogEnabled(t *testing.T) {
	cfg := Default()
	if !cfg.AccessLogEnabled() {
		t.Error("AccessLogEnabled should default to true")
	}

	disabled := false
	cfg.AccessLog = &disabled
	if cfg.AccessLogEnabled() {
		t.Error("AccessLogEnabled should be false when access_log=false")
	}

	enabled := true
	cfg.AccessLog = &enabled
	if !cfg.AccessLogEnabled() {
		t.Error("AccessLogEnabled should be true when access_log=true")
	}
}

func TestSecurityHeadersDefault(t *testing.T) {
	r := Route{Path: "/", StaticDir: "/www"}
	if !r.SecurityHeadersDefault() {
		t.Error("SecurityHeadersDefault should default to true")
	}

	disabled := false
	r.SecurityHeaders = &disabled
	if r.SecurityHeadersDefault() {
		t.Error("SecurityHeadersDefault should be false when security_headers=false")
	}

	enabled := true
	r.SecurityHeaders = &enabled
	if !r.SecurityHeadersDefault() {
		t.Error("SecurityHeadersDefault should be true when security_headers=true")
	}
}

func TestSecurityHeadersEnabled(t *testing.T) {
	cfg := Default()
	if cfg.SecurityHeadersEnabled() {
		t.Error("SecurityHeadersEnabled should be false with no routes")
	}

	cfg.Routes = []Route{{Path: "/", StaticDir: "/www"}}
	if !cfg.SecurityHeadersEnabled() {
		t.Error("SecurityHeadersEnabled should be true when routes have default security headers")
	}

	disabled := false
	cfg.Routes = []Route{{Path: "/", StaticDir: "/www", SecurityHeaders: &disabled}}
	if cfg.SecurityHeadersEnabled() {
		t.Error("SecurityHeadersEnabled should be false when all routes have security_headers=false")
	}

	cfg.Routes = []Route{{Path: "/", StaticDir: "/www", SecurityHeaders: &disabled}}
	cfg.Routes = append(cfg.Routes, Route{Path: "/api", StaticDir: "/api"})
	if !cfg.SecurityHeadersEnabled() {
		t.Error("SecurityHeadersEnabled should be true when any route has default (nil) security headers")
	}
}

func TestEffectiveMaxBodySize_ZeroDefault(t *testing.T) {
	r := Route{Path: "/", Upstreams: []string{"http://localhost:8080"}}
	if s := r.EffectiveMaxBodySize(0); s != 0 {
		t.Errorf("effective = %d, want 0", s)
	}
}

func TestEffectiveMaxBodySize_RouteOverride(t *testing.T) {
	r := Route{Path: "/", Upstreams: []string{"http://localhost:8080"}, MaxBodySize: 1000}
	if s := r.EffectiveMaxBodySize(500); s != 1000 {
		t.Errorf("effective = %d, want 1000", s)
	}

	r.MaxBodySize = 0
	if s := r.EffectiveMaxBodySize(500); s != 500 {
		t.Errorf("effective = %d, want 500", s)
	}
}

func TestLoad_SetHeadersOnStatic(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","static_dir":"/www","set_headers":{"X-Custom":"value"}}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Routes[0].SetHeaders["X-Custom"] != "value" {
		t.Errorf("set_headers on static = %q, want %q", cfg.Routes[0].SetHeaders["X-Custom"], "value")
	}
}
