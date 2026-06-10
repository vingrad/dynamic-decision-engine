package backtest

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

func TestWalkForward(t *testing.T) {
	var scenarios []Scenario
	for _, f := range corpusFiles {
		scenarios = append(scenarios, loadScenario(t, f))
	}

	res, err := WalkForward(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios, 7)
	if err != nil {
		t.Fatal(err)
	}
	if res.TrainDecisions != 7 || res.EvalDecisions != 5 {
		t.Fatalf("expected a 7/5 split of the corpus decisions, got %d/%d", res.TrainDecisions, res.EvalDecisions)
	}
	if res.Curve.Empty() {
		t.Error("expected a fitted curve from the training decisions")
	}
	if res.BrierRaw <= 0 || res.BrierCalibrated <= 0 {
		t.Errorf("expected positive Brier scores, got raw=%v calibrated=%v", res.BrierRaw, res.BrierCalibrated)
	}
	// On the current tiny corpus the held-out calibrated Brier is WORSE than
	// raw — the walk-forward eval is the gate that says a curve fit on this
	// little data must not be installed. If this flips, the corpus has grown
	// enough to revisit shipping a default curve.
	if res.BrierCalibrated <= res.BrierRaw {
		t.Logf("calibration now helps out-of-sample (raw=%v calibrated=%v) — consider revisiting policy defaults", res.BrierRaw, res.BrierCalibrated)
	}

	// Deterministic: same inputs, same result.
	again, err := WalkForward(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios, 7)
	if err != nil {
		t.Fatal(err)
	}
	if res.BrierRaw != again.BrierRaw || res.BrierCalibrated != again.BrierCalibrated || len(res.Curve.Bins) != len(again.Curve.Bins) {
		t.Error("walk-forward not deterministic across runs")
	}
}

func TestWalkForwardRejectsBadSplit(t *testing.T) {
	scenarios := []Scenario{loadScenario(t, "testdata/scenario.json")}
	if _, err := WalkForward(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios, 0); err == nil {
		t.Error("trainN 0 should be rejected")
	}
	if _, err := WalkForward(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios, 99); err == nil {
		t.Error("trainN beyond the decision count should be rejected")
	}
}
