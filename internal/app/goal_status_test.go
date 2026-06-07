package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

func TestCreateGoalDefaultsActive(t *testing.T) {
	s := newTestService()
	g := makeGoal(t, s)
	if g.Status != domain.GoalActive {
		t.Fatalf("expected new goal to be active, got %q", g.Status)
	}
	if g.UpdatedAt.IsZero() || !g.UpdatedAt.Equal(g.CreatedAt) {
		t.Fatalf("expected updated_at == created_at on creation, got %v vs %v", g.UpdatedAt, g.CreatedAt)
	}
}

func TestUpdateGoalStatusOnHoldThenResolve(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)

	paused, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{GoalID: g.ID, Status: domain.GoalOnHold})
	if err != nil {
		t.Fatalf("on_hold: %v", err)
	}
	if paused.Status != domain.GoalOnHold || paused.Resolution != nil {
		t.Fatalf("unexpected on_hold goal: %+v", paused)
	}

	resolved, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
		GoalID:           g.ID,
		Status:           domain.GoalResolved,
		ResolutionResult: domain.OutcomeSuccess,
		ResolutionNotes:  "shipped",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != domain.GoalResolved {
		t.Fatalf("expected resolved, got %q", resolved.Status)
	}
	if resolved.Resolution == nil || resolved.Resolution.Result != domain.OutcomeSuccess || resolved.Resolution.Notes != "shipped" {
		t.Fatalf("unexpected resolution: %+v", resolved.Resolution)
	}
	if resolved.Resolution.ResolvedAt.IsZero() {
		t.Fatal("expected resolved_at to be stamped")
	}

	// Persisted state reflects the transition.
	got, err := s.GetGoal(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.GoalResolved || got.Resolution == nil {
		t.Fatalf("persisted goal not resolved: %+v", got)
	}
}

func TestUpdateGoalStatusRejectsTerminalTransition(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)

	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
		GoalID: g.ID, Status: domain.GoalAbandoned, ResolutionResult: domain.OutcomeFailure,
	}); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	// A terminal goal cannot move again.
	_, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{GoalID: g.ID, Status: domain.GoalActive})
	if !isValidation(err) {
		t.Fatalf("expected validation error transitioning out of terminal, got %v", err)
	}
}

func TestUpdateGoalStatusResolutionRules(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	// Terminal status without a resolution result is rejected.
	g1 := makeGoal(t, s)
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{GoalID: g1.ID, Status: domain.GoalResolved}); !isValidation(err) {
		t.Fatalf("expected validation error for missing resolution, got %v", err)
	}

	// Resolution supplied for a non-terminal transition is rejected.
	g2 := makeGoal(t, s)
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
		GoalID: g2.ID, Status: domain.GoalOnHold, ResolutionNotes: "nope",
	}); !isValidation(err) {
		t.Fatalf("expected validation error for stray resolution, got %v", err)
	}
}

func TestUpdateGoalStatusErrors(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{Status: domain.GoalOnHold}); !isValidation(err) {
		t.Fatalf("expected validation error for missing goal_id, got %v", err)
	}
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{GoalID: "g", Status: "bogus"}); !isValidation(err) {
		t.Fatalf("expected validation error for bad status, got %v", err)
	}
	g := makeGoal(t, s)
	_ = g
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{GoalID: "goal_missing", Status: domain.GoalOnHold}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown goal, got %v", err)
	}
}

func TestNonActiveGoalRejectsSignalsAndPlans(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	// GeneratePlan is rejected once a goal leaves active.
	g1 := makeGoal(t, s)
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{GoalID: g1.ID, Status: domain.GoalOnHold}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GeneratePlan(ctx, g1.ID); !errors.Is(err, ErrGoalNotActive) {
		t.Fatalf("expected ErrGoalNotActive generating plan on on_hold goal, got %v", err)
	}

	// ApplySignal is rejected on a non-active goal even when a plan exists.
	g2 := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
		GoalID: g2.ID, Status: domain.GoalResolved, ResolutionResult: domain.OutcomeSuccess,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplySignal(ctx, SignalInput{GoalID: g2.ID, Kind: "noise"}); !errors.Is(err, ErrGoalNotActive) {
		t.Fatalf("expected ErrGoalNotActive sending signal to resolved goal, got %v", err)
	}
}

func TestRecordOutcomeAllowedOnResolvedGoal(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
		GoalID: g.ID, Status: domain.GoalResolved, ResolutionResult: domain.OutcomeSuccess,
	}); err != nil {
		t.Fatal(err)
	}
	// Outcomes are historical evidence and remain recordable after resolution.
	if _, err := s.RecordOutcome(ctx, OutcomeInput{
		GoalID: g.ID, PlanVersion: 1, MoveRank: 1, Result: domain.OutcomeSuccess,
	}); err != nil {
		t.Fatalf("expected outcome recordable on resolved goal, got %v", err)
	}
}

func TestUpdateGoalStatusConcurrentTransitionsCASOneWinner(t *testing.T) {
	s := newTestService()
	ctx := context.Background()
	g := makeGoal(t, s)

	// Two transitions race from active: resolve vs abandon. The compare-and-swap
	// must let exactly one win; the loser sees ErrConflict, not a silent clobber.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []domain.GoalStatus{domain.GoalResolved, domain.GoalAbandoned}
	wg.Add(2)
	for i, target := range targets {
		go func(i int, target domain.GoalStatus) {
			defer wg.Done()
			_, errs[i] = s.UpdateGoalStatus(ctx, UpdateGoalStatusInput{
				GoalID: g.ID, Status: target, ResolutionResult: domain.OutcomeInconclusive,
			})
		}(i, target)
	}
	wg.Wait()

	// The loser is rejected via one of two legitimate paths depending on
	// interleaving: if it read before the winner committed, the compare-and-swap
	// fails (ErrConflict); if it read after, validation sees a terminal status and
	// rejects it. Either way there is no silent clobber.
	winners, losers := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, storage.ErrConflict) || isValidation(err):
			losers++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("expected exactly one winner and one rejected loser, got winners=%d losers=%d", winners, losers)
	}

	// The stored goal is terminal and matches whichever transition won.
	got, err := s.GetGoal(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Status.Terminal() {
		t.Fatalf("expected terminal status after race, got %q", got.Status)
	}
}

func isValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
