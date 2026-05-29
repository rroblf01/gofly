# Changelog

## v1.0.0 (2026-05-29)

First stable release. From this version the JSON config schema and CLI flags are
covered by [SemVer](https://semver.org/): no incompatible changes within `1.x`.

### Features

- **HTTP/2** — negotiated automatically over TLS (ALPN `h2`); plaintext stays HTTP/1.1. No config needed.
- **Prometheus metrics** — new `/metrics` endpoint (text exposition format, zero-dependency) exposing request counts by status class, in-flight requests, response bytes, cumulative request duration, goroutine/heap gauges, and per-upstream health/in-flight. Toggle with `"metrics": false`.
- **Active health checks** — `health_check_path` + `health_check_interval` periodically probe each upstream and toggle it in/out of rotation, complementing the existing passive detection.
- **`least_conn` load balancing** — new `strategy` value that routes to the healthy upstream with the fewest in-flight requests (round-robin tie-break).
- **`gofly -t`** — load and validate the configuration **and build the routing table**, then exit; non-zero on error (like `nginx -t`). Catches route conflicts that would otherwise panic at startup. Wired into the systemd unit's `ExecStartPre`.
- **Build version injection** — `-version` now reports the real build version via `-ldflags -X main.version`, also surfaced as `gofly_build_info`.

### Hardening / robustness

- **Host-aware routing** — virtual hosts can now share a path (e.g. several domains serving `/`); requests dispatch by `Host` with exact-match priority and a catch-all fallback. Previously this panicked at startup (`ServeMux` duplicate pattern).
- **Panic recovery** — a panicking handler no longer drops the connection: it returns 500, is logged structurally, and is counted as 5xx in metrics (the server already survived, but it's now observable).
- **Duplicate-route detection** — `validate()` rejects two routes with the same `(path, server_name)` instead of allowing a silently-dead route.
- **Slowloris** — `ReadHeaderTimeout` is now set; `http.Server.ErrorLog` is routed through the structured logger so TLS/connection errors stay in the JSON stream.
- **`X-Forwarded-For` not trusted by default** — the rate limiter keys on the socket peer address; set `"trust_forwarded_for": true` only behind a trusted proxy. Prevents spoofed per-IP limit bypass.
- **gzip skips bodyless responses** — 204/304/1xx are no longer gzip-encoded (which would have emitted an illegal body); `Content-Length` is stripped on compressed responses.

### Operability

- `SECURITY.md` with reporting policy and a deployment threat-model note (notably the trust boundary around `X-Forwarded-For`).
- `deploy/gofly.service` systemd unit: validates config before start, `SIGHUP` reload, runs unprivileged with `CAP_NET_BIND_SERVICE` and a hardened sandbox.
- CI workflow running gofmt, `go vet`, `go test -race`, build, and a zero-dependency guard on `go.mod`.

### Fixes

- **Trailing-slash route panic** — a proxy/static route whose `path` already ended in `/` (e.g. `/api/`, as in the example config) registered the same `ServeMux` pattern twice and panicked at startup. Fixed; the example `config.json` now boots.
- `X-Forwarded-For` is now the clean client IP (no port, no duplication) — delegated to `httputil.ReverseProxy` on the proxy path and set explicitly with proper chaining on the WebSocket path.

### Performance

- **In-memory static cache** — new `static_cache_ttl` route option holds resolved small files (≤1 MiB, ≤4096 entries) in memory and serves them from a `bytes.Reader`, skipping the per-request `open`/`stat`/`read` syscalls. Conditional requests and byte ranges are served from the cached copy. In load tests this lifts gofly from ~53% to **96% of nginx throughput** (192,872 vs 201,084 req/s) at **lower average latency** (0.43 ms vs 0.46 ms), or **74% of nginx throughput at ~22 MB RSS** with the default GC.
- **Pooled gzip writers** — `sync.Pool` per compression level reuses the compressor window instead of allocating ~256 KiB on every gzipped response. New `gzip_level` and `gzip_min_length` route options (the latter sends small bodies uncompressed). Gzip now also strips the stale `Content-Length` so compressed responses are correct over the wire.
- **Tuned upstream transport** — `MaxIdleConnsPerHost` 10 → 256, `MaxIdleConns` 1024, `ForceAttemptHTTP2`, and 64 KiB read/write buffers reduce connection churn under reverse-proxy load.
- **Sharded, bounded rate limiter** — per-IP buckets are spread across 256 independently-locked shards (was a single `RWMutex`), and a background janitor evicts idle buckets after `rate_limit.idle_ttl` (default 10m), fixing unbounded memory growth under a wide or spoofed client base.
- **Pooled WebSocket relay buffers** — the two `io.Copy` directions reuse pooled 32 KiB buffers instead of allocating per connection.
- **Allocation-free static hot path** — ETag and `Content-Range` built with `strconv.AppendInt` (no `fmt.Sprintf`); the static root is resolved to absolute once at startup, removing a per-request `filepath.Abs`/`os.Getwd` syscall.
- **Configurable GC** — new `gogc` config field maps to `debug.SetGCPercent`.

### Options added

- Route: `static_cache_ttl`, `precompressed` (default `true`), `gzip_level`, `gzip_min_length`, `strategy: "least_conn"`, `health_check_path`, `health_check_interval`
- Global: `gogc`, `metrics` (default `true`), `trust_forwarded_for` (default `false`), `rate_limit.idle_ttl`

## v0.1.0 (2026-05-29)

### Features

- **Reverse proxy** — round-robin across multiple upstreams, health checks, passive failure detection with auto-recovery (`max_fails` / `fail_timeout`), retry on error
- **WebSocket proxy** — transparent WebSocket upgrade and bidirectional relay
- **Static file server** — serve files from disk with path traversal protection, prefix stripping, directory index (`index.html`)
- **SPA fallback** — `spa: true` serves `index.html` for any unmatched route
- **Autoindex** — `autoindex: true` generates HTML directory listings (hides dotfiles, prioritizes `index.html`)
- **ETag / 304** — `If-None-Match` and `If-Modified-Since` support with `Last-Modified` header
- **Range / 206** — byte range requests with `Accept-Ranges: bytes`, suffix and open-ended range support
- **Pre-compressed gzip static** — serves `file.gz` when `Accept-Encoding: gzip` is present
- **Custom error pages** — per-route `error_pages` map (e.g. `{404: "/404.html"}`)
- **Security headers** — `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy` enabled by default, configurable per route
- **CORS / custom headers** — `set_headers` and `remove_headers` on both static and proxy routes
- **Host-based routing** — `server_name` directs requests to specific routes by Host header
- **Path rewriting** — `rewrite` with variable expansion (`$1`, `$host`, `$remote_addr`, etc.)
- **Header variable expansion** — `${VAR}` in config values resolved from environment variables
- **Configless mode** — serve a static directory with `-root /path` and optional `-port`, no config file required
- **SIGHUP reload** — hot-reload config without restart
- **HTTP/HTTPS simultaneous** — serve both HTTP and TLS on separate ports (`tls_port`)
- **TLS** — certificate-based HTTPS with configurable cert/key paths
- **Rate limiting** — per-IP token bucket with configurable rate and burst
- **Gzip compression** — per-route gzip middleware for dynamic responses
- **Body size limits** — global and per-route `max_body_size` enforcement
- **Access log** — structured JSON logging to stdout, configurable via `access_log` setting
- **Graceful shutdown** — drains active connections with 30-second timeout
- **Health endpoint** — `GET /health` returns `{"status":"ok"}`
- **Docker HEALTHCHECK** — `-health` flag performs TCP dial to configured port

### Optimizations

- **sendfile(2) zero-copy** — `w.(io.ReaderFrom).ReadFrom(f)` delegates to `sendfile(2)` on Linux for static file serving, completely bypassing userspace
- **SO_REUSEPORT** — Linux: multiple goroutines accept on separate kernel-distributed sockets, configurable via `workers`
- **TCP tuning** — `TCP_DEFER_ACCEPT` + `TCP_QUICKACK` on Linux listener socket
- **Pre-compiled Cache-Control** — header string built at init time, not per-request
- **Pre-built ReverseProxy** — created once in `New()`, not per-request
- **Atomic round-robin** — `atomic.Uint64`, no locks on hot path
- **ResponseWriter pool** — `sync.Pool` reuses wrapper structs across requests
- **Sniff buffer pool** — 512-byte `sync.Pool` for MIME sniffing
- **Direct header map assignment** — `h["Content-Type"] = []string{ctype}` avoids `Header().Set()` canonicalization overhead
- **Async logging** — buffered channel (16384 capacity), background goroutine, non-blocking write
- **Memory limit** — `debug.SetMemoryLimit(100MiB)` by default, configurable via `memory_limit` or `GOMEMLIMIT`

### Code quality

- Zero external dependencies (pure Go stdlib)
- 150+ test functions across 6 packages, all pass with `-race`
- `vet` clean
- Multi-arch Docker image targeting scratch (~7.5 MB)

### Architecture

```
config.json → config.Load() → Server → middleware stack
  ├── rate limiter (per-IP token bucket)
  ├── access log (async, non-blocking)
  └── ServeMux
       ├── static handler (sendfile, ETag, Range, SPA, autoindex, error pages)
       └── reverse proxy (round-robin, health checks, WebSocket, retry)
```
