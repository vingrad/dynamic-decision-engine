package finance

import (
	"math"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// PortfolioPosition is one sized thesis as the portfolio aggregator sees it:
// the suggested fraction of equity, the stop distance (so Fraction*StopFrac is
// the capital at risk if stopped out), and the dated bar series used to
// estimate co-movement with the other theses.
type PortfolioPosition struct {
	Ticker   string
	Fraction float64
	StopFrac float64
	Bars     []marketdata.Bar
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
				rho = Correlation(positions[i].Bars, positions[j].Bars)
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

// Correlation is the Pearson correlation of two bar series' returns aligned on
// common bar dates — never by index position, which would pair mismatched
// calendar windows across cadences or gappy histories. Too little overlap (or
// a flat series) yields 1: co-movement is assumed, not dismissed, when it
// cannot be measured, so an unmeasurable pair never earns a hedging credit.
func Correlation(a, b []marketdata.Bar) float64 {
	ra, rb := alignedReturns(a, b)
	n := len(ra)
	if n < 2 {
		return 1
	}

	var meanA, meanB float64
	for i := 0; i < n; i++ {
		meanA += ra[i]
		meanB += rb[i]
	}
	meanA /= float64(n)
	meanB /= float64(n)

	var cov, varA, varB float64
	for i := 0; i < n; i++ {
		da, db := ra[i]-meanA, rb[i]-meanB
		cov += da * db
		varA += da * da
		varB += db * db
	}
	if varA == 0 || varB == 0 {
		return 1
	}
	return clampRange(cov/math.Sqrt(varA*varB), -1, 1)
}

// alignedReturns pairs the two series' bar-to-bar returns on common bar dates,
// in chronological order. A return is keyed by the date of its later bar; only
// dates where BOTH series have a return survive.
func alignedReturns(a, b []marketdata.Bar) (ra, rb []float64) {
	byDate := func(bars []marketdata.Bar) map[time.Time]float64 {
		out := make(map[time.Time]float64, len(bars))
		for i := 1; i < len(bars); i++ {
			prev := bars[i-1].Close
			if prev == 0 {
				continue
			}
			out[bars[i].Date.UTC()] = (bars[i].Close - prev) / prev
		}
		return out
	}
	mb := byDate(b)
	for i := 1; i < len(a); i++ {
		prev := a[i-1].Close
		if prev == 0 {
			continue
		}
		date := a[i].Date.UTC()
		if rB, ok := mb[date]; ok {
			ra = append(ra, (a[i].Close-prev)/prev)
			rb = append(rb, rB)
		}
	}
	return ra, rb
}
