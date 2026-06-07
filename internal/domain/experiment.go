package domain

// Experiment is a testable action attached to a move: a small, time-boxed bet
// with explicit success signals and kill/pivot criteria. Experiments are how the
// engine turns a recommendation into something the player can validate cheaply.
type Experiment struct {
	Title          string   `json:"title"`
	DurationDays   int      `json:"duration_days"`
	SuccessSignals []string `json:"success_signals"`
	KillCriteria   []string `json:"kill_criteria"`
}
