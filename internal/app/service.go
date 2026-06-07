// Package app is the application/use-case layer. It owns all orchestration of the
// domain — generating plans, applying signals, recording outcomes — so that
// transport adapters (HTTP API, CLI) are thin callers with a single source of
// truth for each use-case. It depends on the engine (reasoning) and the storage
// repository (persistence), but knows nothing about HTTP.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// maxReplanAttempts bounds the optimistic-concurrency retry loop in ApplySignal.
const maxReplanAttempts = 3

// Service implements the engine's use-cases. It is safe for concurrent use.
type Service struct {
	repo    storage.Repository
	eng     *engine.Engine
	metrics Metrics
	clock   func() time.Time
	log     *slog.Logger
}

// Option customises a Service.
type Option func(*Service)

// WithMetrics sets the metrics sink.
func WithMetrics(m Metrics) Option { return func(s *Service) { s.metrics = m } }

// WithClock overrides the time source (useful for deterministic tests).
func WithClock(clock func() time.Time) Option { return func(s *Service) { s.clock = clock } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// New constructs a Service.
func New(repo storage.Repository, eng *engine.Engine, opts ...Option) *Service {
	s := &Service{
		repo:    repo,
		eng:     eng,
		metrics: nopMetrics{},
		clock:   func() time.Time { return time.Now().UTC() },
		log:     slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateGoal validates and persists a new goal.
func (s *Service) CreateGoal(ctx context.Context, in CreateGoalInput) (domain.Goal, error) {
	if in.Objective == "" {
		return domain.Goal{}, invalid("objective is required")
	}
	goal := domain.Goal{
		ID:        domain.NewID("goal"),
		PlayerID:  in.PlayerID,
		Objective: in.Objective,
		Metric:    in.Metric,
		Target:    in.Target,
		Context:   in.Context,
		CreatedAt: s.clock(),
	}
	if err := s.repo.CreateGoal(ctx, &goal); err != nil {
		return domain.Goal{}, err
	}
	return goal, nil
}

// GetGoal returns a goal by ID.
func (s *Service) GetGoal(ctx context.Context, id string) (domain.Goal, error) {
	return s.repo.GetGoal(ctx, id)
}

// ListGoals returns a page of goals.
func (s *Service) ListGoals(ctx context.Context, page storage.Page) ([]domain.Goal, error) {
	return s.repo.ListGoals(ctx, page)
}

// GeneratePlan produces and persists the initial plan (version 1) for a goal.
func (s *Service) GeneratePlan(ctx context.Context, goalID string) (domain.PlanVersion, error) {
	goal, err := s.repo.GetGoal(ctx, goalID)
	if err != nil {
		return domain.PlanVersion{}, err
	}

	// A goal has at most one plan.
	if _, err := s.repo.GetPlanByGoal(ctx, goalID); err == nil {
		return domain.PlanVersion{}, ErrPlanExists
	} else if !errors.Is(err, storage.ErrNotFound) {
		return domain.PlanVersion{}, err
	}

	version, err := s.eng.GenerateInitialPlan(ctx, goal)
	if err != nil {
		return domain.PlanVersion{}, err
	}

	now := s.clock()
	plan := domain.Plan{
		ID:             version.PlanID,
		GoalID:         goal.ID,
		CurrentVersion: 0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreatePlan(ctx, &plan); err != nil {
		return domain.PlanVersion{}, err
	}
	if err := s.repo.CreatePlanVersion(ctx, &version); err != nil {
		return domain.PlanVersion{}, err
	}
	s.metrics.PlanVersionCreated()
	return version, nil
}

// GetPlan returns a plan head and its current version.
func (s *Service) GetPlan(ctx context.Context, planID string) (PlanView, error) {
	plan, err := s.repo.GetPlan(ctx, planID)
	if err != nil {
		return PlanView{}, err
	}
	current, err := s.repo.GetCurrentPlanVersion(ctx, planID)
	if err != nil {
		return PlanView{}, err
	}
	return PlanView{Plan: plan, CurrentVersion: current}, nil
}

// GetGoalPlan returns the plan (and current version) for a goal.
func (s *Service) GetGoalPlan(ctx context.Context, goalID string) (PlanView, error) {
	plan, err := s.repo.GetPlanByGoal(ctx, goalID)
	if err != nil {
		return PlanView{}, err
	}
	current, err := s.repo.GetCurrentPlanVersion(ctx, plan.ID)
	if err != nil {
		return PlanView{}, err
	}
	return PlanView{Plan: plan, CurrentVersion: current}, nil
}

// ListPlanVersions returns the immutable version history of a plan.
func (s *Service) ListPlanVersions(ctx context.Context, planID string, page storage.Page) ([]domain.PlanVersion, error) {
	return s.repo.ListPlanVersions(ctx, planID, page)
}

// ApplySignal stores a signal and re-evaluates the goal's current plan. If the
// signal materially changes the recommendation, a new immutable plan version is
// created.
//
// The version-creation step is wrapped in an optimistic-concurrency retry: if a
// concurrent signal has already written the next version (detected via
// storage.ErrConflict on the UNIQUE(plan_id, version) constraint), the loop
// reloads the current version and re-evaluates against it. This serialises
// concurrent signals correctly instead of surfacing a conflict to the caller.
func (s *Service) ApplySignal(ctx context.Context, in SignalInput) (SignalResult, error) {
	if in.GoalID == "" || in.Kind == "" {
		return SignalResult{}, invalid("goal_id and kind are required")
	}

	goal, err := s.repo.GetGoal(ctx, in.GoalID)
	if err != nil {
		return SignalResult{}, err
	}
	plan, err := s.repo.GetPlanByGoal(ctx, in.GoalID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return SignalResult{}, ErrNoPlanForGoal
		}
		return SignalResult{}, err
	}

	signal := domain.Signal{
		ID:          domain.NewID("sig"),
		GoalID:      in.GoalID,
		Kind:        in.Kind,
		Description: in.Description,
		Payload:     in.Payload,
		CreatedAt:   s.clock(),
	}
	if err := s.repo.CreateSignal(ctx, &signal); err != nil {
		return SignalResult{}, err
	}

	for attempt := 0; attempt < maxReplanAttempts; attempt++ {
		current, err := s.repo.GetCurrentPlanVersion(ctx, plan.ID)
		if err != nil {
			return SignalResult{}, err
		}

		result, err := s.eng.Replan(ctx, goal, current, signal.Note())
		if err != nil {
			return SignalResult{}, err
		}
		s.metrics.ReplanEvaluated(result.Material)

		if !result.Material {
			return SignalResult{Signal: signal, Material: false, Reason: result.Reason, PlanVersion: current}, nil
		}

		err = s.repo.CreatePlanVersion(ctx, &result.Candidate)
		switch {
		case err == nil:
			s.metrics.PlanVersionCreated()
			return SignalResult{Signal: signal, Material: true, Reason: result.Reason, PlanVersion: result.Candidate}, nil
		case errors.Is(err, storage.ErrConflict):
			// A concurrent signal advanced the version first; retry against the
			// new current version.
			s.log.Debug("replan version conflict, retrying", "plan_id", plan.ID, "attempt", attempt+1)
			continue
		default:
			return SignalResult{}, err
		}
	}
	return SignalResult{}, storage.ErrConflict
}

// RecordOutcome validates and stores an outcome for a move or experiment.
func (s *Service) RecordOutcome(ctx context.Context, in OutcomeInput) (domain.Outcome, error) {
	if in.GoalID == "" {
		return domain.Outcome{}, invalid("goal_id is required")
	}
	if !in.Result.Valid() {
		return domain.Outcome{}, invalid("result must be one of: success, failure, partial, inconclusive")
	}
	outcome := domain.Outcome{
		ID:              domain.NewID("out"),
		GoalID:          in.GoalID,
		MoveTitle:       in.MoveTitle,
		Result:          in.Result,
		ObservedSignals: in.ObservedSignals,
		Notes:           in.Notes,
		CreatedAt:       s.clock(),
	}
	if err := s.repo.CreateOutcome(ctx, &outcome); err != nil {
		return domain.Outcome{}, err
	}
	return outcome, nil
}

// Ping checks that the underlying storage is reachable (used for readiness).
func (s *Service) Ping(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

// Evaluate runs the planner against a self-contained goal and returns a plan
// without persisting anything.
func (s *Service) Evaluate(ctx context.Context, in EvaluateInput) (domain.PlanVersion, error) {
	if in.Objective == "" {
		return domain.PlanVersion{}, invalid("objective is required")
	}
	goal := domain.Goal{
		ID:        domain.NewID("goal"),
		Objective: in.Objective,
		Metric:    in.Metric,
		Target:    in.Target,
		Context:   in.Context,
		CreatedAt: s.clock(),
	}
	return s.eng.Evaluate(ctx, goal, in.SignalNote)
}
