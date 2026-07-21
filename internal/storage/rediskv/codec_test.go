package rediskv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablo-martins/500mb-club-go/internal/domain"
)

func batteryPtr(v float64) *float64 { return &v }

func TestEncodeDecodePoint_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		give domain.Point
	}{
		{
			name: "without battery",
			give: domain.Point{TS: 1715800000000, Lat: -23.5505, Lon: -46.6333, AX: 0.11, AY: -0.04, AZ: 9.81},
		},
		{
			name: "with battery",
			give: domain.Point{TS: 1715800000100, Lat: 90, Lon: 180, Battery: batteryPtr(0.82), AX: 0, AY: 0, AZ: 0},
		},
		{
			name: "battery zero (must distinguish from unset)",
			give: domain.Point{TS: 1, Lat: -90, Lon: -180, Battery: batteryPtr(0), AX: -1.5, AY: -2.5, AZ: -3.5},
		},
		{
			name: "negative accelerations",
			give: domain.Point{TS: 42, Lat: 0, Lon: 0, AX: -9.81, AY: -9.81, AZ: -9.81},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encoded := EncodePoint(tt.give)
			assert.Len(t, encoded, pointEncodedLen)

			got, err := DecodePoint(encoded)
			require.NoError(t, err)

			assert.Equal(t, tt.give.TS, got.TS)
			assert.InDelta(t, tt.give.Lat, got.Lat, 1e-12)
			assert.InDelta(t, tt.give.Lon, got.Lon, 1e-12)
			assert.InDelta(t, tt.give.AX, got.AX, 1e-12)
			assert.InDelta(t, tt.give.AY, got.AY, 1e-12)
			assert.InDelta(t, tt.give.AZ, got.AZ, 1e-12)

			if tt.give.Battery == nil {
				assert.Nil(t, got.Battery)
			} else {
				require.NotNil(t, got.Battery)
				assert.InDelta(t, *tt.give.Battery, *got.Battery, 1e-12)
			}
		})
	}
}

func TestEncodePoint_NonceMakesIdenticalPointsUnique(t *testing.T) {
	p := domain.Point{TS: 100, Lat: 1, Lon: 2, AX: 3, AY: 4, AZ: 5}
	a := EncodePoint(p)
	b := EncodePoint(p)
	assert.NotEqual(t, a, b, "two encodings of the same point must differ (random nonce) to avoid ZSET member collisions")
}

func TestDecodePoint_ShortBuffer(t *testing.T) {
	_, err := DecodePoint([]byte{1, 2, 3})
	assert.ErrorIs(t, err, errShortBuffer)
}

func TestDecodePoint_IgnoresTrailingBytes(t *testing.T) {
	p := domain.Point{TS: 7, Lat: 1, Lon: 1, AX: 1, AY: 1, AZ: 1}
	encoded := EncodePoint(p)
	withTrailing := append(encoded, 0xFF, 0xFF, 0xFF)

	got, err := DecodePoint(withTrailing)
	require.NoError(t, err)
	assert.Equal(t, p.TS, got.TS)
}
