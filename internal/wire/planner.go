package wire

import (
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// PlannerDeps are the collaborators the planner router is assembled from.
type PlannerDeps struct {
	// Base is the underlying model planner (mock/anthropic/openai) used by the
	// text-based domains via a GuidedPlanner.
	Base llm.Planner
	// Provider, when non-nil, enables the numeric finance planner for the
	// investing domain.
	Provider marketdata.Provider
	// Cache, when non-nil, memoises text/LLM planner results (deterministic, so no
	// expiry). Keys are namespaced by base name and prompt.
	Cache llm.PlanCache
	// FinanceCache, when non-nil, memoises finance planner results. It MUST have a
	// TTL (NewMemoryCacheTTL) because finance output depends on as-of market data;
	// a non-expiring cache here would serve stale plans.
	FinanceCache llm.PlanCache
	CacheObs     llm.CacheObserver
	// FinanceInner optionally adds LLM narration to finance theses (hybrid mode).
	FinanceInner llm.Planner
	// FinanceNow overrides the finance planner's clock (backtests inject sim time).
	FinanceNow func() time.Time
}

// BuildPlannerRouter assembles a per-domain llm.PlannerRouter from the registry
// (overlaid with policy) and the given dependencies.
//
// Composition order is load-bearing: Guided(Caching(base)) so the guided planner
// sets the domain prompt override BEFORE the cache computes its key — otherwise
// every domain would collide on one cache entry.
func BuildPlannerRouter(reg *pack.Registry, pol policy.Policy, deps PlannerDeps) llm.Planner {
	cached := deps.Base
	if deps.Cache != nil {
		cached = llm.NewCachingPlanner(deps.Base, deps.Cache, deps.CacheObs)
	}

	routes := map[string]llm.Planner{}
	for _, id := range reg.IDs() {
		d, _ := reg.Get(id)

		// Investing uses the numeric finance planner when a provider is configured.
		if id == "investing" && deps.Provider != nil {
			scoring, _ := effectiveScoring(d, pol)
			fin := llm.Planner(llm.NewFinancePlanner(llm.FinanceConfig{
				Provider:    deps.Provider,
				Scoring:     scoring,
				Inner:       deps.FinanceInner,
				Now:         deps.FinanceNow,
				PackID:      d.ID,
				PackVersion: d.Version,
			}))
			// Finance is cached only via a TTL cache so plans refresh as market data
			// moves; never via the non-expiring text cache.
			if deps.FinanceCache != nil {
				fin = llm.NewCachingPlanner(fin, deps.FinanceCache, deps.CacheObs)
			}
			routes[id] = fin
			continue
		}

		// The generic domain uses the base unchanged (empty template, no pack stamp)
		// so its prompt and provenance stay byte-for-byte as before.
		if id == pack.DefaultDomain {
			routes[id] = cached
			continue
		}

		routes[id] = llm.NewGuidedPlanner(cached, llm.GuidedConfig{
			PackID:         d.ID,
			PackVersion:    d.Version,
			PromptVersion:  d.PromptVersion,
			PromptTemplate: d.PromptTemplate,
		})
	}

	// The default (empty/unknown domain) is the generic, unstamped base.
	return llm.NewPlannerRouter(cached, routes)
}
