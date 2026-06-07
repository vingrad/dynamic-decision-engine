// Package app is the application/use-case layer. It owns all orchestration of the
// domain — generating plans, applying signals, recording outcomes — so that
// transport adapters (HTTP API, CLI) are thin callers with a single source of
// truth for each use-case. It depends on the engine (reasoning) and the storage
// repository (persistence), but knows nothing about HTTP.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// maxReplanAttempts bounds the optimistic-concurrency retry loop in ApplySignal.
const maxReplanAttempts = 3

// Service implements the engine's use-cases. It is safe for concurrent use.
type Service struct {
	repo     storage.Repository
	eng      *engine.Engine
	metrics  Metrics
	clock    func() time.Time
	log      *slog.Logger
	queue    ReplanQueue
	registry *pack.Registry
}

// Option customises a Service.
type Option func(*Service)

// WithMetrics sets the metrics sink.
func WithMetrics(m Metrics) Option { return func(s *Service) { s.metrics = m } }

// WithClock overrides the time source (useful for deterministic tests).
func WithClock(clock func() time.Time) Option { return func(s *Service) { s.clock = clock } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// WithReplanQueue sets the replanning queue. Defaults to a synchronous inline
// queue (preserving the original behaviour); pass an async queue (e.g.
// NewMemoryQueue) to decouple LLM work from signal ingestion.
func WithReplanQueue(q ReplanQueue) Option { return func(s *Service) { s.queue = q } }

// WithRegistry enables per-domain validation of goals against the pack registry.
// When nil, domains are accepted as given (normalised only).
func WithRegistry(r *pack.Registry) Option { return func(s *Service) { s.registry = r } }

// New constructs a Service. It wires the replanning queue's handler to the
// service's own processReplan, defaulting to a synchronous inline queue.
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
	if s.queue == nil {
		s.queue = NewInlineQueue()
	}
	s.queue.Start(s.processReplan)
	return s
}

// Shutdown drains any in-flight asynchronous replanning work.
func (s *Service) Shutdown(ctx context.Context) error {
	return s.queue.Shutdown(ctx)
}

// CreateGoal validates and persists a new goal.
func (s *Service) CreateGoal(ctx context.Context, in CreateGoalInput) (domain.Goal, error) {
	if in.Objective == "" {
		return domain.Goal{}, invalid("objective is required")
	}
	domainKey, err := s.normalizeDomain(in.Domain)
	if err != nil {
		return domain.Goal{}, err
	}
	goal := domain.Goal{
		ID:        domain.NewID("goal"),
		PlayerID:  in.PlayerID,
		Domain:    domainKey,
		Objective: in.Objective,
		Metric:    in.Metric,
		Target:    in.Target,
		Context:   in.Context,
		CreatedAt: s.clock(),
	}
	if err := s.validateGoalDomain(goal); err != nil {
		return domain.Goal{}, err
	}
	if err := s.repo.CreateGoal(ctx, &goal); err != nil {
		return domain.Goal{}, err
	}
	return goal, nil
}

// normalizeDomain canonicalises the default domain to the empty string (so generic
// goals serialise/hash as before) and rejects unknown domains when a registry is
// configured.
func (s *Service) normalizeDomain(in string) (string, error) {
	key := in
	if key == pack.DefaultDomain {
		key = "" // canonical stored value for the default domain
	}
	if s.registry != nil && !s.registry.Known(key) {
		return "", invalid(fmt.Sprintf("unknown domain %q; valid domains: %v", in, s.registry.IDs()))
	}
	return key, nil
}

// validateGoalDomain runs the pack's validation: hard errors block, soft warnings
// are logged. A nil registry skips validation.
func (s *Service) validateGoalDomain(g domain.Goal) error {
	if s.registry == nil {
		return nil
	}
	d, _ := s.registry.Get(g.Domain)
	for _, iss := range d.Validate(g) {
		if iss.Severity == pack.SeverityError {
			return invalid(iss.Field + ": " + iss.Message)
		}
		s.log.Warn("goal validation warning", "domain", g.Domain, "field", iss.Field, "message", iss.Message)
	}
	return nil
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
	s.metrics.PlanVersionCreated(goal.Domain)
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

// ApplySignal stores a signal and schedules re-evaluation of the goal's current
// plan via the replan queue. With the default inline queue the work runs
// synchronously and the result (material + new version) is returned immediately;
// with an asynchronous queue the signal is accepted, a job is scheduled, and the
// result is StatusPending — callers poll the plan's versions for the outcome.
//
// The actual replanning (LLM call + optimistic-concurrency version write) lives in
// processReplan, so it runs identically on either path — off the request goroutine
// when async.
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
	current, err := s.repo.GetCurrentPlanVersion(ctx, plan.ID)
	if err != nil {
		return SignalResult{}, err
	}

	signal := domain.Signal{
		ID:          domain.NewID("sig"),
		GoalID:      in.GoalID,
		Kind:        in.Kind,
		Description: in.Description,
		Payload:     in.Payload,
		CreatedAt:   s.clock(),
		Status:      string(StatusPending),
	}
	if err := s.repo.CreateSignal(ctx, &signal); err != nil {
		return SignalResult{}, err
	}

	s.metrics.ReplanEnqueued(goal.Domain)
	enq, err := s.queue.Enqueue(ctx, ReplanJob{
		GoalID:        goal.ID,
		PlanID:        plan.ID,
		Domain:        goal.Domain,
		SignalID:      signal.ID,
		SignalKind:    signal.Kind,
		SignalNote:    signal.Note(),
		SignalPayload: signal.Payload,
		EnqueuedAt:    s.clock(),
	})
	if err != nil {
		return SignalResult{}, err
	}

	if !enq.Synchronous {
		// Asynchronous: accepted; the new version (if any) appears once a worker runs.
		return SignalResult{Signal: signal, Status: StatusPending, Material: false, PlanVersion: current}, nil
	}

	out := enq.Outcome
	status := StatusUnchanged
	if out.Material {
		status = StatusApplied
	}
	return SignalResult{Signal: signal, Status: status, Material: out.Material, Reason: out.Reason, PlanVersion: out.Version}, nil
}

// GetSignal returns a stored signal, including its replanning status — useful for
// polling the outcome of an asynchronously-processed signal.
func (s *Service) GetSignal(ctx context.Context, id string) (domain.Signal, error) {
	return s.repo.GetSignal(ctx, id)
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
	domainKey, err := s.normalizeDomain(in.Domain)
	if err != nil {
		return domain.PlanVersion{}, err
	}
	goal := domain.Goal{
		ID:        domain.NewID("goal"),
		Domain:    domainKey,
		Objective: in.Objective,
		Metric:    in.Metric,
		Target:    in.Target,
		Context:   in.Context,
		CreatedAt: s.clock(),
	}
	if err := s.validateGoalDomain(goal); err != nil {
		return domain.PlanVersion{}, err
	}
	return s.eng.Evaluate(ctx, goal, in.SignalNote)
}
