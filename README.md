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
- **JSON structured logging** — via `log/slog`, async with channel buffer (non-blocking)
- **SO_REUSEPORT** — multiple accept goroutines on Linux, configurable via `workers`
- **sendfile(2)** — kernel zero-copy static file serving (via `io.ReaderFrom`)
- **Pre-compressed static files** — serve `.gz` files when client accepts gzip (gzip_static)
- **Pooled buffers** — `sync.Pool` for sniff buffer and response writers
- **Pure stdlib** — zero external dependencies
- **Multi-architecture** builds (amd64, arm64)
- **Scratch-based Docker image** (~7 MB)

## Quick start

### Using pre-built images (ghcr.io)

Pull the latest image from GitHub Container Registry:

```bash
docker pull ghcr.io/rroblf01/gofly:latest
```

Serve a static site with zero configuration:

```bash
docker run -d --name gofly \
  -p 80:80 \
  -v /path/to/your/site:/www:ro \
  ghcr.io/rroblf01/gofly:latest
```

With a custom config file:

```bash
docker run -d --name gofly \
  -p 80:80 \
  -p 443:443 \
  -v /path/to/config.json:/etc/gofly/config.json:ro \
  -v /path/to/your/site:/www:ro \
  ghcr.io/rroblf01/gofly:latest
```

With environment variable expansion in config:

```bash
docker run -d --name gofly \
  -p 80:80 \
  -e SITE_ROOT=/www \
  -v /path/to/config.json:/etc/gofly/config.json:ro \
  -v /path/to/your/site:/www:ro \
  ghcr.io/rroblf01/gofly:latest
```

### Building locally

```bash
make docker

# Run
docker run -d --name gofly -p 80:80 -v ./www:/www:ro gofly:latest
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
  "workers": 1,
  "access_log": true,
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
|---|---|---|---|---|
| `port` | int | `80` | HTTP listen port |
| `workers` | int | `1` | Number of SO_REUSEPORT accept goroutines (Linux only) |
| `access_log` | bool | `true` | Enable/disable access log (disable for maximum throughput) |
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

### Image registry

Pre-built images are published to **GitHub Container Registry** on every release:

```
ghcr.io/rroblf01/gofly:latest
ghcr.io/rroblf01/gofly:<version>    (e.g. v0.1.0)
ghcr.io/rroblf01/gofly:<major>     (e.g. 0)
ghcr.io/rroblf01/gofly:<major>.<minor> (e.g. 0.1)
```

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

### Usage examples

**Configless mode** — serve a static site, no config file needed:

```bash
docker run -d --name gofly \
  -p 80:80 \
  -v /path/to/site:/www:ro \
  ghcr.io/rroblf01/gofly:latest
```

**With config file** — proxy, TLS, rate limiting, etc.:

```bash
docker run -d --name gofly \
  -p 80:80 \
  -p 443:443 \
  -v /path/to/config.json:/etc/gofly/config.json:ro \
  -v /path/to/site:/www:ro \
  ghcr.io/rroblf01/gofly:latest
```

**Configless + custom port**:

```bash
docker run -d --name gofly \
  -p 8080:8080 \
  -v /path/to/site:/www:ro \
  ghcr.io/rroblf01/gofly:latest -root /www -port 8080
```

**Production — GOGC=off with built-in memory limit** (100 MB default):

```bash
docker run -d --name gofly \
  -p 80:80 \
  -v /path/to/site:/www:ro \
  -e GOGC=off \
  ghcr.io/rroblf01/gofly:latest
```

### Memory management

gofly sets a **soft memory limit** of **100 MB** by default (via `debug.SetMemoryLimit`). The Go runtime triggers GC when the heap approaches this limit, keeping RSS bounded even under high load.

Override in config:

```json
{
  "memory_limit": 268435456
}
```

Or via environment variable (takes precedence over the config default):

```bash
docker run -e GOMEMLIMIT=200MiB -e GOGC=off ...
```

### Docker Compose

```bash
# Start gofly
docker compose up -d

# Start with example backend
docker compose --profile example up -d
```

## Performance

### Load test (wrk — 100 concurrent connections, 4 threads, 15s)

Same static page (`www/index.html`, ~800 B), same hardware (AMD Ryzen 5 3600, 12 cores).

| Config | Requests/s | Latency (avg) | Memory (RSS) |
|---|---|---|---|
| gofly (1 worker, access_log off) | 108,566 | 1.12 ms | ~21 MB |
| gofly (SO_REUSEPORT ×12, access_log off) | 114,185 | 1.07 ms | ~21 MB |
| gofly (SO_REUSEPORT ×12, access_log off, GOGC=off, GOMEMLIMIT=200MiB) | **127,051** | **0.88 ms** | **~190 MB** |
| **nginx alpine** (`worker_processes auto`, access_log off) | **136,930** | **0.85 ms** | **~10 MB** |

gofly reaches **93% of nginx throughput** with **zero external dependencies**, a **5× smaller Docker image**, and **memory-safe Go**.

### Comparison with nginx

| Metric | gofly (scratch) | nginx (alpine) | Difference |
|---|---|---|---|
| **Requests/sec** | 127,051 | 136,930 | -7% |
| **Latency (avg)** | 0.88 ms | 0.85 ms | +3% |
| **Image size** | **~7 MB** | ~35 MB | **5× smaller** 🏆 |
| **Dependencies** | **0** (pure stdlib) | libc, PCRE, zlib, OpenSSL | **—** 🏆 |
| **Static binary** | ✅ Yes | ❌ No | **—** 🏆 |
| **Memory safety** | ✅ Yes (Go) | ❌ No (C) | **—** 🏆 |
| **Configuration** | **JSON (simple)** | nginx.conf | **—** 🏆 |

> gofly delivers **93% of nginx throughput** while being **5× smaller**, **zero dependencies**, and **memory-safe by construction**.

### Memory usage

| State | gofly (default GC) | gofly (GOGC=off, GOMEMLIMIT=200MiB) |
|---|---|---|
| Idle (no traffic) | ~10 MB | ~10 MB |
| After sustained load | ~21 MB | ~190 MB |

> `GOMEMLIMIT` caps Go's heap and triggers GC only when needed. Without it, `GOGC=off` lets the heap grow unbounded.

### Architecture and optimizations

gofly achieves its performance through:

- **sendfile(2) zero-copy** — `w.(io.ReaderFrom).ReadFrom(f)` delegates directly to Go's `net.TCPConn.ReadFrom`, which uses `sendfile(2)` on Linux when the source is a regular file. No userspace buffer copy.
- **SO_REUSEPORT** — multiple goroutines accept on separate sockets, kernel distributes connections across them. Configurable via `workers`.
- **Async logging** — log entries go to a buffered channel (16384 capacity), background goroutine drains it. `access_log: false` bypasses the wrapper entirely (zero cost).
- **Pooled response writer** — `sync.Pool` reuses `responseWriter` structs across requests.
- **Sniff buffer pool** — 512-byte `sync.Pool` for `http.DetectContentType` fallback.
- **Direct header map** — `h["Content-Type"] = []string{ctype}` avoids `Header().Set()` overhead.
- **Pre-compiled Cache-Control** — string concatenation at init time, no `fmt.Sprintf` on hot path.
- **Pre-built ReverseProxy** — created once in `New()`, not per-request.
- **Atomic round-robin** — `atomic.Uint64`, no locks.
- **Tuned TCP** — `TCP_DEFER_ACCEPT` + `TCP_QUICKACK` on listener socket (Linux).
- **GC control** — pair `GOGC=off` with `GOMEMLIMIT` for near-zero GC overhead with bounded memory.

### Production tuning

gofly sets a **soft memory limit of 100 MB** by default. Override via `"memory_limit"` in config (bytes) or `GOMEMLIMIT` env var.

For maximum throughput:

```bash
GOGC=off gofly -config config.json
```

- `GOGC=off` — disables GC triggering based on heap growth (memory stays bounded by `debug.SetMemoryLimit`).
- Set `"workers": N` (matching CPU count) for SO_REUSEPORT accept distribution.
- Set `"access_log": false` to bypass the logging wrapper entirely.

## GitHub Actions

On every release (`v*.*.*`), a workflow publishes multi-arch Docker images to `ghcr.io/rroblf01/gofly`:

| Tag | Example |
|---|---|
| `latest` | `ghcr.io/rroblf01/gofly:latest` |
| semver full | `ghcr.io/rroblf01/gofly:v0.1.0` |
| semver minor | `ghcr.io/rroblf01/gofly:0.1` |
| semver major | `ghcr.io/rroblf01/gofly:0` |

Multi-arch: `linux/amd64`, `linux/arm64`.

To trigger a release:

```bash
git tag v0.1.0
git push origin v0.1.0
# Create the release on GitHub → workflow publishes the image
```

### Go benchmarks (internal, per-core)

| Benchmark | Ops | Latency | Allocs/op | Bytes/op |
|---|---|---|---|---|---|
| Reverse proxy (single upstream) | 14,911 | 81.6 µs | 168 | 54,991 |
| Reverse proxy (3 upstreams, round-robin) | 14,486 | 82.1 µs | 167 | 55,274 |
| Static file (small, 13 B) | 35,677 | 34.0 µs | 118 | 15,098 |
| Static file (large, 256 KB) | 20,604 | 54.8 µs | 120 | 17,349 |
| Proxy throughput (sequential) | 8,442 | 136.0 µs | 150 | 46,161 |
| Real-world page (HTML+CSS+JS) | 13,670 | 86.5 µs | 102 | 8,319 |
| **Heap alloc per request** | **8,606** | **135.4 µs** | **10.55 B/req** | **46,121** |

> Note: Parallel benchmarks use `RunParallel` and auto-scale to GOMAXPROCS (12 cores).

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
