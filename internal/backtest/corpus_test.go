package backtest

import (
	"context"
	"strings"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// corpusFiles is the fixture scenario corpus; new scenarios join here.
var corpusFiles = []string{
	"testdata/scenario.json",
	"testdata/scenario_noise.json",
	"testdata/scenario_rotation.json",
	"testdata/scenario_slow_drawdown.json",
}

func newHarness(t *testing.T) *Harness {
	t.Helper()
	prov, err := marketdata.NewOfflineProvider()
	if err != nil {
		t.Fatal(err)
	}
	return New(pack.NewRegistry(), policy.Policy{}, prov)
}

func TestScenarioCorpus(t *testing.T) {
	cases := []struct {
		file  string
		check func(t *testing.T, rep Report)
	}{
		{
			file: "testdata/scenario_noise.json",
			check: func(t *testing.T, rep Report) {
				// Four noise events must not produce a replan; the thesis break must.
				if rep.NoiseRobustness != 1 {
					t.Errorf("noise robustness = %v, want 1", rep.NoiseRobustness)
				}
				if rep.KillRecall != 1 || rep.KillPrecision != 1 {
					t.Errorf("kill recall/precision = %v/%v, want 1/1", rep.KillRecall, rep.KillPrecision)
				}
				if rep.VersionsCreated != 1 {
					t.Errorf("versions created = %d, want 1", rep.VersionsCreated)
				}
			},
		},
		{
			file: "testdata/scenario_rotation.json",
			check: func(t *testing.T, rep Report) {
				// The ACME thesis break must rotate the book onto GLOBEX.
				last := rep.Decisions[len(rep.Decisions)-1]
				if !last.Material || last.TopMove != "Thesis: GLOBEX" {
					t.Errorf("expected material rotation to GLOBEX, got %+v", last)
				}
				if rep.KillRecall != 1 {
					t.Errorf("kill recall = %v, want 1", rep.KillRecall)
				}
			},
		},
		{
			file: "testdata/scenario_slow_drawdown.json",
			check: func(t *testing.T, rep Report) {
				// Known blind spot: the price-move mean-reversion tilt never cuts a
				// slowly bleeding position, so recall is 0 and the Brier score is
				// worse than a coin flip. This scenario exists to measure progress on
				// exactly that — improvements should raise recall / lower Brier.
				if rep.KillRecall != 0 {
					t.Errorf("kill recall = %v; the blind spot apparently moved — update this scenario's expectations", rep.KillRecall)
				}
				if rep.BrierScore <= 0.25 {
					t.Errorf("brier = %v; expected worse-than-coin-flip on the blind-spot scenario", rep.BrierScore)
				}
				if rep.NoiseRobustness != 1 {
					t.Errorf("noise robustness = %v, want 1", rep.NoiseRobustness)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			rep, err := newHarness(t).Run(context.Background(), loadScenario(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, rep)
		})
	}
}

func TestRunMatrix(t *testing.T) {
	var scenarios []Scenario
	for _, f := range corpusFiles {
		scenarios = append(scenarios, loadScenario(t, f))
	}
	configs := map[string]finance.ScoringConfig{
		"default": finance.DefaultScoringConfig(),
		"ev-heavy": {
			Weights:         finance.ScoreWeights{EV: 0.70, Risk: 0.10, Liquidity: 0.10, Horizon: 0.10},
			Risk:            finance.DefaultScoringConfig().Risk,
			RewardRiskRatio: 2.0,
		},
	}

	cells, err := RunMatrix(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios, configs)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != len(scenarios)*len(configs) {
		t.Fatalf("expected %d cells, got %d", len(scenarios)*len(configs), len(cells))
	}
	// Deterministic order: configs sorted by name, scenarios in input order.
	if cells[0].Config != "default" || cells[len(cells)-1].Config != "ev-heavy" {
		t.Errorf("unexpected config order: first %q last %q", cells[0].Config, cells[len(cells)-1].Config)
	}
	for i, c := range cells {
		if c.Scenario != scenarios[i%len(scenarios)].Name {
			t.Errorf("cell %d: scenario %q, want %q", i, c.Scenario, scenarios[i%len(scenarios)].Name)
		}
		if len(c.Report.Decisions) == 0 {
			t.Errorf("cell %d (%s/%s): empty report", i, c.Config, c.Scenario)
		}
	}

	var b strings.Builder
	RenderMatrix(&b, cells)
	if !strings.Contains(b.String(), "ev-heavy") || !strings.Contains(b.String(), "ACME noise gauntlet") {
		t.Errorf("render missing expected rows:\n%s", b.String())
	}

	// Determinism: a second run yields identical cells.
	again, err := RunMatrix(context.Background(), pack.NewRegistry(), policy.Policy{}, scenarios, configs)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cells {
		if cells[i].Report.BrierScore != again[i].Report.BrierScore || cells[i].Report.VersionsCreated != again[i].Report.VersionsCreated {
			t.Errorf("cell %d not deterministic across runs", i)
		}
	}
}
