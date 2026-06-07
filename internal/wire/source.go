package wire

import (
	"context"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
	"github.com/vingrad/dynamic-decision-engine/internal/source"
)

// SourceDeps are the collaborators the per-domain enrichers are assembled from. It is
// the source analogue of PlannerDeps, kept separate so the planner wiring stays free
// of source concerns.
type SourceDeps struct {
	// Sources holds pre-built source adapters keyed by name (e.g. "pricefeed"),
	// mirroring how PlannerDeps.DataSources registers the market-data provider. A
	// domain's SourceKinds are resolved against this registry.
	Sources map[string]source.Source
	// Timeout bounds a single source Fetch; zero means no per-source deadline.
	Timeout time.Duration
	// Now overrides the enricher clock (tests/backtests). nil means time.Now.
	Now func() time.Time
}

// noopEnricher returns the goal unchanged with no contributions. Used for domains
// with no resolvable sources so enrichment never perturbs the snapshot.
type noopEnricher struct{}

func (noopEnricher) Enrich(_ context.Context, goal domain.Goal, _ string, _ map[string]any) (domain.Goal, []domain.SourceContribution) {
	return goal, nil
}

// sourceResolver maps a domain key to its Enricher.
type sourceResolver struct {
	byDomain map[string]engine.Enricher
	def      engine.Enricher
}

// SourcesFor implements engine.SourceResolver. An empty or unknown domain resolves to
// the default (no-op) enricher.
func (r *sourceResolver) SourcesFor(domainKey string) engine.Enricher {
	if e, ok := r.byDomain[domainKey]; ok && domainKey != "" {
		return e
	}
	return r.def
}

// effectiveSourceKinds returns the source keys a domain consults. A policy override
// (full replace, not merge) wins over the pack default, mirroring effectiveIgnoreKinds.
func effectiveSourceKinds(d pack.Descriptor, pol policy.Policy) []string {
	if dp, ok := pol.For(d.ID); ok && dp.SourceKinds != nil {
		return *dp.SourceKinds
	}
	return d.SourceKinds
}

// NewSourceResolver builds an engine.SourceResolver from the registry: each domain's
// (policy-overlaid) SourceKinds are resolved against the wired source registry, in
// declared order, and wrapped in an Enricher. A domain with no resolvable sources is
// omitted and falls through to the default no-op enricher, so its snapshot is
// byte-for-byte unchanged. When deps.Sources is empty the resolver is all-noop.
func NewSourceResolver(reg *pack.Registry, pol policy.Policy, deps SourceDeps) engine.SourceResolver {
	r := &sourceResolver{byDomain: map[string]engine.Enricher{}, def: noopEnricher{}}
	for _, id := range reg.IDs() {
		d, _ := reg.Get(id)
		var srcs []source.Source
		for _, key := range effectiveSourceKinds(d, pol) {
			if s, ok := deps.Sources[key]; ok && s != nil {
				srcs = append(srcs, s)
			}
		}
		if len(srcs) > 0 {
			r.byDomain[id] = source.NewEnricher(srcs, deps.Timeout, deps.Now)
		}
	}
	return r
}
