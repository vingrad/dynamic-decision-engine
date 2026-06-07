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

// Repository is the full persistence interface. All methods take a context for
// cancellation/deadlines and must be safe for concurrent use.
type Repository interface {
	// Players
	CreatePlayer(ctx context.Context, p *domain.Player) error
	GetPlayer(ctx context.Context, id string) (domain.Player, error)

	// Goals
	CreateGoal(ctx context.Context, g *domain.Goal) error
	GetGoal(ctx context.Context, id string) (domain.Goal, error)
	ListGoals(ctx context.Context, page Page) ([]domain.Goal, error)

	// Plans and versions. CreatePlanVersion appends an immutable version and
	// advances the plan's current_version pointer atomically.
	CreatePlan(ctx context.Context, p *domain.Plan) error
	GetPlan(ctx context.Context, id string) (domain.Plan, error)
	GetPlanByGoal(ctx context.Context, goalID string) (domain.Plan, error)
	CreatePlanVersion(ctx context.Context, v *domain.PlanVersion) error
	GetPlanVersion(ctx context.Context, planID string, version int) (domain.PlanVersion, error)
	GetCurrentPlanVersion(ctx context.Context, planID string) (domain.PlanVersion, error)
	ListPlanVersions(ctx context.Context, planID string, page Page) ([]domain.PlanVersion, error)

	// Signals
	CreateSignal(ctx context.Context, s *domain.Signal) error
	ListSignals(ctx context.Context, goalID string, page Page) ([]domain.Signal, error)

	// Outcomes
	CreateOutcome(ctx context.Context, o *domain.Outcome) error
	ListOutcomes(ctx context.Context, goalID string, page Page) ([]domain.Outcome, error)

	// Operational
	Ping(ctx context.Context) error
	Close()
}
