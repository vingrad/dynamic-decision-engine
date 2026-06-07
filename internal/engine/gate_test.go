package engine

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

// spyPlanner records whether GeneratePlan was invoked, so a test can assert the
// gate short-circuited before any (expensive) regeneration.
type spyPlanner struct{ called bool }

func (*spyPlanner) Name() string { return "spy" }
func (p *spyPlanner) GeneratePlan(_ context.Context, _ llm.PlanRequest) (llm.PlanResult, error) {
	p.called = true
	return llm.PlanResult{
		Summary:     "s",
		RankedMoves: []domain.RankedMove{{Rank: 1, Key: "x", Title: "X", Confidence: 0.9}},
	}, nil
}

// kindGate skips when the signal kind is in the ignore set.
type kindGate struct{ ignore map[string]bool }

func (g kindGate) ShouldReplan(_ domain.Goal, signalKind string, _ map[string]any, _ []domain.RankedMove) (bool, string) {
	if g.ignore[signalKind] {
		return false, "kind ignored"
	}
	return true, ""
}

type fakeGateResolver struct{ g ReplanGate }

func (f fakeGateResolver) GateFor(string) ReplanGate { return f.g }

func TestGateShortCircuitsTrivialSignal(t *testing.T) {
	current := domain.PlanVersion{
		PlanID: "plan_1", Version: 1,
		RankedMoves: []domain.RankedMove{{Rank: 1, Key: "a", Title: "A", Confidence: 0.8}},
	}
	spy := &spyPlanner{}
	gate := fakeGateResolver{g: kindGate{ignore: map[string]bool{"noise": true}}}
	e := New(spy, WithGateResolver(gate))

	res, err := e.Replan(context.Background(), testGoal(), current, "noise: x", "noise", nil)
	if err != nil {
		t.Fatal(err)
	}
	if spy.called {
		t.Error("planner should not be called when the gate skips the signal")
	}
	if res.Material {
		t.Errorf("gated signal should be immaterial, reason=%q", res.Reason)
	}
}

func TestGateProceedsForMaterialKind(t *testing.T) {
	current := domain.PlanVersion{
		PlanID: "plan_1", Version: 1,
		RankedMoves: []domain.RankedMove{{Rank: 1, Key: "a", Title: "A", Confidence: 0.8}},
	}
	spy := &spyPlanner{}
	gate := fakeGateResolver{g: kindGate{ignore: map[string]bool{"noise": true}}}
	e := New(spy, WithGateResolver(gate))

	if _, err := e.Replan(context.Background(), testGoal(), current, "important: x", "important", nil); err != nil {
		t.Fatal(err)
	}
	if !spy.called {
		t.Error("planner should be called for a non-ignored signal kind")
	}
}
