package wire

import (
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
// planner — matching the prior "investing without a provider" behaviour.
func buildFinancePlanner(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.Planner, error) {
	provider, ok := deps.DataSources[marketDataKey].(marketdata.Provider)
	if !ok {
		return nil, nil // no market data wired → decline to guided fallback
	}

	fin := llm.Planner(llm.NewFinancePlanner(llm.FinanceConfig{
		Provider:    provider,
		Scoring:     effectiveScoring(d, pol),
		Inner:       deps.FinanceInner,
		Now:         deps.FinanceNow,
		PackID:      d.ID,
		PackVersion: d.Version,
	}))
	// Finance is cached only via a TTL cache so plans refresh as market data moves;
	// never via the non-expiring text cache.
	if deps.FinanceCache != nil {
		fin = llm.NewCachingPlanner(fin, deps.FinanceCache, deps.CacheObs)
	}
	return fin, nil
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
