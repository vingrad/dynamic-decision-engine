package finance

import "math"

// PortfolioPosition is one sized thesis as the portfolio aggregator sees it:
// the suggested fraction of equity, the stop distance (so Fraction*StopFrac is
// the capital at risk if stopped out), and the return series used to estimate
// co-movement with the other theses.
type PortfolioPosition struct {
	Ticker   string
	Fraction float64
	StopFrac float64
	Returns  []float64
}

// AggregateRisk is the portfolio-level capital at risk across concurrent
// theses: sqrt(r' P r) where r_i = Fraction_i * StopFrac_i and P holds the
// pairwise return correlations. Unknown correlations (too little overlapping
// history) are assumed fully correlated — the conservative direction.
func AggregateRisk(positions []PortfolioPosition) float64 {
	var total float64
	for i := range positions {
		ri := positions[i].Fraction * positions[i].StopFrac
		if ri == 0 {
			continue
		}
		for j := range positions {
			rj := positions[j].Fraction * positions[j].StopFrac
			if rj == 0 {
				continue
			}
			rho := 1.0
			if i != j {
				rho = Correlation(positions[i].Returns, positions[j].Returns)
			}
			total += ri * rj * rho
		}
	}
	if total <= 0 {
		return 0
	}
	return math.Sqrt(total)
}

// ScaleToRiskCap returns the uniform factor that brings the aggregate capital
// at risk under the cap (risk scales linearly with a uniform position scale),
// along with the unscaled aggregate. A zero cap or an aggregate already within
// it returns scale 1.
func ScaleToRiskCap(positions []PortfolioPosition, cap float64) (scale, aggregate float64) {
	agg := AggregateRisk(positions)
	if cap <= 0 || agg <= cap {
		return 1, agg
	}
	return cap / agg, agg
}

// Correlation is the Pearson correlation of two return series aligned on their
// most recent overlapping observations. Too little overlap (or a flat series)
// yields 1 — co-movement is assumed, not dismissed, when it cannot be measured.
func Correlation(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < 2 {
		return 1
	}
	a, b = a[len(a)-n:], b[len(b)-n:]

	var meanA, meanB float64
	for i := 0; i < n; i++ {
		meanA += a[i]
		meanB += b[i]
	}
	meanA /= float64(n)
	meanB /= float64(n)

	var cov, varA, varB float64
	for i := 0; i < n; i++ {
		da, db := a[i]-meanA, b[i]-meanB
		cov += da * db
		varA += da * da
		varB += db * db
	}
	if varA == 0 || varB == 0 {
		return 1
	}
	rho := cov / math.Sqrt(varA*varB)
	switch {
	case rho < -1:
		return -1
	case rho > 1:
		return 1
	default:
		return rho
	}
}
