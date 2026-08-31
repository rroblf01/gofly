package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const DefaultMemoryLimit int64 = 100 << 20 // 100 MB

// Memory-conservative defaults for the shared upstream transport. Tuned for a
// small (~100 MB) memory budget rather than maximum throughput; raise via the
// `upstream` config block on high-traffic deployments.
const (
	DefaultUpstreamMaxIdleConns        = 256
	DefaultUpstreamMaxIdleConnsPerHost = 64
	DefaultUpstreamBufferSize          = 32 << 10 // 32 KiB
)

// DefaultStaticCacheMaxBytes bounds the total bytes held by an in-memory static
// cache when static_cache_max_bytes is unset.
const DefaultStaticCacheMaxBytes int64 = 64 << 20 // 64 MiB

type Config struct {
	Port                int        `json:"port"`
	Workers             int        `json:"workers,omitempty"`
	ReadTimeout         Duration   `json:"read_timeout,omitempty"`
	WriteTimeout        Duration   `json:"write_timeout,omitempty"`
	IdleTimeout         Duration   `json:"idle_timeout,omitempty"`
	MaxBodySize         int64      `json:"max_body_size,omitempty"`
	RateLimit           *RateLimit `json:"rate_limit,omitempty"`
	TLS                 *TLSConfig `json:"tls,omitempty"`
	AccessLog           *bool      `json:"access_log,omitempty"`
	MemoryLimit         int64      `json:"memory_limit,omitempty"`
	GOGC                int        `json:"gogc,omitempty"`
	Metrics             *bool      `json:"metrics,omitempty"`
	TrustForwardedFor   *bool      `json:"trust_forwarded_for,omitempty"`
	MaxProcs            int        `json:"max_procs,omitempty"`
	Upstream            *Upstream  `json:"upstream,omitempty"`
	MaxHeaderValueCount int        `json:"max_header_value_count,omitempty"`
	Routes              []Route    `json:"routes"`
}

// Upstream tunes the shared HTTP transport used for every proxy route. Smaller
// values lower the resident memory of idle connection pools at the cost of less
// connection reuse. All fields are optional and fall back to memory-conservative
// defaults (see DefaultUpstream*).
type Upstream struct {
	MaxIdleConns        int  `json:"max_idle_conns,omitempty"`
	MaxIdleConnsPerHost int  `json:"max_idle_conns_per_host,omitempty"`
	BufferSize          int  `json:"buffer_size,omitempty"`
	DisableHTTP2        bool `json:"disable_http2,omitempty"`
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
	Path                string            `json:"path"`
	Host                string            `json:"host,omitempty"`
	ServerName          string            `json:"server_name,omitempty"`
	Upstreams           []string          `json:"upstreams,omitempty"`
	Strategy            string            `json:"strategy,omitempty"`
	SetHeaders          map[string]string `json:"set_headers,omitempty"`
	RemoveHeaders       []string          `json:"remove_headers,omitempty"`
	StaticDir           string            `json:"static_dir,omitempty"`
	BrowserCacheTTL     *Duration         `json:"browser_cache_ttl,omitempty"`
	Rewrite             string            `json:"rewrite,omitempty"`
	UpstreamTimeout     *Duration         `json:"upstream_timeout,omitempty"`
	RetryOnError        bool              `json:"retry_on_error,omitempty"`
	MaxBodySize         int64             `json:"max_body_size,omitempty"`
	MaxFails            int               `json:"max_fails,omitempty"`
	FailTimeout         *Duration         `json:"fail_timeout,omitempty"`
	HealthCheckPath     string            `json:"health_check_path,omitempty"`
	HealthCheckInterval *Duration         `json:"health_check_interval,omitempty"`
	Gzip                *bool             `json:"gzip,omitempty"`
	GzipLevel           *int              `json:"gzip_level,omitempty"`
	GzipMinLength       int               `json:"gzip_min_length,omitempty"`
	SPA                 bool              `json:"spa,omitempty"`
	AutoIndex           bool              `json:"autoindex,omitempty"`
	ErrorPages          map[int]string    `json:"error_pages,omitempty"`
	SecurityHeaders     *bool             `json:"security_headers,omitempty"`
	StaticCacheTTL      *Duration         `json:"static_cache_ttl,omitempty"`
	StaticCacheMaxBytes int64             `json:"static_cache_max_bytes,omitempty"`
	Precompressed       *bool             `json:"precompressed,omitempty"`
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
		if r.Strategy != "" && r.Strategy != "round_robin" && r.Strategy != "least_conn" {
			return fmt.Errorf("routes[%d]: unsupported strategy %q (want round_robin or least_conn)", i, r.Strategy)
		}
		if r.HealthCheckInterval != nil && r.HealthCheckInterval.Duration <= 0 {
			return fmt.Errorf("routes[%d].health_check_interval must be positive", i)
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
		if r.StaticCacheMaxBytes < 0 {
			return fmt.Errorf("routes[%d].static_cache_max_bytes must be non-negative", i)
		}
	}

	if c.MaxProcs < 0 {
		return fmt.Errorf("max_procs must be non-negative")
	}
	if c.MaxHeaderValueCount < 0 {
		return fmt.Errorf("max_header_value_count must be non-negative")
	}
	if c.Upstream != nil {
		if c.Upstream.MaxIdleConns < 0 {
			return fmt.Errorf("upstream.max_idle_conns must be non-negative")
		}
		if c.Upstream.MaxIdleConnsPerHost < 0 {
			return fmt.Errorf("upstream.max_idle_conns_per_host must be non-negative")
		}
		if c.Upstream.BufferSize < 0 {
			return fmt.Errorf("upstream.buffer_size must be non-negative")
		}
	}

	// Two routes with the same (path, server_name) are ambiguous: the second
	// would be unreachable and, before the host-aware mux, panicked at startup.
	seen := make(map[string]int, len(c.Routes))
	for i, r := range c.Routes {
		key := r.ServerName + "\x00" + r.Path
		if first, dup := seen[key]; dup {
			if r.ServerName == "" {
				return fmt.Errorf("routes[%d]: duplicate path %q (already defined at routes[%d])", i, r.Path, first)
			}
			return fmt.Errorf("routes[%d]: duplicate path %q for server_name %q (already defined at routes[%d])", i, r.Path, r.ServerName, first)
		}
		seen[key] = i
	}
	return nil
}

func (c *Config) AccessLogEnabled() bool {
	return c.AccessLog == nil || *c.AccessLog
}

// MetricsEnabled reports whether the /metrics endpoint and counters are active.
// Defaults to true.
func (c *Config) MetricsEnabled() bool {
	return c.Metrics == nil || *c.Metrics
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

// EffectiveStaticCacheMaxBytes returns the byte budget for this route's
// in-memory static cache, falling back to DefaultStaticCacheMaxBytes.
func (r *Route) EffectiveStaticCacheMaxBytes() int64 {
	if r.StaticCacheMaxBytes > 0 {
		return r.StaticCacheMaxBytes
	}
	return DefaultStaticCacheMaxBytes
}

// EffectiveUpstream resolves the transport tuning knobs, applying
// memory-conservative defaults for any field left at zero.
func (c *Config) EffectiveUpstream() Upstream {
	u := Upstream{}
	if c.Upstream != nil {
		u = *c.Upstream
	}
	if u.MaxIdleConns <= 0 {
		u.MaxIdleConns = DefaultUpstreamMaxIdleConns
	}
	if u.MaxIdleConnsPerHost <= 0 {
		u.MaxIdleConnsPerHost = DefaultUpstreamMaxIdleConnsPerHost
	}
	if u.BufferSize <= 0 {
		u.BufferSize = DefaultUpstreamBufferSize
	}
	return u
}
