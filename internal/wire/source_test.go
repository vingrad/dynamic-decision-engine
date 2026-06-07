package wire

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
	"github.com/vingrad/dynamic-decision-engine/internal/source"
)

// factSource is a minimal Source that folds in one fixed fact.
type factSource struct {
	name string
	fact string
}

func (s factSource) Describe() source.Descriptor { return source.Descriptor{Name: s.name} }
func (s factSource) Fetch(context.Context, source.Query) (source.Result, error) {
	return source.Result{SourceName: s.name, Delta: source.ContextDelta{Facts: []string{s.fact}}}, nil
}

func TestNewSourceResolverWiresDeclaredDomain(t *testing.T) {
	reg := pack.NewRegistryFrom(pack.Descriptor{
		ID:          "purchasing",
		Name:        "Purchasing",
		Version:     "1",
		SourceKinds: []string{"pricefeed"},
	})
	deps := SourceDeps{Sources: map[string]source.Source{
		"pricefeed": factSource{name: "pricefeed", fact: "price is 42"},
	}}

	r := NewSourceResolver(reg, policy.Policy{}, deps)

	// The declared domain gets a working enricher.
	enr := r.SourcesFor("purchasing")
	goal, contribs := enr.Enrich(context.Background(), domain.Goal{Domain: "purchasing"}, "", nil)
	if len(goal.Context.Facts) != 1 || goal.Context.Facts[0] != "price is 42" {
		t.Errorf("expected the source fact folded in, got %v", goal.Context.Facts)
	}
	if len(contribs) != 1 || contribs[0].SourceName != "pricefeed" {
		t.Fatalf("expected one pricefeed contribution, got %+v", contribs)
	}

	// A domain that declares no sources is a clean no-op.
	noop := r.SourcesFor("generic")
	g2, c2 := noop.Enrich(context.Background(), domain.Goal{Domain: "generic"}, "", nil)
	if len(g2.Context.Facts) != 0 || len(c2) != 0 {
		t.Errorf("undeclared domain should be a no-op, got facts=%v contribs=%v", g2.Context.Facts, c2)
	}
}

func TestNewSourceResolverPolicyOverride(t *testing.T) {
	reg := pack.NewRegistryFrom(pack.Descriptor{
		ID:          "purchasing",
		Name:        "Purchasing",
		Version:     "1",
		SourceKinds: []string{"pricefeed"}, // pack default
	})
	deps := SourceDeps{Sources: map[string]source.Source{
		"pricefeed": factSource{name: "pricefeed", fact: "default"},
		"override":  factSource{name: "override", fact: "from policy"},
	}}
	// Policy replaces the pack's source list for this domain.
	pol := policy.Policy{Domains: map[string]policy.DomainPolicy{
		"purchasing": {SourceKinds: &[]string{"override"}},
	}}

	r := NewSourceResolver(reg, pol, deps)
	goal, contribs := r.SourcesFor("purchasing").Enrich(context.Background(), domain.Goal{Domain: "purchasing"}, "", nil)
	if len(contribs) != 1 || contribs[0].SourceName != "override" {
		t.Fatalf("expected the policy-overridden source, got %+v", contribs)
	}
	if len(goal.Context.Facts) != 1 || goal.Context.Facts[0] != "from policy" {
		t.Errorf("expected the override fact, got %v", goal.Context.Facts)
	}

	// An explicit empty override disables enrichment.
	polEmpty := policy.Policy{Domains: map[string]policy.DomainPolicy{
		"purchasing": {SourceKinds: &[]string{}},
	}}
	r2 := NewSourceResolver(reg, polEmpty, deps)
	_, c2 := r2.SourcesFor("purchasing").Enrich(context.Background(), domain.Goal{Domain: "purchasing"}, "", nil)
	if len(c2) != 0 {
		t.Errorf("empty override should disable enrichment, got %+v", c2)
	}
}

func TestNewSourceResolverSkipsUnknownKey(t *testing.T) {
	reg := pack.NewRegistryFrom(pack.Descriptor{
		ID:          "purchasing",
		Name:        "Purchasing",
		Version:     "1",
		SourceKinds: []string{"missing"},
	})
	// No sources wired -> the key resolves to nothing -> no-op enricher.
	r := NewSourceResolver(reg, policy.Policy{}, SourceDeps{})
	g, c := r.SourcesFor("purchasing").Enrich(context.Background(), domain.Goal{Domain: "purchasing"}, "", nil)
	if len(g.Context.Facts) != 0 || len(c) != 0 {
		t.Errorf("unresolved source key should yield a no-op, got facts=%v contribs=%v", g.Context.Facts, c)
	}
}
