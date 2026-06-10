package finance

import "testing"

func TestCorrelation(t *testing.T) {
	up := []float64{0.01, 0.02, -0.01, 0.03}
	down := []float64{-0.01, -0.02, 0.01, -0.03}
	if got := Correlation(up, up); !approx(got, 1) {
		t.Errorf("self correlation = %v, want 1", got)
	}
	if got := Correlation(up, down); !approx(got, -1) {
		t.Errorf("mirrored series = %v, want -1", got)
	}
	// Too little overlap or a flat series assumes full co-movement.
	if got := Correlation([]float64{0.01}, up); got != 1 {
		t.Errorf("short overlap = %v, want conservative 1", got)
	}
	if got := Correlation([]float64{0, 0, 0, 0}, up); got != 1 {
		t.Errorf("flat series = %v, want conservative 1", got)
	}
}

func TestAggregateRisk(t *testing.T) {
	corr := []float64{0.01, 0.02, -0.01, 0.03}
	anti := []float64{-0.01, -0.02, 0.01, -0.03}

	// Two perfectly correlated 1%-at-risk positions add linearly: 2%.
	a := PortfolioPosition{Fraction: 0.10, StopFrac: 0.10, Returns: corr}
	b := PortfolioPosition{Fraction: 0.20, StopFrac: 0.05, Returns: corr}
	if got := AggregateRisk([]PortfolioPosition{a, b}); !approx(got, 0.02) {
		t.Errorf("correlated aggregate = %v, want 0.02", got)
	}
	// Perfectly anti-correlated equal positions hedge to ~zero.
	h := PortfolioPosition{Fraction: 0.10, StopFrac: 0.10, Returns: anti}
	if got := AggregateRisk([]PortfolioPosition{a, h}); got > 1e-9 {
		t.Errorf("hedged aggregate = %v, want ~0", got)
	}
	// Zero-fraction positions contribute nothing.
	if got := AggregateRisk([]PortfolioPosition{{Fraction: 0, StopFrac: 0.5, Returns: corr}}); got != 0 {
		t.Errorf("empty book = %v, want 0", got)
	}
}

func TestScaleToRiskCap(t *testing.T) {
	corr := []float64{0.01, 0.02, -0.01, 0.03}
	positions := []PortfolioPosition{
		{Fraction: 0.10, StopFrac: 0.10, Returns: corr}, // 1% at risk
		{Fraction: 0.20, StopFrac: 0.05, Returns: corr}, // 1% at risk
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
	// Zero cap disables aggregation.
	if scale, _ := ScaleToRiskCap(positions, 0); scale != 1 {
		t.Errorf("zero cap should disable, got scale %v", scale)
	}
}
