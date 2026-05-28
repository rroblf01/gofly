# gofly

A fast, lightweight HTTP reverse proxy and static file server written in Go. Designed as a minimal alternative to nginx with zero external dependencies, a tiny memory footprint, and a Docker image built on `scratch` (~5 MB).

## Features

- **Reverse proxy** with round-robin load balancing
- **Static file serving** with optional cache-control headers
- **TLS/HTTPS** support
- **Graceful shutdown** on SIGINT/SIGTERM
- **JSON structured logging** via `log/slog`
- **Pure stdlib** — zero external dependencies
- **Multi-architecture** builds (amd64, arm64)
- **Scratch-based Docker image** (~5 MB)

## Quick start

### Using Docker

```bash
# Pull and run
docker run -d --name gofly -p 80:80 rroblf01/gofly

# With custom config
docker run -d --name gofly \
  -p 80:80 \
  -v /path/to/config.json:/etc/gofly/config.json \
  rroblf01/gofly
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
  "routes": [
    {
      "path": "/api/",
      "upstreams": ["http://localhost:3001", "http://localhost:3002"],
      "strategy": "round_robin",
      "set_headers": {
        "X-Forwarded-For": "$remote_addr",
        "X-Real-IP": "$remote_addr"
      }
    },
    {
      "path": "/",
      "static_dir": "/var/www/html",
      "browser_cache_ttl": "3600s"
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

| Field | Type | Default | Description |
|---|---|---|---|
| `port` | int | `80` | HTTP listen port |
| `read_timeout` | string | `"30s"` | Max duration for reading request |
| `write_timeout` | string | `"30s"` | Max duration for writing response |
| `idle_timeout` | string | `"120s"` | Max duration for idle connections |
| `routes[].path` | string | — | URL path pattern (e.g. `/api/`, `/`) |
| `routes[].upstreams` | []string | — | Backend URLs for reverse proxy |
| `routes[].strategy` | string | `"round_robin"` | Load balancing strategy |
| `routes[].static_dir` | string | — | Directory to serve static files from |
| `routes[].browser_cache_ttl` | string | — | Cache-Control max-age (e.g. `"3600s"`) |
| `routes[].set_headers` | object | — | Headers to inject into upstream requests |
| `routes[].remove_headers` | []string | — | Headers to strip from upstream requests |
| `routes[].host` | string | — | Override the Host header |
| `tls.enabled` | bool | `false` | Enable HTTPS |
| `tls.cert_file` | string | — | Path to TLS certificate |
| `tls.key_file` | string | — | Path to TLS key |
| `tls.auto_cert` | bool | `false` | Auto-generate self-signed cert |
| `tls.cache_dir` | string | — | Cache directory for auto cert |

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
1. `golang:1.26-alpine` — builder stage
2. `scratch` — final stage (~5 MB image)

The binary is statically compiled with `CGO_ENABLED=0` and stripped symbols.

## Performance

- Reverse proxy adds ~0.5ms latency (p99) under load
- Static file serving via `http.FileServer` with optional cache headers
- Full GC control via `GOGC` and `GOMEMLIMIT` env vars

## License

MIT
