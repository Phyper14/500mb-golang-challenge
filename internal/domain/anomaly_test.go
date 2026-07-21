package domain

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pointWithAZ(az float64) Point {
	return Point{TS: 1, Lat: 0, Lon: 0, AX: 0, AY: 0, AZ: az}
}

func TestComputeAnomaly_InsufficientSamples(t *testing.T) {
	tests := []struct {
		name string
		give int
	}{
		{name: "zero points", give: 0},
		{name: "one point", give: 1},
		{name: "exactly one below minimum", give: MinAnomalySamples - 1},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			window := make([]Point, tt.give)
			for i := range window {
				window[i] = pointWithAZ(9.8)
			}
			_, ok := ComputeAnomaly(window)
			assert.False(t, ok)
		})
	}
}

func TestComputeAnomaly_MinimumSamplesBoundary(t *testing.T) {
	window := make([]Point, MinAnomalySamples)
	for i := range window {
		window[i] = pointWithAZ(9.8)
	}
	result, ok := ComputeAnomaly(window)
	require.True(t, ok)
	assert.Equal(t, MinAnomalySamples, result.Samples)
}

func TestComputeAnomaly_ConstantValues_ZeroStdDevAndZScore(t *testing.T) {
	window := make([]Point, 10)
	for i := range window {
		window[i] = pointWithAZ(9.8)
	}
	result, ok := ComputeAnomaly(window)
	require.True(t, ok)
	assert.InDelta(t, 0, result.StdDev, 1e-9)
	assert.InDelta(t, 0, result.ZScore, 1e-9)
	assert.False(t, result.Anomalous)
	assert.InDelta(t, 9.8, result.Mean, 1e-9)
}

func TestComputeAnomaly_KnownDistribution(t *testing.T) {
	// magnitudes: 1,2,3,4,5,6,7,8,9,10 (az values chosen so magnitude == az)
	window := make([]Point, 10)
	for i := 0; i < 10; i++ {
		window[i] = pointWithAZ(float64(i + 1))
	}
	result, ok := ComputeAnomaly(window)
	require.True(t, ok)

	// mean = 5.5; population variance = sum((x-mean)^2)/10 = 8.25; stddev = sqrt(8.25)
	wantMean := 5.5
	wantStdDev := math.Sqrt(8.25)
	wantZ := (10.0 - wantMean) / wantStdDev

	assert.InDelta(t, wantMean, result.Mean, 1e-9)
	assert.InDelta(t, wantStdDev, result.StdDev, 1e-9)
	assert.InDelta(t, wantZ, result.ZScore, 1e-9)
	assert.Equal(t, 10, result.Samples)
}

func TestComputeAnomaly_AnomalousSpike(t *testing.T) {
	// 255 calm points followed by one violent spike as the most recent.
	window := make([]Point, 256)
	for i := 0; i < 255; i++ {
		window[i] = pointWithAZ(9.8)
	}
	window[255] = pointWithAZ(9.8 + 100) // huge spike, most recent point

	result, ok := ComputeAnomaly(window)
	require.True(t, ok)
	assert.True(t, result.Anomalous)
	assert.Greater(t, math.Abs(result.ZScore), AnomalyZThreshold)
	assert.Equal(t, AnomalyWindow, result.Samples)
}

func TestComputeAnomaly_OnlyLastPointMatters(t *testing.T) {
	// A spike in the MIDDLE of the window should not make the *latest*
	// point anomalous if the latest point itself is close to the mean.
	window := make([]Point, 20)
	for i := range window {
		window[i] = pointWithAZ(9.8)
	}
	window[10] = pointWithAZ(500) // spike buried in the middle
	window[19] = pointWithAZ(9.8) // latest point is calm

	result, ok := ComputeAnomaly(window)
	require.True(t, ok)
	// The latest point equals the calm baseline; the buried spike inflates
	// stddev, so |z| for the latest point should be small (near/at zero
	// relative distance) and not flagged as anomalous.
	assert.False(t, result.Anomalous)
}

func TestComputeAnomaly_NegativeZScore(t *testing.T) {
	window := make([]Point, 10)
	for i := 0; i < 9; i++ {
		window[i] = pointWithAZ(10)
	}
	window[9] = pointWithAZ(0) // latest point far below the mean

	result, ok := ComputeAnomaly(window)
	require.True(t, ok)
	assert.Less(t, result.ZScore, 0.0)
}
