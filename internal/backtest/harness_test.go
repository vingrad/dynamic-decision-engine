package backtest

import (
	"context"
	"encoding/json"
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
	h := New(pack.NewRegistry(), policy.Policy{}, prov)
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
}
