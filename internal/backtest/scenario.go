package backtest

import (
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Scenario is the input to a backtest: a goal and a chronological list of signals.
type Scenario struct {
	Name string `json:"name"`
	// StartAt is when the initial plan is generated. Defaults to the first event.
	StartAt time.Time `json:"start_at,omitempty"`
	// Goal is the investing goal under test (its Domain should be "investing").
	Goal domain.Goal `json:"goal"`
	// Events are the signals to replay, expected in chronological order.
	Events []TimelineEvent `json:"events"`
	// FixtureDir optionally points the offline market-data provider at a directory
	// of scenario-specific fixtures instead of the embedded defaults.
	FixtureDir string `json:"fixture_dir,omitempty"`
}

// TimelineEvent is a single signal at a point in time.
type TimelineEvent struct {
	At      time.Time      `json:"at"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload,omitempty"`
	// ShouldKill is an optional analyst label: the event is one where a correct
	// system should react (cut/replan). Used to score decision quality.
	ShouldKill bool `json:"should_kill,omitempty"`
}
