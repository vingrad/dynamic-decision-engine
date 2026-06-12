package finance

import "github.com/vingrad/dynamic-decision-engine/internal/marketdata"

// Prior clamp bounds: the prior is a modest base-rate tilt, never a conviction
// call. Honesty framing per the package doc: a heuristic, not a calibrated
// forecast.
const (
	priorFloor = 0.35
	priorCeil  = 0.65
)

// PriorTilts are the signed, per-component tilts around the 0.5 base rate. Each
// OK flag reports whether the component carried any information at all — a tilt
// of zero with OK=true means "looked, found the neutral band", while OK=false
// means "no data to look at".
type PriorTilts struct {
	Valuation   float64 // PE bands + price/book bands, summed
	ValuationOK bool
	Quality     float64 // earnings-quality (EPS sign) tilt
	QualityOK   bool
	Momentum    float64 // trailing cumulative-return tilt
	MomentumOK  bool
}

// ComputePriorTilts evaluates the per-component tilts from point-in-time
// fundamentals and trailing price momentum:
//
//   - valuation: cheap PE tilts up, expensive PE tilts down (neutral band 16–22);
//     very cheap price/book up, very rich down
//   - quality: positive EPS slightly up, losses down
//   - momentum: trailing cumulative return beyond ±10% tilts with its sign
func ComputePriorTilts(f marketdata.Fundamentals, returns []float64) PriorTilts {
	var t PriorTilts

	if f.PE > 0 {
		t.ValuationOK = true
		switch {
		case f.PE <= 12:
			t.Valuation += 0.05
		case f.PE <= 16:
			t.Valuation += 0.03
		case f.PE <= 22:
			// neutral band
		case f.PE <= 30:
			t.Valuation -= 0.02
		default:
			t.Valuation -= 0.05
		}
	}
	if f.PB > 0 {
		t.ValuationOK = true
		switch {
		case f.PB <= 1.5:
			t.Valuation += 0.02
		case f.PB >= 4:
			t.Valuation -= 0.02
		}
	}
	if f.EPS != 0 {
		t.QualityOK = true
		if f.EPS > 0 {
			t.Quality += 0.01
		} else {
			t.Quality -= 0.04
		}
	}
	if cum, ok := cumulativeReturn(returns); ok {
		t.MomentumOK = true
		switch {
		case cum >= 0.10:
			t.Momentum += 0.03
		case cum <= -0.10:
			t.Momentum -= 0.03
		}
	}
	return t
}

// PriorWeights scales each tilt component; {1, 1, 1} reproduces the blended
// baseline exactly. A zero value normalizes to the baseline so a strategy that
// doesn't care about the prior can leave it unset.
type PriorWeights struct {
	Valuation float64 `json:"valuation"`
	Quality   float64 `json:"quality"`
	Momentum  float64 `json:"momentum"`
}

// Normalize maps the zero value onto the neutral {1, 1, 1} weighting so an
// unset field block never silently zeroes the prior.
func (w PriorWeights) Normalize() PriorWeights {
	if w == (PriorWeights{}) {
		return PriorWeights{Valuation: 1, Quality: 1, Momentum: 1}
	}
	return w
}

// WinProbPriorWeighted blends the component tilts under the given weights. Only
// components that carried information contribute. It returns ok=false when no
// component is informed or the weighted net tilt is zero — reporting a flat
// prior as informed would re-enable the volatility-scaled EV path, the exact
// pathology NeutralEVScore exists to prevent. The clamp stays the global
// [priorFloor, priorCeil]: no weighting gets to be more "convinced" than the
// blended baseline ever was.
func WinProbPriorWeighted(t PriorTilts, w PriorWeights) (float64, bool) {
	tilt := 0.0
	informed := false
	if t.ValuationOK {
		informed = true
		tilt += w.Valuation * t.Valuation
	}
	if t.QualityOK {
		informed = true
		tilt += w.Quality * t.Quality
	}
	if t.MomentumOK {
		informed = true
		tilt += w.Momentum * t.Momentum
	}
	if !informed || tilt == 0 {
		return 0, false
	}
	return clampRange(0.5+tilt, priorFloor, priorCeil), true
}

// WinProbPrior derives a heuristic win-probability prior from point-in-time
// fundamentals and trailing price momentum, blending every component at its
// baseline weight. It returns ok=false when neither fundamentals nor enough
// price history carry any information, so callers can keep the EV component
// neutral.
func WinProbPrior(f marketdata.Fundamentals, returns []float64) (float64, bool) {
	return WinProbPriorWeighted(ComputePriorTilts(f, returns), PriorWeights{Valuation: 1, Quality: 1, Momentum: 1})
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
