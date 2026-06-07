// Package wire is the composition layer. It is the single place that imports the
// domain packs together with the engine, LLM planners and market-data providers,
// turning pure pack descriptors (overlaid with policy) into the running
// collaborators the engine and service consume: a per-domain
// engine.EvaluatorResolver and an llm.PlannerRouter.
//
// Keeping this assembly here is what lets every other package stay decoupled —
// pack imports only domain/finance, engine imports only domain, llm never imports
// pack — while still supporting many domains tuned by data.
package wire

import (
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// effectiveDelta returns a domain's materiality threshold, applying any policy
// override on top of the pack default.
func effectiveDelta(d pack.Descriptor, pol policy.Policy) float64 {
	if dp, ok := pol.For(d.ID); ok && dp.ConfidenceDelta != nil {
		return *dp.ConfidenceDelta
	}
	return d.Eval.ConfidenceDelta
}

// effectiveScoring returns a domain's scoring config, applying any policy override
// on top of the pack default. Returns (zero,false) for domains with no scoring.
func effectiveScoring(d pack.Descriptor, pol policy.Policy) (finance.ScoringConfig, bool) {
	if dp, ok := pol.For(d.ID); ok && dp.Scoring != nil {
		return *dp.Scoring, true
	}
	if d.Scoring != nil {
		return *d.Scoring, true
	}
	return finance.ScoringConfig{}, false
}

// evaluatorResolver maps a domain key to its materiality policy.
type evaluatorResolver struct {
	byDomain map[string]engine.Evaluator
	def      engine.Evaluator
}

// NewEvaluatorResolver builds an engine.EvaluatorResolver from the registry,
// turning each descriptor's (policy-overlaid) threshold into a ThresholdEvaluator.
func NewEvaluatorResolver(reg *pack.Registry, pol policy.Policy) engine.EvaluatorResolver {
	r := &evaluatorResolver{byDomain: map[string]engine.Evaluator{}}
	for _, id := range reg.IDs() {
		d, _ := reg.Get(id)
		r.byDomain[id] = engine.ThresholdEvaluator{ConfidenceDelta: effectiveDelta(d, pol)}
	}
	def, _ := reg.Get(pack.DefaultDomain)
	r.def = engine.ThresholdEvaluator{ConfidenceDelta: effectiveDelta(def, pol)}
	return r
}

// EvaluatorFor implements engine.EvaluatorResolver. An empty or unknown domain
// resolves to the default policy.
func (r *evaluatorResolver) EvaluatorFor(domainKey string) engine.Evaluator {
	if e, ok := r.byDomain[domainKey]; ok && domainKey != "" {
		return e
	}
	return r.def
}
