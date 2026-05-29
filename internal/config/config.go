package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const DefaultMemoryLimit int64 = 100 << 20 // 100 MB

type Config struct {
	Port         int        `json:"port"`
	Workers      int        `json:"workers,omitempty"`
	ReadTimeout  Duration   `json:"read_timeout,omitempty"`
	WriteTimeout Duration   `json:"write_timeout,omitempty"`
	IdleTimeout  Duration   `json:"idle_timeout,omitempty"`
	MaxBodySize  int64      `json:"max_body_size,omitempty"`
	RateLimit    *RateLimit `json:"rate_limit,omitempty"`
	TLS          *TLSConfig `json:"tls,omitempty"`
	AccessLog    *bool      `json:"access_log,omitempty"`
	MemoryLimit  int64      `json:"memory_limit,omitempty"`
	GOGC         int        `json:"gogc,omitempty"`
	Routes       []Route    `json:"routes"`
}

type RateLimit struct {
	RequestsPerSecond float64   `json:"requests_per_second"`
	Burst             int       `json:"burst"`
	IdleTTL           *Duration `json:"idle_ttl,omitempty"`
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	TLSPort  int    `json:"tls_port,omitempty"`
}

type Route struct {
	Path            string            `json:"path"`
	Host            string            `json:"host,omitempty"`
	ServerName      string            `json:"server_name,omitempty"`
	Upstreams       []string          `json:"upstreams,omitempty"`
	Strategy        string            `json:"strategy,omitempty"`
	SetHeaders      map[string]string `json:"set_headers,omitempty"`
	RemoveHeaders   []string          `json:"remove_headers,omitempty"`
	StaticDir       string            `json:"static_dir,omitempty"`
	BrowserCacheTTL *Duration         `json:"browser_cache_ttl,omitempty"`
	Rewrite         string            `json:"rewrite,omitempty"`
	UpstreamTimeout *Duration         `json:"upstream_timeout,omitempty"`
	RetryOnError    bool              `json:"retry_on_error,omitempty"`
	MaxBodySize     int64             `json:"max_body_size,omitempty"`
	MaxFails        int               `json:"max_fails,omitempty"`
	FailTimeout     *Duration         `json:"fail_timeout,omitempty"`
	Gzip            *bool             `json:"gzip,omitempty"`
	GzipLevel       *int              `json:"gzip_level,omitempty"`
	GzipMinLength   int               `json:"gzip_min_length,omitempty"`
	SPA             bool              `json:"spa,omitempty"`
	AutoIndex       bool              `json:"autoindex,omitempty"`
	ErrorPages      map[int]string    `json:"error_pages,omitempty"`
	SecurityHeaders *bool             `json:"security_headers,omitempty"`
	StaticCacheTTL  *Duration         `json:"static_cache_ttl,omitempty"`
	Precompressed   *bool             `json:"precompressed,omitempty"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func Default() Config {
	return Config{
		Port:         80,
		ReadTimeout:  Duration{30 * time.Second},
		WriteTimeout: Duration{30 * time.Second},
		IdleTimeout:  Duration{120 * time.Second},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	cfg.expandEnv()

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c *Config) expandEnv() {
	for i := range c.Routes {
		c.Routes[i].Path = os.Expand(c.Routes[i].Path, os.Getenv)
		c.Routes[i].Host = os.Expand(c.Routes[i].Host, os.Getenv)
		c.Routes[i].ServerName = os.Expand(c.Routes[i].ServerName, os.Getenv)
		c.Routes[i].StaticDir = os.Expand(c.Routes[i].StaticDir, os.Getenv)
		c.Routes[i].Rewrite = os.Expand(c.Routes[i].Rewrite, os.Getenv)
		if c.Routes[i].SetHeaders != nil {
			expanded := make(map[string]string, len(c.Routes[i].SetHeaders))
			for k, v := range c.Routes[i].SetHeaders {
				expanded[os.Expand(k, os.Getenv)] = os.Expand(v, os.Getenv)
			}
			c.Routes[i].SetHeaders = expanded
		}
		if c.Routes[i].ErrorPages != nil {
			expanded := make(map[int]string, len(c.Routes[i].ErrorPages))
			for k, v := range c.Routes[i].ErrorPages {
				expanded[k] = os.Expand(v, os.Getenv)
			}
			c.Routes[i].ErrorPages = expanded
		}
	}
	if c.TLS != nil {
		c.TLS.CertFile = os.Expand(c.TLS.CertFile, os.Getenv)
		c.TLS.KeyFile = os.Expand(c.TLS.KeyFile, os.Getenv)
	}
}

func (c *Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range [1-65535]", c.Port)
	}
	if c.TLS != nil && c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("tls.cert_file is required when tls is enabled")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.key_file is required when tls is enabled")
		}
	}
	if c.RateLimit != nil {
		if c.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("rate_limit.requests_per_second must be positive")
		}
		if c.RateLimit.Burst < 1 {
			return fmt.Errorf("rate_limit.burst must be at least 1")
		}
	}
	for i, r := range c.Routes {
		if r.Path == "" {
			return fmt.Errorf("routes[%d].path is required", i)
		}
		if r.StaticDir != "" && len(r.Upstreams) > 0 {
			return fmt.Errorf("routes[%d]: static_dir and upstreams are mutually exclusive", i)
		}
		if r.StaticDir == "" && len(r.Upstreams) == 0 {
			return fmt.Errorf("routes[%d]: static_dir or upstreams required", i)
		}
		if r.Strategy != "" && r.Strategy != "round_robin" {
			return fmt.Errorf("routes[%d]: unsupported strategy %q", i, r.Strategy)
		}
		if r.MaxFails < 0 {
			return fmt.Errorf("routes[%d].max_fails must be non-negative", i)
		}
		if r.GzipLevel != nil && (*r.GzipLevel < -2 || *r.GzipLevel > 9) {
			return fmt.Errorf("routes[%d].gzip_level %d out of range [-2,9]", i, *r.GzipLevel)
		}
		if r.GzipMinLength < 0 {
			return fmt.Errorf("routes[%d].gzip_min_length must be non-negative", i)
		}
		if r.StaticCacheTTL != nil && r.StaticCacheTTL.Duration < 0 {
			return fmt.Errorf("routes[%d].static_cache_ttl must be non-negative", i)
		}
	}
	return nil
}

func (c *Config) AccessLogEnabled() bool {
	return c.AccessLog == nil || *c.AccessLog
}

func (c *Config) EffectiveMemoryLimit() int64 {
	if c.MemoryLimit > 0 {
		return c.MemoryLimit
	}
	return DefaultMemoryLimit
}

func (c *Config) SecurityHeadersEnabled() bool {
	for _, r := range c.Routes {
		if r.SecurityHeaders == nil || *r.SecurityHeaders {
			return true
		}
	}
	return false
}

func (r *Route) EffectiveMaxBodySize(defaultSize int64) int64 {
	if r.MaxBodySize > 0 {
		return r.MaxBodySize
	}
	return defaultSize
}

func (r *Route) SecurityHeadersDefault() bool {
	return r.SecurityHeaders == nil || *r.SecurityHeaders
}

// PrecompressedEnabled reports whether the handler should probe for a sibling
// .gz file. Defaults to true to preserve historical behaviour; set it to false
// to skip the extra stat syscall on routes that never ship pre-compressed
// assets.
func (r *Route) PrecompressedEnabled() bool {
	return r.Precompressed == nil || *r.Precompressed
}

// GzipCompressionLevel returns the configured gzip level, or -1
// (gzip.DefaultCompression) when unset.
func (r *Route) GzipCompressionLevel() int {
	if r.GzipLevel != nil {
		return *r.GzipLevel
	}
	return -1
}
