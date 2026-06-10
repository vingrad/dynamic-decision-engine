package domain

// RankedMove is a possible strategic or operational action positioned within a
// ranked action path. Each move carries the decision-support metadata a reviewer
// needs to judge it: confidence, expected impact, effort, risk and rationale,
// plus a first experiment and fallback options.
type RankedMove struct {
	Rank int `json:"rank"`
	// Key is a stable, semantic identifier for the move that survives rewording of
	// the display Title. Materiality is judged on Key (falling back to a normalised
	// Title when empty), so re-phrasing a move does not read as a changed plan.
	Key         string  `json:"key,omitempty"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"` // 0.0 - 1.0
	// RawConfidence is the confidence before any calibration curve is applied
	// (equal to Confidence when no curve is installed; 0 means "not recorded" on
	// plans that predate the field). Calibration is refit against this value so
	// successive fits stay in the same domain.
	RawConfidence  float64    `json:"raw_confidence,omitempty"`
	ExpectedImpact Level      `json:"expected_impact"`
	Effort         Level      `json:"effort"`
	Risk           Level      `json:"risk"`
	Rationale      string     `json:"rationale"`
	Experiment     Experiment `json:"experiment"`
	FallbackMoves  []string   `json:"fallback_moves"`
	// DependsOn lists the Keys of moves in the SAME plan version that must
	// complete before this move can start. The moves of a version form a DAG (no
	// cycles); SanitizeMoveGraph enforces this. DependsOn is the authoritative
	// ordering constraint — an orchestrator derives the concurrently-runnable set
	// from it (moves whose dependencies are all satisfied). It is independent of
	// Rank: rank is the strength of the recommendation, not the execution order.
	DependsOn []string `json:"depends_on,omitempty"`
	// ParallelGroup is an optional display/grouping hint labelling moves intended
	// to run together (e.g. "core", "validate"). It is advisory only; DependsOn is
	// the source of truth for ordering.
	ParallelGroup string `json:"parallel_group,omitempty"`
}
