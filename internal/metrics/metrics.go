// Package metrics provides a tiny, dependency-free metrics registry exposed in
// the Prometheus text exposition format. It tracks process-wide HTTP counters
// updated on the request hot path with atomics, plus a snapshot of Go runtime
// stats taken at scrape time.
package metrics

import (
	"io"
	"runtime"
	"strconv"
	"sync/atomic"
)

// requestsByClass counts responses bucketed by status class. Index 0 holds
// anything outside 1xx-5xx; indexes 1..5 hold 1xx..5xx.
var (
	requestsByClass  [6]atomic.Uint64
	responseBytes    atomic.Uint64
	requestsInFlight atomic.Int64
	durationNanos    atomic.Uint64
	requestCount     atomic.Uint64
)

var buildVersion atomic.Value // string

func init() { buildVersion.Store("dev") }

// SetVersion records the build version reported by gofly_build_info.
func SetVersion(v string) { buildVersion.Store(v) }

func version() string {
	if v, ok := buildVersion.Load().(string); ok {
		return v
	}
	return "dev"
}

func classIndex(status int) int {
	c := status / 100
	if c < 1 || c > 5 {
		return 0
	}
	return c
}

// IncInFlight / DecInFlight bracket an in-progress request.
func IncInFlight() { requestsInFlight.Add(1) }
func DecInFlight() { requestsInFlight.Add(-1) }

// Observe records a completed request.
func Observe(status int, bytes, durNanos int64) {
	requestsByClass[classIndex(status)].Add(1)
	requestCount.Add(1)
	if bytes > 0 {
		responseBytes.Add(uint64(bytes))
	}
	if durNanos > 0 {
		durationNanos.Add(uint64(durNanos))
	}
}

// WriteTo emits the base metric set in Prometheus text exposition format.
// Callers may append additional lines (e.g. per-upstream gauges) afterwards.
func WriteTo(w io.Writer) {
	var b []byte

	b = append(b, "# HELP gofly_build_info Build version, always 1.\n"...)
	b = append(b, "# TYPE gofly_build_info gauge\n"...)
	b = append(b, `gofly_build_info{version="`...)
	b = append(b, escapeLabel(version())...)
	b = append(b, "\"} 1\n"...)

	b = append(b, "# HELP gofly_requests_total Total HTTP responses by status class.\n"...)
	b = append(b, "# TYPE gofly_requests_total counter\n"...)
	classes := [6]string{"other", "1xx", "2xx", "3xx", "4xx", "5xx"}
	for i, name := range classes {
		b = append(b, `gofly_requests_total{status_class="`...)
		b = append(b, name...)
		b = append(b, `"} `...)
		b = strconv.AppendUint(b, requestsByClass[i].Load(), 10)
		b = append(b, '\n')
	}

	b = append(b, "# HELP gofly_requests_in_flight Requests currently being served.\n"...)
	b = append(b, "# TYPE gofly_requests_in_flight gauge\n"...)
	b = append(b, "gofly_requests_in_flight "...)
	b = strconv.AppendInt(b, requestsInFlight.Load(), 10)
	b = append(b, '\n')

	b = append(b, "# HELP gofly_response_bytes_total Total response body bytes written.\n"...)
	b = append(b, "# TYPE gofly_response_bytes_total counter\n"...)
	b = append(b, "gofly_response_bytes_total "...)
	b = strconv.AppendUint(b, responseBytes.Load(), 10)
	b = append(b, '\n')

	b = append(b, "# HELP gofly_request_duration_seconds_sum Cumulative request duration.\n"...)
	b = append(b, "# TYPE gofly_request_duration_seconds_sum counter\n"...)
	b = append(b, "gofly_request_duration_seconds_sum "...)
	b = strconv.AppendFloat(b, float64(durationNanos.Load())/1e9, 'f', 6, 64)
	b = append(b, '\n')
	b = append(b, "gofly_request_duration_seconds_count "...)
	b = strconv.AppendUint(b, requestCount.Load(), 10)
	b = append(b, '\n')

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	b = append(b, "# HELP gofly_goroutines Number of goroutines.\n"...)
	b = append(b, "# TYPE gofly_goroutines gauge\n"...)
	b = append(b, "gofly_goroutines "...)
	b = strconv.AppendInt(b, int64(runtime.NumGoroutine()), 10)
	b = append(b, '\n')
	b = append(b, "# HELP gofly_heap_alloc_bytes Heap bytes currently allocated.\n"...)
	b = append(b, "# TYPE gofly_heap_alloc_bytes gauge\n"...)
	b = append(b, "gofly_heap_alloc_bytes "...)
	b = strconv.AppendUint(b, ms.HeapAlloc, 10)
	b = append(b, '\n')
	b = append(b, "# HELP gofly_heap_sys_bytes Heap bytes obtained from the OS.\n"...)
	b = append(b, "# TYPE gofly_heap_sys_bytes gauge\n"...)
	b = append(b, "gofly_heap_sys_bytes "...)
	b = strconv.AppendUint(b, ms.HeapSys, 10)
	b = append(b, '\n')

	w.Write(b)
}

// EscapeLabel escapes a Prometheus label value (\, ", newline) so callers can
// safely interpolate dynamic strings such as upstream URLs.
func EscapeLabel(s string) string { return escapeLabel(s) }

// escapeLabel escapes a Prometheus label value (\, ", newline).
func escapeLabel(s string) string {
	if !needsEscape(s) {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '"':
			b = append(b, '\\', s[i])
		case '\n':
			b = append(b, '\\', 'n')
		default:
			b = append(b, s[i])
		}
	}
	return string(b)
}

func needsEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' || s[i] == '"' || s[i] == '\n' {
			return true
		}
	}
	return false
}
