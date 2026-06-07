package domain

import "time"

// OutcomeResult classifies how a move or experiment turned out.
type OutcomeResult string

const (
	OutcomeSuccess      OutcomeResult = "success"
	OutcomeFailure      OutcomeResult = "failure"
	OutcomePartial      OutcomeResult = "partial"
	OutcomeInconclusive OutcomeResult = "inconclusive"
)

// Valid reports whether the result is recognised.
func (r OutcomeResult) Valid() bool {
	switch r {
	case OutcomeSuccess, OutcomeFailure, OutcomePartial, OutcomeInconclusive:
		return true
	default:
		return false
	}
}

// Outcome is the recorded result of a move or experiment. Outcomes close the
// learning loop: they are the observed evidence the engine and its reviewers use
// to judge whether the current plan is working.
//
// An outcome references the move it concerns by its stable address inside an
// immutable plan snapshot — (PlanVersion, MoveRank) — rather than by a free-text
// title. Because plan versions are append-only, that address is permanent and
// unambiguous: it always points at the exact move that was acted on, even after
// later replans regenerate the move set.
type Outcome struct {
	ID     string `json:"id"`
	GoalID string `json:"goal_id"`
	// PlanVersion and MoveRank address the move within the goal's (immutable) plan
	// version. Together they are the authoritative reference; MoveTitle is derived.
	PlanVersion int `json:"plan_version"`
	MoveRank    int `json:"move_rank"`
	// MoveTitle is the server-derived title of the move at (PlanVersion, MoveRank),
	// snapshotted at record time for human readability. It is never client input.
	MoveTitle       string        `json:"move_title,omitempty"`
	Result          OutcomeResult `json:"result"`
	ObservedSignals []string      `json:"observed_signals,omitempty"`
	Notes           string        `json:"notes,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
}
