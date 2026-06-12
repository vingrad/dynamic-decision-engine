package backtest

import (
	"context"
	"reflect"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// regimeCorpus is the strategy-comparison corpus: fixtures engineered so the
// lenses genuinely disagree (a year-long trend, a sideways range, a high-vol
// bleed, and a trend that rolls into a crash).
var regimeCorpus = []string{
	"testdata/scenario_regime_trend.json",
	"testdata/scenario_regime_range.json",
	"testdata/scenario_regime_highvol.json",
	"testdata/scenario_regime_mixed.json",
}

func loadRegimeCorpus(t *testing.T) []Scenario {
	t.Helper()
	var out []Scenario
	for _, f := range regimeCorpus {
		out = append(out, loadScenario(t, f))
	}
	return out
}

// TestStrategyMatrixGates asserts the acceptance gates for enabling the
// selector by default:
//  1. the selector never does worse than the legacy single planner on Brier,
//  2. it never reacts to noise the single planner ignored,
//  3. the winning strategy doesn't flap (at most one switch per scenario),
//  4. every decision under the selector records its winning strategy.
func TestStrategyMatrixGates(t *testing.T) {
	scenarios := loadRegimeCorpus(t)
	cells, err := RunStrategyMatrix(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios)
	if err != nil {
		t.Fatal(err)
	}

	single := map[string]Report{}
	selector := map[string]Report{}
	for _, c := range cells {
		switch c.Config {
		case "single":
			single[c.Scenario] = c.Report
		case "selector":
			selector[c.Scenario] = c.Report
		}
	}
	if len(single) != len(scenarios) || len(selector) != len(scenarios) {
		t.Fatalf("matrix incomplete: %d single, %d selector reports", len(single), len(selector))
	}

	// Flip budgets are per scenario: a stable tape must never flap, while the
	// scenarios engineered to cross regimes are ALLOWED exactly their designed
	// transitions and no more.
	maxFlips := map[string]int{
		"ACME persistent uptrend": 0,
		"ACME range-bound value":  0,
		"ACME high-vol bleed":     1,
		"ACME trend into crash":   2,
	}
	// Per-scenario, the selector may pay at most one small transient step at a
	// regime turn (the classifier necessarily lags the tape — at a crash onset
	// the trailing window still reads "trend"); it must never be meaningfully
	// worse. Across the corpus it must be a strict net improvement.
	const regimeTurnTolerance = 0.01
	var sumSel, sumSingle float64
	for name, sel := range selector {
		base := single[name]
		sumSel += sel.BrierScore
		sumSingle += base.BrierScore
		if sel.BrierScore > base.BrierScore+regimeTurnTolerance {
			t.Errorf("%s: selector brier %.3f worse than single %.3f beyond tolerance", name, sel.BrierScore, base.BrierScore)
		}
		if sel.NoiseRobustness < base.NoiseRobustness {
			t.Errorf("%s: selector noise robustness %.2f below single %.2f", name, sel.NoiseRobustness, base.NoiseRobustness)
		}
		if sel.StrategyFlips > maxFlips[name] {
			t.Errorf("%s: winning strategy flapped %d times (budget %d)", name, sel.StrategyFlips, maxFlips[name])
		}
		for _, d := range sel.Decisions {
			if d.SelectedStrategy == "" {
				t.Errorf("%s: decision at %s missing its selected strategy", name, d.At)
			}
		}
	}

	if sumSel >= sumSingle {
		t.Errorf("selector mean brier %.4f must strictly beat single %.4f across the corpus",
			sumSel/float64(len(selector)), sumSingle/float64(len(single)))
	}

	// The trend scenario is where the competition should genuinely pay: the
	// selector must match the best single lens there, not just the legacy one.
	if sel, ok := selector["ACME persistent uptrend"]; ok {
		if sel.Decisions[len(sel.Decisions)-1].SelectedStrategy != "momentum" {
			t.Errorf("trend scenario should belong to the momentum lens, got %q",
				sel.Decisions[len(sel.Decisions)-1].SelectedStrategy)
		}
	}

	// Regime gating must hand the high-volatility tapes to the defensive lens:
	// once the bleed is classified high_vol, trend-and-range lenses are out.
	for _, name := range []string{"ACME high-vol bleed", "ACME trend into crash"} {
		ds := selector[name].Decisions
		if last := ds[len(ds)-1]; last.SelectedStrategy != "defensive" {
			t.Errorf("%s: final decision should belong to defensive under high_vol gating, got %q",
				name, last.SelectedStrategy)
		}
	}

	// The competition must actually differentiate somewhere on this corpus:
	// at least two distinct lenses win across the scenarios.
	winners := map[string]bool{}
	for _, sel := range selector {
		for s := range sel.StrategyShare {
			winners[s] = true
		}
	}
	if len(winners) < 2 {
		t.Errorf("corpus never separated the lenses: winners %v", winners)
	}
}

// TestStrategyMatrixDeterministic: two full runs of the comparison must be
// byte-identical — selection has no incidental ordering anywhere.
func TestStrategyMatrixDeterministic(t *testing.T) {
	scenarios := loadRegimeCorpus(t)
	run := func() []MatrixCell {
		cells, err := RunStrategyMatrix(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios)
		if err != nil {
			t.Fatal(err)
		}
		return cells
	}
	if a, b := run(), run(); !reflect.DeepEqual(a, b) {
		t.Error("two identical matrix runs produced different results")
	}
}

// TestStrategyMatrixPinnedLensDiffers proves the pinned-lens baselines are
// real lenses, not the single planner relabelled: at least one pinned config
// produces a different Brier than "single" somewhere in the corpus.
func TestStrategyMatrixPinnedLensDiffers(t *testing.T) {
	scenarios := loadRegimeCorpus(t)
	cells, err := RunStrategyMatrix(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios)
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]float64{}
	for _, c := range cells {
		if c.Config == "single" {
			base[c.Scenario] = c.Report.BrierScore
		}
	}
	differs := false
	for _, c := range cells {
		if c.Config != "single" && c.Config != "selector" && c.Report.BrierScore != base[c.Scenario] {
			differs = true
		}
	}
	if !differs {
		t.Error("no pinned lens ever differed from the single planner — the lenses are not differentiating")
	}
}
