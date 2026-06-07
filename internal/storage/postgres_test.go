package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/logging"
)

// newPostgresForTest connects to the database named by DATABASE_URL. The whole
// suite is skipped when that variable is unset, so unit runs and CI without a
// database stay green; the CI integration job provides a real Postgres.
func newPostgresForTest(t *testing.T) *PostgresRepository {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repo, err := NewPostgres(ctx, Options{DatabaseURL: url, MaxConns: 4}, logging.New("error", "text"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(repo.Close)
	return repo
}

func TestPostgresPlanLifecycle(t *testing.T) {
	repo := newPostgresForTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	goalID := domain.NewID("goal")
	if err := repo.CreateGoal(ctx, &domain.Goal{
		ID:        goalID,
		Objective: "integration objective",
		Metric:    "metric",
		Context:   domain.Context{Assets: []domain.Asset{{Name: "asset"}}},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	planID := domain.NewID("plan")
	if err := repo.CreatePlan(ctx, &domain.Plan{ID: planID, GoalID: goalID, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	for v := 1; v <= 2; v++ {
		err := repo.CreatePlanVersion(ctx, &domain.PlanVersion{
			PlanID:          planID,
			Version:         v,
			Goal:            "integration objective",
			Summary:         "summary",
			RankedMoves:     []domain.RankedMove{{Rank: 1, Title: "A", Confidence: 0.8, Experiment: domain.Experiment{Title: "e", DurationDays: 7}}},
			Provenance:      domain.DecisionProvenance{Planner: "mock"},
			InputSnapshotID: "snap_x",
			CreatedAt:       now,
		})
		if err != nil {
			t.Fatalf("create version %d: %v", v, err)
		}
	}

	cur, err := repo.GetCurrentPlanVersion(ctx, planID)
	if err != nil {
		t.Fatalf("get current: %v", err)
	}
	if cur.Version != 2 {
		t.Errorf("expected current version 2, got %d", cur.Version)
	}

	// Immutability: re-inserting an existing version conflicts.
	err = repo.CreatePlanVersion(ctx, &domain.PlanVersion{
		PlanID: planID, Version: 1, Goal: "x", Summary: "x", InputSnapshotID: "x", CreatedAt: now,
	})
	if err == nil || err != ErrConflict {
		t.Errorf("expected ErrConflict re-inserting version, got %v", err)
	}

	versions, err := repo.ListPlanVersions(ctx, planID, Page{Limit: 10})
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}

func TestPostgresTxCommitAndRollback(t *testing.T) {
	repo := newPostgresForTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	goalID := domain.NewID("goal")
	if err := repo.CreateGoal(ctx, &domain.Goal{ID: goalID, Objective: "tx", CreatedAt: now}); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	// Commit: plan head + first version atomically.
	planID := domain.NewID("plan")
	version := &domain.PlanVersion{
		PlanID: planID, Version: 1, Goal: "tx", Summary: "s", InputSnapshotID: "snap", CreatedAt: now,
	}
	if err := repo.Tx(ctx, func(tx Repository) error {
		if err := tx.CreatePlan(ctx, &domain.Plan{ID: planID, GoalID: goalID, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		return tx.CreatePlanVersion(ctx, version)
	}); err != nil {
		t.Fatalf("tx commit: %v", err)
	}
	if cur, err := repo.GetCurrentPlanVersion(ctx, planID); err != nil || cur.Version != 1 {
		t.Errorf("committed version missing: v=%d err=%v", cur.Version, err)
	}

	// Rollback: a plan head written then aborted must not persist.
	goal2 := domain.NewID("goal")
	if err := repo.CreateGoal(ctx, &domain.Goal{ID: goal2, Objective: "tx2", CreatedAt: now}); err != nil {
		t.Fatalf("create goal2: %v", err)
	}
	rbPlan := domain.NewID("plan")
	sentinel := errors.New("rollback")
	err := repo.Tx(ctx, func(tx Repository) error {
		if err := tx.CreatePlan(ctx, &domain.Plan{ID: rbPlan, GoalID: goal2, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	if _, err := repo.GetPlanByGoal(ctx, goal2); !errors.Is(err, ErrNotFound) {
		t.Errorf("rolled-back plan head should be absent, got %v", err)
	}
}
