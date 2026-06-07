package app

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// makePendingSignal stores a signal directly in status "pending" (as a crashed
// process would leave it: persisted but never processed by the lost queue).
func makePendingSignal(t *testing.T, repo storage.Repository, goalID string) domain.Signal {
	t.Helper()
	sig := domain.Signal{
		ID:          domain.NewID("sig"),
		GoalID:      goalID,
		Kind:        "competitive_shift",
		Description: "free tier launched",
		CreatedAt:   time.Now().UTC(),
		Status:      string(StatusPending),
	}
	if err := repo.CreateSignal(context.Background(), &sig); err != nil {
		t.Fatalf("create pending signal: %v", err)
	}
	return sig
}

// TestRecoverPendingReenqueues: a restarted async service re-enqueues signals left
// pending by a crash, drains them, and skips signals whose goal no longer resolves.
func TestRecoverPendingReenqueues(t *testing.T) {
	repo := storage.NewMemory()
	ctx := context.Background()

	// Build a goal + plan with an inline service (simulating the pre-crash process).
	pre := New(repo, engine.New(llm.NewMockPlanner()))
	g := makeGoal(t, pre)
	if _, err := pre.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	// Two recoverable pending signals + one orphan whose goal does not exist.
	s1 := makePendingSignal(t, repo, g.ID)
	s2 := makePendingSignal(t, repo, g.ID)
	orphan := makePendingSignal(t, repo, "goal_ghost")

	// Fresh async service over the same repo (restart: empty in-memory seen set).
	q := NewMemoryQueue(2, 16, discardLogger())
	post := New(repo, engine.New(llm.NewMockPlanner()), WithReplanQueue(q))

	n, err := post.RecoverPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 recoverable signals enqueued, got %d", n)
	}

	if err := post.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	// First signal is material -> v2; the identical second is immaterial -> stays v2.
	view, err := post.GetGoalPlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.CurrentVersion.Version != 2 {
		t.Errorf("expected version 2 after recovery drain, got %d", view.CurrentVersion.Version)
	}
	for _, id := range []string{s1.ID, s2.ID} {
		got, err := post.GetSignal(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == string(StatusPending) {
			t.Errorf("signal %s should be terminal after recovery, still pending", id)
		}
	}
	// The orphan was skipped, not processed, and caused no panic.
	if got, _ := post.GetSignal(ctx, orphan.ID); got.Status != string(StatusPending) {
		t.Errorf("orphan signal should remain pending (skipped), got %q", got.Status)
	}
}

// TestRecoverPendingIdempotentVersion: re-running a recovered replan whose effect is
// already baked into the current version produces no duplicate version.
func TestRecoverPendingIdempotentVersion(t *testing.T) {
	repo := storage.NewMemory()
	ctx := context.Background()

	s := New(repo, engine.New(llm.NewMockPlanner())) // inline
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	// A material signal advances to v2 (and is marked applied inline).
	r, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"})
	if err != nil {
		t.Fatal(err)
	}
	if r.PlanVersion.Version != 2 {
		t.Fatalf("expected v2 from the first signal, got %d", r.PlanVersion.Version)
	}

	// The crash window: an identical signal is still pending while v2 already exists.
	stuck := makePendingSignal(t, repo, g.ID)

	q := NewMemoryQueue(1, 8, discardLogger())
	post := New(repo, engine.New(llm.NewMockPlanner()), WithReplanQueue(q))
	if _, err := post.RecoverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if err := post.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	view, err := post.GetGoalPlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.CurrentVersion.Version != 2 {
		t.Errorf("recovery must not create a duplicate version; expected v2, got %d", view.CurrentVersion.Version)
	}
	got, err := post.GetSignal(ctx, stuck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(StatusUnchanged) {
		t.Errorf("recovered idempotent signal should be unchanged, got %q", got.Status)
	}
}

// blockingPlanner blocks until its context is cancelled, simulating a hung planner.
type blockingPlanner struct{}

func (blockingPlanner) Name() string { return "blocking" }
func (blockingPlanner) GeneratePlan(ctx context.Context, _ llm.PlanRequest) (llm.PlanResult, error) {
	<-ctx.Done()
	return llm.PlanResult{}, ctx.Err()
}

// TestMemoryQueueWorkerTimeout: a hung planner is cancelled by the per-job timeout
// rather than wedging the worker; the signal ends failed with a deadline error.
func TestMemoryQueueWorkerTimeout(t *testing.T) {
	repo := storage.NewMemory()
	ctx := context.Background()

	good := New(repo, engine.New(llm.NewMockPlanner()))
	g := makeGoal(t, good)
	if _, err := good.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	q := NewMemoryQueue(1, 8, discardLogger(), WithQueueTimeout(50*time.Millisecond))
	bad := New(repo, engine.New(blockingPlanner{}), WithReplanQueue(q))
	r, err := bad.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "x", Description: "hang"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- bad.Shutdown(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker wedged: shutdown did not drain within 5s")
	}

	got, err := bad.GetSignal(ctx, r.Signal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(StatusFailed) || got.Error == "" {
		t.Errorf("timed-out replan should be failed with an error, got status=%q error=%q", got.Status, got.Error)
	}
}

// flakyPlanner fails its first failFor calls, then delegates to inner.
type flakyPlanner struct {
	failFor int32
	calls   int32
	inner   llm.Planner
}

func (p *flakyPlanner) Name() string { return "flaky" }
func (p *flakyPlanner) GeneratePlan(ctx context.Context, req llm.PlanRequest) (llm.PlanResult, error) {
	if atomic.AddInt32(&p.calls, 1) <= p.failFor {
		return llm.PlanResult{}, fmt.Errorf("transient upstream error")
	}
	return p.inner.GeneratePlan(ctx, req)
}

// TestProcessReplanTransientRetry: a transient planner error is retried (not marked
// failed) when within the retry budget, and ends failed once the budget is exceeded.
func TestProcessReplanTransientRetry(t *testing.T) {
	build := func(t *testing.T, failFor int32, retries int) (*Service, string) {
		t.Helper()
		repo := storage.NewMemory()
		ctx := context.Background()
		good := New(repo, engine.New(llm.NewMockPlanner()))
		g := makeGoal(t, good)
		if _, err := good.GeneratePlan(ctx, g.ID); err != nil {
			t.Fatal(err)
		}
		q := NewMemoryQueue(1, 8, discardLogger())
		planner := &flakyPlanner{failFor: failFor, inner: llm.NewMockPlanner()}
		svc := New(repo, engine.New(planner), WithReplanQueue(q), WithReplanRetries(retries))
		r, err := svc.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "x", Description: "free tier launched"})
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		return svc, r.Signal.ID
	}

	t.Run("within budget succeeds", func(t *testing.T) {
		svc, sigID := build(t, 2, 2) // fails twice, succeeds on the 3rd (attempts 0,1,2)
		got, err := svc.GetSignal(context.Background(), sigID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == string(StatusFailed) {
			t.Errorf("retry should have recovered the transient error, got failed: %q", got.Error)
		}
	})

	t.Run("exceeds budget fails", func(t *testing.T) {
		svc, sigID := build(t, 5, 2) // 3 attempts, all fail
		got, err := svc.GetSignal(context.Background(), sigID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != string(StatusFailed) {
			t.Errorf("exhausted retries should mark failed, got %q", got.Status)
		}
	})
}
