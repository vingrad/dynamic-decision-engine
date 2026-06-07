package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// PostgresRepository is a pgx/pgxpool-backed Repository. It uses real
// transactions for plan-version creation so that appending an immutable version
// and advancing the plan's current_version pointer happen atomically.
type PostgresRepository struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewPostgres connects to PostgreSQL, verifies connectivity, runs migrations and
// returns a ready repository.
func NewPostgres(ctx context.Context, opts Options, log *slog.Logger) (*PostgresRepository, error) {
	cfg, err := pgxpool.ParseConfig(opts.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("storage: parse database url: %w", err)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresRepository{pool: pool, log: log}, nil
}

// Players ---------------------------------------------------------------------

func (r *PostgresRepository) CreatePlayer(ctx context.Context, p *domain.Player) error {
	md, err := marshalJSON(p.Metadata)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO players (id, kind, name, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		p.ID, string(p.Kind), p.Name, md, p.CreatedAt)
	return mapError(err)
}

func (r *PostgresRepository) GetPlayer(ctx context.Context, id string) (domain.Player, error) {
	var p domain.Player
	var kind string
	var md []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, kind, name, metadata, created_at FROM players WHERE id = $1`, id).
		Scan(&p.ID, &kind, &p.Name, &md, &p.CreatedAt)
	if err != nil {
		return domain.Player{}, mapError(err)
	}
	p.Kind = domain.PlayerKind(kind)
	if err := unmarshalJSON(md, &p.Metadata); err != nil {
		return domain.Player{}, err
	}
	return p, nil
}

// Goals -----------------------------------------------------------------------

func (r *PostgresRepository) CreateGoal(ctx context.Context, g *domain.Goal) error {
	cx, err := marshalJSON(g.Context)
	if err != nil {
		return err
	}
	var playerID any
	if g.PlayerID != "" {
		playerID = g.PlayerID
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO goals (id, player_id, domain, objective, metric, target, context, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		g.ID, playerID, g.Domain, g.Objective, g.Metric, g.Target, cx, g.CreatedAt)
	return mapError(err)
}

func (r *PostgresRepository) GetGoal(ctx context.Context, id string) (domain.Goal, error) {
	return r.scanGoal(r.pool.QueryRow(ctx, `
		SELECT id, COALESCE(player_id, ''), COALESCE(domain, ''), objective, metric, target, context, created_at
		FROM goals WHERE id = $1`, id))
}

func (r *PostgresRepository) ListGoals(ctx context.Context, page Page) ([]domain.Goal, error) {
	page = page.Normalize()
	rows, err := r.pool.Query(ctx, `
		SELECT id, COALESCE(player_id, ''), COALESCE(domain, ''), objective, metric, target, context, created_at
		FROM goals ORDER BY created_at DESC, id LIMIT $1 OFFSET $2`, page.Limit, page.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	out := make([]domain.Goal, 0)
	for rows.Next() {
		g, err := r.scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// rowScanner abstracts pgx.Row and pgx.Rows for shared scan helpers.
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PostgresRepository) scanGoal(row rowScanner) (domain.Goal, error) {
	var g domain.Goal
	var cx []byte
	if err := row.Scan(&g.ID, &g.PlayerID, &g.Domain, &g.Objective, &g.Metric, &g.Target, &cx, &g.CreatedAt); err != nil {
		return domain.Goal{}, mapError(err)
	}
	if err := unmarshalJSON(cx, &g.Context); err != nil {
		return domain.Goal{}, err
	}
	return g, nil
}

// Plans -----------------------------------------------------------------------

func (r *PostgresRepository) CreatePlan(ctx context.Context, p *domain.Plan) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO plan (id, goal_id, current_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`,
		p.ID, p.GoalID, p.CurrentVersion, p.CreatedAt, p.UpdatedAt)
	return mapError(err)
}

func (r *PostgresRepository) GetPlan(ctx context.Context, id string) (domain.Plan, error) {
	return r.scanPlan(r.pool.QueryRow(ctx, `
		SELECT id, goal_id, current_version, created_at, updated_at FROM plan WHERE id = $1`, id))
}

func (r *PostgresRepository) GetPlanByGoal(ctx context.Context, goalID string) (domain.Plan, error) {
	return r.scanPlan(r.pool.QueryRow(ctx, `
		SELECT id, goal_id, current_version, created_at, updated_at FROM plan WHERE goal_id = $1`, goalID))
}

func (r *PostgresRepository) scanPlan(row rowScanner) (domain.Plan, error) {
	var p domain.Plan
	if err := row.Scan(&p.ID, &p.GoalID, &p.CurrentVersion, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return domain.Plan{}, mapError(err)
	}
	return p, nil
}

// CreatePlanVersion appends an immutable version and advances current_version in
// a single transaction.
func (r *PostgresRepository) CreatePlanVersion(ctx context.Context, v *domain.PlanVersion) error {
	moves, err := marshalJSON(v.RankedMoves)
	if err != nil {
		return err
	}
	prov, err := marshalJSON(v.Provenance)
	if err != nil {
		return err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx, `
		INSERT INTO plan_version
			(plan_id, version, goal, summary, ranked_moves, provenance, input_snapshot_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		v.PlanID, v.Version, v.Goal, v.Summary, moves, prov, v.InputSnapshotID, v.CreatedAt); err != nil {
		return mapError(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE plan SET current_version = $2, updated_at = $3
		WHERE id = $1 AND current_version < $2`,
		v.PlanID, v.Version, v.CreatedAt); err != nil {
		return mapError(err)
	}
	return mapError(tx.Commit(ctx))
}

func (r *PostgresRepository) GetPlanVersion(ctx context.Context, planID string, version int) (domain.PlanVersion, error) {
	return r.scanVersion(r.pool.QueryRow(ctx, `
		SELECT plan_id, version, goal, summary, ranked_moves, provenance, input_snapshot_id, created_at
		FROM plan_version WHERE plan_id = $1 AND version = $2`, planID, version))
}

func (r *PostgresRepository) GetCurrentPlanVersion(ctx context.Context, planID string) (domain.PlanVersion, error) {
	return r.scanVersion(r.pool.QueryRow(ctx, `
		SELECT pv.plan_id, pv.version, pv.goal, pv.summary, pv.ranked_moves, pv.provenance, pv.input_snapshot_id, pv.created_at
		FROM plan_version pv
		JOIN plan p ON p.id = pv.plan_id AND p.current_version = pv.version
		WHERE pv.plan_id = $1`, planID))
}

func (r *PostgresRepository) ListPlanVersions(ctx context.Context, planID string, page Page) ([]domain.PlanVersion, error) {
	page = page.Normalize()
	rows, err := r.pool.Query(ctx, `
		SELECT plan_id, version, goal, summary, ranked_moves, provenance, input_snapshot_id, created_at
		FROM plan_version WHERE plan_id = $1 ORDER BY version ASC LIMIT $2 OFFSET $3`,
		planID, page.Limit, page.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	out := make([]domain.PlanVersion, 0)
	for rows.Next() {
		v, err := r.scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) scanVersion(row rowScanner) (domain.PlanVersion, error) {
	var v domain.PlanVersion
	var moves, prov []byte
	if err := row.Scan(&v.PlanID, &v.Version, &v.Goal, &v.Summary, &moves, &prov, &v.InputSnapshotID, &v.CreatedAt); err != nil {
		return domain.PlanVersion{}, mapError(err)
	}
	if err := unmarshalJSON(moves, &v.RankedMoves); err != nil {
		return domain.PlanVersion{}, err
	}
	if err := unmarshalJSON(prov, &v.Provenance); err != nil {
		return domain.PlanVersion{}, err
	}
	return v, nil
}

// Signals ---------------------------------------------------------------------

func (r *PostgresRepository) CreateSignal(ctx context.Context, s *domain.Signal) error {
	payload, err := marshalJSON(s.Payload)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO signal (id, goal_id, kind, description, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		s.ID, s.GoalID, s.Kind, s.Description, payload, s.CreatedAt)
	return mapError(err)
}

func (r *PostgresRepository) ListSignals(ctx context.Context, goalID string, page Page) ([]domain.Signal, error) {
	page = page.Normalize()
	rows, err := r.pool.Query(ctx, `
		SELECT id, goal_id, kind, description, payload, created_at
		FROM signal WHERE goal_id = $1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`,
		goalID, page.Limit, page.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	out := make([]domain.Signal, 0)
	for rows.Next() {
		var s domain.Signal
		var payload []byte
		if err := rows.Scan(&s.ID, &s.GoalID, &s.Kind, &s.Description, &payload, &s.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		if err := unmarshalJSON(payload, &s.Payload); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetSignal(ctx context.Context, id string) (domain.Signal, error) {
	var s domain.Signal
	var payload []byte
	var processed *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT id, goal_id, kind, description, payload, created_at,
		       COALESCE(status, 'pending'), COALESCE(reason, ''), COALESCE(result_version, 0), COALESCE(error, ''), processed_at
		FROM signal WHERE id = $1`, id).Scan(
		&s.ID, &s.GoalID, &s.Kind, &s.Description, &payload, &s.CreatedAt,
		&s.Status, &s.Reason, &s.ResultVersion, &s.Error, &processed)
	if err != nil {
		return domain.Signal{}, mapError(err)
	}
	if err := unmarshalJSON(payload, &s.Payload); err != nil {
		return domain.Signal{}, err
	}
	s.ProcessedAt = processed
	return s, nil
}

func (r *PostgresRepository) MarkSignalProcessed(ctx context.Context, id, status string, resultVersion int, reason, errMsg string, at time.Time) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE signal SET status = $2, result_version = $3, reason = $4, error = $5, processed_at = $6
		WHERE id = $1`,
		id, status, resultVersion, reason, errMsg, at)
	if err != nil {
		return mapError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Outcomes --------------------------------------------------------------------

func (r *PostgresRepository) CreateOutcome(ctx context.Context, o *domain.Outcome) error {
	observed, err := marshalJSON(o.ObservedSignals)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO outcome (id, goal_id, move_title, result, observed_signals, notes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		o.ID, o.GoalID, o.MoveTitle, string(o.Result), observed, o.Notes, o.CreatedAt)
	return mapError(err)
}

func (r *PostgresRepository) ListOutcomes(ctx context.Context, goalID string, page Page) ([]domain.Outcome, error) {
	page = page.Normalize()
	rows, err := r.pool.Query(ctx, `
		SELECT id, goal_id, move_title, result, observed_signals, notes, created_at
		FROM outcome WHERE goal_id = $1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`,
		goalID, page.Limit, page.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	out := make([]domain.Outcome, 0)
	for rows.Next() {
		var o domain.Outcome
		var result string
		var observed []byte
		if err := rows.Scan(&o.ID, &o.GoalID, &o.MoveTitle, &result, &observed, &o.Notes, &o.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		o.Result = domain.OutcomeResult(result)
		if err := unmarshalJSON(observed, &o.ObservedSignals); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Operational -----------------------------------------------------------------

func (r *PostgresRepository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func (r *PostgresRepository) Close() { r.pool.Close() }

// helpers ---------------------------------------------------------------------

// marshalJSON encodes a value for a JSONB column. nil maps/slices are stored as
// empty JSON containers so reads never see SQL NULL.
func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("storage: marshal json: %w", err)
	}
	return string(b), nil
}

func unmarshalJSON(b []byte, dst any) error {
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("storage: unmarshal json: %w", err)
	}
	return nil
}

// mapError translates pgx/driver errors into the package's sentinel errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrConflict
		case "23503": // foreign_key_violation -> referenced entity missing
			return ErrNotFound
		}
	}
	return err
}
