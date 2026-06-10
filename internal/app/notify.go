package app

import (
	"context"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Event type names. Keep in sync with webhookEventNames in internal/config.
const (
	EventGoalCreated       = "goal.created"
	EventPlanCreated       = "plan.created"
	EventSignalReceived    = "signal.received"
	EventReplanCompleted   = "replan.completed"
	EventOutcomeRecorded   = "outcome.recorded"
	EventGoalStatusChanged = "goal.status_changed"
)

// Event is the envelope handed to a Notifier after a use-case commits. Its JSON
// encoding is the wire format webhook receivers see.
type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Payload   any       `json:"payload"`
}

// Notifier receives domain events after they are durably committed. Emit must
// not block and must never fail the calling use-case — event fan-out is
// best-effort, the store is the source of truth. Like Metrics, the interface
// lives here so the service stays free of transport dependencies.
type Notifier interface {
	Emit(ctx context.Context, evt Event)
}

// nopNotifier is the default no-op Notifier used when none is supplied.
type nopNotifier struct{}

func (nopNotifier) Emit(context.Context, Event) {}

// WithNotifier sets the event sink.
func WithNotifier(n Notifier) Option {
	return func(s *Service) {
		if n != nil {
			s.notifier = n
		}
	}
}

// emit wraps a payload in an Event envelope and forwards it to the notifier.
func (s *Service) emit(ctx context.Context, typ string, payload any) {
	s.notifier.Emit(ctx, Event{
		ID:        domain.NewID("evt"),
		Type:      typ,
		CreatedAt: s.clock(),
		Payload:   payload,
	})
}

// GoalCreatedPayload is the payload of a goal.created event.
type GoalCreatedPayload struct {
	Goal domain.Goal `json:"goal"`
}

// PlanCreatedPayload is the payload of a plan.created event (the initial plan).
type PlanCreatedPayload struct {
	GoalID  string             `json:"goal_id"`
	Version domain.PlanVersion `json:"version"`
}

// SignalReceivedPayload is the payload of a signal.received event. Status is
// pending when replanning was scheduled asynchronously; otherwise it carries
// the inline replanning result (applied or unchanged).
type SignalReceivedPayload struct {
	Signal domain.Signal `json:"signal"`
	Status SignalStatus  `json:"status"`
}

// ReplanCompletedPayload is the payload of a replan.completed event: the
// terminal result of re-evaluating a plan against a signal.
type ReplanCompletedPayload struct {
	GoalID   string       `json:"goal_id"`
	PlanID   string       `json:"plan_id"`
	SignalID string       `json:"signal_id"`
	Status   SignalStatus `json:"status"` // applied | unchanged | failed
	Reason   string       `json:"reason,omitempty"`
	Error    string       `json:"error,omitempty"`
	// Version is the resulting plan version: the new version when applied, the
	// surviving current version when unchanged, nil when failed.
	Version *domain.PlanVersion `json:"version,omitempty"`
}

// OutcomeRecordedPayload is the payload of an outcome.recorded event.
type OutcomeRecordedPayload struct {
	Outcome domain.Outcome `json:"outcome"`
}

// GoalStatusChangedPayload is the payload of a goal.status_changed event.
type GoalStatusChangedPayload struct {
	Goal           domain.Goal       `json:"goal"`
	PreviousStatus domain.GoalStatus `json:"previous_status"`
}
