# Changelog

## v1.1.2 (2026-08-05)

### Performance

- **Fewer allocations on the static cache hot path** — `cacheEntry` now precomputes the `Content-Type`/`Content-Length`/`Etag`/`Last-Modified` header slices once when a file is cached, instead of building a new `[]string{...}` for each on every hit; `Cache-Control` and `set_headers` are precomputed the same way on the `Handler`. Cuts `serveCached` from 14 to **10 allocs/op** (`BenchmarkCacheHit`: ~2.3 µs → ~1.85 µs).
- **Reverse proxy: one fewer string build and one fewer full var-scan per request** — `UpstreamState` precomputes `url.String()` once in `New()` instead of reconstructing it on every proxied request for the `UpstreamSetter`/logging hook; `expandVars` now short-circuits on a literal (no `$`) instead of running five `strings.Contains`/`ReplaceAll` passes. Reverse-proxy benchmarks (`BenchmarkProxy_SingleUpstream`/`MultipleUpstreams`) drop from ~34-37 µs/op to ~29-30 µs/op.

### Chores

- **Go toolchain pinned to 1.26.5** — `go.mod` and the `Dockerfile` base image (`golang:1.26.5-alpine`, was the floating `golang:1.26-alpine` tag) are now pinned to a specific patch version for reproducible builds.

### Documentation

- **Re-measured every benchmark figure** in the Performance section against real `nginx:alpine` and gofly containers (Docker, `--network host`, `wrk`) on the same machine as before. Notably: the in-memory static cache (`static_cache_ttl`) now measures *faster* than no-cache even on a single hot file (194k vs 92k req/s) — superseding the previous "leave it off for a hot file" advice, which no longer holds on current code.
- **Corrected the nginx memory comparison.** The previous ~58 MB nginx figure was a naive `ps aux` RSS sum across nginx's 13 worker processes, which double-counts shared library pages (libc, PCRE, zlib, OpenSSL) mapped once physically but reported per process. Measured via cgroup `memory.current` (`docker stats`) — the number that actually counts against a container memory limit — nginx uses **~13 MB** for the same workload; gofly's own footprint (single-process, so both methods agree for it) is now honestly ~2× nginx's instead of the previously claimed ~3× smaller.
- **Documented that Go 1.25+ already sets `GOMAXPROCS` from the container's cgroup CPU quota** (`GODEBUG=containermaxprocs`, on by default) — verified directly (`docker run --cpus=2 golang:1.26.5-alpine`). `max_procs` remains useful to cap *below* a generous/unlimited quota to trade throughput for a smaller footprint, not to work around a container-detection gap.

## v1.1.1 (2026-06-02)

### Fixes

- **Pre-compressed static serving omitted `Content-Encoding`** — a route serving a `.gz` sibling (`precompressed`, on by default) returned the compressed bytes with the identity `Content-Type` and **no `Content-Encoding: gzip`**, so clients rendered raw gzip as text (garbage output). The pre-compressed path now sets `Content-Encoding: gzip` and `Vary: Accept-Encoding`; a request that does not accept gzip still gets the identity file. In addition, the gzip middleware no longer re-compresses a response that already declares a `Content-Encoding`, so enabling both `gzip` and `precompressed` on the same route can no longer double-encode the body. Regression tests added for both paths.

## v1.1.0 (2026-06-02)

Backwards-compatible within `1.x`: all new config fields are optional and default
to the previous behaviour.

### Features

- **nginx config converter (`gofly -convert nginx.conf`)** — one-shot migration aid that parses the common static-serving and reverse-proxy subset of an nginx config and prints an equivalent gofly `config.json` to stdout, with a warning on stderr for every directive it skips or only partially translates (regex `location`, `rewrite`/`if`/`map`, `limit_req`, `gzip_types`, …). It is **not** a runtime nginx parser — gofly still loads JSON only — so the output is reviewed once and committed. New `internal/nginx` package; documented build-time conversion pattern for the `scratch` Docker image.
- **Upstream transport tuning** — new optional `upstream` config block: `max_idle_conns`, `max_idle_conns_per_host`, `buffer_size`, and `disable_http2`. Defaults are memory-conservative (tuned for a ~100 MB budget); raise them for high-traffic reverse-proxy deployments.
- **`static_cache_max_bytes`** — bounds the total bytes held by a route's in-memory static cache (default 64 MiB), complementing the existing 4096-entry / ≤1 MiB-per-file caps so the footprint stays inside the process memory limit regardless of file sizes.
- **`max_procs`** — maps to `runtime.GOMAXPROCS` for pinning the scheduler's OS-thread count.

### Performance

- **Leaner static cache hot path** — the cache-hit path (`serveCached`) now precomputes `Last-Modified`/`Content-Length` at insertion and shares the constant security-header slices package-level, cutting a cache hit from 21 to **14 allocations** (~13% less CPU), guarded by `BenchmarkCacheHit`. On a single hot file `sendfile` still wins (the cache path is intentionally a no-op there); the cache's gain is the many-distinct-files workload, where it roughly doubles throughput (~95k → ~189k req/s) by eliminating the per-request `open`/`fstat`.
- **Reverse-proxy transport refactor** — improved buffer management and header handling drop a single-upstream proxy request from ~69 µs / ~54 KB allocated to **~37.7 µs / ~17 KB**.
- **Rate limiter** — removed the now-redundant per-bucket mutex; the shard lock already serialises token-bucket access, so each `allow()` does one fewer lock.

### Documentation

- **Corrected performance analysis.** The previous "kernel/loopback-bound" explanation was wrong: a bare Go `net/http` handler sustains ~233k req/s on the same loopback path. The real single-file limiter is the per-request `open()`+`fstat()` on the static path (drops Go from ~233k to ~95k); gofly tracks stock `http.FileServer`. Documented why more goroutines or a worker pool do **not** help (the handler already runs 50–100× faster than the socket can feed it — in-process it does >1M req/s/core), and why matching nginx's `open_file_cache` is not clean in pure-stdlib Go (`sendfile` offset semantics + `net/http` hiding the conn fd behind `Hijacker`).
- Refreshed every benchmark figure across both workloads (single hot file and 1000 random files), for gofly and nginx, all over **HTTP/1.1** — with an explicit note that the comparison is like-for-like protocol (wrk is HTTP/1.1-only; nginx listens plaintext) and that HTTP/2 would not change the result on loopback.
- Added the nginx → gofly migration guide and config-directive mapping table.

### Options added

- Global: `max_procs`, `upstream` { `max_idle_conns`, `max_idle_conns_per_host`, `buffer_size`, `disable_http2` }
- Route: `static_cache_max_bytes`

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
