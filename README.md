# gofly

A fast, lightweight HTTP reverse proxy and static file server written in Go. Designed as a minimal alternative to nginx with **zero external dependencies**, a tiny memory footprint, and a Docker image built on `scratch` (~7 MB).

## Features

- **Reverse proxy** with `round_robin` or `least_conn` load balancing
- **WebSocket support** — transparent WebSocket proxying
- **HTTP/2** — negotiated automatically on the TLS listener (HTTP/1.1 on plaintext)
- **Active + passive health checks** — periodic probing (`health_check_path`) plus auto-disable of failing upstreams with cooldown
- **Prometheus metrics** — `/metrics` endpoint in text exposition format (requests, bytes, latency, in-flight, per-upstream health)
- **Retry on failure** — failover to next upstream on connection errors
- **In-memory static cache** — optional `static_cache_ttl` serves small files from RAM
- **Config test** — `gofly -t` validates the config without starting
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
|---|---|---|---|
| `port` | int | `80` | HTTP listen port |
| `workers` | int | `1` | Number of SO_REUSEPORT accept goroutines (Linux only) |
| `access_log` | bool | `true` | Enable/disable access log (disable for maximum throughput) |
| `read_timeout` | string | `"30s"` | Max duration for reading request |
| `write_timeout` | string | `"30s"` | Max duration for writing response |
| `idle_timeout` | string | `"120s"` | Max duration for idle connections |
| `max_body_size` | int | `0` (unlimited) | Max request body size in bytes |
| `memory_limit` | int | `104857600` (100 MB) | Soft heap limit in bytes (`debug.SetMemoryLimit`) |
| `gogc` | int | — | `GOGC` percent (`debug.SetGCPercent`); higher = fewer GC cycles, more RAM |
| `metrics` | bool | `true` | Expose the `/metrics` endpoint and record counters |
| `trust_forwarded_for` | bool | `false` | Trust `X-Forwarded-For` for the client IP (rate limiting). Enable only behind a trusted proxy |
| `rate_limit` | object | — | Global rate limiting config |
| `rate_limit.requests_per_second` | float | — | Requests per second per IP |
| `rate_limit.burst` | int | — | Burst capacity |
| `rate_limit.idle_ttl` | string | `"10m"` | Evict a per-IP bucket after this idle time (bounds rate-limiter memory) |
| `tls` | object | — | TLS configuration |

#### Route config

| Field | Type | Default | Description |
|---|---|---|---|
| `path` | string | — | URL path pattern (e.g. `/api/`, `/`) |
| `server_name` | string | — | Match requests by Host header (virtual host) |
| `upstreams` | []string | — | Backend URLs for reverse proxy |
| `strategy` | string | `"round_robin"` | Load balancing: `round_robin` or `least_conn` (fewest in-flight) |
| `health_check_path` | string | — | If set, actively probe this path on each upstream and toggle health |
| `health_check_interval` | string | `"10s"` | Interval between active health probes |
| `static_dir` | string | — | Directory to serve static files from |
| `static_cache_ttl` | string | — | Cache small files (≤1 MiB) in memory for this TTL, skipping open/stat/read syscalls. Takes precedence over `precompressed`. |
| `precompressed` | bool | `true` | Probe for a sibling `.gz` file when the client accepts gzip. Set `false` to skip the stat syscall on routes without pre-compressed assets. |
| `browser_cache_ttl` | string | — | Cache-Control max-age (e.g. `"3600s"`) |
| `gzip` | bool | `false` | Enable gzip compression |
| `gzip_level` | int | `-1` (default) | gzip compression level `[-2,9]` (`1`=fastest, `9`=best, `-1`=default) |
| `gzip_min_length` | int | `0` | Minimum response size in bytes before compressing (0 = always) |
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

## CLI flags

| Flag | Description |
|---|---|
| `-config <path>` | Path to the JSON config file (default `/etc/gofly/config.json`, or `$GOFLY_CONFIG`) |
| `-root <dir>` | Configless mode: serve a static directory, no config file needed |
| `-port <n>` | Override the listen port |
| `-t` | Test (load + validate) the configuration and exit; non-zero on error |
| `-debug` | Enable debug-level logging |
| `-version` | Print the build version and exit |
| `-health` | Perform a TCP health check against a running instance (used by Docker `HEALTHCHECK`) |

```bash
gofly -t -config /etc/gofly/config.json   # validate before deploy/reload
gofly -version                            # e.g. "gofly v1.0.0"
```

## API

### Health check

```
GET /health
```

Returns `{"status":"ok"}` with HTTP 200.

### Metrics

```
GET /metrics
```

Prometheus text exposition format (enabled by default; disable with `"metrics": false`):

```
gofly_build_info{version="v1.0.0"} 1
gofly_requests_total{status_class="2xx"} 12345
gofly_requests_in_flight 3
gofly_response_bytes_total 9876543
gofly_request_duration_seconds_sum 42.123456
gofly_request_duration_seconds_count 12345
gofly_goroutines 24
gofly_heap_alloc_bytes 12582912
gofly_upstream_healthy{route="/api",upstream="http://10.0.0.1:8080"} 1
gofly_upstream_in_flight{route="/api",upstream="http://10.0.0.1:8080"} 2
```

### HTTP/2

HTTP/2 is negotiated automatically over TLS (ALPN `h2`); plaintext connections
use HTTP/1.1. No configuration required — just enable `tls`.

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

Same static page (`www/index.html`, 798 B), same machine for every row
(AMD Ryzen 5 3600, 6 cores / 12 threads, Linux 6.18). nginx runs in Docker with
`--network host`, `worker_processes auto`, `access_log off`, `sendfile on`.

| Config | Requests/s | Latency (avg) | Memory (RSS) |
|---|---|---|---|
| gofly (1 worker, no cache, default GC) | 97,023 | 1.26 ms | **~20 MB** |
| gofly (×12, no cache, default GC) | 93,240 | 1.31 ms | ~21 MB |
| gofly (×12, **static_cache_ttl**, default GC) | 87,168 | 1.28 ms | ~21 MB |
| gofly (×12, **static_cache_ttl**, GOGC=off, GOMEMLIMIT=200MiB) | 95,447 | 1.16 ms | ~93 MB |
| gofly (×12, **static_cache_ttl**, GOGC=off, **metrics off**) | 97,886 | 1.13 ms | ~93 MB |
| **nginx alpine** (`worker_processes auto`) | **255,882** | **0.54 ms** | ~58 MB (13 procs) |

On this hardware gofly tops out around **~99k req/s regardless of worker count,
static cache, or GC settings** — the limiter is the kernel syscall + loopback
network path (≈35% `sys`, ≈8% `softirq`, ≈23% idle during the run), not gofly's
user-space code. That puts it at roughly **38% of nginx's throughput** at about
**2.3× the latency**. nginx's event-driven core and `sendfile` batching extract
more from the same kernel path.

Two caveats on the numbers:

- The `/metrics` wrapper costs only ~2–5% here; set `"metrics": false` for the
  last few percent.
- The in-memory cache (`static_cache_ttl`) doesn't move throughput on this
  workload because an 798 B file already serves from the page cache via
  `sendfile`. Its win is eliminating the `open`/`stat`/`read` syscalls, which
  matters under many distinct files or a cold page cache — not a single hot file.

### Comparison with nginx

Best gofly config (×12 workers, `static_cache_ttl`, GOGC=off + GOMEMLIMIT=200MiB, metrics off) vs nginx alpine, same machine:

| Metric | gofly (scratch) | nginx (alpine) | Difference |
|---|---|---|---|
| **Requests/sec** | 97,886 | 255,882 | **-62%** |
| **Latency (avg)** | 1.13 ms | **0.54 ms** | **+109%** |
| **RSS under load (default GC)** | **~20 MB** | ~58 MB (13 procs) | **~3× smaller** 🏆 |
| **Image size** | **~7 MB** | ~35 MB | **5× smaller** 🏆 |
| **Dependencies** | **0** (pure stdlib) | libc, PCRE, zlib, OpenSSL | **—** 🏆 |
| **Static binary** | ✅ Yes | ❌ No | **—** 🏆 |
| **Memory safety** | ✅ Yes (Go) | ❌ No (C) | **—** 🏆 |
| **Configuration** | **JSON (simple)** | nginx.conf | **—** 🏆 |

> nginx remains the throughput/latency leader on raw static serving — about
> 2.6× the requests/sec here. gofly trades that for a **~7 MB static binary,
> zero dependencies, memory safety, and a ~20 MB resident footprint** (a third
> of nginx's) under the default GC. Pick gofly when footprint, deployment
> simplicity, and safety matter more than topping out a single static endpoint;
> pick nginx when maximum static throughput is the goal.

> ⚠️ Throughput is hardware- and kernel-dependent. These figures are from one
> machine (Ryzen 5 3600, 12 threads, loopback). Re-run on your target hardware
> before relying on absolute numbers.

### Memory usage

| State | gofly (default GC) | gofly (GOGC=off, GOMEMLIMIT=200MiB) |
|---|---|---|
| Idle (no traffic) | ~10 MB | ~10 MB |
| Sustained load, no cache | ~21 MB | ~92 MB |
| Sustained load, static cache | ~21 MB | ~93 MB |

> The static cache is bounded two ways: a total-byte budget
> (`static_cache_max_bytes`, default 64 MiB) and a count cap (4096 entries),
> with individual files ≤1 MiB, evicted after `static_cache_ttl`. `GOMEMLIMIT`
> caps Go's heap and triggers GC only when needed; without it, `GOGC=off` lets
> the heap grow up to that limit.

### Architecture and optimizations

gofly achieves its performance through:

- **In-memory static cache** — optional `static_cache_ttl` holds resolved small files (≤1 MiB each) in memory, bounded by a total-byte budget (`static_cache_max_bytes`, default 64 MiB) and a 4096-entry count cap, skipping `open`/`stat`/`read` syscalls on hits and serving straight from a `bytes.Reader`. Conditional requests and byte ranges are honored from the cached copy.
- **sendfile(2) zero-copy** — `w.(io.ReaderFrom).ReadFrom(f)` delegates directly to Go's `net.TCPConn.ReadFrom`, which uses `sendfile(2)` on Linux when the source is a regular file. No userspace buffer copy.
- **SO_REUSEPORT** — multiple goroutines accept on separate sockets, kernel distributes connections across them. Configurable via `workers`.
- **Pooled gzip writers** — `sync.Pool` per compression level reuses the ~256 KiB compressor window instead of reallocating it per response; optional `gzip_min_length` skips compressing tiny payloads.
- **Tuned upstream transport** — 64 idle keepalive conns/host (256 total), `ForceAttemptHTTP2`, and 32 KiB read/write buffers cut connection churn under proxy load while keeping idle-pool memory bounded; all tunable via the `upstream` config block.
- **Sharded rate limiter** — per-IP buckets are spread across 256 independently-locked shards; a background janitor evicts idle buckets (`rate_limit.idle_ttl`) so memory stays bounded.
- **Async logging** — log entries go to a buffered channel (16384 capacity), background goroutine drains it. `access_log: false` bypasses the wrapper entirely (zero cost).
- **Pooled response writer** — `sync.Pool` reuses `responseWriter` structs across requests; WebSocket relays reuse pooled 32 KiB buffers.
- **Sniff buffer pool** — 512-byte `sync.Pool` for `http.DetectContentType` fallback.
- **Direct header map** — `h["Content-Type"] = []string{ctype}` avoids `Header().Set()` overhead.
- **Allocation-free hot path** — ETag and `Content-Range` are built with `strconv.AppendInt`, and the static root is resolved to an absolute path once at startup (no per-request `filepath.Abs`/`os.Getwd`).
- **Pre-built ReverseProxy** — created once in `New()`, not per-request.
- **Atomic round-robin** — `atomic.Uint64`, no locks.
- **Tuned TCP** — `TCP_DEFER_ACCEPT` + `TCP_QUICKACK` on listener socket (Linux).
- **GC control** — set `gogc` (or pair `GOGC=off` with `GOMEMLIMIT`) for near-zero GC overhead with bounded memory.

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

### Go benchmarks (internal)

Full client→server→loopback round trips via `httptest`, AMD Ryzen 5 3600
(6 cores / 12 threads), `go test -bench=. -benchmem -benchtime=2s`.

| Benchmark | Latency | Allocs/op | Bytes/op |
|---|---|---|---|
| Reverse proxy (single upstream) | 69.0 µs | 165 | 54,346 |
| Reverse proxy (3 upstreams, round-robin) | 70.2 µs | 164 | 54,581 |
| Static file (small, 13 B) | 32.1 µs | 119 | 15,741 |
| Static file (large, 256 KB) | 52.9 µs | 120 | 18,701 |
| Proxy throughput (sequential) | 132.8 µs | 148 | 46,035 |
| Real-world page (HTML+CSS+JS) | 84.6 µs | 100 | 8,357 |
| **Heap alloc per request** | — | — | **8.1 B/req** |

> Note: Parallel benchmarks use `RunParallel` and auto-scale to GOMAXPROCS (12 threads).

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

## Versioning & stability

gofly follows [Semantic Versioning](https://semver.org/). From **1.0.0** onward:

- The **JSON config schema** (field names and semantics) and the **CLI flags**
  are part of the public API. They will not change incompatibly within a major
  version; new fields are added in a backward-compatible way.
- A config that loads on `1.x` will keep loading on every later `1.y`.
- The `/metrics` metric names follow Prometheus conventions and are kept stable
  within a major version.
- Not yet covered by the stability guarantee (may change in a minor release):
  the access-log JSON field set and the autoindex HTML output.

Validate a config against the running version with `gofly -t -config <path>`.

## Roadmap (post-1.0)

Deliberately **out of scope for 1.0**, planned for later `1.x`:

- ACME / Let's Encrypt automatic certificates
- HTTP/3 (QUIC)
- Brotli compression
- Response/content caching (proxy cache)
- Weighted load balancing and `ip_hash`
- IP allow/deny lists and basic auth

These would be added without breaking the 1.0 config contract. Keeping them out
of 1.0 is intentional: the goal is a small, stable core with **zero external
dependencies** (some of the above, e.g. HTTP/3, would require adding one).

## License

MIT

