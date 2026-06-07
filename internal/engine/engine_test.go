package engine

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

func testGoal() domain.Goal {
	return domain.Goal{
		ID:        "goal_1",
		Objective: "Win the market",
		Metric:    "share",
		Context: domain.Context{
			Assets:      []domain.Asset{{Name: "great team"}},
			Constraints: []domain.Constraint{{Name: "tight budget"}},
		},
	}
}

func TestGenerateInitialPlanIsVersionOne(t *testing.T) {
	e := New(llm.NewMockPlanner())
	v, err := e.GenerateInitialPlan(context.Background(), testGoal())
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != 1 {
		t.Errorf("expected version 1, got %d", v.Version)
	}
	if v.PlanID == "" {
		t.Error("expected a plan id")
	}
	if len(v.RankedMoves) != 3 {
		t.Errorf("expected 3 moves, got %d", len(v.RankedMoves))
	}
	if v.Provenance.InputSnapshotID == "" {
		t.Error("expected provenance snapshot id")
	}
}

func TestReplanMaterialOnSignal(t *testing.T) {
	e := New(llm.NewMockPlanner())
	ctx := context.Background()

	current, err := e.GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}

	res, err := e.Replan(ctx, testGoal(), current, "competitor: launched free tier")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Material {
		t.Errorf("expected a signal to be material, reason=%q", res.Reason)
	}
	if res.Candidate.Version != current.Version+1 {
		t.Errorf("candidate should be version %d, got %d", current.Version+1, res.Candidate.Version)
	}
	if res.Candidate.PlanID != current.PlanID {
		t.Error("replan should keep the same plan id")
	}
}

func TestReplanImmaterialWithoutChange(t *testing.T) {
	e := New(llm.NewMockPlanner())
	ctx := context.Background()

	current, _ := e.GenerateInitialPlan(ctx, testGoal())
	// Replanning with no signal note reproduces the same moves, so it should not
	// be considered material.
	res, err := e.Replan(ctx, testGoal(), current, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Material {
		t.Errorf("expected no material change, got reason=%q", res.Reason)
	}
}

func TestThresholdEvaluator(t *testing.T) {
	ev := NewThresholdEvaluator()
	base := []domain.RankedMove{
		{Title: "A", Confidence: 0.80},
		{Title: "B", Confidence: 0.60},
	}

	tests := []struct {
		name      string
		candidate []domain.RankedMove
		want      bool
	}{
		{"identical", base, false},
		{"top changed", []domain.RankedMove{{Title: "X", Confidence: 0.80}, {Title: "B", Confidence: 0.60}}, true},
		{"reordered", []domain.RankedMove{{Title: "B", Confidence: 0.60}, {Title: "A", Confidence: 0.80}}, true},
		{"confidence shift", []domain.RankedMove{{Title: "A", Confidence: 0.65}, {Title: "B", Confidence: 0.60}}, true},
		{"small confidence shift", []domain.RankedMove{{Title: "A", Confidence: 0.78}, {Title: "B", Confidence: 0.60}}, false},
		{"empty candidate", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := ev.IsMaterial(base, tt.candidate)
			if got != tt.want {
				t.Errorf("IsMaterial() = %v (%s), want %v", got, reason, tt.want)
			}
		})
	}

	if material, _ := ev.IsMaterial(nil, base); !material {
		t.Error("no prior plan should always be material")
	}
}
