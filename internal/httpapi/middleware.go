package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/pablo-martins/500mb-club-go/internal/metrics"
)

// statusRecorder wraps http.ResponseWriter to capture the status code
// written by the handler, defaulting to 200 (mirrors net/http's own
// behavior when WriteHeader is never called explicitly).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// InstrumentMetrics wraps next with Prometheus counters/histograms
// labeled by the low-cardinality route pattern (r.Pattern, set by
// net/http.ServeMux for registered "METHOD /path" patterns) rather than
// the raw path, which would explode cardinality with device ids.
func InstrumentMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}

		metrics.HTTPRequestsTotal.WithLabelValues(
			route, r.Method, strconv.Itoa(rec.status),
		).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(route, r.Method).
			Observe(time.Since(start).Seconds())
	})
}
