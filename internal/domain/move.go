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
	Key            string     `json:"key,omitempty"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Confidence     float64    `json:"confidence"` // 0.0 - 1.0
	ExpectedImpact Level      `json:"expected_impact"`
	Effort         Level      `json:"effort"`
	Risk           Level      `json:"risk"`
	Rationale      string     `json:"rationale"`
	Experiment     Experiment `json:"experiment"`
	FallbackMoves  []string   `json:"fallback_moves"`
}
