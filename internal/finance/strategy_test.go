package finance

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// TestWinProbPriorWeightedNeutralMatchesBaseline runs representative inputs
// through both the legacy blended path and the decomposed weighted path at
// neutral weights: the decomposition must be bit-for-bit invisible.
func TestWinProbPriorWeightedNeutralMatchesBaseline(t *testing.T) {
	cases := []struct {
		name    string
		f       marketdata.Fundamentals
		returns []float64
	}{
		{name: "no information"},
		{name: "single return", returns: []float64{0.04}},
		{name: "cheap PE positive EPS", f: marketdata.Fundamentals{PE: 14, PB: 2.0, EPS: 6.6}},
		{name: "very cheap PE and PB", f: marketdata.Fundamentals{PE: 10, PB: 1.2, EPS: 3.0}},
		{name: "rich PE and PB", f: marketdata.Fundamentals{PE: 30, PB: 5.0, EPS: 1.3}},
		{name: "loss-maker extreme valuation", f: marketdata.Fundamentals{PE: 45, PB: 6.0, EPS: -2.0}},
		{name: "neutral PE band alone", f: marketdata.Fundamentals{PE: 20}},
		{name: "positive momentum", returns: []float64{0.05, 0.05, 0.05}},
		{name: "negative momentum", returns: []float64{-0.05, -0.05, -0.05}},
		{name: "small momentum alone", returns: []float64{0.01, 0.01}},
		{name: "max bearish at floor", f: marketdata.Fundamentals{PE: 45, PB: 6.0, EPS: -2.0}, returns: []float64{-0.10, -0.10}},
	}
	neutral := PriorWeights{Valuation: 1, Quality: 1, Momentum: 1}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantP, wantOK := WinProbPrior(tc.f, tc.returns)
			gotP, gotOK := WinProbPriorWeighted(ComputePriorTilts(tc.f, tc.returns), neutral)
			if gotOK != wantOK || gotP != wantP {
				t.Errorf("weighted neutral = (%v, %v), baseline = (%v, %v)", gotP, gotOK, wantP, wantOK)
			}
		})
	}
}

func TestWinProbPriorWeightedTilts(t *testing.T) {
	// A trending loss-maker: momentum up, quality down, valuation rich.
	f := marketdata.Fundamentals{PE: 35, EPS: -1.0}
	returns := []float64{0.06, 0.06, 0.06}
	tilts := ComputePriorTilts(f, returns)

	momentum, okM := WinProbPriorWeighted(tilts, PriorWeights{Valuation: 0.3, Quality: 0.7, Momentum: 1.8})
	value, okV := WinProbPriorWeighted(tilts, PriorWeights{Valuation: 1.6, Quality: 1.0, Momentum: 0.4})
	if !okM || !okV {
		t.Fatalf("both lenses should be informed: momentum ok=%v value ok=%v", okM, okV)
	}
	// The momentum lens forgives valuation and rides the trend; the value lens
	// punishes the rich multiple and the losses.
	if momentum <= value {
		t.Errorf("momentum lens %v should rank the trending name above the value lens %v", momentum, value)
	}

	// Clamp bounds hold under extreme weights.
	if p, ok := WinProbPriorWeighted(tilts, PriorWeights{Valuation: 100, Quality: 100, Momentum: 0.0001}); ok && (p < priorFloor || p > priorCeil) {
		t.Errorf("weighted prior %v escaped clamp [%v, %v]", p, priorFloor, priorCeil)
	}

	// Weights that cancel the net tilt are uninformed, preserving the
	// NeutralEVScore guard.
	cancel := PriorWeights{Valuation: 0, Quality: 0, Momentum: 0}
	if _, ok := WinProbPriorWeighted(tilts, cancel); ok {
		t.Error("zero weights must read as uninformed, not an informed 0.50")
	}
}

func TestPriorWeightsNormalize(t *testing.T) {
	if got := (PriorWeights{}).Normalize(); got != (PriorWeights{Valuation: 1, Quality: 1, Momentum: 1}) {
		t.Errorf("zero value should normalize to neutral, got %+v", got)
	}
	set := PriorWeights{Valuation: 1.6, Quality: 1, Momentum: 0.4}
	if got := set.Normalize(); got != set {
		t.Errorf("set weights must pass through unchanged, got %+v", got)
	}
}

func TestStrategyParamsApply(t *testing.T) {
	base := DefaultScoringConfig()

	t.Run("zero strategy is identity on a normalized base", func(t *testing.T) {
		if got := (StrategyParams{}).Apply(base); got != base {
			t.Errorf("Apply(zero) = %+v, want base %+v", got, base)
		}
	})

	t.Run("weights and ratio replace when set", func(t *testing.T) {
		s := StrategyParams{
			Weights:         ScoreWeights{EV: 0.5, Risk: 0.2, Liquidity: 0.2, Horizon: 0.1},
			RewardRiskRatio: 1.8,
		}
		got := s.Apply(base)
		if got.Weights != s.Weights || got.RewardRiskRatio != 1.8 {
			t.Errorf("Apply did not overlay weights/ratio: %+v", got)
		}
		if got.Risk != base.Risk {
			t.Errorf("risk budget must be untouched without a RiskScale: %+v", got.Risk)
		}
	})

	t.Run("risk scale tightens caps and scales kelly", func(t *testing.T) {
		s := StrategyParams{RiskScale: RiskScale{Kelly: 0.5, PerTradeRisk: 0.75, Aggregate: 0.75}}
		got := s.Apply(base)
		if !approx(got.Risk.KellyFraction, base.Risk.KellyFraction*0.5) {
			t.Errorf("kelly = %v, want halved", got.Risk.KellyFraction)
		}
		if !approx(got.Risk.MaxPortfolioRiskPct, base.Risk.MaxPortfolioRiskPct*0.75) ||
			!approx(got.Risk.MaxPositionPct, base.Risk.MaxPositionPct*0.75) {
			t.Errorf("per-trade caps not tightened: %+v", got.Risk)
		}
		if !approx(got.Risk.MaxAggregateRiskPct, base.Risk.MaxAggregateRiskPct*0.75) {
			t.Errorf("aggregate cap = %v, want tightened", got.Risk.MaxAggregateRiskPct)
		}
	})

	t.Run("caps may only tighten", func(t *testing.T) {
		s := StrategyParams{RiskScale: RiskScale{PerTradeRisk: 2.0, Aggregate: 1.5}}
		got := s.Apply(base)
		if got.Risk.MaxPortfolioRiskPct != base.Risk.MaxPortfolioRiskPct ||
			got.Risk.MaxPositionPct != base.Risk.MaxPositionPct ||
			got.Risk.MaxAggregateRiskPct != base.Risk.MaxAggregateRiskPct {
			t.Errorf("loosening multipliers must be ignored: %+v", got.Risk)
		}
	})

	t.Run("disabled aggregate cap stays disabled", func(t *testing.T) {
		b := base
		b.Risk.MaxAggregateRiskPct = -1
		s := StrategyParams{RiskScale: RiskScale{Aggregate: 0.5}}
		if got := s.Apply(b); got.Risk.MaxAggregateRiskPct != -1 {
			t.Errorf("negative (disabled) aggregate cap must pass through, got %v", got.Risk.MaxAggregateRiskPct)
		}
	})
}

func TestDefaultStrategySet(t *testing.T) {
	set := DefaultStrategySet()
	if len(set) != 3 {
		t.Fatalf("expected 3 strategies, got %d", len(set))
	}
	// The order is canonical: it is the last-resort selection tie-break.
	wantOrder := []string{"value", "momentum", "defensive"}
	seen := map[string]bool{}
	for i, s := range set {
		if s.Name != wantOrder[i] {
			t.Errorf("strategy[%d] = %q, want %q", i, s.Name, wantOrder[i])
		}
		if seen[s.Name] {
			t.Errorf("duplicate strategy name %q", s.Name)
		}
		seen[s.Name] = true
		if s.RiskScale.PerTradeRisk > 1 || s.RiskScale.Aggregate > 1 {
			t.Errorf("%s: cap multipliers must not loosen: %+v", s.Name, s.RiskScale)
		}
	}
}
