package backtest

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

func loadScenario(t *testing.T, path string) Scenario {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sc Scenario
	if err := json.Unmarshal(data, &sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestHarnessRun(t *testing.T) {
	prov, err := marketdata.NewOfflineProvider()
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(pack.NewRegistry(), policy.Policy{}, prov)
	if err != nil {
		t.Fatal(err)
	}
	sc := loadScenario(t, "testdata/scenario.json")

	rep, err := h.Run(context.Background(), sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(rep.Decisions))
	}

	// The thesis_break must produce a material replan, and recall over the single
	// should_kill event must be 1.
	last := rep.Decisions[len(rep.Decisions)-1]
	if last.Kind != "thesis_break" || !last.Material {
		t.Errorf("thesis_break should be material: %+v", last)
	}
	if rep.KillRecall != 1 {
		t.Errorf("expected kill recall 1, got %v", rep.KillRecall)
	}
	if rep.VersionsCreated < 1 {
		t.Errorf("expected at least one version created, got %d", rep.VersionsCreated)
	}
	// Illustrative PnL is derived from offline ACME quotes (100 -> 108 = +8%).
	if rep.HypotheticalPnL <= 0 {
		t.Errorf("expected positive illustrative pnl from fixtures, got %v", rep.HypotheticalPnL)
	}

	// The 30% valuation gap is material new information (the upside replaces the
	// default reward:risk assumption), so the engine versions the plan on it.
	// The event is labeled non-kill, so the noise metric records the reaction.
	if rep.NoiseRobustness != 0 {
		t.Errorf("expected noise robustness 0 (valuation gap is material), got %v", rep.NoiseRobustness)
	}
	if rep.KillPrecision != 0.5 {
		t.Errorf("expected kill precision 0.5, got %v", rep.KillPrecision)
	}
	// Calibration: confidence should beat a coin flip (Brier 0.25) on this
	// scenario — ACME rallies while held, and the broken thesis ends at zero
	// confidence with a zero forward return.
	if rep.BrierScore <= 0 || rep.BrierScore >= 0.25 {
		t.Errorf("expected brier score in (0, 0.25), got %v", rep.BrierScore)
	}
	first, last := rep.Decisions[0], rep.Decisions[len(rep.Decisions)-1]
	if math.Abs(first.ForwardReturn-0.08) > 1e-9 {
		t.Errorf("expected +8%% forward return on first decision from fixtures, got %v", first.ForwardReturn)
	}
	if last.ForwardReturn != 0 {
		t.Errorf("final decision has no forward window, expected 0, got %v", last.ForwardReturn)
	}
}

func TestTerminalDecisionLabeledByAnalyst(t *testing.T) {
	// A scenario's final decision has a zero-length forward window; its label
	// must come from the analyst kill label, not read as an automatic failure.
	sc := loadScenario(t, "testdata/scenario.json")
	sc.Events = append(sc.Events[:1], TimelineEvent{
		At:         sc.Events[1].At,
		Kind:       "macro",
		Payload:    map[string]any{"indicator": "pmi", "value": 51.0, "prior": 50.5},
		ShouldKill: false,
	})

	rep, err := newHarness(t).Run(context.Background(), sc)
	if err != nil {
		t.Fatal(err)
	}
	last := rep.Decisions[len(rep.Decisions)-1]
	if last.Label != 1 {
		t.Errorf("healthy terminal decision should be labeled 1 via the analyst label, got %v", last.Label)
	}
}
