package wire

import (
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// PlannerDeps are the collaborators the planner router is assembled from.
type PlannerDeps struct {
	// Base is the underlying model planner (mock/anthropic/openai) used by the
	// text-based domains via a GuidedPlanner.
	Base llm.Planner
	// DataSources holds external data providers keyed by name (e.g. "marketdata"),
	// from which a numeric domain's planner builder pulls the source it needs. A
	// domain whose builder finds no matching source declines and falls back to the
	// guided text planner.
	DataSources map[string]DataSource
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
		// The text/LLM cache has no TTL: identical decision inputs reuse one result
		// for the process lifetime. This intentionally also memoises multi-model
		// compositions (verify/route/ensemble) by input snapshot — same semantics as
		// caching a single LLM. Finance, whose output depends on as-of market data,
		// uses the separate TTL cache below instead.
		cached = llm.NewCachingPlanner(deps.Base, deps.Cache, deps.CacheObs)
	}

	routes := map[string]llm.Planner{}
	for _, id := range reg.IDs() {
		d, _ := reg.Get(id)

		// A domain that declares a PlannerKind is built by the matching registered
		// builder (e.g. the numeric finance planner). A builder that returns a nil
		// planner declines (e.g. its data source is not wired), and the domain falls
		// through to the guided/base text path below — preserving prior behaviour.
		if b, ok := plannerBuilders[d.PlannerKind]; ok && d.PlannerKind != "" {
			if p, err := b(d, pol, deps); err == nil && p != nil {
				routes[id] = p
				continue
			}
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
