package llm

import (
	"context"
	"reflect"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func sampleGoal() domain.Goal {
	return domain.Goal{
		ID:        "goal_test",
		Objective: "Reach 1000 customers",
		Metric:    "customers",
		Target:    "1000",
		Context: domain.Context{
			Assets:      []domain.Asset{{Name: "founder network"}},
			Constraints: []domain.Constraint{{Name: "small team"}},
		},
	}
}

func TestMockPlannerDeterministic(t *testing.T) {
	p := NewMockPlanner()
	ctx := context.Background()

	a, err := p.GeneratePlan(ctx, PlanRequest{Goal: sampleGoal()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.GeneratePlan(ctx, PlanRequest{Goal: sampleGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("mock planner is not deterministic for identical input")
	}
}

func TestMockPlannerShape(t *testing.T) {
	p := NewMockPlanner()
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: sampleGoal()})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.RankedMoves) != 3 {
		t.Fatalf("expected 3 ranked moves, got %d", len(res.RankedMoves))
	}
	for i, m := range res.RankedMoves {
		if m.Rank != i+1 {
			t.Errorf("move %d has rank %d", i, m.Rank)
		}
		if m.Title == "" || m.Description == "" || m.Rationale == "" {
			t.Errorf("move %d missing required text fields", i)
		}
		if m.Confidence < 0 || m.Confidence > 1 {
			t.Errorf("move %d confidence out of range: %v", i, m.Confidence)
		}
		if !m.ExpectedImpact.Valid() || !m.Effort.Valid() || !m.Risk.Valid() {
			t.Errorf("move %d has invalid level enum", i)
		}
		if m.Experiment.Title == "" || m.Experiment.DurationDays <= 0 {
			t.Errorf("move %d missing experiment", i)
		}
		if len(m.Experiment.SuccessSignals) == 0 || len(m.Experiment.KillCriteria) == 0 {
			t.Errorf("move %d experiment missing success/kill criteria", i)
		}
		if len(m.FallbackMoves) == 0 {
			t.Errorf("move %d missing fallback moves", i)
		}
	}

	// Confidence should be strictly decreasing with rank.
	if !(res.RankedMoves[0].Confidence > res.RankedMoves[1].Confidence &&
		res.RankedMoves[1].Confidence > res.RankedMoves[2].Confidence) {
		t.Error("confidence should decrease with rank")
	}

	if res.Provenance.Planner != "mock" || res.Provenance.PromptVersion != mockPromptVersion {
		t.Errorf("unexpected provenance: %+v", res.Provenance)
	}
	if res.Provenance.InputSnapshotID == "" || res.Provenance.ReasoningSummary == "" {
		t.Error("provenance must include snapshot id and reasoning summary")
	}
}

func TestMockPlannerSignalLowersConfidence(t *testing.T) {
	p := NewMockPlanner()
	ctx := context.Background()

	base, _ := p.GeneratePlan(ctx, PlanRequest{Goal: sampleGoal()})
	withSignal, _ := p.GeneratePlan(ctx, PlanRequest{Goal: sampleGoal(), SignalNote: "competitor launched"})

	if !(base.RankedMoves[0].Confidence-withSignal.RankedMoves[0].Confidence >= 0.10) {
		t.Errorf("a signal should reduce top-move confidence by at least 0.10: base=%v signal=%v",
			base.RankedMoves[0].Confidence, withSignal.RankedMoves[0].Confidence)
	}
	if base.Provenance.InputSnapshotID == withSignal.Provenance.InputSnapshotID {
		t.Error("snapshot id should differ when a signal note is present")
	}
}

func TestMockPlannerRequiresObjective(t *testing.T) {
	p := NewMockPlanner()
	if _, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{}}); err == nil {
		t.Fatal("expected error for empty objective")
	}
}
