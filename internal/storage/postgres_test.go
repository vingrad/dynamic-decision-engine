package storage

import (
	"context"
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
