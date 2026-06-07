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
type Outcome struct {
	ID              string        `json:"id"`
	GoalID          string        `json:"goal_id"`
	MoveTitle       string        `json:"move_title,omitempty"` // the move/experiment this result concerns
	Result          OutcomeResult `json:"result"`
	ObservedSignals []string      `json:"observed_signals,omitempty"`
	Notes           string        `json:"notes,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
}
