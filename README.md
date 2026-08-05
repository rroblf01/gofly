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
| `-convert <nginx.conf>` | Convert a subset of an nginx config to gofly JSON on stdout (warnings on stderr), then exit |
| `-debug` | Enable debug-level logging |
| `-version` | Print the build version and exit |
| `-health` | Perform a TCP health check against a running instance (used by Docker `HEALTHCHECK`) |

```bash
gofly -t -config /etc/gofly/config.json   # validate before deploy/reload
gofly -version                            # e.g. "gofly v1.0.0"
```

### Migrating from nginx (`-convert`)

`-convert` is a one-shot migration aid: it parses the common static-serving and
reverse-proxy subset of an nginx config and prints an equivalent gofly
`config.json` to **stdout**, with a warning on **stderr** for every directive it
skipped or only partially translated. It is *not* a runtime nginx parser — gofly
still loads JSON only — so you review the output once and commit it.

```bash
gofly -convert /etc/nginx/nginx.conf > config.json   # JSON on stdout, warnings on stderr
gofly -t -config config.json                         # then validate the result
```

**Translated:** `listen` (port + `ssl`), `server_name`, `root`, `location`
(prefix, `=`, `^~`), `try_files …/index.html` → `spa`, `proxy_pass` +
`upstream` blocks (incl. `least_conn`), `add_header`/`proxy_set_header` →
`set_headers`, `expires` → `browser_cache_ttl`, `gzip`/`gzip_min_length`/
`gzip_comp_level`, and `ssl_certificate`/`ssl_certificate_key` → `tls`.

**Not translated (warned, not silently dropped):** regex `location ~`/`~*`
(gofly is prefix-only), `rewrite`/`return`, `if`, `map`, `limit_req`, `gzip_types`
(gofly gzips by size, not MIME type), and any directive outside the supported
set. Review every warning — the converted route set may need hand-editing where
nginx used a feature gofly does not have.

#### Converting at build time in a Dockerfile

The runtime gofly image is `scratch` — no shell — so it cannot run
`gofly -convert nginx.conf > config.json` itself (the `>` redirection needs a
shell). Do the conversion in a small intermediate stage that *has* a shell,
using the gofly binary copied straight out of the published image, then copy the
generated `config.json` into the final `scratch` stage:

```dockerfile
# Stage: convert nginx.conf -> config.json (needs a shell for the redirection)
FROM alpine:3 AS nginx-convert
COPY --from=ghcr.io/rroblf01/gofly:latest /gofly /gofly
COPY nginx.conf /nginx.conf
# Convert; warnings print to the build log. `-t` fails the build if the result
# is invalid, so a bad conversion never ships.
RUN /gofly -convert /nginx.conf > /config.json && /gofly -t -config /config.json

# Final image
FROM ghcr.io/rroblf01/gofly:latest
COPY --from=nginx-convert /config.json /etc/gofly/config.json
COPY --from=builder /app/dist/ /www/
EXPOSE 80
CMD ["-config", "/etc/gofly/config.json"]
```

- `COPY --from=ghcr.io/rroblf01/gofly:latest /gofly /gofly` pulls the static
  binary out of the published image — no separate build needed. It runs on
  `alpine` because it is fully static (`CGO_ENABLED=0`).
- Conversion happens **once, at build time**; the resulting `config.json` is
  baked into the image. Watch the build log for `WARN` lines and review them.
- `&& /gofly -t -config /config.json` makes a config that fails validation fail
  the build, so a broken conversion never reaches production.
- Need to keep editing the config? Commit the converted `config.json` instead and
  `COPY` it directly — converting on every build re-applies the same warnings and
  silently re-drops anything you patched by hand.

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
(AMD Ryzen 5 3600, 6 cores / 12 threads, Linux 6.18). Both servers run in Docker
with `--network host`; gofly built from this repo's `Dockerfile`
(`scratch`, Go 1.26.5), nginx is the official `nginx:alpine` image with
`worker_processes auto`, `access_log off`, `sendfile on`. Memory is the
container's cgroup-accounted usage (`docker stats`) sampled mid-run — see the
[Memory usage](#memory-usage) note below on why this differs from a naive
per-process `ps` sum for a multi-process server like nginx.
**Both servers are tested over HTTP/1.1 with keep-alive** — `wrk` only speaks
HTTP/1.1, and nginx listens plaintext with no `http2` directive, so this is a
like-for-like protocol comparison (see the HTTP/2 note below).

| Config | Requests/s | Latency (avg) | Memory (RSS, cgroup) |
|---|---|---|---|
| gofly (×12 workers, no cache, default GC) | 92,500 | 1.39 ms | ~27 MB |
| gofly (×12 workers, **static_cache_ttl**, default GC) | **194,300** | 0.69 ms | ~24 MB |
| **nginx alpine** (`worker_processes auto`) | 220,800 | 0.75 ms | ~13 MB |

Medians of 3 runs. Unlike an earlier version of this table, the cache row is
now the fastest gofly config on this single hot file too — see
[Where the in-memory cache earns its place](#where-the-in-memory-cache-earns-its-place-many-distinct-files)
for why that flipped and why the old "leave it off for a hot file" advice no
longer holds on current code.

On a single hot file, gofly with the cache reaches **~88% of nginx's
throughput** at roughly the same latency; without it, gofly tops out around
**~92k req/s**, **42% of nginx**, at **1.9× the latency**.

#### What actually limits it (it is not the kernel, and it is not Go)

An earlier version of this section claimed the ceiling was "the kernel syscall +
loopback path." That is wrong, and two stdlib reference points on the same
machine show why:

| Reference server | Requests/s | What it does per request |
|---|---|---|
| stock `net/http`, write 5 bytes (no I/O) | **233,000** | one `write()` |
| stock `http.FileServer` (same 798 B file) | 94,300 | `open`+`fstat`+`sendfile` |
| gofly (no cache) | 92,500 | `open`+`fstat`+`sendfile` |
| nginx alpine | 220,800 | cached fd + `sendfile` |

- The loopback path is **not** the wall: a bare Go handler does **233k** on it.
- Go's `net/http` is **not** the wall either — that 233k *is* `net/http`.
- The wall is the **per-request `open()` + `fstat()` on the static path**: it
  drops Go from 233k to ~95–99k. gofly tracks stock `http.FileServer` (and edges
  past it on the optimised header path), so this is the cost of file serving in
  `net/http`, not gofly's code.
- nginx is ~2× faster on a hot file because its `open_file_cache` keeps the fd +
  stat resident, so it pays neither syscall per request — it serves much closer
  to its own write ceiling.

#### Where the in-memory cache earns its place: many distinct files

Its win is eliminating the per-request `open`/`fstat`/`read`, which is exactly
the limiter identified above — and, as the numbers below show, that turns out
to help a single hot file just as much as it helps a thousand distinct ones.
Across 1000 distinct files hit at random (`wrk` + a random-path Lua script):

| Config | Single hot file | 1000 files (random) |
|---|---|---|
| gofly (no cache) | 92,500 | 78,200 |
| gofly (**static_cache_ttl**) | 194,300 | **156,100** |
| nginx alpine | 220,800 | 253,500 |

With the cache, gofly reaches **~62% of nginx** on the many-files workload —
and, on this run, actually *beats* its own no-cache config on the single hot
file too (194,300 vs 92,500). That contradicts an earlier version of this
table, which measured the cache as slightly *slower* on one hot file and
recommended leaving it off for a handful of hot files. Re-measuring on current
code shows the opposite, and it matches the architecture, not the old data:
the cache row skips the per-request `open()`+`fstat()` entirely (serving from
an already-resident `bytes.Reader`), which is exactly the syscall pair
identified above as the throughput ceiling — eliminating it should help a
single file exactly as much as it helps a thousand. Take the old "leave it off
for a hot file" advice as superseded; when in doubt, measure your own workload.
The cache hot path itself is allocation-lean and deterministic regardless — 10
allocs and ~1.9 µs per hit (down from 14 allocs after precomputing the
per-entry header slices — see `serveCached` in
`internal/static/static.go`, guarded by `BenchmarkCacheHit`).

**Rule of thumb:** enable `static_cache_ttl` whenever the working set fits the
configured byte/entry budget — on this measurement it wins on both a single
hot file and many distinct files, since either way it removes the per-request
`open()`+`fstat()`.

The `/metrics` wrapper costs ~2–5%; set `"metrics": false` for the last few percent.

#### Why gofly does not match nginx's `open_file_cache` on a hot file

Closing the single-file gap would mean caching the open fd and `sendfile`-ing it
with an explicit offset, the way nginx does. In pure-stdlib Go that is not clean:
`(*os.File).ReadFrom`/`sendfile` advances the shared file offset, so one cached
fd cannot be `sendfile`d concurrently without racing; a `SectionReader` (`pread`,
offset-explicit) sidesteps the race but is no longer an `*os.File`, so `net/http`
stops using `sendfile` and falls back to a user-space copy — the same path the
in-memory cache already takes. A `syscall.Sendfile` with an explicit offset would
work but needs the connection's raw fd, which `net/http` deliberately hides
behind `Hijacker` — taking it over means hand-rolling HTTP write/keep-alive.
gofly keeps the stdlib server (and its zero-dependency, memory-safe footprint)
and uses the in-memory cache instead, which wins on the workload that the
per-request syscalls actually hurt.

#### The handler is not the bottleneck — so pools / more goroutines do not help

A natural instinct is to chase the nginx gap with more concurrency: a goroutine
worker pool, more workers, a bigger accept fan-out. Measured in-process
(`go test -bench`, `RunParallel`, 12 cores), the cache-hit handler costs:

| In-process cache hit | ns/op | ≈ throughput |
|---|---|---|
| 1 file (one hot key) | ~1,150 | ~870k req/s/core |
| 1000 files (spread) | ~775 | ~1.29M req/s/core |

The handler sustains **over a million requests per second per core** in
user-space, yet the socket-level load test tops out at 78k–194k req/s *total*
for gofly. gofly's own code runs **50–100× faster than the requests it
receives over a socket** — it is nowhere near the bottleneck. Conclusions:

- **A worker pool would slow things down.** Go's runtime is already an M:N
  scheduler (a goroutine pool over `GOMAXPROCS` OS threads) and `net/http` is
  already goroutine-per-connection. Adding a second pool on top only adds a queue
  and a hand-off.
- **More accept sockets alone do nothing.** `workers` (SO_REUSEPORT accept
  socket count) at 1 vs 12, `GOMAXPROCS` held at the default 12 either way,
  measured within noise of each other (~104k-107k). With 100 client
  connections there are already 100 handler goroutines; adding more accept
  sockets than there are connections changes nothing. `GOMAXPROCS` itself is a
  different knob and does matter — see
  [Memory usage](#where-the-load-time-rss-actually-goes-and-two-things-worth-knowing)
  for the RAM/throughput trade-off in scaling it down.
- **Micro-optimising the handler does nothing for throughput either** — there is
  50–100× of headroom. (It still helps *allocations*; see below.)

The real wall is the **per-request socket + syscall path of `net/http`**
(accept → read/parse request → write response → `sendfile`/`write` → loopback),
against nginx's hand-tuned epoll event loop + cached fds + `tcp_nopush`. That is
an architectural difference in the HTTP server, not something a pool fixes.
Closing it would require either the raw-`sendfile`-with-hijack route rejected
above, or replacing `net/http` with a custom epoll server — which would discard
the zero-dependency, memory-safe, simple design that is the point of gofly.

#### The one low-cost lever: fewer allocations, not more goroutines

The only user-space win that matters is cutting per-request allocations: fewer
allocations let a *tighter* GC (`GOGC`) hit the same throughput at lower RSS, or
the same RSS with less GC CPU — which is the trade-off that actually concerns a
memory-budgeted deployment. That is why the cache hot path
(`serveCached`) precomputes `Last-Modified`/`Content-Length` at insertion and
writes the body directly instead of going through a reader, shares the
constant security-header slices package-level, and — as of the most recent
pass — precomputes each cache entry's header `[]string` values once instead of
rebuilding them per hit (down from 21 to 14 to **10 allocs/hit**, guarded by
`BenchmarkCacheHit`). Beyond that the handler already allocates little;
most remaining per-request allocations live inside `net/http` and are not
reachable without leaving the stdlib server. Diminishing returns — so gofly stops
here rather than trading its footprint and simplicity for a few percent.

#### Is the gap because gofly uses HTTP/1.1 and nginx uses HTTP/2?

No — both servers serve **HTTP/1.1** in these tests, so the gap is not a protocol
difference:

- `wrk` is an HTTP/1.1-only client; it never negotiates HTTP/2. Whatever the
  server supports, the load is driven over HTTP/1.1.
- The nginx config listens plaintext (`listen 8390;`) with **no `http2`
  directive and no TLS**, so nginx also answers HTTP/1.1 here.
- gofly negotiates HTTP/2 automatically over TLS (ALPN `h2`); plaintext is
  HTTP/1.1 (see [HTTP/2](#http2)). These benchmarks are plaintext, so gofly is on
  HTTP/1.1 too.

HTTP/2 would not flip this result anyway. Its wins — header compression, request
multiplexing over a single connection — target many concurrent streams over
higher-latency links, not raw throughput of small independent requests on
loopback. For this workload HTTP/2's single-connection multiplexing (with its
framing overhead and per-connection flow control) is typically **slower** than
HTTP/1.1 across many keep-alive connections, which is exactly what `wrk -c100`
drives. The bottleneck is the per-request syscall path described above, and it is
the same under either protocol.

### Comparison with nginx

Fastest gofly config (×12 workers, **static_cache_ttl**, default GC) vs nginx
alpine, same machine:

| Metric | gofly (scratch) | nginx (alpine) | Difference |
|---|---|---|---|
| **Requests/sec** (single hot file) | 194,300 | 220,800 | **-12%** |
| **Requests/sec** (1000 files) | 156,100 | 253,500 | **-38%** |
| **Latency (avg, single file)** | 0.69 ms | 0.75 ms | **-8%** |
| **RSS under load (cgroup-accounted)** | ~24-31 MB | ~13 MB | **~2× larger** |
| **Image size** | **~7.6 MB** | ~62 MB | **8× smaller** 🏆 |
| **Dependencies** | **0** (pure stdlib) | libc, PCRE, zlib, OpenSSL | **—** 🏆 |
| **Static binary** | ✅ Yes | ❌ No | **—** 🏆 |
| **Memory safety** | ✅ Yes (Go) | ❌ No (C) | **—** 🏆 |
| **Configuration** | **JSON (simple)** | nginx.conf | **—** 🏆 |

> The RSS row flips the direction of an older version of this table, which
> reported nginx at ~58 MB using a naive `ps aux` RSS sum across its 13
> processes. That double-counts nginx's shared library pages (libc, PCRE,
> zlib, OpenSSL — mapped once physically, but `ps` reports the full mapping
> per process): measured directly, that same container is ~45.6 MB by naive
> sum but only **~13 MB** by `docker stats` (cgroup `memory.current`, which
> dedupes shared pages) — the number that actually counts against a container
> memory limit. gofly is single-process, so both methods agree for it; nginx's
> real memory efficiency was understated before and is genuinely excellent
> once measured the same way. See [Memory usage](#memory-usage) for what
> actually drives gofly's side of this gap — mainly `GOMAXPROCS` — and note
> that these benchmarks run without a `--cpus` limit, so gofly schedules for
> all 12 host cores; a CPU-quota-limited deployment gets a lower `GOMAXPROCS`
> (and less RAM) automatically on Go 1.25+.

> nginx remains the throughput leader on raw static serving. gofly trades a
> single-digit-percent throughput gap (once the static cache is on) for a
> **~7.6 MB static binary, zero dependencies, memory safety, and JSON config**
> — at roughly double nginx's resident memory for this workload. Pick gofly
> when footprint-of-the-binary, deployment simplicity, and safety matter more
> than resident memory or topping out a single static endpoint; pick nginx
> when either raw throughput or minimum RSS is the goal.

> ⚠️ Throughput is hardware- and kernel-dependent. These figures are from one
> machine (Ryzen 5 3600, 12 threads, loopback, Linux 6.18). Re-run on your
> target hardware before relying on absolute numbers.

### Memory usage

Measured via `docker stats` (cgroup `memory.current`), same machine as above.

| State | gofly (default GC, 12 workers) | gofly (GOGC=off, GOMEMLIMIT=200MiB) |
|---|---|---|
| Idle (no traffic, just started) | ~6-8 MB | ~6-8 MB |
| Sustained load, no cache | ~27-31 MB | ~95 MB |
| Sustained load, static cache | ~24 MB | ~95 MB |

> The static cache is bounded two ways: a total-byte budget
> (`static_cache_max_bytes`, default 64 MiB) and a count cap (4096 entries),
> with individual files ≤1 MiB, evicted after `static_cache_ttl`. `GOMEMLIMIT`
> caps Go's heap and triggers GC only when needed; without it, `GOGC=off` lets
> the heap grow up to that limit.

#### Where the load-time RSS actually goes, and two things worth knowing

Investigating the ~24-31 MB figure above (vs nginx's ~13 MB for the same
workload) turned up two concrete findings, both reproduced with `docker stats`
+ the `/metrics` goroutine gauge on this machine:

- **`GOMAXPROCS` (`max_procs`), not `workers`, is what drives it.** Holding
  `workers` and `max_procs` equal and sweeping both from 1 to 12 under the same
  100-connection load: RSS goes ~16 MB (1 proc, 20.8k req/s) → 18 MB (2 procs,
  39.2k) → **18.6 MB (4 procs, 72.4k req/s)** → 25 MB (6 procs, 94.8k) → 27-28
  MB (12 procs, 103-106k). `max_procs: 4` gets **~70% of the default
  throughput at ~35% less RAM** — a real knee in the curve, worth setting
  explicitly if you'd rather trade some throughput for a smaller footprint.
  This is *not* a container-detection gap to fix in gofly, though: Go 1.25+
  (gofly's Dockerfile pins 1.26.5) already reads the container's cgroup CPU
  quota at startup and sets `GOMAXPROCS` to it automatically — verified
  directly on this machine (`docker run --cpus=2 golang:1.26.5-alpine ...`
  reports `GOMAXPROCS: 2` out of the box, `GODEBUG=containermaxprocs=0`
  reverts it to the host's 12). So a gofly container that's actually
  CPU-limited already gets the lower, RAM-cheaper `GOMAXPROCS` for free; the
  benchmark numbers above run with **no** `--cpus` limit, which is exactly why
  they default to 12. `max_procs` remains useful for the opposite case — capping
  below an *unlimited or generous* quota to save RAM you don't need for
  throughput — but there is nothing to auto-detect that the runtime doesn't
  already handle.
- **No idle-RAM leak, but RSS does not settle back down after a load spike
  either.** Across repeated 10s-load/15s-idle cycles, goroutine count (via
  `/metrics`) returned to the exact startup baseline (18) after every single
  cycle — no goroutine accumulation. RSS rose from a ~7 MB cold start to a
  ~31-32 MB plateau over the first 5-6 cycles and then held flat (±0.5 MB) for
  3 more cycles and 2+ minutes of subsequent idle, with or without
  `GODEBUG=madvdontneed=1`. That is Go's GC reaching its steady-state
  high-water mark under the default `GOGC=100` pacing (heap allowed to double
  before collecting) and then being slow to hand freed pages back to the OS
  absent renewed allocation pressure — not a leak, since the live heap and
  goroutine count are both flat. Practically: **one traffic burst permanently
  raises the resident baseline** until either a bigger GC cycle or a restart,
  which matters when sizing a memory limit for a bursty workload. `GOMEMLIMIT`
  (already documented above) is the existing lever for this; there is no
  idle-triggered `debug.FreeOSMemory()` call today.

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
- **Direct header map, precomputed slices** — the cache hot path assigns `h["Content-Type"] = e.ctypeHdr` (a `[]string` built once when the entry is cached, not per request), avoiding both `Header().Set()` overhead and a per-request `[]string{...}` allocation.
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
| Reverse proxy (single upstream) | 30.0 µs | 155 | 16,911 |
| Reverse proxy (3 upstreams, round-robin) | 29.4 µs | 153 | 16,860 |
| Static file (small, 13 B) | 31.6 µs | 114 | 14,683 |
| Static file (large, 256 KB) | 51.4 µs | 114 | 16,999 |
| Proxy throughput (sequential) | 121.1 µs | 140 | 12,610 |
| Real-world page (HTML+CSS+JS) | 87.8 µs | 96 | 8,305 |
| **Heap alloc per request** | — | — | **16.7 B/req** |

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

