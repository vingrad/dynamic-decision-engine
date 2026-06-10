package finance

import "testing"

func TestNormalize(t *testing.T) {
	// A fully zero config becomes the defaults.
	if got := (ScoringConfig{}).Normalize(); got != DefaultScoringConfig() {
		t.Errorf("zero config = %+v, want defaults", got)
	}

	// A partial override keeps what it sets and fills the rest — setting one
	// knob must never zero the others (KellyFraction 0 would size everything
	// to nothing; MaxAggregateRiskPct 0 would drop the portfolio cap).
	got := (ScoringConfig{Risk: RiskBudget{MaxAggregateRiskPct: 0.04}}).Normalize()
	def := DefaultScoringConfig()
	want := def
	want.Risk.MaxAggregateRiskPct = 0.04
	if got != want {
		t.Errorf("partial override = %+v, want %+v", got, want)
	}

	// Negative caps are the explicit "disable" spelling and survive.
	got = (ScoringConfig{Risk: RiskBudget{MaxAggregateRiskPct: -1, KellyFraction: 0.1}}).Normalize()
	if got.Risk.MaxAggregateRiskPct != -1 || got.Risk.KellyFraction != 0.1 {
		t.Errorf("negative cap / set kelly should survive: %+v", got.Risk)
	}
	if got.Risk.MaxPositionPct != def.Risk.MaxPositionPct {
		t.Errorf("unset fields still fill from defaults: %+v", got.Risk)
	}
}
