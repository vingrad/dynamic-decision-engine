package app

import "errors"

// Sentinel errors raised by the service layer. Transport adapters map these to
// protocol-specific responses (e.g. HTTP status codes).
var (
	// ErrPlanExists indicates a plan has already been generated for a goal.
	ErrPlanExists = errors.New("app: a plan already exists for this goal")
	// ErrNoPlanForGoal indicates an operation needs a plan that does not exist yet.
	ErrNoPlanForGoal = errors.New("app: no plan exists for this goal")
	// ErrGoalNotActive indicates an operation that mutates decision state (sending a
	// signal, generating a plan) was attempted on a goal that is not active. Only
	// active goals accept these; on_hold, resolved and abandoned goals reject them.
	ErrGoalNotActive = errors.New("app: goal is not active; resume it before sending signals or generating plans")
)

// ValidationError signals invalid caller input. It carries a human-readable
// message suitable for returning to the client.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return "app: invalid input: " + e.Msg }

// invalid is a small constructor for a ValidationError.
func invalid(msg string) error { return &ValidationError{Msg: msg} }
