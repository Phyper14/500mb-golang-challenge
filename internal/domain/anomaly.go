package domain

import "math"

// AnomalyResult is the outcome of the z-score computation over a window
// of points.
type AnomalyResult struct {
	ZScore    float64
	Samples   int
	Anomalous bool
	Mean      float64
	StdDev    float64
}

// ComputeAnomaly computes the z-score of the magnitude of the most recent
// point in window against the mean/stddev of the whole window (up to
// AnomalyWindow points, oldest-to-newest order). It returns ok=false when
// there are fewer than MinAnomalySamples points.
//
// The magnitude of each point is sqrt(ax^2+ay^2+az^2). No caching is
// performed here — callers are expected to recompute this on every call,
// as required by the contract.
func ComputeAnomaly(window []Point) (AnomalyResult, bool) {
	n := len(window)
	if n < MinAnomalySamples {
		return AnomalyResult{}, false
	}

	mags := make([]float64, n)
	var sum float64
	for i, p := range window {
		m := p.Magnitude()
		mags[i] = m
		sum += m
	}
	mean := sum / float64(n)

	var variance float64
	for _, m := range mags {
		d := m - mean
		variance += d * d
	}
	variance /= float64(n)
	stddev := math.Sqrt(variance)

	latest := mags[n-1]

	// Guard against floating-point noise: summing near-identical magnitudes
	// can leave stddev as a tiny non-zero residual (e.g. 1e-15) instead of
	// an exact zero, which would otherwise blow up z = tiny/tiny into a
	// spurious value close to 1. Anything below the epsilon is treated as
	// "no variance" and yields z = 0.
	const stddevEpsilon = 1e-9

	var z float64
	if stddev > stddevEpsilon {
		z = (latest - mean) / stddev
	}

	return AnomalyResult{
		ZScore:    z,
		Samples:   n,
		Anomalous: math.Abs(z) > AnomalyZThreshold,
		Mean:      mean,
		StdDev:    stddev,
	}, true
}
