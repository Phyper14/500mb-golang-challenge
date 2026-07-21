// Package metrics defines the Prometheus instrumentation exposed on
// GET /metrics. Kept intentionally small: cardinality is bounded by
// route (a fixed, known set) and status class, never by device id.
package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Registry is the process-wide Prometheus registry used by this service.
// A dedicated registry (instead of the global default) keeps /metrics
// free of collectors we don't opt into explicitly.
var Registry = newRegistry()

// newRegistry builds the registry and attaches the standard Go/process
// collectors (RSS, CPU, GC stats), which feed the benchmark's efficiency
// dimension. Kept as a constructor function rather than init() so the
// wiring stays explicit and independent of package initialization order.
func newRegistry() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return r
}

var (
	// HTTPRequestsTotal counts requests by route, method and status class.
	HTTPRequestsTotal = promauto.With(Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests processed, labeled by route/method/status.",
		},
		[]string{"route", "method", "status"},
	)

	// HTTPRequestDuration measures request latency in seconds, labeled by
	// route/method. Buckets are tuned for a sub-25ms p99 target (contract
	// SLOs are 8-25ms) while still covering slow outliers.
	HTTPRequestDuration = promauto.With(Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, labeled by route/method.",
			Buckets: []float64{.001, .002, .004, .008, .016, .025, .05, .1, .25, .5, 1, 2.5},
		},
		[]string{"route", "method"},
	)
)

// StatusClass returns the "2xx"/"4xx"/"5xx" bucket for a status code,
// keeping the status label's cardinality bounded regardless of how many
// distinct codes the handlers ever return.
func StatusClass(code int) string {
	return strconv.Itoa(code/100) + "xx"
}
