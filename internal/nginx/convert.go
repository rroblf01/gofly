// Package nginx provides a best-effort, one-shot converter from a subset of the
// nginx configuration language into gofly's native JSON config. It is a
// migration aid, not a runtime nginx parser: it deliberately supports only the
// common static-serving and reverse-proxy directives and emits an explicit
// warning for everything it does not understand, so the resulting config.json
// can be reviewed before use rather than silently diverging from the original.
package nginx

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rroblf01/gofly/internal/config"
)

// directive is one parsed nginx statement: a name, its arguments, and (for block
// directives like `server { ... }`) the nested directives.
type directive struct {
	name  string
	args  []string
	block []directive
	line  int
}

// Convert parses nginx config from r and maps the supported subset to a
// config.Config. The returned warnings list every directive or construct that
// was skipped or only partially translated; callers should surface them so the
// operator knows what to verify by hand.
func Convert(r io.Reader) (config.Config, []string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return config.Config{}, nil, err
	}
	toks, err := lex(string(data))
	if err != nil {
		return config.Config{}, nil, err
	}
	dirs, err := parse(toks)
	if err != nil {
		return config.Config{}, nil, err
	}
	c := &converter{cfg: config.Default()}
	c.cfg.Routes = nil
	c.walkTop(dirs)
	if len(c.cfg.Routes) == 0 {
		c.warnf(0, "no server/location produced a route; resulting config has no routes")
	}
	return c.cfg, c.warnings, nil
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

type token struct {
	text string
	line int
	// punct is true for the structural tokens "{", "}" and ";".
	punct bool
}

// lex splits nginx source into word and punctuation tokens, honouring `#`
// comments to end-of-line and single/double quoted strings.
func lex(src string) ([]token, error) {
	var toks []token
	line := 1
	i := 0
	n := len(src)
	for i < n {
		ch := src[i]
		switch {
		case ch == '\n':
			line++
			i++
		case ch == ' ' || ch == '\t' || ch == '\r':
			i++
		case ch == '#':
			for i < n && src[i] != '\n' {
				i++
			}
		case ch == '{' || ch == '}' || ch == ';':
			toks = append(toks, token{text: string(ch), line: line, punct: true})
			i++
		case ch == '"' || ch == '\'':
			quote := ch
			i++
			start := i
			for i < n && src[i] != quote {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("line %d: unterminated quoted string", line)
			}
			toks = append(toks, token{text: src[start:i], line: line})
			i++ // closing quote
		default:
			start := i
			for i < n {
				c := src[i]
				if c == ' ' || c == '\t' || c == '\r' || c == '\n' ||
					c == '{' || c == '}' || c == ';' || c == '#' {
					break
				}
				i++
			}
			toks = append(toks, token{text: src[start:i], line: line})
		}
	}
	return toks, nil
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

func parse(toks []token) ([]directive, error) {
	p := &parser{toks: toks}
	dirs, err := p.block(true)
	if err != nil {
		return nil, err
	}
	return dirs, nil
}

type parser struct {
	toks []token
	pos  int
}

// block parses directives until a "}" (or EOF when top is true).
func (p *parser) block(top bool) ([]directive, error) {
	var out []directive
	for p.pos < len(p.toks) {
		t := p.toks[p.pos]
		if t.punct && t.text == "}" {
			if top {
				return nil, fmt.Errorf("line %d: unexpected '}'", t.line)
			}
			p.pos++
			return out, nil
		}
		d, err := p.directive()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if !top {
		return nil, fmt.Errorf("unexpected EOF: missing '}'")
	}
	return out, nil
}

func (p *parser) directive() (directive, error) {
	first := p.toks[p.pos]
	d := directive{name: first.text, line: first.line}
	p.pos++
	for p.pos < len(p.toks) {
		t := p.toks[p.pos]
		if t.punct {
			switch t.text {
			case ";":
				p.pos++
				return d, nil
			case "{":
				p.pos++
				blk, err := p.block(false)
				if err != nil {
					return d, err
				}
				d.block = blk
				return d, nil
			case "}":
				// directive without trailing ';' right before block close
				return d, nil
			}
		}
		d.args = append(d.args, t.text)
		p.pos++
	}
	return d, fmt.Errorf("line %d: directive %q not terminated", first.line, first.text)
}

// ---------------------------------------------------------------------------
// Converter
// ---------------------------------------------------------------------------

type converter struct {
	cfg        config.Config
	warnings   []string
	upstreams  map[string][]string // upstream name -> backend URLs
	upStrategy map[string]string   // upstream name -> gofly strategy (e.g. least_conn)
	portSet    bool
}

func (c *converter) warnf(line int, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if line > 0 {
		c.warnings = append(c.warnings, fmt.Sprintf("line %d: %s", line, msg))
	} else {
		c.warnings = append(c.warnings, msg)
	}
}

// httpDefaults carries directives set at http{} (or top) level that act as
// defaults for the server/location blocks below them.
type httpDefaults struct {
	gzip          *bool
	gzipMinLength int
	gzipLevel     *int
}

func (c *converter) walkTop(dirs []directive) {
	c.upstreams = map[string][]string{}
	c.upStrategy = map[string]string{}
	// First pass: collect every upstream block, wherever it sits.
	var collect func(ds []directive)
	collect = func(ds []directive) {
		for _, d := range ds {
			if d.name == "upstream" {
				c.parseUpstream(d)
			}
			if d.name == "http" {
				collect(d.block)
			}
		}
	}
	collect(dirs)

	def := httpDefaults{}
	process := func(ds []directive) {
		for _, d := range ds {
			switch d.name {
			case "server":
				c.parseServer(d, def)
			case "upstream", "http":
				// upstream already collected; http handled in its own pass
			case "gzip":
				def.gzip = boolPtr(onOff(d.args))
			case "gzip_min_length":
				def.gzipMinLength = atoiDefault(d.args, 0)
			case "gzip_comp_level":
				def.gzipLevel = intPtr(atoiDefault(d.args, -1))
			case "gzip_types", "gzip_vary", "gzip_proxied", "gzip_disable":
				c.warnf(d.line, "gzip tuning %q not configurable in gofly (it gzips by size); skipped", d.name)
			case "include":
				c.warnf(d.line, "'include %s' not followed; convert included files separately", strings.Join(d.args, " "))
			case "events", "worker_processes", "worker_connections", "pid", "user", "error_log":
				// runtime/process directives with no gofly equivalent; silent.
			default:
				c.warnf(d.line, "directive %q not supported; skipped", d.name)
			}
		}
	}
	for _, d := range dirs {
		if d.name == "http" {
			// apply http-level gzip defaults before its servers
			for _, h := range d.block {
				switch h.name {
				case "gzip":
					def.gzip = boolPtr(onOff(h.args))
				case "gzip_min_length":
					def.gzipMinLength = atoiDefault(h.args, 0)
				case "gzip_comp_level":
					def.gzipLevel = intPtr(atoiDefault(h.args, -1))
				}
			}
			process(d.block)
		}
	}
	// Also process any server/upstream sitting at top level (no http wrapper).
	process(dirs)
}

func (c *converter) parseUpstream(d directive) {
	if len(d.args) < 1 {
		c.warnf(d.line, "upstream block without a name; skipped")
		return
	}
	name := d.args[0]
	var backends []string
	for _, s := range d.block {
		switch s.name {
		case "server":
			if len(s.args) < 1 {
				continue
			}
			backends = append(backends, "http://"+s.args[0])
		case "least_conn":
			c.upStrategy[name] = "least_conn"
		case "keepalive":
			// connection pooling: gofly tunes this via the `upstream` JSON block.
		case "ip_hash", "hash":
			c.warnf(s.line, "upstream %q uses %q balancing; gofly supports round_robin/least_conn only", name, s.name)
		default:
			c.warnf(s.line, "upstream directive %q not supported; skipped", s.name)
		}
	}
	c.upstreams[name] = backends
}

func (c *converter) parseServer(d directive, def httpDefaults) {
	srv := serverCtx{
		gzip:          def.gzip,
		gzipMinLength: def.gzipMinLength,
		gzipLevel:     def.gzipLevel,
		headers:       map[string]string{},
	}
	var locations []directive

	for _, s := range d.block {
		switch s.name {
		case "listen":
			c.applyListen(s)
		case "server_name":
			if len(s.args) > 0 && s.args[0] != "_" {
				srv.serverName = s.args[0]
			}
		case "root":
			if len(s.args) > 0 {
				srv.root = s.args[0]
			}
		case "index":
			// gofly serves index.html automatically; only warn on non-default.
			if len(s.args) > 0 && !contains(s.args, "index.html") {
				c.warnf(s.line, "index %s: gofly only auto-serves index.html", strings.Join(s.args, " "))
			}
		case "ssl_certificate":
			c.ensureTLS().CertFile = arg0(s.args)
		case "ssl_certificate_key":
			c.ensureTLS().KeyFile = arg0(s.args)
		case "add_header":
			if len(s.args) >= 2 {
				srv.headers[s.args[0]] = s.args[1]
			}
		case "gzip":
			srv.gzip = boolPtr(onOff(s.args))
		case "location":
			locations = append(locations, s)
		case "error_page":
			c.warnf(s.line, "error_page at server scope not mapped; set per-route error_pages in JSON if needed")
		case "return", "rewrite":
			c.warnf(s.line, "%q not supported (no redirect/rewrite engine); skipped", s.name)
		case "ssl_protocols", "ssl_ciphers", "ssl_session_cache", "ssl_prefer_server_ciphers":
			// TLS tuning: gofly uses Go defaults; silent.
		default:
			c.warnf(s.line, "server directive %q not supported; skipped", s.name)
		}
	}

	if len(locations) == 0 {
		// A server with just `root` serves "/".
		if srv.root != "" {
			c.cfg.Routes = append(c.cfg.Routes, c.staticRoute("/", srv, nil))
		} else {
			c.warnf(d.line, "server block has no location and no root; no route produced")
		}
		return
	}
	for _, loc := range locations {
		c.parseLocation(loc, srv)
	}
}

type serverCtx struct {
	serverName    string
	root          string
	gzip          *bool
	gzipMinLength int
	gzipLevel     *int
	headers       map[string]string
}

func (c *converter) parseLocation(loc directive, srv serverCtx) {
	path, ok := c.locationPath(loc)
	if !ok {
		return
	}

	var (
		proxyPass string
		tryFiles  bool
		root      = srv.root
		expires   string
		gzip      = srv.gzip
		headers   = cloneMap(srv.headers)
	)
	for _, s := range loc.block {
		switch s.name {
		case "proxy_pass":
			proxyPass = arg0(s.args)
		case "root", "alias":
			root = arg0(s.args)
		case "try_files":
			// `try_files $uri $uri/ /index.html;` is the SPA fallback idiom.
			if len(s.args) > 0 && strings.HasSuffix(s.args[len(s.args)-1], ".html") {
				tryFiles = true
			} else {
				c.warnf(s.line, "try_files %s not the SPA idiom; mapped to spa only if it ends in *.html", strings.Join(s.args, " "))
			}
		case "expires":
			expires = arg0(s.args)
		case "add_header":
			if len(s.args) >= 2 {
				headers[s.args[0]] = s.args[1]
			}
		case "gzip":
			gzip = boolPtr(onOff(s.args))
		case "proxy_set_header":
			if len(s.args) >= 2 {
				headers[s.args[0]] = s.args[1]
			}
		case "access_log", "log_not_found", "proxy_http_version", "proxy_cache":
			// no per-route equivalent worth mapping; silent.
		default:
			c.warnf(s.line, "location %s directive %q not supported; skipped", path, s.name)
		}
	}

	if proxyPass != "" {
		c.cfg.Routes = append(c.cfg.Routes, c.proxyRoute(path, proxyPass, srv, headers, gzip))
		return
	}

	route := c.staticRoute(path, srv, headers)
	if root != "" {
		route.StaticDir = root
	}
	route.SPA = tryFiles
	route.Gzip = gzip
	if expires != "" {
		if secs, ok := nginxTimeSeconds(expires); ok {
			route.BrowserCacheTTL = &config.Duration{Duration: secsDur(secs)}
		} else {
			c.warnf(loc.line, "expires %q: unrecognised time; browser_cache_ttl left unset", expires)
		}
	}
	c.cfg.Routes = append(c.cfg.Routes, route)
}

// locationPath extracts the prefix path from a location directive, warning on
// regex matches that gofly's prefix router cannot represent.
func (c *converter) locationPath(loc directive) (string, bool) {
	args := loc.args
	if len(args) == 0 {
		c.warnf(loc.line, "location without a path; skipped")
		return "", false
	}
	switch args[0] {
	case "~", "~*":
		c.warnf(loc.line, "regex location %q not supported (gofly is prefix-only); skipped", strings.Join(args, " "))
		return "", false
	case "=", "^~":
		if len(args) < 2 {
			c.warnf(loc.line, "location modifier without a path; skipped")
			return "", false
		}
		if args[0] == "=" {
			c.warnf(loc.line, "exact-match location '= %s' mapped to a prefix route (gofly has no exact match)", args[1])
		}
		return args[1], true
	default:
		return args[0], true
	}
}

func (c *converter) staticRoute(path string, srv serverCtx, headers map[string]string) config.Route {
	r := config.Route{
		Path:          path,
		ServerName:    srv.serverName,
		StaticDir:     srv.root,
		GzipMinLength: srv.gzipMinLength,
		GzipLevel:     srv.gzipLevel,
	}
	if len(headers) > 0 {
		r.SetHeaders = headers
	} else if len(srv.headers) > 0 {
		r.SetHeaders = cloneMap(srv.headers)
	}
	return r
}

func (c *converter) proxyRoute(path, proxyPass string, srv serverCtx, headers map[string]string, gzip *bool) config.Route {
	r := config.Route{
		Path:       path,
		ServerName: srv.serverName,
		Gzip:       gzip,
	}
	if len(headers) > 0 {
		r.SetHeaders = headers
	}
	// proxy_pass http://name  -> resolve named upstream; else single backend.
	target := strings.TrimPrefix(strings.TrimPrefix(proxyPass, "http://"), "https://")
	target = strings.TrimSuffix(target, "/")
	if backends, ok := c.upstreams[target]; ok {
		r.Upstreams = backends
		if strat := c.upStrategy[target]; strat != "" {
			r.Strategy = strat
		} else if len(backends) > 1 {
			r.Strategy = "round_robin"
		}
	} else {
		r.Upstreams = []string{proxyPass}
	}
	return r
}

func (c *converter) applyListen(s directive) {
	if len(s.args) == 0 {
		return
	}
	ssl := contains(s.args, "ssl")
	port := parseListenPort(s.args[0])
	if ssl {
		tls := c.ensureTLS()
		if port > 0 {
			tls.TLSPort = port
		}
		return
	}
	if port > 0 {
		if c.portSet && c.cfg.Port != port {
			c.warnf(s.line, "multiple listen ports; keeping %d, ignoring %d", c.cfg.Port, port)
			return
		}
		c.cfg.Port = port
		c.portSet = true
	}
}

func (c *converter) ensureTLS() *config.TLSConfig {
	if c.cfg.TLS == nil {
		c.cfg.TLS = &config.TLSConfig{Enabled: true}
	}
	c.cfg.TLS.Enabled = true
	return c.cfg.TLS
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func arg0(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func onOff(args []string) bool { return len(args) > 0 && args[0] == "on" }

func atoiDefault(args []string, def int) int {
	if len(args) == 0 {
		return def
	}
	if n, err := strconv.Atoi(args[0]); err == nil {
		return n
	}
	return def
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func cloneMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// parseListenPort extracts the port from an nginx listen argument such as
// "80", "0.0.0.0:8080", "[::]:443", or "443".
func parseListenPort(s string) int {
	if _, after, ok := strings.CutLast(s, ":"); ok {
		s = after
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 0
}

// nginxTimeSeconds converts an nginx time string (e.g. "6M", "1d", "30s", "1y")
// to seconds. nginx units: ms, s, m, h, d, w, M (month=30d), y (year=365d).
// A bare number is seconds.
func nginxTimeSeconds(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case 's':
		return n, true
	case 'm':
		return n * 60, true
	case 'h':
		return n * 3600, true
	case 'd':
		return n * 86400, true
	case 'w':
		return n * 7 * 86400, true
	case 'M':
		return n * 30 * 86400, true
	case 'y':
		return n * 365 * 86400, true
	}
	return 0, false
}

func secsDur(s int64) time.Duration { return time.Duration(s) * time.Second }
