package domain

import "time"

// Goal is the objective to optimise toward, together with the context that
// frames it. A goal belongs to a player and is the unit a plan is generated for.
type Goal struct {
	ID        string    `json:"id"`
	PlayerID  string    `json:"player_id,omitempty"`
	Objective string    `json:"objective"`
	Metric    string    `json:"metric,omitempty"` // how progress is measured
	Target    string    `json:"target,omitempty"` // the value/threshold that defines success
	Context   Context   `json:"context"`
	CreatedAt time.Time `json:"created_at"`
}
