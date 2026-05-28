# gofly

A fast, lightweight HTTP reverse proxy and static file server written in Go. Designed as a minimal alternative to nginx with **zero external dependencies**, a tiny memory footprint, and a Docker image built on `scratch` (~7 MB).

## Features

- **Reverse proxy** with round-robin load balancing
- **WebSocket support** — transparent WebSocket proxying
- **Passive health checks** — auto-disable failing upstreams, re-enable after cooldown
- **Retry on failure** — failover to next upstream on connection errors
- **Upstream timeouts** — configurable `upstream_timeout` per route
- **Path rewriting** — rewrite request paths before forwarding to upstreams
- **Host-based routing** — route by `server_name` (virtual hosts)
- **Request header manipulation** — set/remove headers, variable expansion (`$remote_addr`, `$host`, `$scheme`, `$uri`)
- **Rate limiting** — token bucket per IP, configurable via `rate_limit`
- **Body size limit** — enforce `max_body_size` globally or per route
- **Gzip compression** — per-route configurable via `gzip`
- **TLS/HTTPS** — manual certificate support
- **Graceful shutdown** — on SIGINT/SIGTERM with 30s timeout
- **JSON structured logging** — via `log/slog`
- **Pure stdlib** — zero external dependencies
- **Multi-architecture** builds (amd64, arm64)
- **Scratch-based Docker image** (~7 MB)

## Quick start

### Using Docker

```bash
# Build
make docker

# Run
docker run -d --name gofly -p 80:80 gofly:latest

# With custom config and static files
docker run -d --name gofly \
  -p 80:80 \
  -v /path/to/config.json:/etc/gofly/config.json:ro \
  -v /var/www/html:/var/www/html:ro \
  gofly:latest
```

### Building from source

```bash
# Build
make build

# Run with default config
make run

# With a custom config file
./build/gofly -config /path/to/config.json

# Enable debug logging
./build/gofly -config config.json -debug

# Override port
./build/gofly -port 8080
```

### Testing

```bash
# Run all tests
make test

# With race detection
make test-race

# With coverage
make test-cover

# Benchmarks
make bench

# Lint
make lint
```

## Configuration

gofly uses a single JSON config file. Default path: `/etc/gofly/config.json` (override with `-config` flag or `GOFLY_CONFIG` env var).

### Full example

```json
{
  "port": 80,
  "read_timeout": "30s",
  "write_timeout": "30s",
  "idle_timeout": "120s",
  "max_body_size": 1048576,
  "rate_limit": {
    "requests_per_second": 100,
    "burst": 200
  },
  "routes": [
    {
      "path": "/api/",
      "server_name": "api.example.com",
      "upstreams": ["http://localhost:3001", "http://localhost:3002"],
      "strategy": "round_robin",
      "set_headers": {
        "X-Forwarded-For": "$remote_addr",
        "X-Real-IP": "$remote_addr"
      },
      "remove_headers": ["X-Internal"],
      "host": "backend.internal",
      "rewrite": "/v2$uri",
      "retry_on_error": true,
      "max_fails": 5,
      "fail_timeout": "30s",
      "upstream_timeout": "10s"
    },
    {
      "path": "/",
      "static_dir": "/var/www/html",
      "browser_cache_ttl": "3600s",
      "gzip": true
    }
  ],
  "tls": {
    "enabled": false,
    "cert_file": "/etc/gofly/cert.pem",
    "key_file": "/etc/gofly/key.pem"
  }
}
```

### Reference

#### Global config

| Field | Type | Default | Description |
|---|---|---|---|
| `port` | int | `80` | HTTP listen port |
| `read_timeout` | string | `"30s"` | Max duration for reading request |
| `write_timeout` | string | `"30s"` | Max duration for writing response |
| `idle_timeout` | string | `"120s"` | Max duration for idle connections |
| `max_body_size` | int | `0` (unlimited) | Max request body size in bytes |
| `rate_limit` | object | — | Global rate limiting config |
| `rate_limit.requests_per_second` | float | — | Requests per second per IP |
| `rate_limit.burst` | int | — | Burst capacity |
| `tls` | object | — | TLS configuration |

#### Route config

| Field | Type | Default | Description |
|---|---|---|---|
| `path` | string | — | URL path pattern (e.g. `/api/`, `/`) |
| `server_name` | string | — | Match requests by Host header (virtual host) |
| `upstreams` | []string | — | Backend URLs for reverse proxy |
| `strategy` | string | `"round_robin"` | Load balancing strategy |
| `static_dir` | string | — | Directory to serve static files from |
| `browser_cache_ttl` | string | — | Cache-Control max-age (e.g. `"3600s"`) |
| `gzip` | bool | `false` | Enable gzip compression |
| `set_headers` | object | — | Headers to inject into upstream requests (supports `$remote_addr`, `$host`, `$scheme`, `$uri`, `$request_uri`) |
| `remove_headers` | []string | — | Headers to strip from upstream requests |
| `host` | string | — | Override the Host header |
| `rewrite` | string | — | Rewrite request path before forwarding |
| `retry_on_error` | bool | `false` | Retry with next upstream on connection error |
| `max_fails` | int | `1` | Number of failures before upstream is disabled |
| `fail_timeout` | string | `"30s"` | Time until a disabled upstream is re-enabled |
| `upstream_timeout` | string | — | Response header timeout for upstream connections |
| `max_body_size` | int | — | Per-route override for max body size |

### Variable substitution

The following variables are expanded in `set_headers`, `host`, and `rewrite`:

| Variable | Description |
|---|---|
| `$remote_addr` | Client IP address |
| `$host` | Request Host header |
| `$scheme` | `http` or `https` |
| `$uri` | Request URI path |
| `$request_uri` | Full request URI (path + query) |

## API

### Health check

```
GET /health
```

Returns `{"status":"ok"}` with HTTP 200.

## Docker

### Build the image

```bash
make docker
```

### Multi-stage build

The Dockerfile uses:
1. `golang:1.26-alpine` — builder stage (CA certs, compilation)
2. `scratch` — final stage (~7 MB image)

Supports multi-arch builds:
```bash
docker buildx build --platform linux/amd64,linux/arm64 -t gofly:latest .
```

### Docker Compose

```bash
# Start gofly
docker compose up -d

# Start with example backend
docker compose --profile example up -d
```

## Performance

### Benchmark results (AMD Ryzen 5 3600, Go 1.26)

#### Load test (wrk — 100 concurrent connections, 4 threads)

```
Running 10s test @ http://127.0.0.1:9999/index.html
  Latency:   1.25ms avg  (max 41.31ms, 87.78% < 1.25ms)
  Req/Sec:   25.78k avg  (max 28.04k)
  1,026,341 requests in 10.02s
Requests/sec: 102,438
Transfer/sec:  10.16 MB
```

#### Memory usage

| State | Resident (RSS) |
|---|---|
| Idle (server loaded, no traffic) | ~7 MB |
| After 1000 requests | ~12 MB |
| After sustained load | ~12 MB |

#### Go benchmarks

| Benchmark | Ops | Latency | Allocs/op | Bytes/op |
|---|---|---|---|---|
| Reverse proxy (single upstream) | 14,431 | 81.4 µs | 167 | 54,839 |
| Reverse proxy (3 upstreams, round-robin) | 14,899 | 85.2 µs | 166 | 54,703 |
| Static file (small, 13 B) | 36,088 | 33.6 µs | 109 | 14,596 |
| Static file (large, 256 KB) | 20,676 | 52.5 µs | 110 | 17,051 |
| Proxy throughput (sequential) | 7,226 | 138.5 µs | 150 | 46,108 |
| Real-world page (HTML+CSS+JS) | 7,430 | 156.5 µs | 166 | 15,398 |
| **Heap alloc per request** | **7,467** | **143.1 µs** | **13.71 B/req** | **46,174** |

> Note: Parallel benchmarks use `RunParallel` and auto-scale to GOMAXPROCS (12 cores).

### Key design decisions

- **Connection pooling**: `http.Transport` shared across all proxies (100 max idle, 10 per host)
- **Pre-built ReverseProxy**: created once in `New()`, not per-request (zero allocation on hot path)
- **Atomic round-robin**: `atomic.Uint64` — no locks on the hot path
- **Nil-safe logger**: no panic even if logger is not initialized
- **Full GC control**: tune with `GOGC` and `GOMEMLIMIT` env vars

## Architecture

```
                     ┌──────────────┐
                     │  config.json  │
                     └──────┬───────┘
                            │
                    ┌───────▼────────┐
                    │  config.Load() │
                    └───────┬────────┘
                            │
              ┌─────────────▼──────────────┐
              │         Server             │
              │  ┌──────────────────────┐  │
              │  │  middleware stack    │  │
              │  │  ┌──────────────┐    │  │
              │  │  │ rate limit   │    │  │
              │  │  ├──────────────┤    │  │
              │  │  │ access log   │    │  │
              │  │  └──────┬───────┘    │  │
              │  │         │            │  │
              │  │  ┌──────▼───────┐    │  │
              │  │  │ ServeMux     │    │  │
              │  │  └──┬───┬───┬──┘    │  │
              │  │     │   │   │       │  │
              │  │ ┌───▼┐ ┌▼──┐ ┌▼──┐ │  │
              │  │ │px │ │st │ │...│ │  │
              │  │ └───┘ └───┘ └───┘ │  │
              │  └────────────────────┘  │
              └──────────────────────────┘
```

## License

MIT
