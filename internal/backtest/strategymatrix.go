package backtest

import (
	"context"
	"fmt"
	"io"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// RunStrategyMatrix replays every scenario under each strategy mode for the
// investing domain: "single" (selection disabled — the legacy blended
// planner), one "<id>-only" config per declared strategy (that lens pinned via
// policy), and "selector" (the full competition). Cells are ordered (config,
// scenario) with configs in a fixed order — single, the pack's canonical
// strategy order, selector — so two runs of the same corpus are byte-identical.
func RunStrategyMatrix(ctx context.Context, reg *pack.Registry, basePol policy.Policy, scenarios []Scenario) ([]MatrixCell, error) {
	d, ok := reg.Get("investing")
	if !ok {
		return nil, fmt.Errorf("backtest: no investing pack in registry")
	}
	if len(d.Strategies) == 0 {
		return nil, fmt.Errorf("backtest: investing pack declares no strategies")
	}

	type config struct {
		name string
		pol  policy.Policy
	}
	off, on := false, true
	configs := []config{{name: "single", pol: overrideStrategy(basePol, policy.StrategySelection{Enabled: &off})}}
	for _, s := range d.Strategies {
		var disable []string
		for _, other := range d.Strategies {
			if other.ID != s.ID {
				disable = append(disable, other.ID)
			}
		}
		configs = append(configs, config{
			name: s.ID + "-only",
			pol:  overrideStrategy(basePol, policy.StrategySelection{Enabled: &on, Disable: disable}),
		})
	}
	configs = append(configs, config{name: "selector", pol: overrideStrategy(basePol, policy.StrategySelection{Enabled: &on})})

	providerFor := fixtureProviders()
	var cells []MatrixCell
	for _, cfg := range configs {
		for _, sc := range scenarios {
			provider, err := providerFor(sc.FixtureDir)
			if err != nil {
				return nil, err
			}
			rep, err := New(reg, cfg.pol, provider).Run(ctx, sc)
			if err != nil {
				return nil, fmt.Errorf("backtest: config %q scenario %q: %w", cfg.name, sc.Name, err)
			}
			cells = append(cells, MatrixCell{Scenario: sc.Name, Config: cfg.name, Report: rep})
		}
	}
	return cells, nil
}

// overrideStrategy returns a copy of pol with the investing domain's strategy
// selection replaced; all other overrides are preserved.
func overrideStrategy(pol policy.Policy, sel policy.StrategySelection) policy.Policy {
	out := policy.Policy{Domains: map[string]policy.DomainPolicy{}}
	for k, v := range pol.Domains {
		out.Domains[k] = v
	}
	dp := out.Domains["investing"]
	dp.Strategy = &sel
	out.Domains["investing"] = dp
	return out
}

// fixtureProviders returns a memoised loader of offline providers per fixture
// directory ("" = the embedded defaults), shared by the comparison runners.
func fixtureProviders() func(dir string) (marketdata.Provider, error) {
	providers := map[string]marketdata.Provider{}
	return func(dir string) (marketdata.Provider, error) {
		if p, ok := providers[dir]; ok {
			return p, nil
		}
		var opts []marketdata.OfflineOption
		if dir != "" {
			opts = append(opts, marketdata.WithFixtureDir(dir))
		}
		p, err := marketdata.NewOfflineProvider(opts...)
		if err != nil {
			return nil, err
		}
		providers[dir] = p
		return p, nil
	}
}

// RenderStrategyMatrix writes the comparison table plus each selector run's
// strategy share, so winner flapping is visible next to the quality metrics.
func RenderStrategyMatrix(w io.Writer, cells []MatrixCell) {
	fmt.Fprintf(w, "%-28s %-16s %8s %10s %8s %8s %6s  %s\n",
		"scenario", "config", "brier", "precision", "recall", "noise", "flips", "strategy share")
	for _, c := range cells {
		share := ""
		for _, k := range sortedKeys(c.Report.StrategyShare) {
			if share != "" {
				share += " "
			}
			share += fmt.Sprintf("%s:%d", k, c.Report.StrategyShare[k])
		}
		fmt.Fprintf(w, "%-28s %-16s %8.3f %10.2f %8.2f %8.2f %6d  %s\n",
			c.Scenario, c.Config, c.Report.BrierScore, c.Report.KillPrecision,
			c.Report.KillRecall, c.Report.NoiseRobustness, c.Report.StrategyFlips, share)
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
