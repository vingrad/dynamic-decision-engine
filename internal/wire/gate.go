package wire

import (
	"strings"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// effectiveIgnoreKinds returns the signal kinds a domain never replans on,
// applying any policy override (which fully replaces, not merges) on top of the
// pack default.
func effectiveIgnoreKinds(d pack.Descriptor, pol policy.Policy) []string {
	if dp, ok := pol.For(d.ID); ok && dp.IgnoreSignalKinds != nil {
		return *dp.IgnoreSignalKinds
	}
	return d.Eval.IgnoreSignalKinds
}

// KindGate is the default engine.ReplanGate: it skips regeneration when the
// triggering signal's kind is in the domain's ignore set. Kinds are matched
// case-insensitively. An empty set proceeds on every signal.
type KindGate struct {
	ignore map[string]bool
}

// ShouldReplan implements engine.ReplanGate.
func (g KindGate) ShouldReplan(_ domain.Goal, signalKind string, _ map[string]any, _ []domain.RankedMove) (bool, string) {
	if g.ignore[strings.ToLower(strings.TrimSpace(signalKind))] {
		return false, "signal kind \"" + signalKind + "\" is not material for this domain"
	}
	return true, ""
}

// gateResolver maps a domain key to its replan gate.
type gateResolver struct {
	byDomain map[string]engine.ReplanGate
	def      engine.ReplanGate
}

// NewGateResolver builds an engine.GateResolver from the registry, turning each
// descriptor's (policy-overlaid) ignore-kinds list into a KindGate.
func NewGateResolver(reg *pack.Registry, pol policy.Policy) engine.GateResolver {
	r := &gateResolver{byDomain: map[string]engine.ReplanGate{}}
	for _, id := range reg.IDs() {
		d, _ := reg.Get(id)
		r.byDomain[id] = newKindGate(effectiveIgnoreKinds(d, pol))
	}
	def, _ := reg.Get(pack.DefaultDomain)
	r.def = newKindGate(effectiveIgnoreKinds(def, pol))
	return r
}

// GateFor implements engine.GateResolver. An empty or unknown domain resolves to
// the default gate.
func (r *gateResolver) GateFor(domainKey string) engine.ReplanGate {
	if g, ok := r.byDomain[domainKey]; ok && domainKey != "" {
		return g
	}
	return r.def
}

func newKindGate(kinds []string) KindGate {
	ignore := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		ignore[strings.ToLower(strings.TrimSpace(k))] = true
	}
	return KindGate{ignore: ignore}
}
