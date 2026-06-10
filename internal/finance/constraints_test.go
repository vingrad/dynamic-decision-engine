package finance

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func baseBudget() RiskBudget {
	return RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.20, KellyFraction: 0.25, MaxAggregateRiskPct: 0.06}
}

func TestEffectiveRiskBudget(t *testing.T) {
	cases := []struct {
		name        string
		constraints []domain.Constraint
		want        RiskBudget
		note        string
	}{
		{
			name: "no constraints leaves base unchanged",
			want: baseBudget(),
		},
		{
			name: "unrelated kinds leave base unchanged",
			constraints: []domain.Constraint{
				{Name: "2 year horizon", Kind: "time_horizon"},
				{Name: "no leverage", Kind: "mandate"},
			},
			want: baseBudget(),
		},
		{
			name: "drawdown limit tightens position, per-trade and aggregate caps",
			constraints: []domain.Constraint{
				{Name: "max 5% drawdown", Kind: "drawdown_limit"},
			},
			want: RiskBudget{MaxPortfolioRiskPct: 0.01, MaxPositionPct: 0.05, KellyFraction: 0.25, MaxAggregateRiskPct: 0.05},
			note: "drawdown_limit 5%",
		},
		{
			name: "drawdown limit looser than base does not loosen it",
			constraints: []domain.Constraint{
				{Name: "max 50% drawdown", Kind: "drawdown_limit"},
			},
			want: baseBudget(),
			note: "drawdown_limit 50%",
		},
		{
			name: "drawdown percent parsed from description",
			constraints: []domain.Constraint{
				{Name: "drawdown cap", Kind: "drawdown_limit", Description: "no more than 10% peak-to-trough"},
			},
			want: RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.10, KellyFraction: 0.25, MaxAggregateRiskPct: 0.06},
			note: "drawdown_limit 10%",
		},
		{
			name: "unparseable drawdown is ignored",
			constraints: []domain.Constraint{
				{Name: "keep drawdowns small", Kind: "drawdown_limit"},
			},
			want: baseBudget(),
		},
		{
			name: "conservative tolerance halves kelly and concentration",
			constraints: []domain.Constraint{
				{Name: "conservative risk tolerance", Kind: "risk_tolerance"},
			},
			want: RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.10, KellyFraction: 0.125, MaxAggregateRiskPct: 0.06},
			note: "risk_tolerance conservative",
		},
		{
			name: "aggressive tolerance scales kelly only",
			constraints: []domain.Constraint{
				{Name: "aggressive", Kind: "risk_tolerance"},
			},
			want: RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.20, KellyFraction: 0.375, MaxAggregateRiskPct: 0.06},
			note: "risk_tolerance aggressive",
		},
		{
			name: "moderate tolerance is a no-op",
			constraints: []domain.Constraint{
				{Name: "moderate risk tolerance", Kind: "risk_tolerance"},
			},
			want: baseBudget(),
		},
		{
			name: "drawdown and tolerance combine",
			constraints: []domain.Constraint{
				{Name: "max 10% drawdown", Kind: "drawdown_limit"},
				{Name: "low risk appetite", Kind: "risk_tolerance"},
			},
			want: RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.05, KellyFraction: 0.125, MaxAggregateRiskPct: 0.06},
			note: "drawdown_limit 10%, risk_tolerance conservative",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, note := EffectiveRiskBudget(baseBudget(), tc.constraints)
			if got != tc.want {
				t.Errorf("budget = %+v, want %+v", got, tc.want)
			}
			if note != tc.note {
				t.Errorf("note = %q, want %q", note, tc.note)
			}
		})
	}
}

func TestEffectiveRiskBudgetNoFalseKeywordMatch(t *testing.T) {
	// "low" must match as a word, not inside e.g. "follow".
	got, note := EffectiveRiskBudget(baseBudget(), []domain.Constraint{
		{Name: "follow the mandate", Kind: "risk_tolerance"},
	})
	if got != baseBudget() || note != "" {
		t.Errorf("substring should not trigger a tolerance match: %+v %q", got, note)
	}
}

func TestParsePercent(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"max 10% drawdown", 0.10, true},
		{"2.5 %", 0.025, true},
		{"no percent here", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parsePercent(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parsePercent(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
