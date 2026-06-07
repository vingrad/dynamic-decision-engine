// Package engine orchestrates plan generation and dynamic replanning. It is pure
// domain logic: it depends on a Planner (the reasoning boundary) and an Evaluator
// (the materiality policy) but knows nothing about HTTP or persistence. Callers
// are responsible for storing the immutable plan versions it produces.
package engine

import "github.com/vingrad/dynamic-decision-engine/internal/domain"

// Evaluator decides whether a freshly generated set of ranked moves differs
// materially from the current plan — i.e. whether replanning should cut a new
// immutable version or leave the current one in place.
type Evaluator interface {
	// IsMaterial reports whether candidate differs materially from current, with
	// a short human-readable reason suitable for provenance/audit logs.
	IsMaterial(current, candidate []domain.RankedMove) (material bool, reason string)
}

// EvaluatorResolver selects the materiality policy for a goal's domain. It is
// declared here (rather than importing the pack registry) so the engine stays free
// of a pack dependency; the wiring layer builds a resolver from pack descriptors
// and injects it via WithEvaluatorResolver.
type EvaluatorResolver interface {
	EvaluatorFor(domainKey string) Evaluator
}

// ReplanGate is a cheap pre-filter applied before the (expensive) plan
// regeneration on a replan. It lets a domain short-circuit signals that cannot
// move the plan — e.g. a kind it never acts on — without paying for a full
// GeneratePlan call. Returning proceed=false skips regeneration and leaves the
// current version authoritative; the reason is recorded for audit.
type ReplanGate interface {
	// ShouldReplan reports whether the signal warrants regenerating the plan. The
	// returned reason explains the decision (used when proceed is false).
	ShouldReplan(goal domain.Goal, signalKind string, payload map[string]any, current []domain.RankedMove) (proceed bool, reason string)
}

// GateResolver selects the ReplanGate for a goal's domain, mirroring
// EvaluatorResolver. The wiring layer builds it from pack/policy config and injects
// it via WithGateResolver; when absent every signal proceeds to regeneration.
type GateResolver interface {
	GateFor(domainKey string) ReplanGate
}
