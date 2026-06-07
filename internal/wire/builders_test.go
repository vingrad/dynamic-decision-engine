package wire

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// sentinelPlanner is a stand-in numeric planner for a hypothetical second domain.
type sentinelPlanner struct{}

func (sentinelPlanner) Name() string { return "sentinel" }
func (sentinelPlanner) GeneratePlan(context.Context, llm.PlanRequest) (llm.PlanResult, error) {
	return llm.PlanResult{
		RankedMoves: []domain.RankedMove{{Rank: 1, Title: "sentinel move"}},
		Provenance:  domain.DecisionProvenance{Planner: "sentinel", PackID: "sports"},
	}, nil
}

// TestSecondNumericDomainNeedsNoRouterEdit is the proof that the planner-kind
// hardcode is gone: a brand-new numeric domain is added by (a) registering a
// builder for its PlannerKind and (b) supplying a descriptor via NewRegistryFrom —
// with NO change to BuildPlannerRouter. The router must route it to that builder's
// planner.
func TestSecondNumericDomainNeedsNoRouterEdit(t *testing.T) {
	const kind = "sports-test"
	plannerBuilders[kind] = func(pack.Descriptor, policy.Policy, PlannerDeps) (llm.Planner, error) {
		return sentinelPlanner{}, nil
	}
	t.Cleanup(func() { delete(plannerBuilders, kind) })

	reg := pack.NewRegistryFrom(pack.Descriptor{
		ID:          "sports",
		Name:        "Sports",
		Version:     "1",
		PlannerKind: kind,
	})
	router := BuildPlannerRouter(reg, policy.Policy{}, PlannerDeps{Base: llm.NewMockPlanner()})

	res := plan(t, router, "sports")
	if res.Provenance.Planner != "sentinel" {
		t.Fatalf("sports should route to the registered builder, got planner %q", res.Provenance.Planner)
	}

	// And an unrelated built-in text domain still routes normally.
	gr := plan(t, router, "growth")
	if gr.Provenance.Planner != "mock" || gr.Provenance.PackID != "growth" {
		t.Errorf("growth should still route to guided base: %+v", gr.Provenance)
	}
}

// TestFinanceBuilderDeclinesWithoutDataSource verifies the fallback: a finance
// domain with no market-data source wired falls through to the guided text planner
// (the prior "investing without a provider" behaviour).
func TestFinanceBuilderDeclinesWithoutDataSource(t *testing.T) {
	router := BuildPlannerRouter(pack.NewRegistry(), policy.Policy{}, PlannerDeps{
		Base: llm.NewMockPlanner(),
		// no DataSources
	})
	inv := plan(t, router, "investing")
	if inv.Provenance.Planner != "mock" || inv.Provenance.PackID != "investing" {
		t.Errorf("investing without market data should fall back to guided base: %+v", inv.Provenance)
	}
}
