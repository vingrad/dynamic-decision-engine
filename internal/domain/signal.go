package domain

import "time"

// Signal is new information that may require replanning: a market shift, a result
// coming in, a constraint changing. Signals are the trigger for the engine's
// dynamic replanning loop.
type Signal struct {
	ID          string         `json:"id"`
	GoalID      string         `json:"goal_id"`
	Kind        string         `json:"kind"`
	Description string         `json:"description,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`

	// Processing status of the replanning this signal triggered. Populated once the
	// (possibly asynchronous) replan completes, making async outcomes observable.
	Status        string     `json:"status,omitempty"`         // pending|applied|unchanged|failed
	Reason        string     `json:"reason,omitempty"`         // materiality reason or error context
	ResultVersion int        `json:"result_version,omitempty"` // plan version produced, if any
	Error         string     `json:"error,omitempty"`          // error message when status==failed
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
}

// Note returns a compact human-readable description of the signal, used as input
// to the planner when re-evaluating a plan.
func (s Signal) Note() string {
	if s.Description != "" {
		return s.Kind + ": " + s.Description
	}
	return s.Kind
}
