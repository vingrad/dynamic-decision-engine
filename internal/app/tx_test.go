package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// faultRepo wraps a real repository but, inside a transaction, hands fn a repo
// whose CreatePlanVersion fails — so the version write errors mid-transaction.
type faultRepo struct{ storage.Repository }

func (f faultRepo) Tx(ctx context.Context, fn func(tx storage.Repository) error) error {
	return f.Repository.Tx(ctx, func(tx storage.Repository) error {
		return fn(failVersion{tx})
	})
}

type failVersion struct{ storage.Repository }

func (failVersion) CreatePlanVersion(context.Context, *domain.PlanVersion) error {
	return errors.New("version write failed")
}

// TestGeneratePlanAtomicOnVersionFailure proves the orphan bug is fixed: if the
// first version write fails, the plan head is rolled back, not left behind.
func TestGeneratePlanAtomicOnVersionFailure(t *testing.T) {
	mem := storage.NewMemory()
	svc := New(faultRepo{mem}, engine.New(llm.NewMockPlanner()))
	ctx := context.Background()
	g := makeGoal(t, svc)

	if _, err := svc.GeneratePlan(ctx, g.ID); err == nil {
		t.Fatal("expected GeneratePlan to fail when the version write fails")
	}

	// No orphan plan head: the whole unit-of-work rolled back.
	if _, err := mem.GetPlanByGoal(ctx, g.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("plan head should have been rolled back, got %v", err)
	}
}
