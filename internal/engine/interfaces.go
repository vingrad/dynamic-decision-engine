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
