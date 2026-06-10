package app

import (
	"context"
	"sync"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// fakeNotifier records emitted events for assertions.
type fakeNotifier struct {
	mu     sync.Mutex
	events []Event
}

func (f *fakeNotifier) Emit(_ context.Context, evt Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, evt)
}

func (f *fakeNotifier) byType(typ string) []Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Event
	for _, e := range f.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func newNotifyingService(n Notifier, opts ...Option) *Service {
	opts = append([]Option{WithNotifier(n)}, opts...)
	return New(storage.NewMemory(), engine.New(llm.NewMockPlanner()), opts...)
}

func TestNotifyGoalCreated(t *testing.T) {
	n := &fakeNotifier{}
	s := newNotifyingService(n)
	g := makeGoal(t, s)

	evts := n.byType(EventGoalCreated)
	if len(evts) != 1 {
		t.Fatalf("expected 1 goal.created event, got %d", len(evts))
	}
	evt := evts[0]
	if evt.ID == "" || evt.CreatedAt.IsZero() {
		t.Errorf("envelope incomplete: id=%q created_at=%v", evt.ID, evt.CreatedAt)
	}
	p, ok := evt.Payload.(GoalCreatedPayload)
	if !ok {
		t.Fatalf("unexpected payload type %T", evt.Payload)
	}
	if p.Goal.ID != g.ID {
		t.Errorf("payload goal %q != created goal %q", p.Goal.ID, g.ID)
	}
}

func TestNotifyNoEventOnValidationFailure(t *testing.T) {
	n := &fakeNotifier{}
	s := newNotifyingService(n)
	if _, err := s.CreateGoal(context.Background(), CreateGoalInput{}); err == nil {
		t.Fatal("expected validation error")
	}
	if len(n.events) != 0 {
		t.Fatalf("expected no events on validation failure, got %d", len(n.events))
	}
}

func TestNotifyPlanCreated(t *testing.T) {
	n := &fakeNotifier{}
	s := newNotifyingService(n)
	ctx := context.Background()
	g := makeGoal(t, s)
	v1, err := s.GeneratePlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}

	evts := n.byType(EventPlanCreated)
	if len(evts) != 1 {
		t.Fatalf("expected 1 plan.created event, got %d", len(evts))
	}
	p := evts[0].Payload.(PlanCreatedPayload)
	if p.GoalID != g.ID || p.Version.Version != v1.Version {
		t.Errorf("unexpected payload: goal=%q version=%d", p.GoalID, p.Version.Version)
	}

	// A conflicting second generation emits nothing further.
	if _, err := s.GeneratePlan(ctx, g.ID); err == nil {
		t.Fatal("expected ErrPlanExists")
	}
	if got := len(n.byType(EventPlanCreated)); got != 1 {
		t.Errorf("conflict should not emit plan.created, got %d events", got)
	}
}

func TestNotifySignalInline(t *testing.T) {
	n := &fakeNotifier{}
	s := newNotifyingService(n)
	ctx := context.Background()
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	// Material signal: applied.
	r, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"})
	if err != nil {
		t.Fatal(err)
	}
	recv := n.byType(EventSignalReceived)
	if len(recv) != 1 {
		t.Fatalf("expected 1 signal.received event, got %d", len(recv))
	}
	sp := recv[0].Payload.(SignalReceivedPayload)
	if sp.Signal.ID != r.Signal.ID || sp.Status != StatusApplied {
		t.Errorf("unexpected signal.received payload: id=%q status=%q", sp.Signal.ID, sp.Status)
	}
	done := n.byType(EventReplanCompleted)
	if len(done) != 1 {
		t.Fatalf("expected 1 replan.completed event, got %d", len(done))
	}
	rp := done[0].Payload.(ReplanCompletedPayload)
	if rp.SignalID != r.Signal.ID || rp.Status != StatusApplied || rp.GoalID != g.ID {
		t.Errorf("unexpected replan.completed payload: %+v", rp)
	}
	if rp.Version == nil || rp.Version.Version != r.PlanVersion.Version {
		t.Errorf("replan.completed should carry the new version")
	}

	// Identical signal again: immaterial -> unchanged on both events.
	if _, err := s.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "competitive_shift", Description: "free tier launched"}); err != nil {
		t.Fatal(err)
	}
	recv = n.byType(EventSignalReceived)
	if got := recv[len(recv)-1].Payload.(SignalReceivedPayload).Status; got != StatusUnchanged {
		t.Errorf("expected unchanged signal.received, got %q", got)
	}
	done = n.byType(EventReplanCompleted)
	if got := done[len(done)-1].Payload.(ReplanCompletedPayload); got.Status != StatusUnchanged || got.Version == nil {
		t.Errorf("expected unchanged replan.completed with surviving version, got %+v", got)
	}
}

func TestNotifySignalAsync(t *testing.T) {
	n := &fakeNotifier{}
	q := NewMemoryQueue(1, 16, discardLogger())
	s := newNotifyingService(n, WithReplanQueue(q))
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
		t.Fatalf("expected pending result, got %q", r.Status)
	}
	recv := n.byType(EventSignalReceived)
	if len(recv) != 1 || recv[0].Payload.(SignalReceivedPayload).Status != StatusPending {
		t.Fatalf("expected pending signal.received, got %v", recv)
	}

	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	done := n.byType(EventReplanCompleted)
	if len(done) != 1 {
		t.Fatalf("expected 1 replan.completed after drain, got %d", len(done))
	}
	rp := done[0].Payload.(ReplanCompletedPayload)
	if rp.Status != StatusApplied || rp.SignalID != r.Signal.ID {
		t.Errorf("unexpected async replan.completed: %+v", rp)
	}
}

func TestNotifyReplanFailed(t *testing.T) {
	n := &fakeNotifier{}
	repo := storage.NewMemory()
	ctx := context.Background()

	// Goal and initial plan via a working service; then a failing planner handles
	// the replan so the failed path emits.
	good := New(repo, engine.New(llm.NewMockPlanner()))
	g, err := good.CreateGoal(ctx, CreateGoalInput{Objective: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := good.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	q := NewMemoryQueue(1, 8, discardLogger())
	bad := New(repo, engine.New(failingPlanner{}), WithReplanQueue(q), WithNotifier(n))
	if _, err := bad.ApplySignal(ctx, SignalInput{GoalID: g.ID, Kind: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := bad.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	done := n.byType(EventReplanCompleted)
	if len(done) != 1 {
		t.Fatalf("expected 1 replan.completed, got %d", len(done))
	}
	rp := done[0].Payload.(ReplanCompletedPayload)
	if rp.Status != StatusFailed || rp.Error == "" || rp.Version != nil {
		t.Errorf("expected failed payload with error and no version, got %+v", rp)
	}
}

func TestNotifyGoalStatusChanged(t *testing.T) {
	n := &fakeNotifier{}
	s := newNotifyingService(n)
	ctx := context.Background()
	g := makeGoal(t, s)

	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
		GoalID:           g.ID,
		Status:           domain.GoalResolved,
		ResolutionResult: domain.OutcomeSuccess,
	}); err != nil {
		t.Fatal(err)
	}

	evts := n.byType(EventGoalStatusChanged)
	if len(evts) != 1 {
		t.Fatalf("expected 1 goal.status_changed event, got %d", len(evts))
	}
	p := evts[0].Payload.(GoalStatusChangedPayload)
	if p.Goal.Status != domain.GoalResolved || p.PreviousStatus != domain.GoalActive {
		t.Errorf("unexpected payload: status=%q previous=%q", p.Goal.Status, p.PreviousStatus)
	}
}

func TestNotifyOutcomeRecorded(t *testing.T) {
	n := &fakeNotifier{}
	s := newNotifyingService(n)
	ctx := context.Background()
	g := makeGoal(t, s)
	v1, err := s.GeneratePlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}

	out, err := s.RecordOutcome(ctx, OutcomeInput{
		GoalID:      g.ID,
		PlanVersion: v1.Version,
		MoveRank:    1,
		Result:      domain.OutcomeSuccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	evts := n.byType(EventOutcomeRecorded)
	if len(evts) != 1 {
		t.Fatalf("expected 1 outcome.recorded event, got %d", len(evts))
	}
	if p := evts[0].Payload.(OutcomeRecordedPayload); p.Outcome.ID != out.ID {
		t.Errorf("payload outcome %q != recorded %q", p.Outcome.ID, out.ID)
	}
}
