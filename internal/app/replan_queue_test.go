package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestMemoryQueueProcessesAllDistinct: every distinct signal for one plan is
// processed in order — no signal (e.g. a thesis_break) is ever dropped (bug #2).
func TestMemoryQueueProcessesAllDistinct(t *testing.T) {
	var mu sync.Mutex
	var order []string
	q := NewMemoryQueue(1, 16, discardLogger())
	for i := 0; i < 5; i++ {
		if _, err := q.Enqueue(context.Background(), ReplanJob{PlanID: "p1", SignalID: fmt.Sprintf("s%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	q.Start(func(_ context.Context, job ReplanJob) (ReplanOutcome, error) {
		mu.Lock()
		order = append(order, job.SignalID)
		mu.Unlock()
		return ReplanOutcome{Processed: true}, nil
	})
	if err := q.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 5 {
		t.Fatalf("expected all 5 distinct signals processed, got %d (%v)", len(order), order)
	}
	for i, id := range order {
		if id != fmt.Sprintf("s%d", i) {
			t.Errorf("signals processed out of order: %v", order)
			break
		}
	}
}

// TestMemoryQueueSeenEviction: the idempotency set is bounded and evicts oldest.
func TestMemoryQueueSeenEviction(t *testing.T) {
	q := NewMemoryQueue(1, 4, discardLogger())
	q.maxSeen = 2
	q.markSeen("a")
	q.markSeen("b")
	q.markSeen("c") // evicts "a"
	if q.seen["a"] {
		t.Error("oldest seen key should have been evicted")
	}
	if !q.seen["b"] || !q.seen["c"] {
		t.Error("recent seen keys should be retained")
	}
}

// TestMemoryQueueIdempotency: enqueuing the same signal id twice processes it once.
func TestMemoryQueueIdempotency(t *testing.T) {
	var calls atomic.Int32
	q := NewMemoryQueue(1, 16, discardLogger())
	job := ReplanJob{PlanID: "p1", SignalID: "same"}
	if _, err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	q.Start(func(_ context.Context, _ ReplanJob) (ReplanOutcome, error) {
		calls.Add(1)
		return ReplanOutcome{Processed: true}, nil
	})
	if err := q.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 processed (idempotent), got %d", got)
	}
}

func TestMemoryQueueEnqueueAfterShutdown(t *testing.T) {
	q := NewMemoryQueue(1, 4, discardLogger())
	q.Start(func(_ context.Context, _ ReplanJob) (ReplanOutcome, error) { return ReplanOutcome{}, nil })
	if err := q.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(context.Background(), ReplanJob{PlanID: "p", SignalID: "x"}); err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

// TestApplySignalAsync: with an async queue, ApplySignal returns pending and the
// material signal produces version 2 once the worker drains.
func TestApplySignalAsync(t *testing.T) {
	q := NewMemoryQueue(2, 16, discardLogger())
	s := New(storage.NewMemory(), engine.New(llm.NewMockPlanner()), WithReplanQueue(q))
	ctx := context.Background()
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	r, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusPending {
		t.Fatalf("expected pending status, got %q", r.Status)
	}

	// Drain the queue, then the material signal should have produced version 2.
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetGoalPlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.CurrentVersion.Version != 2 {
		t.Errorf("expected version 2 after async replan, got %d", view.CurrentVersion.Version)
	}
}

// failingPlanner always errors, to exercise the failed-status path.
type failingPlanner struct{}

func (failingPlanner) Name() string { return "failing" }
func (failingPlanner) GeneratePlan(context.Context, llm.PlanRequest) (llm.PlanResult, error) {
	return llm.PlanResult{}, fmt.Errorf("boom")
}

// TestApplySignalAsyncDurableStatus: an async signal's terminal status is queryable
// once the worker drains (bug #5).
func TestApplySignalAsyncDurableStatus(t *testing.T) {
	q := NewMemoryQueue(2, 16, discardLogger())
	s := New(storage.NewMemory(), engine.New(llm.NewMockPlanner()), WithReplanQueue(q))
	ctx := context.Background()
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	r, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusPending {
		t.Fatalf("expected pending, got %q", r.Status)
	}
	// At acceptance time the stored signal is pending.
	pre, err := s.GetSignal(ctx, r.Signal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pre.Status != string(StatusPending) {
		t.Errorf("stored signal should be pending pre-drain, got %q", pre.Status)
	}

	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	post, err := s.GetSignal(ctx, r.Signal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if post.Status != string(StatusApplied) || post.ResultVersion != 2 || post.ProcessedAt == nil {
		t.Errorf("expected applied v2 with processed_at, got status=%q v=%d at=%v", post.Status, post.ResultVersion, post.ProcessedAt)
	}
}

// TestApplySignalAsyncFailedStatus: a replan error surfaces as a queryable failed
// status with the error message (the dead-letter requirement).
func TestApplySignalAsyncFailedStatus(t *testing.T) {
	q := NewMemoryQueue(1, 8, discardLogger())
	repo := storage.NewMemory()
	// Build the plan with a working planner, then swap to a failing one for replans.
	good := New(repo, engine.New(llm.NewMockPlanner()))
	ctx := context.Background()
	g := makeGoal(t, good)
	if _, err := good.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	bad := New(repo, engine.New(failingPlanner{}), WithReplanQueue(q))
	r, err := bad.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "x", Description: "boom"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := bad.GetSignal(ctx, r.Signal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(StatusFailed) || got.Error == "" {
		t.Errorf("expected failed status with error, got status=%q error=%q", got.Status, got.Error)
	}
}

// TestApplySignalInlineStatus: the default inline queue reports applied/unchanged
// synchronously (back-compat for CLI/tests).
func TestApplySignalInlineStatus(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	r1, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "x", Description: "free tier"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != StatusApplied || !r1.Material || r1.PlanVersion.Version != 2 {
		t.Fatalf("expected applied v2, got status=%q material=%v v=%d", r1.Status, r1.Material, r1.PlanVersion.Version)
	}
	r2, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "x", Description: "free tier"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != StatusUnchanged || r2.Material {
		t.Fatalf("expected unchanged, got status=%q material=%v", r2.Status, r2.Material)
	}
}
