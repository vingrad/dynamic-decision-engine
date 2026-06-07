package engine

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

// fakeResolver maps domains to evaluators with distinct thresholds.
type fakeResolver struct{ m map[string]Evaluator }

func (f fakeResolver) EvaluatorFor(d string) Evaluator {
	if e, ok := f.m[d]; ok {
		return e
	}
	return f.m["generic"]
}

// fixedPlanner returns a controllable confidence on the top move so materiality
// can be exercised deterministically.
type fixedPlanner struct{ confidence float64 }

func (fixedPlanner) Name() string { return "fixed" }
func (p fixedPlanner) GeneratePlan(_ context.Context, _ llm.PlanRequest) (llm.PlanResult, error) {
	return llm.PlanResult{
		Summary:     "s",
		RankedMoves: []domain.RankedMove{{Rank: 1, Title: "A", Confidence: p.confidence}},
	}, nil
}

func TestEngineNoResolverBackwardCompat(t *testing.T) {
	// With no resolver, behaviour matches the original single-evaluator engine.
	e := New(llm.NewMockPlanner())
	v, err := e.GenerateInitialPlan(context.Background(), testGoal())
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != 1 || len(v.RankedMoves) != 3 {
		t.Fatalf("unexpected plan: version=%d moves=%d", v.Version, len(v.RankedMoves))
	}
}

func TestEngineUsesPerDomainEvaluator(t *testing.T) {
	resolver := fakeResolver{m: map[string]Evaluator{
		"generic":   ThresholdEvaluator{ConfidenceDelta: 0.10},
		"investing": ThresholdEvaluator{ConfidenceDelta: 0.05},
	}}
	// A 0.07 confidence drop: material for investing (0.05), immaterial for generic (0.10).
	current := domain.PlanVersion{
		PlanID: "plan_1", Version: 1,
		RankedMoves: []domain.RankedMove{{Rank: 1, Title: "A", Confidence: 0.80}},
	}
	e := New(fixedPlanner{confidence: 0.73}, WithEvaluatorResolver(resolver))

	inv := testGoal()
	inv.Domain = "investing"
	res, err := e.Replan(context.Background(), inv, current, "sig", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Material {
		t.Errorf("0.07 drop should be material for investing, reason=%q", res.Reason)
	}

	gen := testGoal() // Domain == "" => generic
	res, err = e.Replan(context.Background(), gen, current, "sig", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Material {
		t.Errorf("0.07 drop should be immaterial for generic, reason=%q", res.Reason)
	}
}
