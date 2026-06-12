package backtest

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// MatrixCell is one (scenario, scoring config) replay of a comparison run.
type MatrixCell struct {
	Scenario string `json:"scenario"`
	Config   string `json:"config"`
	Report   Report `json:"report"`
}

// RunMatrix replays every scenario under every named scoring config and returns
// the cells ordered by (config name, scenario order) — deterministic, so config
// changes can be compared in one table. Each config is applied as a policy
// override for the investing domain, exactly as DDE_POLICY would apply it. A
// scenario with a FixtureDir gets its own offline provider; others share the
// embedded fixtures.
func RunMatrix(ctx context.Context, reg *pack.Registry, basePol policy.Policy, scenarios []Scenario, configs map[string]finance.ScoringConfig) ([]MatrixCell, error) {
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)

	providerFor := fixtureProviders()

	var cells []MatrixCell
	for _, name := range names {
		pol := overrideScoring(basePol, configs[name])
		for _, sc := range scenarios {
			provider, err := providerFor(sc.FixtureDir)
			if err != nil {
				return nil, err
			}
			h, err := New(reg, pol, provider)
			if err != nil {
				return nil, err
			}
			rep, err := h.Run(ctx, sc)
			if err != nil {
				return nil, fmt.Errorf("backtest: config %q scenario %q: %w", name, sc.Name, err)
			}
			cells = append(cells, MatrixCell{Scenario: sc.Name, Config: name, Report: rep})
		}
	}
	return cells, nil
}

// overrideScoring returns a copy of pol with the investing domain's scoring
// config replaced; all other overrides are preserved.
func overrideScoring(pol policy.Policy, cfg finance.ScoringConfig) policy.Policy {
	out := policy.Policy{Domains: map[string]policy.DomainPolicy{}}
	for k, v := range pol.Domains {
		out.Domains[k] = v
	}
	dp := out.Domains["investing"]
	dp.Scoring = &cfg
	out.Domains["investing"] = dp
	return out
}

// RenderMatrix writes a compact comparison table of the matrix cells to w.
func RenderMatrix(w io.Writer, cells []MatrixCell) {
	fmt.Fprintf(w, "%-28s %-16s %8s %10s %8s %8s\n", "scenario", "config", "brier", "precision", "recall", "noise")
	for _, c := range cells {
		fmt.Fprintf(w, "%-28s %-16s %8.3f %10.2f %8.2f %8.2f\n",
			c.Scenario, c.Config, c.Report.BrierScore, c.Report.KillPrecision, c.Report.KillRecall, c.Report.NoiseRobustness)
	}
}
