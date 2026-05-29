package metrics

import (
	"strings"
	"testing"
)

func TestObserveAndWrite(t *testing.T) {
	SetVersion("v9.9.9")
	Observe(200, 1024, 1_000_000)
	Observe(404, 0, 500_000)
	IncInFlight()
	defer DecInFlight()

	var sb strings.Builder
	WriteTo(&sb)
	out := sb.String()

	for _, want := range []string{
		`gofly_build_info{version="v9.9.9"} 1`,
		`gofly_requests_total{status_class="2xx"}`,
		`gofly_requests_total{status_class="4xx"}`,
		"gofly_requests_in_flight ",
		"gofly_response_bytes_total ",
		"gofly_request_duration_seconds_sum ",
		"gofly_request_duration_seconds_count ",
		"gofly_goroutines ",
		"gofly_heap_alloc_bytes ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q\n--- got ---\n%s", want, out)
		}
	}

	if !strings.Contains(out, "gofly_requests_in_flight 1") {
		t.Errorf("expected in-flight gauge of 1, got:\n%s", out)
	}
}

func TestEscapeLabel(t *testing.T) {
	cases := map[string]string{
		"plain":          "plain",
		`with"quote`:     `with\"quote`,
		`back\slash`:     `back\\slash`,
		"new\nline":      `new\nline`,
		"http://a:8080/": "http://a:8080/",
	}
	for in, want := range cases {
		if got := EscapeLabel(in); got != want {
			t.Errorf("EscapeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
