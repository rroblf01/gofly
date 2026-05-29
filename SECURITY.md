# Security Policy

## Supported versions

gofly follows [Semantic Versioning](https://semver.org/). Security fixes target
the latest released `1.x` minor version.

| Version | Supported |
|---------|-----------|
| 1.x     | ✅        |
| < 1.0   | ❌        |

## Reporting a vulnerability

Please report security issues privately via GitHub Security Advisories
("Report a vulnerability" on the repository's **Security** tab) rather than a
public issue. Include a description, affected version, and a reproduction if
possible. We aim to acknowledge reports within a few days.

## Threat model and operational notes

gofly is designed to sit at the edge or in front of application backends. A few
things to be aware of when deploying:

- **`X-Forwarded-For` is not trusted by default.** The rate limiter keys on the
  socket peer address, so a direct client cannot spoof `X-Forwarded-For` to
  evade or amplify per-IP limits. Set `"trust_forwarded_for": true` **only** when
  gofly sits behind a trusted load balancer/proxy that sets the header; then the
  first hop is used. The idle-bucket janitor bounds the limiter's memory
  regardless of trust.
- **TLS.** gofly serves HTTP/2 automatically on the TLS listener. It does not
  manage certificates (no ACME yet) — provide `cert_file`/`key_file` and rotate
  them via your own tooling, then `SIGHUP` to reload.
- **Path traversal.** The static file server rejects any path containing `..`
  and confines resolved paths to the configured `static_dir`. Symlinks that
  point outside the root are followed by the OS; avoid placing untrusted
  symlinks inside served directories.
- **`/metrics` and `/health`.** Both are exposed on the main listener. If the
  metrics surface is sensitive, disable it (`"metrics": false`) or restrict
  access at your network/load-balancer layer. gofly does not yet provide
  built-in IP allow/deny lists.
- **Memory limits.** A soft heap limit (`memory_limit`, default 100 MB) bounds
  RSS under load. Tune it for your workload; the in-memory static cache is
  additionally bounded (≤4096 entries, ≤1 MiB each).

## Run as non-root

The Docker image runs as UID 65534 (`nobody`). When binding to ports < 1024
outside Docker, prefer `setcap cap_net_bind_service=+ep` on the binary or a
reverse-proxy/socket-activation setup over running as root.
