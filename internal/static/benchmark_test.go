package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rroblf01/gofly/internal/config"
)

// BenchmarkCacheHit measures the in-memory cache hot path (serveCached): the
// per-request cost and allocations once a file is resident. This is the path
// that dominates a many-distinct-files workload, where the cache avoids the
// per-request open()/fstat() of the streaming path.
func BenchmarkCacheHit(b *testing.B) {
	dir := b.TempDir()
	body := make([]byte, 798)
	for i := range body {
		body[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), body, 0o644); err != nil {
		b.Fatal(err)
	}

	ttl := config.Duration{}
	if err := ttl.UnmarshalJSON([]byte(`"60s"`)); err != nil {
		b.Fatal(err)
	}
	h := New(config.Route{Path: "/", StaticDir: dir, StaticCacheTTL: &ttl})

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	// Warm the cache so every measured iteration is a hit.
	h.ServeHTTP(httptest.NewRecorder(), req)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
