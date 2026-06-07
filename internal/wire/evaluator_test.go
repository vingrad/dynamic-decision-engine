package wire

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

func TestNewEvaluatorResolverDefaults(t *testing.T) {
	r := NewEvaluatorResolver(pack.NewRegistry(), policy.Policy{})

	cases := map[string]float64{
		"":          0.10, // empty -> generic default
		"generic":   0.10,
		"investing": 0.05,
		"growth":    0.10,
		"bogus":     0.10, // unknown -> default
	}
	for domainKey, want := range cases {
		ev, ok := r.EvaluatorFor(domainKey).(engine.ThresholdEvaluator)
		if !ok {
			t.Fatalf("%q: expected ThresholdEvaluator", domainKey)
		}
		if ev.ConfidenceDelta != want {
			t.Errorf("%q: ConfidenceDelta = %v, want %v", domainKey, ev.ConfidenceDelta, want)
		}
	}
}

func TestNewEvaluatorResolverPolicyOverride(t *testing.T) {
	delta := 0.42
	pol := policy.Policy{Domains: map[string]policy.DomainPolicy{
		"investing": {ConfidenceDelta: &delta},
	}}
	r := NewEvaluatorResolver(pack.NewRegistry(), pol)
	ev := r.EvaluatorFor("investing").(engine.ThresholdEvaluator)
	if ev.ConfidenceDelta != delta {
		t.Errorf("policy override not applied: got %v, want %v", ev.ConfidenceDelta, delta)
	}
	// Non-overridden domain keeps its default.
	gen := r.EvaluatorFor("generic").(engine.ThresholdEvaluator)
	if gen.ConfidenceDelta != 0.10 {
		t.Errorf("generic should keep default 0.10, got %v", gen.ConfidenceDelta)
	}
}
