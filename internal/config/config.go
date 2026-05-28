package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

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
	Routes       []Route    `json:"routes"`
}

type RateLimit struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	Burst             int     `json:"burst"`
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
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

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
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
	}
	return nil
}

func (c *Config) AccessLogEnabled() bool {
	return c.AccessLog == nil || *c.AccessLog
}

func (r *Route) EffectiveMaxBodySize(defaultSize int64) int64 {
	if r.MaxBodySize > 0 {
		return r.MaxBodySize
	}
	return defaultSize
}
