package engine

import (
	"context"
	"fmt"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

// ReplanResult is the outcome of re-evaluating a plan against a new signal.
type ReplanResult struct {
	// Material reports whether the signal changed the recommended action path
	// enough to warrant a new immutable version.
	Material bool
	// Reason is a short, audit-friendly explanation of the materiality decision.
	Reason string
	// Candidate is the freshly generated version. When Material is true it is the
	// next version (current.Version + 1) and should be persisted; when false it is
	// returned for inspection but the current version remains authoritative.
	Candidate domain.PlanVersion
}

// Replan re-evaluates a goal in light of a new signal. It regenerates moves with
// the signal folded into the planner input, then asks the evaluator whether the
// change is material. The current plan version is never mutated — when the change
// is material a new version (N+1) is produced for the caller to persist.
func (e *Engine) Replan(ctx context.Context, goal domain.Goal, current domain.PlanVersion, signalNote, signalKind string, signalPayload map[string]any) (ReplanResult, error) {
	// Cheap pre-filter: skip regeneration entirely for signals a domain never acts
	// on, so trivial signals don't pay for a full (LLM) plan generation.
	if gate := e.gateFor(goal); gate != nil {
		if proceed, reason := gate.ShouldReplan(goal, signalKind, signalPayload, current.RankedMoves); !proceed {
			return ReplanResult{Material: false, Reason: reason}, nil
		}
	}

	res, err := e.planner.GeneratePlan(ctx, llm.PlanRequest{Goal: goal, SignalNote: signalNote, SignalKind: signalKind, SignalPayload: signalPayload, CurrentMoves: current.RankedMoves})
	if err != nil {
		return ReplanResult{}, fmt.Errorf("engine: replan: %w", err)
	}

	candidate := e.buildVersion(current.PlanID, current.Version+1, goal, res)
	material, reason := e.evaluatorFor(goal).IsMaterial(current.RankedMoves, candidate.RankedMoves)

	return ReplanResult{
		Material:  material,
		Reason:    reason,
		Candidate: candidate,
	}, nil
}
