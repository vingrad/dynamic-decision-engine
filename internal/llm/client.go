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

	// SystemPromptOverride, when non-empty, is domain guidance appended to the
	// base system prompt by model-backed planners. The GuidedPlanner sets it from
	// a pack's prompt template; the mock ignores it (its determinism must not vary
	// by domain). Empty == today's behaviour.
	SystemPromptOverride string

	// SignalKind is the kind of the triggering signal (domain.Signal.Kind), used by
	// numeric planners to interpret SignalPayload. Empty for an initial plan.
	SignalKind string

	// SignalPayload carries the structured data attached to the triggering signal
	// (domain.Signal.Payload). Numeric planners (e.g. the finance planner) parse it;
	// text-only planners ignore it.
	SignalPayload map[string]any
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
