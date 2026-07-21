package domain

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func battery(v float64) *float64 { return &v }

func TestValidDeviceID(t *testing.T) {
	tests := []struct {
		give string
		want bool
	}{
		{give: "dev-1", want: true},
		{give: "dev_1", want: true},
		{give: "ABC123", want: true},
		{give: "a", want: true},
		{give: "", want: false},
		{give: "has space", want: false},
		{give: "has/slash", want: false},
		{give: "x", want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.give, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ValidDeviceID(tt.give))
		})
	}

	t.Run("too long (65 chars)", func(t *testing.T) {
		t.Parallel()
		long := make([]byte, 65)
		for i := range long {
			long[i] = 'a'
		}
		assert.False(t, ValidDeviceID(string(long)))
	})

	t.Run("exactly 64 chars is valid", func(t *testing.T) {
		t.Parallel()
		s := make([]byte, 64)
		for i := range s {
			s[i] = 'a'
		}
		assert.True(t, ValidDeviceID(string(s)))
	})
}

func TestPoint_Magnitude(t *testing.T) {
	tests := []struct {
		name string
		give Point
		want float64
	}{
		{name: "zero vector", give: Point{AX: 0, AY: 0, AZ: 0}, want: 0},
		{name: "unit x", give: Point{AX: 1, AY: 0, AZ: 0}, want: 1},
		{name: "3-4-0 triangle", give: Point{AX: 3, AY: 4, AZ: 0}, want: 5},
		{name: "gravity at rest", give: Point{AX: 0, AY: 0, AZ: 9.8}, want: 9.8},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tt.want, tt.give.Magnitude(), 1e-9)
		})
	}
}

func TestPoint_Validate(t *testing.T) {
	tests := []struct {
		name    string
		give    Point
		wantErr error
	}{
		{
			name:    "valid point without battery",
			give:    Point{TS: 1, Lat: -23.5, Lon: -46.6, AX: 0.1, AY: 0.2, AZ: 9.8},
			wantErr: nil,
		},
		{
			name:    "valid point with battery",
			give:    Point{TS: 1, Lat: 0, Lon: 0, Battery: battery(0.5), AX: 0, AY: 0, AZ: 0},
			wantErr: nil,
		},
		{
			name:    "ts zero",
			give:    Point{TS: 0, Lat: 0, Lon: 0, AX: 0, AY: 0, AZ: 0},
			wantErr: ErrMissingField,
		},
		{
			name:    "ts negative",
			give:    Point{TS: -1, Lat: 0, Lon: 0, AX: 0, AY: 0, AZ: 0},
			wantErr: ErrMissingField,
		},
		{
			name:    "lat above range",
			give:    Point{TS: 1, Lat: 90.1, Lon: 0, AX: 0, AY: 0, AZ: 0},
			wantErr: ErrOutOfRange,
		},
		{
			name:    "lat below range",
			give:    Point{TS: 1, Lat: -90.1, Lon: 0, AX: 0, AY: 0, AZ: 0},
			wantErr: ErrOutOfRange,
		},
		{
			name:    "lat boundary valid (90)",
			give:    Point{TS: 1, Lat: 90, Lon: 0, AX: 0, AY: 0, AZ: 0},
			wantErr: nil,
		},
		{
			name:    "lon above range",
			give:    Point{TS: 1, Lat: 0, Lon: 180.1, AX: 0, AY: 0, AZ: 0},
			wantErr: ErrOutOfRange,
		},
		{
			name:    "lon below range",
			give:    Point{TS: 1, Lat: 0, Lon: -180.1, AX: 0, AY: 0, AZ: 0},
			wantErr: ErrOutOfRange,
		},
		{
			name:    "battery above range",
			give:    Point{TS: 1, Lat: 0, Lon: 0, Battery: battery(1.1), AX: 0, AY: 0, AZ: 0},
			wantErr: ErrOutOfRange,
		},
		{
			name:    "battery below range",
			give:    Point{TS: 1, Lat: 0, Lon: 0, Battery: battery(-0.1), AX: 0, AY: 0, AZ: 0},
			wantErr: ErrOutOfRange,
		},
		{
			name:    "battery boundary zero valid",
			give:    Point{TS: 1, Lat: 0, Lon: 0, Battery: battery(0), AX: 0, AY: 0, AZ: 0},
			wantErr: nil,
		},
		{
			name:    "ax NaN",
			give:    Point{TS: 1, Lat: 0, Lon: 0, AX: math.NaN(), AY: 0, AZ: 0},
			wantErr: ErrNotFinite,
		},
		{
			name:    "ay +Inf",
			give:    Point{TS: 1, Lat: 0, Lon: 0, AX: 0, AY: math.Inf(1), AZ: 0},
			wantErr: ErrNotFinite,
		},
		{
			name:    "az -Inf",
			give:    Point{TS: 1, Lat: 0, Lon: 0, AX: 0, AY: 0, AZ: math.Inf(-1)},
			wantErr: ErrNotFinite,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.give.Validate()
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.True(t, errors.Is(err, tt.wantErr))
		})
	}
}

func TestValidateBatch(t *testing.T) {
	validPoint := Point{TS: 1, Lat: 0, Lon: 0, AX: 0, AY: 0, AZ: 0}

	t.Run("empty batch", func(t *testing.T) {
		err := ValidateBatch(nil)
		assert.True(t, errors.Is(err, ErrEmptyBatch))
	})

	t.Run("single valid point", func(t *testing.T) {
		err := ValidateBatch([]Point{validPoint})
		assert.NoError(t, err)
	})

	t.Run("exactly max points is valid", func(t *testing.T) {
		points := make([]Point, MaxBatchPoints)
		for i := range points {
			points[i] = validPoint
		}
		err := ValidateBatch(points)
		assert.NoError(t, err)
	})

	t.Run("exceeds max points", func(t *testing.T) {
		points := make([]Point, MaxBatchPoints+1)
		for i := range points {
			points[i] = validPoint
		}
		err := ValidateBatch(points)
		assert.True(t, errors.Is(err, ErrBatchTooLarge))
	})

	t.Run("one invalid point in the middle fails the whole batch", func(t *testing.T) {
		points := []Point{validPoint, {TS: 0}, validPoint}
		err := ValidateBatch(points)
		assert.True(t, errors.Is(err, ErrMissingField))
	})
}
