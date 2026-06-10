package finance

import "github.com/vingrad/dynamic-decision-engine/internal/marketdata"

// Prior clamp bounds: the prior is a modest base-rate tilt, never a conviction
// call. Honesty framing per the package doc: a heuristic, not a calibrated
// forecast.
const (
	priorFloor = 0.35
	priorCeil  = 0.65
)

// WinProbPrior derives a heuristic win-probability prior from point-in-time
// fundamentals and trailing price momentum. Each component contributes a small,
// transparent tilt around the 0.5 base rate:
//
//   - valuation: cheap PE tilts up, expensive PE tilts down (neutral band 16–22)
//   - price/book: very cheap up, very rich down
//   - earnings quality: positive EPS slightly up, losses down
//   - momentum: trailing cumulative return beyond ±10% tilts with its sign
//
// It returns ok=false when neither fundamentals nor enough price history carry
// any information, so callers can keep the EV component neutral.
func WinProbPrior(f marketdata.Fundamentals, returns []float64) (float64, bool) {
	tilt := 0.0
	informed := false

	if f.PE > 0 {
		informed = true
		switch {
		case f.PE <= 12:
			tilt += 0.05
		case f.PE <= 16:
			tilt += 0.03
		case f.PE <= 22:
			// neutral band
		case f.PE <= 30:
			tilt -= 0.02
		default:
			tilt -= 0.05
		}
	}
	if f.PB > 0 {
		informed = true
		switch {
		case f.PB <= 1.5:
			tilt += 0.02
		case f.PB >= 4:
			tilt -= 0.02
		}
	}
	if f.EPS != 0 {
		informed = true
		if f.EPS > 0 {
			tilt += 0.01
		} else {
			tilt -= 0.04
		}
	}
	if cum, ok := cumulativeReturn(returns); ok {
		informed = true
		switch {
		case cum >= 0.10:
			tilt += 0.03
		case cum <= -0.10:
			tilt -= 0.03
		}
	}

	// A zero net tilt carries no information: reporting it as informed would
	// re-enable the volatility-scaled EV path under an effectively flat prior —
	// the exact pathology NeutralEVScore exists to prevent.
	if !informed || tilt == 0 {
		return 0, false
	}
	return clampRange(0.5+tilt, priorFloor, priorCeil), true
}

// cumulativeReturn compounds a return series; it needs at least two observations
// to say anything about momentum.
func cumulativeReturn(returns []float64) (float64, bool) {
	if len(returns) < 2 {
		return 0, false
	}
	cum := 1.0
	for _, r := range returns {
		cum *= 1 + r
	}
	return cum - 1, true
}

func clampRange(x, lo, hi float64) float64 {
	switch {
	case x < lo:
		return lo
	case x > hi:
		return hi
	default:
		return x
	}
}
