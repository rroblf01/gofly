package nginx

import (
	"strings"
	"testing"
	"time"
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
	if cfg.Routes[0].ResponseHeaders["X-Frame-Options"] != "SAMEORIGIN" {
		t.Errorf("quoted header not parsed: %v", cfg.Routes[0].ResponseHeaders)
	}
}

func TestConvertAddHeaderGoesToResponseHeaders(t *testing.T) {
	src := `server {
  listen 80;
  location /api/ {
    proxy_pass http://localhost:3000;
    proxy_set_header X-Real-IP $remote_addr;
    add_header X-Frame-Options DENY;
  }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Routes[0]
	if r.SetHeaders["X-Real-IP"] != "$remote_addr" {
		t.Errorf("proxy_set_header should map to request set_headers: %v", r.SetHeaders)
	}
	if _, ok := r.SetHeaders["X-Frame-Options"]; ok {
		t.Errorf("add_header leaked into request set_headers: %v", r.SetHeaders)
	}
	if r.ResponseHeaders["X-Frame-Options"] != "DENY" {
		t.Errorf("add_header should map to response_headers: %v", r.ResponseHeaders)
	}
}

func TestConvertLocationAddHeaderDropsInherited(t *testing.T) {
	src := `server {
  listen 80;
  add_header X-Server one;
  location /a/ { root /www; add_header X-Location two; }
  location /b/ { root /www; }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(cfg.Routes))
	}
	// nginx: a location add_header suppresses the server-level ones entirely.
	if _, ok := cfg.Routes[0].ResponseHeaders["X-Server"]; ok {
		t.Errorf("location add_header should drop inherited X-Server: %v", cfg.Routes[0].ResponseHeaders)
	}
	if cfg.Routes[0].ResponseHeaders["X-Location"] != "two" {
		t.Errorf("location add_header missing: %v", cfg.Routes[0].ResponseHeaders)
	}
	// A location without add_header still inherits the server ones.
	if cfg.Routes[1].ResponseHeaders["X-Server"] != "one" {
		t.Errorf("inherited server add_header missing: %v", cfg.Routes[1].ResponseHeaders)
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

func TestConvertRootVsAlias(t *testing.T) {
	src := `server {
  listen 80;
  location /sub/ { root /www; }
  location /files/ { alias /var/www/files/; }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(cfg.Routes))
	}
	// nginx `root` appends the location path: /sub/ maps to /www/sub/...
	if got := cfg.Routes[0].StaticDir; got != "/www/sub" {
		t.Errorf("root location static_dir = %q, want /www/sub", got)
	}
	// `alias` replaces the matched prefix, so it is used verbatim.
	if got := cfg.Routes[1].StaticDir; got != "/var/www/files/" {
		t.Errorf("alias location static_dir = %q, want /var/www/files/", got)
	}
}

func TestConvertInheritedRootAppendsLocationPath(t *testing.T) {
	src := `server {
  listen 80;
  root /var/www;
  location /assets/ { expires 1h; }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(cfg.Routes))
	}
	if got := cfg.Routes[0].StaticDir; got != "/var/www/assets" {
		t.Errorf("inherited root static_dir = %q, want /var/www/assets", got)
	}
}

func TestConvertSendTimeout(t *testing.T) {
	src := `http {
  send_timeout 15s;
  server {
    listen 80;
    send_timeout 30s;
    location /a/ { root /www; }
    location /b/ { root /www; send_timeout 5s; }
  }
}`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SendTimeout == nil || cfg.SendTimeout.Duration != 15*time.Second {
		t.Errorf("http send_timeout = %v, want 15s", cfg.SendTimeout)
	}
	if len(cfg.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(cfg.Routes))
	}
	if r := cfg.Routes[0].SendTimeout; r == nil || r.Duration != 30*time.Second {
		t.Errorf("server-level send_timeout = %v, want 30s", r)
	}
	if r := cfg.Routes[1].SendTimeout; r == nil || r.Duration != 5*time.Second {
		t.Errorf("location send_timeout = %v, want 5s", r)
	}
}

func TestConvertServerRootOnly(t *testing.T) {
	src := `server { listen 80; root /srv/www; }`
	cfg, _, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].StaticDir != "/srv/www" {
		t.Fatalf("server-root-only route = %+v", cfg.Routes)
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
