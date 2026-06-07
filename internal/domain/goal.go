package domain

import "time"

// GoalStatus is the lifecycle state of a goal — its "case file" status. A goal
// starts Active and ends in a terminal state (Resolved or Abandoned); OnHold is a
// non-terminal pause. Lifecycle is metadata about the decision, not an input to
// the planner: it deliberately does not influence the input snapshot or plan.
type GoalStatus string

const (
	// GoalActive is the default: the decision is being worked.
	GoalActive GoalStatus = "active"
	// GoalOnHold is a deliberate, reversible pause.
	GoalOnHold GoalStatus = "on_hold"
	// GoalResolved is terminal: the decision concluded and the result is recorded.
	GoalResolved GoalStatus = "resolved"
	// GoalAbandoned is terminal: the decision was dropped without being resolved.
	GoalAbandoned GoalStatus = "abandoned"
)

// Valid reports whether the status is one of the recognised values.
func (s GoalStatus) Valid() bool {
	switch s {
	case GoalActive, GoalOnHold, GoalResolved, GoalAbandoned:
		return true
	default:
		return false
	}
}

// Terminal reports whether the status is an end state from which no further
// transition is allowed.
func (s GoalStatus) Terminal() bool {
	return s == GoalResolved || s == GoalAbandoned
}

// CanTransitionTo reports whether a goal may move from s to next. Transitions out
// of a terminal state are forbidden (history is final), the target must be a
// recognised status, and a no-op transition to the same state is rejected.
func (s GoalStatus) CanTransitionTo(next GoalStatus) bool {
	if !next.Valid() || s.Terminal() || s == next {
		return false
	}
	return true
}

// Resolution records how a goal concluded. It is set only when a goal enters a
// terminal status (Resolved or Abandoned) and is absent otherwise. The Result
// reuses the OutcomeResult vocabulary so a concluded decision reads consistently
// with the per-move outcomes that informed it.
type Resolution struct {
	Result     OutcomeResult `json:"result"`
	Notes      string        `json:"notes,omitempty"`
	ResolvedAt time.Time     `json:"resolved_at"`
}

// Goal is the objective to optimise toward, together with the context that
// frames it. A goal belongs to a player and is the unit a plan is generated for.
// It is also the durable identity of a decision ("case file"): all plans,
// signals and outcomes hang off the goal id, and Status/Resolution track the
// decision's lifecycle.
type Goal struct {
	ID         string      `json:"id"`
	PlayerID   string      `json:"player_id,omitempty"`
	Domain     string      `json:"domain,omitempty"` // decision domain / pack key: generic|investing|growth|career; empty resolves to generic
	Objective  string      `json:"objective"`
	Metric     string      `json:"metric,omitempty"` // how progress is measured
	Target     string      `json:"target,omitempty"` // the value/threshold that defines success
	Context    Context     `json:"context"`
	Status     GoalStatus  `json:"status,omitempty"`     // lifecycle state; empty is treated as active for legacy rows
	Resolution *Resolution `json:"resolution,omitempty"` // present only once the goal is in a terminal status
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at,omitempty"` // last lifecycle change; equals CreatedAt at creation
}
