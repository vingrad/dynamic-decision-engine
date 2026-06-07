package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

func newTestService() *Service {
	return New(storage.NewMemory(), engine.New(llm.NewMockPlanner()))
}

func makeGoal(t *testing.T, s *Service) domain.Goal {
	t.Helper()
	g, err := s.CreateGoal(context.Background(), CreateGoalInput{
		Objective: "Grow to 1000 customers",
		Metric:    "customers",
		Context: domain.Context{
			Assets:      []domain.Asset{{Name: "founder network"}},
			Constraints: []domain.Constraint{{Name: "small team"}},
		},
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	return g
}

func TestCreateGoalValidation(t *testing.T) {
	s := newTestService()
	_, err := s.CreateGoal(context.Background(), CreateGoalInput{})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

func TestGeneratePlanAndConflict(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)

	v1, err := s.GeneratePlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 || len(v1.RankedMoves) != 3 {
		t.Fatalf("unexpected v1: version=%d moves=%d", v1.Version, len(v1.RankedMoves))
	}

	if _, err := s.GeneratePlan(ctx, g.ID); !errors.Is(err, ErrPlanExists) {
		t.Fatalf("expected ErrPlanExists, got %v", err)
	}

	if _, err := s.GeneratePlan(ctx, "goal_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown goal, got %v", err)
	}
}

func TestApplySignalMaterialThenImmaterial(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	// First signal: materially lowers confidence -> new version 2.
	r1, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"})
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Material || r1.PlanVersion.Version != 2 {
		t.Fatalf("expected material v2, got material=%v v=%d", r1.Material, r1.PlanVersion.Version)
	}

	// Second identical signal: the penalty is already baked into v2, so replanning
	// against v2 yields no material change -> stays at v2.
	r2, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Material || r2.PlanVersion.Version != 2 {
		t.Fatalf("expected immaterial v2, got material=%v v=%d", r2.Material, r2.PlanVersion.Version)
	}
}

func TestApplySignalRequiresPlan(t *testing.T) {
	s := newTestService()
	g := makeGoal(t, s)
	_, err := s.ApplySignal(context.Background(), SignalInput{GoalID: g.ID, Kind: "x"})
	if !errors.Is(err, ErrNoPlanForGoal) {
		t.Fatalf("expected ErrNoPlanForGoal, got %v", err)
	}
}

// TestApplySignalConcurrent exercises the optimistic-concurrency retry: many
// signals arrive at once for the same plan. The retry must turn version conflicts
// into correct serialized behavior — every call returns without error, and the
// version history is contiguous with no duplicates or lost writes.
func TestApplySignalConcurrent(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)
	plan, err := s.GeneratePlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}

	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent ApplySignal returned error: %v", e)
		}
	}

	versions, err := s.ListPlanVersions(ctx, plan.PlanID, storage.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range versions {
		if v.Version != i+1 {
			t.Fatalf("versions not contiguous: index %d has version %d (%v)", i, v.Version, versionNums(versions))
		}
	}
	cur, err := s.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.CurrentVersion.Version != len(versions) {
		t.Fatalf("current version %d != last version %d", cur.CurrentVersion.Version, len(versions))
	}
}

func versionNums(vs []domain.PlanVersion) []int {
	out := make([]int, len(vs))
	for i, v := range vs {
		out[i] = v.Version
	}
	return out
}
