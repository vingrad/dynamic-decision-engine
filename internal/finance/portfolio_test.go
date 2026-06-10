package finance

import (
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// barsWithReturns builds a weekly bar series realizing the given returns,
// starting at startDay (days after 2026-01-02) so date overlap is controllable.
func barsWithReturns(startDay int, rets ...float64) []marketdata.Bar {
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).AddDate(0, 0, startDay)
	bars := []marketdata.Bar{{Date: start, Close: 100}}
	for i, r := range rets {
		bars = append(bars, marketdata.Bar{
			Date:  start.AddDate(0, 0, 7*(i+1)),
			Close: bars[i].Close * (1 + r),
		})
	}
	return bars
}

func TestCorrelation(t *testing.T) {
	up := barsWithReturns(0, 0.01, 0.02, -0.01, 0.03)
	down := barsWithReturns(0, -0.01, -0.02, 0.01, -0.03)
	if got := Correlation(up, up); !approx(got, 1) {
		t.Errorf("self correlation = %v, want 1", got)
	}
	if got := Correlation(up, down); !approx(got, -1) {
		t.Errorf("mirrored series = %v, want -1", got)
	}
	// Too little overlap or a flat series assumes full co-movement.
	if got := Correlation(barsWithReturns(0, 0.01), up); got != 1 {
		t.Errorf("short overlap = %v, want conservative 1", got)
	}
	if got := Correlation(barsWithReturns(0, 0, 0, 0, 0), up); got != 1 {
		t.Errorf("flat series = %v, want conservative 1", got)
	}
}

func TestCorrelationAlignsByDate(t *testing.T) {
	up := barsWithReturns(0, 0.01, 0.02, -0.01, 0.03)
	// A series on completely disjoint dates has no overlapping returns: the
	// conservative rho=1 must apply, never an index-positional comparison.
	shifted := barsWithReturns(3, -0.01, -0.02, 0.01, -0.03)
	if got := Correlation(up, shifted); got != 1 {
		t.Errorf("disjoint dates = %v, want conservative 1 (no measurable overlap)", got)
	}
	// A sparser series sharing most dates correlates only on the common-date
	// returns, which co-move here.
	sparse := []marketdata.Bar{up[0], up[1], up[2], up[4]}
	got := Correlation(up, sparse)
	if got <= 0 {
		t.Errorf("common-date overlap should measure positive co-movement, got %v", got)
	}
}

func TestAggregateRisk(t *testing.T) {
	corr := barsWithReturns(0, 0.01, 0.02, -0.01, 0.03)
	anti := barsWithReturns(0, -0.01, -0.02, 0.01, -0.03)

	// Two perfectly correlated 1%-at-risk positions add linearly: 2%.
	a := PortfolioPosition{Fraction: 0.10, StopFrac: 0.10, Bars: corr}
	b := PortfolioPosition{Fraction: 0.20, StopFrac: 0.05, Bars: corr}
	if got := AggregateRisk([]PortfolioPosition{a, b}); !approx(got, 0.02) {
		t.Errorf("correlated aggregate = %v, want 0.02", got)
	}
	// Perfectly anti-correlated equal positions hedge to ~zero.
	h := PortfolioPosition{Fraction: 0.10, StopFrac: 0.10, Bars: anti}
	if got := AggregateRisk([]PortfolioPosition{a, h}); got > 1e-9 {
		t.Errorf("hedged aggregate = %v, want ~0", got)
	}
	// Zero-fraction positions contribute nothing.
	if got := AggregateRisk([]PortfolioPosition{{Fraction: 0, StopFrac: 0.5, Bars: corr}}); got != 0 {
		t.Errorf("empty book = %v, want 0", got)
	}
}

func TestScaleToRiskCap(t *testing.T) {
	corr := barsWithReturns(0, 0.01, 0.02, -0.01, 0.03)
	positions := []PortfolioPosition{
		{Fraction: 0.10, StopFrac: 0.10, Bars: corr}, // 1% at risk
		{Fraction: 0.20, StopFrac: 0.05, Bars: corr}, // 1% at risk
	}
	// Cap above the 2% aggregate: no scaling.
	if scale, agg := ScaleToRiskCap(positions, 0.06); scale != 1 || !approx(agg, 0.02) {
		t.Errorf("uncapped: scale=%v agg=%v", scale, agg)
	}
	// Cap at 1%: scale halves, and risk scales linearly with it.
	scale, agg := ScaleToRiskCap(positions, 0.01)
	if !approx(scale, 0.5) || !approx(agg, 0.02) {
		t.Errorf("capped: scale=%v agg=%v, want 0.5 / 0.02", scale, agg)
	}
	// A negative cap explicitly disables aggregation.
	if scale, _ := ScaleToRiskCap(positions, -1); scale != 1 {
		t.Errorf("negative cap should disable, got scale %v", scale)
	}
}
