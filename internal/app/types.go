package app

import "github.com/vingrad/dynamic-decision-engine/internal/domain"

// Input types are transport-agnostic: the API and CLI both map their own request
// shapes onto these before calling the service.

// CreateGoalInput is the input to CreateGoal.
type CreateGoalInput struct {
	PlayerID  string
	Domain    string
	Objective string
	Metric    string
	Target    string
	Context   domain.Context
}

// SignalInput is the input to ApplySignal.
type SignalInput struct {
	GoalID      string
	Kind        string
	Description string
	Payload     map[string]any
}

// OutcomeInput is the input to RecordOutcome.
type OutcomeInput struct {
	GoalID          string
	MoveTitle       string
	Result          domain.OutcomeResult
	ObservedSignals []string
	Notes           string
}

// EvaluateInput is the input to the stateless Evaluate use-case.
type EvaluateInput struct {
	Domain     string
	Objective  string
	Metric     string
	Target     string
	Context    domain.Context
	SignalNote string
}

// SignalStatus describes how a signal's replanning was handled.
type SignalStatus string

const (
	// StatusApplied means replanning ran and a new immutable version was created.
	StatusApplied SignalStatus = "applied"
	// StatusUnchanged means replanning ran but the change was immaterial.
	StatusUnchanged SignalStatus = "unchanged"
	// StatusPending means the signal was accepted and replanning was scheduled
	// asynchronously; poll the plan's versions for the result.
	StatusPending SignalStatus = "pending"
	// StatusFailed means async replanning errored; see the signal's error field.
	StatusFailed SignalStatus = "failed"
)

// SignalResult is the outcome of applying a signal: the stored signal, how it was
// handled, the materiality decision, and the resulting (possibly new) plan version.
// For asynchronous (pending) results, Material is false and PlanVersion is the
// current version at acceptance time.
type SignalResult struct {
	Signal      domain.Signal
	Status      SignalStatus
	Material    bool
	Reason      string
	PlanVersion domain.PlanVersion
}

// PlanView bundles a plan head with its current version for read endpoints.
type PlanView struct {
	Plan           domain.Plan
	CurrentVersion domain.PlanVersion
}
