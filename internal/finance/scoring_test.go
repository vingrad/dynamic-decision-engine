package finance

import (
	"math"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestExpectedValueAndScore(t *testing.T) {
	// 60% chance of +20%, 40% chance of -10%: EV = 0.12 - 0.04 = 0.08.
	ev := ExpectedValue(0.6, 0.20, 0.10)
	if !approx(ev, 0.08) {
		t.Errorf("EV = %v, want 0.08", ev)
	}
	if got := EVScore(ev); !approx(got, 0.58) {
		t.Errorf("EVScore = %v, want 0.58", got)
	}
	// Win prob clamps.
	if ev := ExpectedValue(1.5, 0.2, 0.1); !approx(ev, 0.2) {
		t.Errorf("clamped EV = %v, want 0.2", ev)
	}
}

func TestRiskScore(t *testing.T) {
	if got := RiskScore(0, 0); !approx(got, 1) {
		t.Errorf("zero vol/dd => 1, got %v", got)
	}
	if got := RiskScore(0.4, 0.2); !approx(got, 0.7) {
		t.Errorf("RiskScore(0.4,0.2) = %v, want 0.7", got)
	}
	if got := RiskScore(2, 2); got != 0 {
		t.Errorf("extreme risk should clamp to 0, got %v", got)
	}
}

func TestLiquidityAndHorizonFit(t *testing.T) {
	if got := LiquidityFit(0, 100); got != 0 {
		t.Errorf("no volume => 0, got %v", got)
	}
	if got := LiquidityFit(100, 0); got != 1 {
		t.Errorf("no intended notional => 1, got %v", got)
	}
	if got := HorizonFit(0, 0); got != 0.5 {
		t.Errorf("unknown horizon => 0.5, got %v", got)
	}
	if got := HorizonFit(30, 30); got != 1 {
		t.Errorf("matching horizon => 1, got %v", got)
	}
}

func TestComposite(t *testing.T) {
	s := ThesisScore{ExpectedValueScore: 1, RiskScore: 1, LiquidityFitScore: 1, HorizonFitScore: 1}
	if got := Composite(s, ScoreWeights{EV: 1}); got != 1 {
		t.Errorf("all-ones composite should be 1, got %v", got)
	}
	// Zero weights fall back to equal weighting.
	if got := Composite(ThesisScore{ExpectedValueScore: 1}, ScoreWeights{}); got != 0.25 {
		t.Errorf("equal-weight fallback = %v, want 0.25", got)
	}
}

func TestMapToLevels(t *testing.T) {
	impact, effort, risk := MapToLevels(ThesisScore{Composite: 0.8, RiskScore: 0.9, LiquidityFitScore: 0.9})
	if impact != domain.LevelHigh {
		t.Errorf("composite 0.8 => high impact, got %v", impact)
	}
	if risk != domain.LevelLow {
		t.Errorf("safety 0.9 => low risk, got %v", risk)
	}
	if effort != domain.LevelLow {
		t.Errorf("high liquidity => low effort, got %v", effort)
	}
	// Illiquid name => high effort; low safety => high risk.
	_, effort, risk = MapToLevels(ThesisScore{Composite: 0.5, RiskScore: 0.1, LiquidityFitScore: 0.1})
	if risk != domain.LevelHigh {
		t.Errorf("safety 0.1 => high risk, got %v", risk)
	}
	if effort != domain.LevelHigh {
		t.Errorf("low liquidity => high effort, got %v", effort)
	}
}

func TestParseHorizonDays(t *testing.T) {
	cases := map[string]int{
		"2 years":   730,
		"18 months": 540,
		"6 weeks":   42,
		"30 days":   30,
		"1y":        365,
		"1 quarter": 91,
		"no number": 0,
		"":          0,
	}
	for in, want := range cases {
		if got := ParseHorizonDays(in); got != want {
			t.Errorf("ParseHorizonDays(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestRewardRiskRatioAffectsSize(t *testing.T) {
	budget := RiskBudget{MaxPortfolioRiskPct: 1, MaxPositionPct: 1, KellyFraction: 1}
	// Higher reward:risk (bigger b) increases the Kelly fraction for the same odds.
	small := PositionFractionKelly(0.6, 0.10, 0.10, budget) // b=1
	big := PositionFractionKelly(0.6, 0.30, 0.10, budget)   // b=3
	if !(big.SuggestedFraction > small.SuggestedFraction) {
		t.Errorf("higher reward:risk should size larger: small=%v big=%v", small.SuggestedFraction, big.SuggestedFraction)
	}
}

func TestPositionFractionKelly(t *testing.T) {
	budget := RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.20, KellyFraction: 0.25}
	// Negative edge => no position.
	if ps := PositionFractionKelly(0.2, 0.2, 0.2, budget); ps.SuggestedFraction != 0 {
		t.Errorf("negative edge should size to 0, got %v", ps.SuggestedFraction)
	}
	// Positive edge gets capped by the per-trade risk cap (0.02/lossFrac).
	ps := PositionFractionKelly(0.7, 0.40, 0.20, budget)
	if ps.SuggestedFraction <= 0 {
		t.Fatalf("expected positive size, got %v", ps.SuggestedFraction)
	}
	if ps.SuggestedFraction > 0.20 {
		t.Errorf("size exceeds concentration cap: %v", ps.SuggestedFraction)
	}
	// risk cap = 0.02/0.20 = 0.10
	if ps.SuggestedFraction > 0.10+1e-9 {
		t.Errorf("size exceeds risk cap 0.10: %v (cap=%q)", ps.SuggestedFraction, ps.BindingCap)
	}
}

func TestReturnsVolatilityDrawdown(t *testing.T) {
	bars := []marketdata.Bar{
		{Close: 100}, {Close: 110}, {Close: 99},
	}
	r := Returns(bars)
	if len(r) != 2 {
		t.Fatalf("expected 2 returns, got %d", len(r))
	}
	if Volatility(r) <= 0 {
		t.Error("expected positive volatility")
	}
	if dd := MaxDrawdown(bars); !approx(dd, (110.0-99.0)/110.0) {
		t.Errorf("max drawdown = %v", dd)
	}
}
