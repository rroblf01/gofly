# Changelog

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
