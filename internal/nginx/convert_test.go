package nginx

import (
	"strings"
	"testing"
)

func TestConvertStaticSPA(t *testing.T) {
	src := `
http {
  gzip on;
  gzip_min_length 1024;
  server {
    listen 80;
    server_name example.com;
    root /var/www;
    location / {
      try_files $uri $uri/ /index.html;
      expires 6M;
    }
  }
}`
	cfg, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 80 {
		t.Errorf("port = %d, want 80", cfg.Port)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(cfg.Routes))
	}
	r := cfg.Routes[0]
	if r.Path != "/" || r.StaticDir != "/var/www" || r.ServerName != "example.com" {
		t.Errorf("route mismatch: %+v", r)
	}
	if !r.SPA {
		t.Error("expected spa=true from try_files")
	}
	if r.Gzip == nil || !*r.Gzip {
		t.Error("expected gzip=true from http-level default")
	}
	if r.GzipMinLength != 1024 {
		t.Errorf("gzip_min_length = %d, want 1024", r.GzipMinLength)
	}
	// expires 6M -> 6*30*86400 = 15552000s
	if r.BrowserCacheTTL == nil || int64(r.BrowserCacheTTL.Seconds()) != 15552000 {
		t.Errorf("browser_cache_ttl = %v, want 15552000s", r.BrowserCacheTTL)
	}
	_ = warns
}

func TestConvertProxyUpstreamLeastConn(t *testing.T) {
	src := `
http {
  upstream api {
    least_conn;
    server 10.0.0.1:3001;
    server 10.0.0.2:3002;
  }
  server {
    listen 80;
    location /api/ {
      proxy_pass http://api;
      proxy_set_header X-Real-IP $remote_addr;
    }
  }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(cfg.Routes))
	}
	r := cfg.Routes[0]
	if r.Path != "/api/" {
		t.Errorf("path = %q, want /api/", r.Path)
	}
	if len(r.Upstreams) != 2 || r.Upstreams[0] != "http://10.0.0.1:3001" {
		t.Errorf("upstreams = %v", r.Upstreams)
	}
	if r.Strategy != "least_conn" {
		t.Errorf("strategy = %q, want least_conn", r.Strategy)
	}
	if r.SetHeaders["X-Real-IP"] != "$remote_addr" {
		t.Errorf("missing proxy_set_header mapping: %v", r.SetHeaders)
	}
}

func TestConvertSingleProxyPassNoUpstreamBlock(t *testing.T) {
	src := `server {
  listen 8080;
  location / { proxy_pass http://localhost:9000; }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Errorf("port = %d, want 8080", cfg.Port)
	}
	r := cfg.Routes[0]
	if len(r.Upstreams) != 1 || r.Upstreams[0] != "http://localhost:9000" {
		t.Errorf("upstreams = %v", r.Upstreams)
	}
	if r.Strategy != "" {
		t.Errorf("strategy = %q, want empty for single upstream", r.Strategy)
	}
}

func TestConvertTLS(t *testing.T) {
	src := `server {
  listen 443 ssl;
  ssl_certificate /etc/ssl/cert.pem;
  ssl_certificate_key /etc/ssl/key.pem;
  root /www;
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS == nil || !cfg.TLS.Enabled {
		t.Fatal("expected TLS enabled")
	}
	if cfg.TLS.CertFile != "/etc/ssl/cert.pem" || cfg.TLS.KeyFile != "/etc/ssl/key.pem" {
		t.Errorf("tls files: %+v", cfg.TLS)
	}
	if cfg.TLS.TLSPort != 443 {
		t.Errorf("tls_port = %d, want 443", cfg.TLS.TLSPort)
	}
	// server with root and no location -> a "/" route
	if len(cfg.Routes) != 1 || cfg.Routes[0].Path != "/" {
		t.Errorf("expected one / route, got %+v", cfg.Routes)
	}
}

func TestConvertRegexLocationWarns(t *testing.T) {
	src := `server {
  listen 80;
  root /www;
  location ~* \.(css|js)$ { expires 1y; }
  location / { try_files $uri /index.html; }
}`
	cfg, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	// only the "/" route survives; regex location is dropped
	if len(cfg.Routes) != 1 || cfg.Routes[0].Path != "/" {
		t.Errorf("expected only / route, got %+v", cfg.Routes)
	}
	if !hasWarning(warns, "regex location") {
		t.Errorf("expected a regex-location warning, got %v", warns)
	}
}

func TestConvertUnknownDirectiveWarns(t *testing.T) {
	src := `server {
  listen 80;
  root /www;
  limit_req_zone $binary_remote_addr zone=one:10m rate=1r/s;
}`
	_, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "limit_req_zone") {
		t.Errorf("expected warning for limit_req_zone, got %v", warns)
	}
}

func TestConvertListenWithAddress(t *testing.T) {
	src := `server { listen 0.0.0.0:8080; root /www; }`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Errorf("port = %d, want 8080", cfg.Port)
	}
}

func TestConvertCommentsAndQuotes(t *testing.T) {
	src := `
# a comment
server {
  listen 80;       # inline comment
  root /www;
  add_header X-Frame-Options "SAMEORIGIN";
  location / { try_files $uri /index.html; }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routes[0].SetHeaders["X-Frame-Options"] != "SAMEORIGIN" {
		t.Errorf("quoted header not parsed: %v", cfg.Routes[0].SetHeaders)
	}
}

func TestConvertParseErrorUnterminated(t *testing.T) {
	if _, _, err := Convert(strings.NewReader(`server { listen 80;`)); err == nil {
		t.Error("expected error for missing closing brace")
	}
}

func TestNginxTimeSeconds(t *testing.T) {
	cases := map[string]int64{
		"30s": 30, "5m": 300, "1h": 3600, "1d": 86400,
		"1w": 604800, "6M": 15552000, "1y": 31536000, "120": 120,
	}
	for in, want := range cases {
		got, ok := nginxTimeSeconds(in)
		if !ok || got != want {
			t.Errorf("nginxTimeSeconds(%q) = %d,%v; want %d", in, got, ok, want)
		}
	}
	if _, ok := nginxTimeSeconds("bogus"); ok {
		t.Error("expected bogus time to fail")
	}
}

func hasWarning(warns []string, substr string) bool {
	for _, w := range warns {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
