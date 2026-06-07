package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

// Engine generates plans and drives the replanning loop. It is safe for
// concurrent use provided its Planner and Evaluator are.
type Engine struct {
	planner   llm.Planner
	evaluator Evaluator
	clock     func() time.Time
}

// Option customises an Engine.
type Option func(*Engine)

// WithEvaluator overrides the default materiality evaluator.
func WithEvaluator(e Evaluator) Option {
	return func(en *Engine) { en.evaluator = e }
}

// WithClock overrides the time source (useful for deterministic tests).
func WithClock(clock func() time.Time) Option {
	return func(en *Engine) { en.clock = clock }
}

// New constructs an Engine around the given planner, applying any options.
func New(planner llm.Planner, opts ...Option) *Engine {
	e := &Engine{
		planner:   planner,
		evaluator: NewThresholdEvaluator(),
		clock:     time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// GenerateInitialPlan produces version 1 of a plan for the given goal. The
// returned PlanVersion is not persisted; the caller stores it. A fresh plan ID is
// allocated so the caller can create the corresponding Plan head.
func (e *Engine) GenerateInitialPlan(ctx context.Context, goal domain.Goal) (domain.PlanVersion, error) {
	return e.Evaluate(ctx, goal, "")
}

// Evaluate produces a standalone version-1 plan for a goal, optionally folding a
// signal note into the planner input. It is the stateless primitive behind both
// GenerateInitialPlan and the /v1/evaluate endpoint; nothing is persisted.
func (e *Engine) Evaluate(ctx context.Context, goal domain.Goal, signalNote string) (domain.PlanVersion, error) {
	res, err := e.planner.GeneratePlan(ctx, llm.PlanRequest{Goal: goal, SignalNote: signalNote})
	if err != nil {
		return domain.PlanVersion{}, fmt.Errorf("engine: evaluate: %w", err)
	}
	return e.buildVersion(domain.NewID("plan"), 1, goal, res), nil
}

// buildVersion assembles an immutable PlanVersion from a planner result.
func (e *Engine) buildVersion(planID string, version int, goal domain.Goal, res llm.PlanResult) domain.PlanVersion {
	return domain.PlanVersion{
		PlanID:          planID,
		Version:         version,
		Goal:            goal.Objective,
		Summary:         res.Summary,
		RankedMoves:     res.RankedMoves,
		Provenance:      res.Provenance,
		InputSnapshotID: res.Provenance.InputSnapshotID,
		CreatedAt:       e.clock(),
	}
}
