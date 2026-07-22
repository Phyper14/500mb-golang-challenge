// Package domain contains the core business rules of the telemetry
// ingestion service: point validation and anomaly detection. It has no
// dependency on HTTP or storage so it can be unit tested in isolation.
package domain

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
)

// MaxBatchPoints is the maximum number of points accepted in a single
// batch ingestion request (contract: docs/pt-br/api.md).
const MaxBatchPoints = 100

// AnomalyWindow is the number of most recent points considered by the
// anomaly z-score calculation (contract: docs/pt-br/api.md).
const AnomalyWindow = 256

// MinAnomalySamples is the minimum number of points required to compute
// an anomaly score; devices below this return "not enough samples".
const MinAnomalySamples = 8

// AnomalyZThreshold marks a point as anomalous when its z-score magnitude
// exceeds this value.
const AnomalyZThreshold = 3.0

var (
	// ErrMissingField is returned when a required field is absent from the payload.
	ErrMissingField = errors.New("missing required field")
	// ErrOutOfRange is returned when a field value is outside its valid range.
	ErrOutOfRange = errors.New("value out of range")
	// ErrNotFinite is returned when a numeric field is NaN or +/-Inf.
	ErrNotFinite = errors.New("value is not finite")
	// ErrInvalidDeviceID is returned when the device id does not match the
	// pattern required by the contract.
	ErrInvalidDeviceID = errors.New("invalid device id")
	// ErrEmptyBatch is returned when a batch payload contains zero points.
	ErrEmptyBatch = errors.New("batch must contain at least one point")
	// ErrBatchTooLarge is returned when a batch payload exceeds MaxBatchPoints.
	ErrBatchTooLarge = errors.New("batch exceeds maximum allowed points")
)

// deviceIDPattern mirrors the contract: ^[a-zA-Z0-9_-]{1,64}$
var deviceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ValidDeviceID reports whether id matches the contract pattern.
func ValidDeviceID(id string) bool {
	return deviceIDPattern.MatchString(id)
}

// Point is a single telemetry reading from a device.
type Point struct {
	TS      int64   `json:"ts"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Battery float64 `json:"-"` // Handled via custom marshaler
	HasBattery bool `json:"-"`
	AX      float64 `json:"ax"`
	AY      float64 `json:"ay"`
	AZ      float64 `json:"az"`
}

// MarshalJSON implements json.Marshaler.
func (p Point) MarshalJSON() ([]byte, error) {
	type Alias Point
	aux := struct {
		Alias
		Battery *float64 `json:"battery,omitempty"`
	}{
		Alias: (Alias)(p),
	}
	if p.HasBattery {
		aux.Battery = &p.Battery
	}
	return json.Marshal(aux)
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *Point) UnmarshalJSON(data []byte) error {
	type Alias Point
	aux := struct {
		Alias
		Battery *float64 `json:"battery,omitempty"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*p = Point(aux.Alias)
	if aux.Battery != nil {
		p.Battery = *aux.Battery
		p.HasBattery = true
	}
	return nil
}

// Magnitude returns sqrt(ax^2 + ay^2 + az^2).
func (p Point) Magnitude() float64 {
	return math.Sqrt(p.AX*p.AX + p.AY*p.AY + p.AZ*p.AZ)
}

// Validate checks every field of the point against the contract rules.
// ts must be positive; lat in [-90,90]; lon in [-180,180]; battery (if
// present) in [0,1]; ax/ay/az must be finite numbers.
func (p Point) Validate() error {
	if p.TS <= 0 {
		return ErrMissingField
	}
	if p.Lat < -90 || p.Lat > 90 {
		return ErrOutOfRange
	}
	if p.Lon < -180 || p.Lon > 180 {
		return ErrOutOfRange
	}
	if p.HasBattery && (p.Battery < 0 || p.Battery > 1) {
		return ErrOutOfRange
	}
	if !isFinite(p.AX) || !isFinite(p.AY) || !isFinite(p.AZ) {
		return ErrNotFinite
	}
	return nil
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// ValidateBatch checks batch-level cardinality and delegates per-point
// validation to Point.Validate. It returns ErrEmptyBatch/ErrBatchTooLarge
// for cardinality violations, or the first per-point validation error.
func ValidateBatch(points []Point) error {
	if len(points) == 0 {
		return ErrEmptyBatch
	}
	if len(points) > MaxBatchPoints {
		return ErrBatchTooLarge
	}
	for _, p := range points {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}
