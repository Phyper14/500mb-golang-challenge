package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablo-martins/500mb-club-go/internal/domain"
	"github.com/pablo-martins/500mb-club-go/internal/storagetest"
)

func newTestServer(t *testing.T) (*http.ServeMux, *storagetest.FakeStore) {
	t.Helper()
	store := storagetest.NewFakeStore()
	srv := NewServer(store, "test-instance")
	mux := http.NewServeMux()
	srv.Routes(mux)
	return mux, store
}

func doRequest(mux *http.ServeMux, method, target string, body []byte) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func validPointJSON() []byte {
	p := map[string]any{
		"ts": 1715800000000, "lat": -23.5505, "lon": -46.6333,
		"battery": 0.82, "ax": 0.11, "ay": -0.04, "az": 9.81,
	}
	b, _ := json.Marshal(p)
	return b
}

// ---------------------------------------------------------------------
// healthz / readyz
// ---------------------------------------------------------------------

func TestHandleHealthz(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/healthz", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test-instance", rec.Header().Get("X-Instance-Id"))
	assert.Contains(t, rec.Body.String(), "ok")
}

func TestHandleReadyz_Ready(t *testing.T) {
	mux, store := newTestServer(t)
	store.SetPingError(nil)

	rec := doRequest(mux, "GET", "/readyz", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test-instance", rec.Header().Get("X-Instance-Id"))
}

func TestHandleReadyz_NotReady(t *testing.T) {
	mux, store := newTestServer(t)
	store.SetPingError(errors.New("boom"))

	rec := doRequest(mux, "GET", "/readyz", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "test-instance", rec.Header().Get("X-Instance-Id"))
}

// ---------------------------------------------------------------------
// POST /devices/{id}/telemetry
// ---------------------------------------------------------------------

func TestHandlePostTelemetry_Accepted(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry", validPointJSON())

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, "test-instance", rec.Header().Get("X-Instance-Id"))
}

func TestHandlePostTelemetry_InvalidDeviceID(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/has%20space/telemetry", validPointJSON())
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetry_MalformedJSON(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry", []byte("{not json"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetry_MissingRequiredField(t *testing.T) {
	mux, _ := newTestServer(t)
	body := []byte(`{"lat":0,"lon":0,"ax":0,"ay":0,"az":0}`) // ts missing -> ts=0 -> invalid
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetry_OutOfRange(t *testing.T) {
	mux, _ := newTestServer(t)
	body := []byte(`{"ts":1,"lat":200,"lon":0,"ax":0,"ay":0,"az":0}`)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetry_TooLargeBody(t *testing.T) {
	mux, _ := newTestServer(t)
	huge := bytes.Repeat([]byte("a"), MaxBodyBytes+10)
	body := append([]byte(`{"ts":1,"lat":0,"lon":0,"ax":0,"ay":0,"az":0,"pad":"`), huge...)
	body = append(body, []byte(`"}`)...)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry", body)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestHandlePostTelemetry_StorageError(t *testing.T) {
	mux, store := newTestServer(t)
	store.FailNextWrite(errors.New("boom"))
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry", validPointJSON())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------
// POST /devices/{id}/telemetry/batch
// ---------------------------------------------------------------------

func batchJSON(n int) []byte {
	points := make([]map[string]any, n)
	for i := range points {
		points[i] = map[string]any{
			"ts": 1000 + i, "lat": 0.0, "lon": 0.0, "ax": 0.0, "ay": 0.0, "az": 9.8,
		}
	}
	b, _ := json.Marshal(map[string]any{"points": points})
	return b
}

func TestHandlePostTelemetryBatch_Accepted(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(2))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	var resp struct {
		Accepted int `json:"accepted"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Accepted)
}

func TestHandlePostTelemetryBatch_Empty(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", []byte(`{"points":[]}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetryBatch_ExceedsLimit(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(domain.MaxBatchPoints+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestHandlePostTelemetryBatch_ExactlyMaxIsAccepted(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(domain.MaxBatchPoints))
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestHandlePostTelemetryBatch_MalformedJSON(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", []byte("{not json"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetryBatch_InvalidPoint(t *testing.T) {
	mux, _ := newTestServer(t)
	body := []byte(`{"points":[{"ts":1,"lat":999,"lon":0,"ax":0,"ay":0,"az":0}]}`)
	rec := doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePostTelemetryBatch_InvalidDeviceID(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "POST", "/devices/bad%20id/telemetry/batch", batchJSON(1))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------
// GET /devices/{id}/telemetry
// ---------------------------------------------------------------------

func TestHandleGetTelemetry_ReturnsIngestedPoints(t *testing.T) {
	mux, _ := newTestServer(t)
	doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(5))

	rec := doRequest(mux, "GET", "/devices/dev-1/telemetry?from=0&to=999999999999999", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Points     []map[string]any `json:"points"`
		NextCursor *string          `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Points, 5)
	assert.Nil(t, resp.NextCursor)
}

func TestHandleGetTelemetry_MissingFrom(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/dev-1/telemetry?to=100", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetTelemetry_MissingTo(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/dev-1/telemetry?from=0", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetTelemetry_FromGreaterThanTo(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/dev-1/telemetry?from=200&to=100", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetTelemetry_LimitOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		limit string
	}{
		{name: "zero", limit: "0"},
		{name: "negative", limit: "-1"},
		{name: "above max", limit: "501"},
		{name: "not a number", limit: "abc"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			mux, _ := newTestServer(t)
			rec := doRequest(mux, "GET", "/devices/dev-1/telemetry?from=0&to=100&limit="+tt.limit, nil)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestHandleGetTelemetry_EmptyResultIs200(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/never-seen/telemetry?from=0&to=100", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Points []map[string]any `json:"points"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Points)
}

func TestHandleGetTelemetry_Pagination(t *testing.T) {
	mux, _ := newTestServer(t)
	doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(10))

	rec1 := doRequest(mux, "GET", "/devices/dev-1/telemetry?from=0&to=999999999999999&limit=4", nil)
	require.Equal(t, http.StatusOK, rec1.Code)
	var page1 struct {
		Points     []map[string]any `json:"points"`
		NextCursor *string          `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &page1))
	require.Len(t, page1.Points, 4)
	require.NotNil(t, page1.NextCursor)

	rec2 := doRequest(mux, "GET", "/devices/dev-1/telemetry?from=0&to=999999999999999&limit=4&cursor="+*page1.NextCursor, nil)
	require.Equal(t, http.StatusOK, rec2.Code)
	var page2 struct {
		Points []map[string]any `json:"points"`
	}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &page2))
	assert.NotEqual(t, page1.Points[0]["ts"], page2.Points[0]["ts"])
}

func TestHandleGetTelemetry_InvalidDeviceID(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/bad%20id/telemetry?from=0&to=100", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------
// GET /devices/{id}/anomaly
// ---------------------------------------------------------------------

func TestHandleGetAnomaly_InsufficientSamples(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/dev-1/anomaly", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetAnomaly_EnoughSamples(t *testing.T) {
	mux, _ := newTestServer(t)
	doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(domain.MinAnomalySamples))

	rec := doRequest(mux, "GET", "/devices/dev-1/anomaly", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		ZScore    float64 `json:"z_score"`
		Samples   int     `json:"samples"`
		Anomalous bool    `json:"anomalous"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, domain.MinAnomalySamples, resp.Samples)
}

func TestHandleGetAnomaly_InvalidDeviceID(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/devices/bad%20id/anomaly", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------
// GET /metrics
// ---------------------------------------------------------------------

func TestHandleMetrics_OK(t *testing.T) {
	mux, _ := newTestServer(t)
	rec := doRequest(mux, "GET", "/metrics", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test-instance", rec.Header().Get("X-Instance-Id"))
}

// ---------------------------------------------------------------------
// X-Instance-Id on every response (contract requirement)
// ---------------------------------------------------------------------

func TestEveryResponse_HasInstanceIDHeader(t *testing.T) {
	mux, _ := newTestServer(t)

	responses := []*httptest.ResponseRecorder{
		doRequest(mux, "GET", "/healthz", nil),
		doRequest(mux, "GET", "/readyz", nil),
		doRequest(mux, "GET", "/metrics", nil),
		doRequest(mux, "POST", "/devices/dev-1/telemetry", validPointJSON()),
		doRequest(mux, "POST", "/devices/dev-1/telemetry/batch", batchJSON(1)),
		doRequest(mux, "GET", "/devices/dev-1/telemetry?from=0&to=999999999999999", nil),
		doRequest(mux, "GET", "/devices/dev-1/anomaly", nil),
	}

	for i, rec := range responses {
		assert.NotEmpty(t, rec.Header().Get("X-Instance-Id"), "response %d missing X-Instance-Id", i)
	}
}
