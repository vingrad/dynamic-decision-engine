// Package llm defines the boundary between the decision engine and whatever
// produces structured reasoning. The default implementation is a deterministic
// mock so the system runs with no API keys; a real model client can be slotted in
// behind the Planner interface without touching the engine.
package llm

import (
	"context"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// PlanRequest is the input to a planner: the goal (with its context snapshot) and
// an optional note describing a new signal that prompted a re-plan.
type PlanRequest struct {
	Goal       domain.Goal
	SignalNote string
}

// PlanResult is a planner's output. The engine wraps this into an immutable
// PlanVersion; the planner itself is unaware of versioning or persistence.
type PlanResult struct {
	Summary     string
	RankedMoves []domain.RankedMove
	Provenance  domain.DecisionProvenance
	Invocation  domain.ModelInvocation
}

// Planner is the reasoning boundary. Implementations may call a real model or be
// fully deterministic. They must be safe for concurrent use.
type Planner interface {
	// Name identifies the planner in provenance records (e.g. "mock", "openai").
	Name() string
	// GeneratePlan produces ranked moves and provenance for the given request.
	GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error)
}

// PlanVerifier reviews a proposed plan and returns a per-move verdict. It is the
// capability behind cross-model verification: a different provider critiques the
// proposer's plan rather than generating its own. Real provider adapters
// implement both Planner and PlanVerifier.
type PlanVerifier interface {
	// VerifierName identifies the verifier in provenance (e.g. "openai").
	VerifierName() string
	// VerifyPlan critiques the proposed plan for the given goal.
	VerifyPlan(ctx context.Context, goal domain.Goal, proposed PlanResult) (Verdict, domain.ModelInvocation, error)
}
