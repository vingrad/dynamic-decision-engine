package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func newGoal(id string) *domain.Goal {
	return &domain.Goal{ID: id, Objective: "obj " + id, CreatedAt: time.Now().UTC()}
}

func newVersion(planID string, v int) *domain.PlanVersion {
	return &domain.PlanVersion{
		PlanID:      planID,
		Version:     v,
		Goal:        "obj",
		Summary:     "summary",
		RankedMoves: []domain.RankedMove{{Rank: 1, Title: "A", Confidence: 0.8}},
		CreatedAt:   time.Now().UTC(),
	}
}

func TestMemoryGoalRoundTrip(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()

	g := newGoal("goal_1")
	if err := repo.CreateGoal(ctx, g); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetGoal(ctx, "goal_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Objective != g.Objective {
		t.Errorf("objective mismatch: %q", got.Objective)
	}

	if _, err := repo.GetGoal(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if err := repo.CreateGoal(ctx, g); !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict on duplicate, got %v", err)
	}
}

func TestMemoryTxCommit(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	if err := repo.CreateGoal(ctx, newGoal("goal_tx")); err != nil {
		t.Fatal(err)
	}

	err := repo.Tx(ctx, func(tx Repository) error {
		if err := tx.CreatePlan(ctx, &domain.Plan{ID: "plan_tx", GoalID: "goal_tx", CreatedAt: time.Now().UTC()}); err != nil {
			return err
		}
		return tx.CreatePlanVersion(ctx, newVersion("plan_tx", 1))
	})
	if err != nil {
		t.Fatalf("tx commit: %v", err)
	}

	if _, err := repo.GetPlanByGoal(ctx, "goal_tx"); err != nil {
		t.Errorf("plan head should be committed: %v", err)
	}
	if v, err := repo.GetCurrentPlanVersion(ctx, "plan_tx"); err != nil || v.Version != 1 {
		t.Errorf("version should be committed: v=%d err=%v", v.Version, err)
	}
}

func TestMemoryTxRollback(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	// A goal created before the tx must survive a rollback untouched.
	if err := repo.CreateGoal(ctx, newGoal("goal_keep")); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("boom")
	err := repo.Tx(ctx, func(tx Repository) error {
		if err := tx.CreatePlan(ctx, &domain.Plan{ID: "plan_rb", GoalID: "goal_keep", CreatedAt: time.Now().UTC()}); err != nil {
			return err
		}
		return sentinel // abort after a successful write
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}

	// The plan head written inside the tx must NOT be persisted.
	if _, err := repo.GetPlanByGoal(ctx, "goal_keep"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rolled-back plan head should be absent, got %v", err)
	}
	// The pre-existing goal is untouched.
	if _, err := repo.GetGoal(ctx, "goal_keep"); err != nil {
		t.Errorf("pre-existing goal should survive rollback: %v", err)
	}
}

// TestMemoryTxRollbackVersionsAliasing guards the inner-map copy: a rolled-back
// version append must not mutate the live per-plan version map.
func TestMemoryTxRollbackVersionsAliasing(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	if err := repo.CreatePlan(ctx, &domain.Plan{ID: "plan_a", GoalID: "g", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreatePlanVersion(ctx, newVersion("plan_a", 1)); err != nil {
		t.Fatal(err)
	}

	_ = repo.Tx(ctx, func(tx Repository) error {
		if err := tx.CreatePlanVersion(ctx, newVersion("plan_a", 2)); err != nil {
			return err
		}
		return errors.New("rollback")
	})

	if v, err := repo.GetCurrentPlanVersion(ctx, "plan_a"); err != nil || v.Version != 1 {
		t.Errorf("rolled-back version 2 leaked into live store: v=%d err=%v", v.Version, err)
	}
}

func TestMemoryOutcomeRoundTrip(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()

	o := &domain.Outcome{
		ID:          "out_1",
		GoalID:      "goal_1",
		PlanVersion: 2,
		MoveRank:    1,
		MoveTitle:   "Double down on founder network",
		Result:      domain.OutcomePartial,
		CreatedAt:   time.Now().UTC(),
	}
	if err := repo.CreateOutcome(ctx, o); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListOutcomes(ctx, "goal_1", Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(got))
	}
	if got[0].PlanVersion != 2 || got[0].MoveRank != 1 || got[0].MoveTitle != o.MoveTitle {
		t.Errorf("move reference not preserved: %+v", got[0])
	}
}

func TestMemoryPlanVersionsAppendOnly(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()

	now := time.Now().UTC()
	plan := &domain.Plan{ID: "plan_1", GoalID: "goal_1", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}

	for v := 1; v <= 3; v++ {
		if err := repo.CreatePlanVersion(ctx, newVersion("plan_1", v)); err != nil {
			t.Fatalf("create version %d: %v", v, err)
		}
	}

	// current_version should track the latest.
	cur, err := repo.GetCurrentPlanVersion(ctx, "plan_1")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Version != 3 {
		t.Errorf("expected current version 3, got %d", cur.Version)
	}

	// Versions are immutable: recreating an existing version must fail.
	if err := repo.CreatePlanVersion(ctx, newVersion("plan_1", 2)); !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict overwriting a version, got %v", err)
	}

	// Listing returns versions in ascending order.
	versions, err := repo.ListPlanVersions(ctx, "plan_1", Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	for i, v := range versions {
		if v.Version != i+1 {
			t.Errorf("versions out of order at %d: %d", i, v.Version)
		}
	}

	// Creating a version for an unknown plan fails.
	if err := repo.CreatePlanVersion(ctx, newVersion("plan_missing", 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for unknown plan, got %v", err)
	}
}

func TestMemoryPagination(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		if err := repo.CreateGoal(ctx, newGoal("goal_"+id)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repo.ListGoals(ctx, Page{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Errorf("expected 2 goals, got %d", len(page))
	}
}

func TestMemoryImmutableReturns(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	g := newGoal("goal_x")
	g.Context.Facts = []string{"original"}
	if err := repo.CreateGoal(ctx, g); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.GetGoal(ctx, "goal_x")
	got.Context.Facts[0] = "mutated"

	again, _ := repo.GetGoal(ctx, "goal_x")
	if again.Context.Facts[0] != "original" {
		t.Error("mutating a returned value leaked into the store")
	}
}

func TestMemoryConcurrentVersionWrites(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repo.CreatePlan(ctx, &domain.Plan{ID: "plan_c", GoalID: "g", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for v := 1; v <= n; v++ {
		wg.Add(1)
		go func(version int) {
			defer wg.Done()
			errs <- repo.CreatePlanVersion(ctx, newVersion("plan_c", version))
		}(v)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}

	versions, _ := repo.ListPlanVersions(ctx, "plan_c", Page{Limit: 100})
	if len(versions) != n {
		t.Errorf("expected %d versions, got %d", n, len(versions))
	}
}
