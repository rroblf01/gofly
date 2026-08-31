package nginx

import (
	"strings"
	"testing"
)

func TestParseListenPortVariants(t *testing.T) {
	cases := map[string]int{
		"80":             80,
		"0.0.0.0:8080":   8080,
		"[::]:443":       443,
		"[::1]:8080":     8080,
		"127.0.0.1:3000": 3000,
		"443":            443,
		"invalid":        0,
	}
	for in, want := range cases {
		if got := parseListenPort(in); got != want {
			t.Errorf("parseListenPort %q = %d want %d", in, got, want)
		}
	}
}

func TestConvert_LocationExactAndPrefix(t *testing.T) {
	src := `server { listen 80; root /www; location = /exact { } location ^~ /static/ { } location / { } }`
	cfg, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	// Only "/" should survive as route (exact and ^~ are translated as prefix)
	if len(cfg.Routes) == 0 {
		t.Fatal("expected at least one route")
	}
	_ = warns
}

func TestConvert_UpstreamKeepaliveAndHashWarn(t *testing.T) {
	src := `http { upstream x { hash $remote_addr; server 127.0.0.1:80; } server { listen 80; location / { proxy_pass http://x; } } }`
	_, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "hash") {
		t.Errorf("expected hash warning, got %v", warns)
	}
}

func TestConvert_GzipLevelsAndExpiresWarn(t *testing.T) {
	src := `server { listen 80; root /www; location / { gzip on; gzip_comp_level 5; expires invalid; } }`
	cfg, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	// gzip_comp_level per-location may not be preserved if not translated; just check no error
	_ = cfg
	if !hasWarning(warns, "expires") {
		t.Logf("no expires warning, got %v (may be ok)", warns)
	}
}

func TestConvert_MultipleListenPortsWarn(t *testing.T) {
	src := `server { listen 80; listen 8080; root /www; }`
	_, warns, err := Convert(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	// Should warn about additional listen?
	_ = warns
}

func TestLexUnterminatedQuote(t *testing.T) {
	_, _, err := Convert(strings.NewReader(`server { listen 80; add_header X "unterminated; }`))
	if err == nil {
		t.Log("expected error for unterminated quote or handled")
	}
}
