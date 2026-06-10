package finance

// Confidence calibration: a deterministic, binned-isotonic mapping from the
// confidence the scorer states to the success frequency actually observed in
// recorded outcomes. Fit offline (dde calibrate, or the backtest walk-forward
// eval) and carried into the planner via the policy file — the scoring math
// itself stays untouched.

// calibrationBinCount fixes the stated-confidence binning over [0,1].
const calibrationBinCount = 5

// CalibrationSample pairs a stated confidence with its realized outcome.
type CalibrationSample struct {
	Confidence float64 `json:"confidence" yaml:"confidence"`
	Success    bool    `json:"success" yaml:"success"`
}

// CalibrationBin is one fitted point of the curve: at stated confidence Mid the
// observed success rate over N samples was Rate (after monotone pooling).
type CalibrationBin struct {
	Mid  float64 `json:"mid" yaml:"mid"`
	Rate float64 `json:"rate" yaml:"rate"`
	N    int     `json:"n" yaml:"n"`
}

// CalibrationCurve maps stated confidence onto observed frequency. An empty
// curve is the identity, so an unfit or absent curve never changes behaviour.
type CalibrationCurve struct {
	Bins []CalibrationBin `json:"bins,omitempty" yaml:"bins,omitempty"`
}

// Empty reports whether the curve carries no fitted points.
func (c CalibrationCurve) Empty() bool { return len(c.Bins) == 0 }

// Apply maps a stated confidence through the curve: linear interpolation
// between bin midpoints, clamped to the end bins. Identity when empty.
func (c CalibrationCurve) Apply(conf float64) float64 {
	if c.Empty() {
		return conf
	}
	conf = clamp01(conf)
	bins := c.Bins
	if conf <= bins[0].Mid {
		return bins[0].Rate
	}
	last := bins[len(bins)-1]
	if conf >= last.Mid {
		return last.Rate
	}
	for i := 1; i < len(bins); i++ {
		if conf <= bins[i].Mid {
			lo, hi := bins[i-1], bins[i]
			t := (conf - lo.Mid) / (hi.Mid - lo.Mid)
			return lo.Rate + t*(hi.Rate-lo.Rate)
		}
	}
	return last.Rate // unreachable; bins cover (first.Mid, last.Mid]
}

// FitCalibration bins samples by stated confidence and pools adjacent
// violators (weighted PAVA) so the fitted rates are monotone non-decreasing in
// stated confidence. No samples yield an empty (identity) curve. Deterministic:
// same samples, same curve.
func FitCalibration(samples []CalibrationSample) CalibrationCurve {
	type agg struct {
		successes int
		n         int
	}
	bins := make([]agg, calibrationBinCount)
	for _, s := range samples {
		i := int(clamp01(s.Confidence) * calibrationBinCount)
		if i == calibrationBinCount {
			i--
		}
		bins[i].n++
		if s.Success {
			bins[i].successes++
		}
	}

	// Pool-adjacent-violators over the non-empty bins, weighting by count.
	type block struct {
		mid, rate, weight float64
		n                 int
	}
	var blocks []block
	for i, b := range bins {
		if b.n == 0 {
			continue
		}
		blocks = append(blocks, block{
			mid:    (float64(i) + 0.5) / calibrationBinCount,
			rate:   float64(b.successes) / float64(b.n),
			weight: float64(b.n),
			n:      b.n,
		})
		for len(blocks) > 1 && blocks[len(blocks)-2].rate > blocks[len(blocks)-1].rate {
			a, b := blocks[len(blocks)-2], blocks[len(blocks)-1]
			merged := block{
				// The pooled block keeps the weighted mean rate; midpoints
				// average by weight so interpolation stays anchored sensibly.
				mid:    (a.mid*a.weight + b.mid*b.weight) / (a.weight + b.weight),
				rate:   (a.rate*a.weight + b.rate*b.weight) / (a.weight + b.weight),
				weight: a.weight + b.weight,
				n:      a.n + b.n,
			}
			blocks = blocks[:len(blocks)-2]
			blocks = append(blocks, merged)
		}
	}

	curve := CalibrationCurve{}
	for _, b := range blocks {
		curve.Bins = append(curve.Bins, CalibrationBin{Mid: round2(b.mid), Rate: round2(b.rate), N: b.n})
	}
	return curve
}
