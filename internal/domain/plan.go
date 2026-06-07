package domain

import "time"

// Plan is the mutable head of a decision: it points at the current strategy for a
// goal. The plan itself holds only identity and a pointer to the current version;
// all substantive content lives in immutable PlanVersion snapshots.
type Plan struct {
	ID             string    `json:"plan_id"`
	GoalID         string    `json:"goal_id"`
	CurrentVersion int       `json:"current_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PlanVersion is an immutable, versioned snapshot of a plan. Versions are
// append-only: replanning never mutates an existing version, it creates a new one.
// This is what makes the decision state auditable and replayable over time.
//
// The JSON shape intentionally mirrors the documented plan output contract
// (plan_id, version, goal, summary, ranked_moves, provenance).
type PlanVersion struct {
	PlanID          string             `json:"plan_id"`
	Version         int                `json:"version"`
	Goal            string             `json:"goal"`
	Summary         string             `json:"summary"`
	RankedMoves     []RankedMove       `json:"ranked_moves"`
	Provenance      DecisionProvenance `json:"provenance"`
	InputSnapshotID string             `json:"input_snapshot_id"`
	CreatedAt       time.Time          `json:"created_at"`
}
