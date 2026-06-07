package main

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

func TestPlannerFromSpec(t *testing.T) {
	cfg := config.Default() // Planner "mock"

	cases := map[string]struct {
		spec policy.PlannerSpec
		want string // expected planner Name()
	}{
		"empty inherits global":   {policy.PlannerSpec{}, "mock"},
		"model only keeps mock":   {policy.PlannerSpec{Model: "claude-x"}, "mock"},
		"single backend override": {policy.PlannerSpec{Planner: "anthropic"}, "anthropic"},
		"multi ensemble": {policy.PlannerSpec{
			Planner: "multi", MultiMode: "ensemble", MultiProviders: []string{"mock", "mock"},
		}, "multi:ensemble"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := plannerFromSpec(c.spec, cfg).Name(); got != c.want {
				t.Errorf("planner name = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDomainBaseResolver(t *testing.T) {
	cfg := config.Default()
	global := newPlanner(cfg)
	pol := policy.Policy{Domains: map[string]policy.DomainPolicy{
		"growth": {Planner: &policy.PlannerSpec{Planner: "anthropic"}},
	}}
	resolve := domainBaseResolver(cfg, pol, global)

	// Override domain resolves to its own planner.
	if got := resolve("growth").Name(); got != "anthropic" {
		t.Errorf("growth should resolve to anthropic, got %q", got)
	}
	// A domain with no override returns the SAME global instance — this identity is
	// what collapses the wire cacheWrap to a single shared wrapper.
	if resolve("career") != global {
		t.Error("non-override domain should return the global base instance")
	}
	// Memoised: repeat calls return the same instance.
	if resolve("growth") != resolve("growth") {
		t.Error("resolver should memoise per domain id")
	}
}
