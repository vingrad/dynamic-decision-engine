package engine

import (
	"math"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// DefaultConfidenceDelta is the change in the top move's confidence that, on its
// own, is considered material enough to warrant a new plan version.
const DefaultConfidenceDelta = 0.10

// ThresholdEvaluator is the default materiality policy. A candidate is material
// when the top recommendation changes, the ordering of moves changes, or the
// confidence on the top move shifts by at least ConfidenceDelta.
type ThresholdEvaluator struct {
	ConfidenceDelta float64
}

// NewThresholdEvaluator returns an evaluator with the default threshold.
func NewThresholdEvaluator() ThresholdEvaluator {
	return ThresholdEvaluator{ConfidenceDelta: DefaultConfidenceDelta}
}

// IsMaterial implements Evaluator.
func (t ThresholdEvaluator) IsMaterial(current, candidate []domain.RankedMove) (bool, string) {
	if len(current) == 0 {
		return true, "no prior plan to compare against"
	}
	if len(candidate) == 0 {
		return true, "candidate plan has no moves"
	}

	if current[0].Title != candidate[0].Title {
		return true, "top-ranked move changed"
	}
	if !sameOrder(current, candidate) {
		return true, "ranking of moves changed"
	}

	delta := math.Abs(current[0].Confidence - candidate[0].Confidence)
	if delta >= t.confidenceDelta() {
		return true, "confidence on the top move shifted materially"
	}
	return false, "no material change in the recommended action path"
}

func (t ThresholdEvaluator) confidenceDelta() float64 {
	if t.ConfidenceDelta <= 0 {
		return DefaultConfidenceDelta
	}
	return t.ConfidenceDelta
}

// sameOrder reports whether two move lists recommend the same titles in the same
// order. Differing lengths count as a change in order.
func sameOrder(a, b []domain.RankedMove) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Title != b[i].Title {
			return false
		}
	}
	return true
}
