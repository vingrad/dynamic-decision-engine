package app

import "github.com/vingrad/dynamic-decision-engine/internal/domain"

// Input types are transport-agnostic: the API and CLI both map their own request
// shapes onto these before calling the service.

// CreateGoalInput is the input to CreateGoal.
type CreateGoalInput struct {
	PlayerID  string
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
	Objective  string
	Metric     string
	Target     string
	Context    domain.Context
	SignalNote string
}

// SignalResult is the outcome of applying a signal: the stored signal, the
// materiality decision, and the resulting (possibly new) plan version.
type SignalResult struct {
	Signal      domain.Signal
	Material    bool
	Reason      string
	PlanVersion domain.PlanVersion
}

// PlanView bundles a plan head with its current version for read endpoints.
type PlanView struct {
	Plan           domain.Plan
	CurrentVersion domain.PlanVersion
}
