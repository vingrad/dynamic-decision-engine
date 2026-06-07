package engine

import (
	"math"
	"strings"
	"unicode"

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

	if moveKey(current[0]) != moveKey(candidate[0]) {
		return true, "top-ranked move changed"
	}
	if !sameOrder(current, candidate) {
		return true, "ranking of moves changed"
	}

	delta := math.Abs(current[0].Confidence - candidate[0].Confidence)
	if delta >= t.confidenceDelta() {
		return true, "confidence on the top move shifted materially"
	}

	// Same moves in the same order can still be a different action path if their
	// execution structure changed — the dependency graph or the parallel grouping.
	if changed, reason := structureChange(current, candidate); changed {
		return true, reason
	}
	return false, "no material change in the recommended action path"
}

func (t ThresholdEvaluator) confidenceDelta() float64 {
	if t.ConfidenceDelta <= 0 {
		return DefaultConfidenceDelta
	}
	return t.ConfidenceDelta
}

// sameOrder reports whether two move lists recommend the same moves in the same
// order, compared by stable key. Differing lengths count as a change in order.
func sameOrder(a, b []domain.RankedMove) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if moveKey(a[i]) != moveKey(b[i]) {
			return false
		}
	}
	return true
}

// structureChange reports whether the execution structure of the moves differs
// between two plans, comparing moves by stable key. It is only meaningful once the
// caller has established the moves and their order match (so keys line up). It
// detects a changed dependency graph or a changed parallel grouping and returns a
// specific, audit-friendly reason.
func structureChange(current, candidate []domain.RankedMove) (bool, string) {
	cand := make(map[string]domain.RankedMove, len(candidate))
	for _, m := range candidate {
		cand[moveKey(m)] = m
	}
	for _, c := range current {
		n, ok := cand[moveKey(c)]
		if !ok {
			continue // move set differences are handled by the order check
		}
		if !sameStringSet(c.DependsOn, n.DependsOn) {
			return true, "execution dependencies changed"
		}
		if c.ParallelGroup != n.ParallelGroup {
			return true, "parallel grouping changed"
		}
	}
	return false, ""
}

// sameStringSet reports whether two string slices contain the same elements,
// ignoring order and duplicates. Used to compare dependency lists where the order
// of declaration carries no meaning.
func sameStringSet(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
		delete(set, s)
	}
	return len(set) == 0
}

// moveKey returns a move's stable identity for materiality comparison: its
// explicit Key when set, otherwise a slug-normalised Title (lowercased, with runs
// of non-alphanumeric characters collapsed to single hyphens). The fallback means
// trivial title edits — punctuation, casing, spacing — do not read as a new move.
func moveKey(m domain.RankedMove) string {
	if m.Key != "" {
		return m.Key
	}
	return slug(m.Title)
}

// slug normalises a string to lowercase alphanumeric tokens joined by hyphens.
func slug(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen && b.Len() > 0 {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}
