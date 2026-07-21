package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/pablo-martins/500mb-club-go/internal/metrics"
)

func TestInstrumentMetrics_RecordsRequestWithRoutePattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /devices/{id}/telemetry", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := InstrumentMetrics(mux)

	req := httptest.NewRequest("GET", "/devices/dev-1/telemetry", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	count := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(
		"GET /devices/{id}/telemetry", "GET", "200",
	))
	assert.Equal(t, float64(1), count)
}

func TestInstrumentMetrics_DefaultsToUnmatchedRoute(t *testing.T) {
	mux := http.NewServeMux() // no routes registered
	wrapped := InstrumentMetrics(mux)

	req := httptest.NewRequest("GET", "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	count := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(
		"unmatched", "GET", "404",
	))
	assert.Equal(t, float64(1), count)
}

func TestInstrumentMetrics_CapturesNonDefaultStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /devices/{id}/telemetry", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	wrapped := InstrumentMetrics(mux)

	req := httptest.NewRequest("POST", "/devices/dev-1/telemetry", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	count := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(
		"POST /devices/{id}/telemetry", "POST", "202",
	))
	assert.Equal(t, float64(1), count)
}
