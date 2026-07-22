// Package httpapi wires the telemetry HTTP contract (docs/pt-br/api.md)
// on top of a storage.Store, using only the standard library router
// (net/http.ServeMux, Go 1.22+ method+path patterns) to keep the
// footprint and allocation profile as small as possible.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Phyper14/500mb-golang-challenge/internal/domain"
	"github.com/Phyper14/500mb-golang-challenge/internal/metrics"
	"github.com/Phyper14/500mb-golang-challenge/internal/storage"
)

// MaxBodyBytes caps the size of any request body. Larger bodies are
// rejected with 413 before JSON decoding even starts.
const MaxBodyBytes = 64 * 1024 // 64 KiB: generous for a 100-point batch

// DefaultRangeLimit and MaxRangeLimit bound the `limit` query parameter
// of GET /devices/{id}/telemetry, per the contract.
const (
	DefaultRangeLimit = 100
	MaxRangeLimit     = 500
	MinRangeLimit     = 1
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	store      storage.Store
	instanceID string
}

// NewServer builds a Server. instanceID is echoed back on every response
// via the X-Instance-Id header, as required by the contract.
func NewServer(store storage.Store, instanceID string) *Server {
	return &Server{store: store, instanceID: instanceID}
}

// Routes registers every contract endpoint on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("POST /devices/{id}/telemetry", s.handlePostTelemetry)
	mux.HandleFunc("GET /devices/{id}/telemetry", s.handleGetTelemetry)
	mux.HandleFunc("POST /devices/{id}/telemetry/batch", s.handlePostTelemetryBatch)
	mux.HandleFunc("GET /devices/{id}/anomaly", s.handleGetAnomaly)
}

// metricsHandler is the shared Prometheus exposition handler. It's
// stateless and safe to reuse across requests/instances.
var metricsHandler = promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})

// handleMetrics exposes Prometheus metrics, adding the contract-mandated
// X-Instance-Id header that promhttp's handler wouldn't set on its own.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)
	metricsHandler.ServeHTTP(w, r)
}

// handleHealthz is a pure liveness probe: it never touches storage.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadyz checks storage connectivity.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)
	if err := s.store.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// telemetryRequest mirrors domain.Point's wire format exactly.
type telemetryRequest struct {
	TS      int64    `json:"ts"`
	Lat     float64  `json:"lat"`
	Lon     float64  `json:"lon"`
	Battery *float64 `json:"battery,omitempty"`
	AX      float64  `json:"ax"`
	AY      float64  `json:"ay"`
	AZ      float64  `json:"az"`
}

func (r telemetryRequest) toPoint() domain.Point {
	p := domain.Point{
		TS: r.TS, Lat: r.Lat, Lon: r.Lon,
		AX: r.AX, AY: r.AY, AZ: r.AZ,
	}
	if r.Battery != nil {
		p.Battery = *r.Battery
		p.HasBattery = true
	}
	return p
}

// handlePostTelemetry ingests a single point.
func (s *Server) handlePostTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)

	id := r.PathValue("id")
	if !domain.ValidDeviceID(id) {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	body, ok := readLimitedBody(w, r)
	if !ok {
		return
	}

	var req telemetryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	point := req.toPoint()
	if err := point.Validate(); err != nil {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	if err := s.store.InsertPoint(r.Context(), id, point); err != nil {
		writeStatus(w, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

type telemetryBatchRequest struct {
	Points []telemetryRequest `json:"points"`
}

// batchResponsePrefix/-Suffix let us hand-encode {"accepted":N} without
// paying for encoding/json reflection on the hottest write path's
// sibling endpoint.
const batchResponsePrefix = `{"accepted":`
const batchResponseSuffix = `}`

// handlePostTelemetryBatch ingests 1..MaxBatchPoints points in one call.
func (s *Server) handlePostTelemetryBatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)

	id := r.PathValue("id")
	if !domain.ValidDeviceID(id) {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	body, ok := readLimitedBody(w, r)
	if !ok {
		return
	}

	var req telemetryBatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	if len(req.Points) > domain.MaxBatchPoints {
		writeStatus(w, http.StatusRequestEntityTooLarge)
		return
	}

	points := make([]domain.Point, len(req.Points))
	for i, pr := range req.Points {
		points[i] = pr.toPoint()
	}

	if err := domain.ValidateBatch(points); err != nil {
		if errors.Is(err, domain.ErrBatchTooLarge) {
			writeStatus(w, http.StatusRequestEntityTooLarge)
			return
		}
		writeStatus(w, http.StatusBadRequest)
		return
	}

	accepted, err := s.store.InsertBatch(r.Context(), id, points)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	var buf [32]byte
	b := strconv.AppendInt(buf[:0], int64(accepted), 10)
	_, _ = w.Write([]byte(batchResponsePrefix))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte(batchResponseSuffix))
}

// telemetryPointResponse and rangeResponse mirror the wire format of GET
// /devices/{id}/telemetry.
type telemetryPointResponse struct {
	TS      int64    `json:"ts"`
	Lat     float64  `json:"lat"`
	Lon     float64  `json:"lon"`
	Battery *float64 `json:"battery,omitempty"`
	AX      float64  `json:"ax"`
	AY      float64  `json:"ay"`
	AZ      float64  `json:"az"`
}

type rangeResponse struct {
	Points     []telemetryPointResponse `json:"points"`
	NextCursor *string                  `json:"next_cursor"`
}

// handleGetTelemetry queries a device's points within a time window.
func (s *Server) handleGetTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)

	id := r.PathValue("id")
	if !domain.ValidDeviceID(id) {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	q := r.URL.Query()

	from, ok := parseRequiredInt64(q.Get("from"))
	if !ok {
		writeStatus(w, http.StatusBadRequest)
		return
	}
	to, ok := parseRequiredInt64(q.Get("to"))
	if !ok {
		writeStatus(w, http.StatusBadRequest)
		return
	}
	if from > to {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	limit := DefaultRangeLimit
	if raw := q.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < MinRangeLimit || parsed > MaxRangeLimit {
			writeStatus(w, http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	cursor := q.Get("cursor")

	page, err := s.store.QueryRange(r.Context(), id, from, to, limit, cursor)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidCursor) {
			writeStatus(w, http.StatusBadRequest)
			return
		}
		writeStatus(w, http.StatusInternalServerError)
		return
	}

	resp := rangeResponse{Points: make([]telemetryPointResponse, len(page.Points))}
	for i, p := range page.Points {
		resp.Points[i] = telemetryPointResponse{
			TS: p.TS, Lat: p.Lat, Lon: p.Lon,
			AX: p.AX, AY: p.AY, AZ: p.AZ,
		}
		if p.HasBattery {
			resp.Points[i].Battery = &p.Battery
		}
	}
	if page.NextCursor != "" {
		resp.NextCursor = &page.NextCursor
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if body, jsonErr := json.Marshal(resp); jsonErr == nil {
		_, _ = w.Write(body)
	}
}

type anomalyResponse struct {
	ZScore    float64 `json:"z_score"`
	Samples   int     `json:"samples"`
	Anomalous bool    `json:"anomalous"`
	Mean      float64 `json:"mean"`
	StdDev    float64 `json:"stddev"`
}

// handleGetAnomaly computes the acceleration z-score over the device's
// most recent window. Per the contract, no caching is allowed: this is
// always recomputed from the raw window on every call.
func (s *Server) handleGetAnomaly(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-Id", s.instanceID)

	id := r.PathValue("id")
	if !domain.ValidDeviceID(id) {
		writeStatus(w, http.StatusBadRequest)
		return
	}

	window, err := s.store.RecentWindow(r.Context(), id)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError)
		return
	}

	result, ok := domain.ComputeAnomaly(window)
	if !ok {
		writeStatus(w, http.StatusNotFound)
		return
	}

	resp := anomalyResponse{
		ZScore: result.ZScore, Samples: result.Samples,
		Anomalous: result.Anomalous, Mean: result.Mean, StdDev: result.StdDev,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if body, jsonErr := json.Marshal(resp); jsonErr == nil {
		_, _ = w.Write(body)
	}
}

// readLimitedBody reads the request body up to MaxBodyBytes+1. If the
// body exceeds the limit it writes 413 and returns ok=false. Callers
// must not write to w after ok=false.
func readLimitedBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	limited := io.LimitReader(r.Body, MaxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		writeStatus(w, http.StatusBadRequest)
		return nil, false
	}
	if len(body) > MaxBodyBytes {
		writeStatus(w, http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return body, true
}

// parseRequiredInt64 parses a required int64 query parameter; ok=false
// when raw is empty or not a valid integer.
func parseRequiredInt64(raw string) (int64, bool) {
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func writeStatus(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}
