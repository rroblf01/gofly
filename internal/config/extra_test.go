package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEffectiveStaticCacheMaxBytes(t *testing.T) {
	r := Route{}
	if r.EffectiveStaticCacheMaxBytes() != DefaultStaticCacheMaxBytes {
		t.Errorf("default max bytes %d want %d", r.EffectiveStaticCacheMaxBytes(), DefaultStaticCacheMaxBytes)
	}
	r.StaticCacheMaxBytes = 1024
	if r.EffectiveStaticCacheMaxBytes() != 1024 {
		t.Errorf("explicit max bytes %d want 1024", r.EffectiveStaticCacheMaxBytes())
	}
}

func TestEffectiveUpstream(t *testing.T) {
	c := Config{}
	u := c.EffectiveUpstream()
	if u.MaxIdleConns != DefaultUpstreamMaxIdleConns || u.MaxIdleConnsPerHost != DefaultUpstreamMaxIdleConnsPerHost || u.BufferSize != DefaultUpstreamBufferSize {
		t.Errorf("defaults mismatch %+v", u)
	}
	c.Upstream = &Upstream{MaxIdleConns: 10, MaxIdleConnsPerHost: 5, BufferSize: 1024}
	u2 := c.EffectiveUpstream()
	if u2.MaxIdleConns != 10 || u2.MaxIdleConnsPerHost != 5 || u2.BufferSize != 1024 {
		t.Errorf("explicit upstream mismatch %+v", u2)
	}
	// zero values fallback to defaults
	c.Upstream = &Upstream{MaxIdleConns: 0, MaxIdleConnsPerHost: 0, BufferSize: 0}
	u3 := c.EffectiveUpstream()
	if u3.MaxIdleConns != DefaultUpstreamMaxIdleConns {
		t.Errorf("zero fallback failed")
	}
}

func TestValidate_Branches(t *testing.T) {
	base := `{"routes":[{"path":"/","static_dir":"/www"}]}`
	cases := []struct {
		name string
		json string
		fail bool
	}{
		{"path empty", `{"routes":[{"path":"","static_dir":"/www"}]}`, true},
		{"gzip_level out of range", `{"routes":[{"path":"/","static_dir":"/www","gzip_level":10}]}`, true},
		{"gzip_min_length negative", `{"routes":[{"path":"/","static_dir":"/www","gzip_min_length":-1}]}`, true},
		{"static_cache_ttl negative", `{"routes":[{"path":"/","static_dir":"/www","static_cache_ttl":"-1s"}]}`, true},
		{"static_cache_max_bytes negative", `{"routes":[{"path":"/","static_dir":"/www","static_cache_max_bytes":-1}]}`, true},
		{"max_procs negative", `{"max_procs":-1,"routes":[{"path":"/","static_dir":"/www"}]}`, true},
		{"max_header_value_count negative", `{"max_header_value_count":-1,"routes":[{"path":"/","static_dir":"/www"}]}`, true},
		{"upstream negative", `{"upstream":{"max_idle_conns":-1},"routes":[{"path":"/","static_dir":"/www"}]}`, true},
		{"upstream per host negative", `{"upstream":{"max_idle_conns_per_host":-1},"routes":[{"path":"/","static_dir":"/www"}]}`, true},
		{"upstream buffer negative", `{"upstream":{"buffer_size":-1},"routes":[{"path":"/","static_dir":"/www"}]}`, true},
		{"health_check_interval non-positive", `{"routes":[{"path":"/","static_dir":"/www","health_check_interval":"0s"}]}`, true},
		{"valid base", base, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cfg.json")
			os.WriteFile(path, []byte(tc.json), 0644)
			_, err := Load(path)
			if tc.fail && err == nil {
				t.Error("expected error but got nil")
			}
			if !tc.fail && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_TLSKeyMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	js := `{"tls":{"enabled":true,"cert_file":"/tmp/cert.pem"},"routes":[{"path":"/","static_dir":"/www"}]}`
	os.WriteFile(path, []byte(js), 0644)
	if _, err := Load(path); err == nil {
		t.Error("expected error for missing key_file")
	}
}

func TestValidate_PortBounds(t *testing.T) {
	for _, port := range []int{0, 70000} {
		c := Config{Port: port, Routes: []Route{{Path: "/", StaticDir: "/www"}}}
		if err := c.validate(); err == nil {
			t.Errorf("port %d should fail", port)
		}
	}
	c := Config{Port: 65535, Routes: []Route{{Path: "/", StaticDir: "/www"}}}
	if err := c.validate(); err != nil {
		t.Errorf("65535 should pass, got %v", err)
	}
}

func TestExpandEnv_RewriteAndHost(t *testing.T) {
	os.Setenv("GOFLY_HOST", "example.com")
	os.Setenv("GOFLY_REWRITE", "/v2")
	defer os.Unsetenv("GOFLY_HOST")
	defer os.Unsetenv("GOFLY_REWRITE")
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	js := `{"routes":[{"path":"/","static_dir":"/www","host":"${GOFLY_HOST}","rewrite":"${GOFLY_REWRITE}"}]}`
	os.WriteFile(path, []byte(js), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routes[0].Host != "example.com" {
		t.Errorf("host %q want example.com", cfg.Routes[0].Host)
	}
	if cfg.Routes[0].Rewrite != "/v2" {
		t.Errorf("rewrite %q want /v2", cfg.Routes[0].Rewrite)
	}
}

func TestLoad_MaxHeaderValueCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	js := `{"max_header_value_count":10,"routes":[{"path":"/","static_dir":"/www"}]}`
	os.WriteFile(path, []byte(js), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxHeaderValueCount != 10 {
		t.Errorf("max_header %d want 10", cfg.MaxHeaderValueCount)
	}
}

func TestLoad_EffectiveMemoryLimitExplicit(t *testing.T) {
	c := Config{MemoryLimit: 50 << 20}
	if c.EffectiveMemoryLimit() != 50<<20 {
		t.Error("explicit memory limit failed")
	}
}

func TestLoad_UpstreamAndMaxProcs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	js := `{"max_procs":2,"upstream":{"max_idle_conns":10,"max_idle_conns_per_host":5,"buffer_size":1024},"routes":[{"path":"/","static_dir":"/www"}]}`
	os.WriteFile(path, []byte(js), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxProcs != 2 {
		t.Errorf("max_procs %d want 2", cfg.MaxProcs)
	}
	if cfg.Upstream.MaxIdleConns != 10 {
		t.Errorf("upstream mismatch")
	}
	_ = time.Now
}
