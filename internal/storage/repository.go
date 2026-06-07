// Package storage defines the persistence boundary for the decision engine and
// provides two implementations: an in-memory store (the zero-infrastructure
// default, also used in tests) and a PostgreSQL store for real deployments.
//
// PlanVersions are append-only everywhere: there is no update or delete path for
// them, which is what guarantees the immutability and auditability of decision
// state.
package storage

import (
	"context"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// DefaultPageLimit and MaxPageLimit bound list queries so a caller can never pull
// an unbounded result set.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// Page describes a limit/offset window for list queries.
type Page struct {
	Limit  int
	Offset int
}

// Normalize clamps a page to sane bounds, applying defaults for zero values.
func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = DefaultPageLimit
	}
	if p.Limit > MaxPageLimit {
		p.Limit = MaxPageLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// The interface is segregated into one role per aggregate so consumers can depend
// on just the slice they use (interface-segregation). Repository composes them all
// for stores and wiring that genuinely need the whole surface. All methods take a
// context for cancellation/deadlines and must be safe for concurrent use.

// PlayerStore persists players.
type PlayerStore interface {
	CreatePlayer(ctx context.Context, p *domain.Player) error
	GetPlayer(ctx context.Context, id string) (domain.Player, error)
}

// GoalStore persists goals.
type GoalStore interface {
	CreateGoal(ctx context.Context, g *domain.Goal) error
	GetGoal(ctx context.Context, id string) (domain.Goal, error)
	ListGoals(ctx context.Context, page Page) ([]domain.Goal, error)
}

// PlanStore persists plans and their immutable versions. CreatePlanVersion appends
// a version and advances the plan's current_version pointer atomically.
type PlanStore interface {
	CreatePlan(ctx context.Context, p *domain.Plan) error
	GetPlan(ctx context.Context, id string) (domain.Plan, error)
	GetPlanByGoal(ctx context.Context, goalID string) (domain.Plan, error)
	CreatePlanVersion(ctx context.Context, v *domain.PlanVersion) error
	GetPlanVersion(ctx context.Context, planID string, version int) (domain.PlanVersion, error)
	GetCurrentPlanVersion(ctx context.Context, planID string) (domain.PlanVersion, error)
	ListPlanVersions(ctx context.Context, planID string, page Page) ([]domain.PlanVersion, error)
}

// SignalStore persists signals and the terminal status of the replans they trigger.
type SignalStore interface {
	CreateSignal(ctx context.Context, s *domain.Signal) error
	GetSignal(ctx context.Context, id string) (domain.Signal, error)
	ListSignals(ctx context.Context, goalID string, page Page) ([]domain.Signal, error)
	// MarkSignalProcessed records the terminal status of the replan a signal
	// triggered (applied|unchanged|failed), making async outcomes queryable.
	MarkSignalProcessed(ctx context.Context, id, status string, resultVersion int, reason, errMsg string, at time.Time) error
}

// OutcomeStore persists outcomes recorded against goals.
type OutcomeStore interface {
	CreateOutcome(ctx context.Context, o *domain.Outcome) error
	ListOutcomes(ctx context.Context, goalID string, page Page) ([]domain.Outcome, error)
}

// Pinger reports store health; Closer releases store resources. Split out so an
// operational consumer (e.g. a health check) need not depend on data methods.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Closer releases a store's resources.
type Closer interface {
	Close()
}

// Repository is the full persistence interface, composed from the per-aggregate
// stores. Concrete implementations (MemoryRepository, PostgresRepository) satisfy
// it; consumers that touch every aggregate depend on it, while narrower consumers
// can depend on a single store interface above.
type Repository interface {
	PlayerStore
	GoalStore
	PlanStore
	SignalStore
	OutcomeStore
	Pinger
	Closer
}
