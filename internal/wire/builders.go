package wire

import (
	"context"
	"strings"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// DataSource is the generic marker for an external data provider wired into the
// planner deps. Concrete sources (e.g. marketdata.Provider) are recovered by a
// builder via a type assertion. Keeping it a marker keeps PlannerDeps free of any
// single domain's data shape.
type DataSource interface {
	Name() string
}

// PlannerBuilder constructs the planner for a domain that declares a PlannerKind.
// Returning a nil planner declines (e.g. the required data source is not wired),
// in which case BuildPlannerRouter falls back to the guided/base text planner.
type PlannerBuilder func(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.Planner, error)

// plannerBuilders maps a descriptor's PlannerKind to its builder. Adding a new
// numeric domain registers a builder here and sets the descriptor's PlannerKind —
// no edit to BuildPlannerRouter.
var plannerBuilders = map[string]PlannerBuilder{
	"finance": buildFinancePlanner,
}

// buildFinancePlanner builds the numeric finance planner for a "finance"-kind
// domain. It pulls the "marketdata" data source; if it is absent or of the wrong
// type, it declines (returns nil) so the domain falls back to the guided text
// planner — matching the prior "investing without a provider" behaviour. When
// the descriptor declares strategies and policy enables selection, it builds
// one child planner per strategy and composes them under a selector instead.
func buildFinancePlanner(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.Planner, error) {
	provider, ok := deps.DataSources[marketDataKey].(marketdata.Provider)
	if !ok {
		return nil, nil // no market data wired → decline to guided fallback
	}

	if strategies := activeStrategies(d, pol); selectionEnabled(d, pol) && len(strategies) >= 2 {
		return buildFinanceSelector(d, pol, deps, provider, strategies)
	} else if selectionEnabled(d, pol) && len(strategies) == 1 {
		// Exactly one strategy left (policy disabled the rest): pin that lens as
		// the single planner — no competition, but the lens's prior tilts and
		// scoring overlay still apply. This is how a fixed-strategy baseline is
		// expressed (backtest comparisons, or an operator pinning a lens).
		return buildFinanceChild(d, pol, deps, provider, strategies[0], deps.FinanceInner), nil
	}

	cfg := llm.FinanceConfig{
		Provider:       provider,
		Scoring:        effectiveScoring(d, pol),
		Inner:          deps.FinanceInner,
		Now:            deps.FinanceNow,
		PackID:         d.ID,
		PackVersion:    d.Version,
		PromptTemplate: d.PromptTemplate,
	}
	if dp, ok := pol.For(d.ID); ok && dp.Calibration != nil {
		cfg.Calibration = dp.Calibration
	}
	fin := llm.Planner(llm.NewFinancePlanner(cfg))
	// Finance is cached only via a TTL cache so plans refresh as market data moves;
	// never via the non-expiring text cache.
	if deps.FinanceCache != nil {
		fin = llm.NewCachingPlanner(fin, deps.FinanceCache, deps.CacheObs)
	}
	return fin, nil
}

// selectionEnabled reports whether the strategy competition is on for a domain.
// An explicit policy setting wins; otherwise a domain that declares strategies
// competes them by default — the investing pack's backtest gates
// (TestStrategyMatrixGates) earned the flip, and policy remains the off switch.
func selectionEnabled(d pack.Descriptor, pol policy.Policy) bool {
	if dp, ok := pol.For(d.ID); ok && dp.Strategy != nil && dp.Strategy.Enabled != nil {
		return *dp.Strategy.Enabled
	}
	return len(d.Strategies) > 0
}

// activeStrategies returns the descriptor's strategies minus any the policy
// disables. Unknown IDs in the disable list are simply ignored so a policy
// file survives pack evolution.
func activeStrategies(d pack.Descriptor, pol policy.Policy) []pack.StrategyDescriptor {
	disabled := map[string]bool{}
	if dp, ok := pol.For(d.ID); ok && dp.Strategy != nil {
		for _, id := range dp.Strategy.Disable {
			disabled[id] = true
		}
	}
	var out []pack.StrategyDescriptor
	for _, s := range d.Strategies {
		if !disabled[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// buildFinanceSelector assembles the competing finance children — one per
// strategy lens, each caching independently (the strategy ID is in the child's
// name and so its cache key) — under a SelectorPlanner. The hybrid narrator
// attaches to the selector, never to children, so narration runs once on the
// winner. The selector itself is never cached: selection is cheap and pure,
// and its hysteresis input (the incumbent strategy) must stay live.
func buildFinanceSelector(d pack.Descriptor, pol policy.Policy, deps PlannerDeps, provider marketdata.Provider, strategies []pack.StrategyDescriptor) (llm.Planner, error) {
	dp, hasPolicy := pol.For(d.ID)

	children := make([]llm.StrategyChild, 0, len(strategies))
	for _, s := range strategies {
		// Children carry no narrator: narration runs once on the winner, inside
		// the selector.
		child := buildFinanceChild(d, pol, deps, provider, s, nil)
		children = append(children, llm.StrategyChild{ID: s.ID, Planner: child, Regimes: s.Regimes})
	}

	selCfg := llm.SelectorConfig{
		Children:       children,
		Inner:          deps.FinanceInner,
		PromptTemplate: d.PromptTemplate,
		Regime:         financeRegimeFn(provider, deps.FinanceNow),
	}
	if hasPolicy && dp.Strategy != nil {
		selCfg.Weights = dp.Strategy.Weights
		if dp.Strategy.IncumbentMargin != nil {
			selCfg.IncumbentMargin = *dp.Strategy.IncumbentMargin
		}
	}
	return llm.NewSelectorPlanner(selCfg)
}

// buildFinanceChild builds one strategy lens as a finance planner: the lens's
// params overlay the domain's base scoring, its prior tilts apply, and its
// strategy-suffixed name keys the TTL cache separately per lens.
func buildFinanceChild(d pack.Descriptor, pol policy.Policy, deps PlannerDeps, provider marketdata.Provider, s pack.StrategyDescriptor, inner llm.Planner) llm.Planner {
	dp, hasPolicy := pol.For(d.ID)
	params := strategyParams(s, dp, hasPolicy)
	base := effectiveScoring(d, pol).Normalize()
	cfg := llm.FinanceConfig{
		Provider:       provider,
		Scoring:        params.Apply(base),
		Inner:          inner,
		Now:            deps.FinanceNow,
		PackID:         d.ID,
		PackVersion:    d.Version,
		PromptTemplate: d.PromptTemplate,
		StrategyID:     s.ID,
		PriorWeights:   params.Prior,
		// All lenses state confidence on the domain's BASE weighting, so the
		// selector compares like with like (same scale, different evidence).
		ConfidenceWeights: base.Weights,
	}
	if hasPolicy && dp.Calibration != nil {
		cfg.Calibration = dp.Calibration
	}
	child := llm.Planner(llm.NewFinancePlanner(cfg))
	if deps.FinanceCache != nil {
		child = llm.NewCachingPlanner(child, deps.FinanceCache, deps.CacheObs)
	}
	return child
}

// financeRegimeFn classifies the goal-level market regime from one year of
// point-in-time bars per ticker asset, as of the (possibly simulated) clock.
// Fetch failures and thin history simply leave that ticker unknown — an
// unknown regime gates nothing, so degraded data can only widen the field,
// never narrow it. The regime is recorded in provenance either way, which is
// what makes per-regime outcome fitting possible later.
func financeRegimeFn(provider marketdata.Provider, now func() time.Time) llm.RegimeFn {
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context, g domain.Goal) (string, error) {
		asOf := now()
		var readings []finance.RegimeReading
		for _, a := range g.Context.Assets {
			if !strings.EqualFold(a.Kind, "ticker") {
				continue
			}
			bars, err := provider.HistoricalBars(ctx, strings.ToUpper(strings.TrimSpace(a.Name)), asOf.AddDate(-1, 0, 0), asOf)
			if err != nil {
				continue
			}
			readings = append(readings, finance.ClassifyRegime(bars))
		}
		return string(finance.CombineRegimes(readings)), nil
	}
}

// strategyParams resolves one strategy's numeric tuning: a policy override
// wins, else the descriptor's opaque Scoring (when it holds
// *finance.StrategyParams), else a zero params that leaves the base config
// untouched.
func strategyParams(s pack.StrategyDescriptor, dp policy.DomainPolicy, hasPolicy bool) finance.StrategyParams {
	if hasPolicy && dp.Strategy != nil {
		if p, ok := dp.Strategy.Params[s.ID]; ok && p != nil {
			return *p
		}
	}
	if p, ok := s.Scoring.(*finance.StrategyParams); ok && p != nil {
		return *p
	}
	return finance.StrategyParams{Name: s.ID}
}

// marketDataKey is the DataSources registry key for the market-data provider.
const marketDataKey = "marketdata"

// effectiveScoring resolves a domain's finance scoring config: a policy override
// wins, else the descriptor's opaque Scoring (if it holds a *finance.ScoringConfig).
// A zero config is fine — NewFinancePlanner substitutes DefaultScoringConfig().
func effectiveScoring(d pack.Descriptor, pol policy.Policy) finance.ScoringConfig {
	if dp, ok := pol.For(d.ID); ok && dp.Scoring != nil {
		return *dp.Scoring
	}
	if sc, ok := d.Scoring.(*finance.ScoringConfig); ok && sc != nil {
		return *sc
	}
	return finance.ScoringConfig{}
}
