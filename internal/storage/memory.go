package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// MemoryRepository is a concurrency-safe in-memory Repository. It is the default
// store when no database is configured and is used throughout the unit tests, so
// the system is fully exercisable with zero infrastructure.
//
// Stored values are copied on the way in and out so callers cannot mutate state
// through retained references — the same immutability guarantee the SQL store
// gets from the database.
type MemoryRepository struct {
	mu sync.RWMutex

	players  map[string]domain.Player
	goals    map[string]domain.Goal
	plans    map[string]domain.Plan
	byGoal   map[string]string                     // goalID -> planID
	versions map[string]map[int]domain.PlanVersion // planID -> version -> snapshot
	signals  []domain.Signal
	outcomes []domain.Outcome
}

// NewMemory returns an empty in-memory repository.
func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		players:  make(map[string]domain.Player),
		goals:    make(map[string]domain.Goal),
		plans:    make(map[string]domain.Plan),
		byGoal:   make(map[string]string),
		versions: make(map[string]map[int]domain.PlanVersion),
	}
}

// Tx runs fn against a staged copy of the store and adopts it only if fn succeeds,
// so a failing multi-write use-case leaves the store unchanged. The whole tx holds
// the write lock (serialising it against other access); fn operates on a separate
// repository with its own lock, so calling its methods does not re-enter m.mu.
func (m *MemoryRepository) Tx(_ context.Context, fn func(tx Repository) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	staged := m.snapshotLocked()
	if err := fn(staged); err != nil {
		return err // discard staged: rollback
	}
	m.adoptLocked(staged)
	return nil
}

// snapshotLocked returns a deep copy of the store as a standalone repository. Every
// container is copied — including the inner per-plan version maps and the signal
// and outcome slices — so mutations on the snapshot cannot reach m before adoption.
func (m *MemoryRepository) snapshotLocked() *MemoryRepository {
	s := &MemoryRepository{
		players:  make(map[string]domain.Player, len(m.players)),
		goals:    make(map[string]domain.Goal, len(m.goals)),
		plans:    make(map[string]domain.Plan, len(m.plans)),
		byGoal:   make(map[string]string, len(m.byGoal)),
		versions: make(map[string]map[int]domain.PlanVersion, len(m.versions)),
	}
	for k, v := range m.players {
		s.players[k] = clonePlayer(v)
	}
	for k, v := range m.goals {
		s.goals[k] = cloneGoal(v)
	}
	for k, v := range m.plans {
		s.plans[k] = v
	}
	for k, v := range m.byGoal {
		s.byGoal[k] = v
	}
	for planID, vers := range m.versions {
		inner := make(map[int]domain.PlanVersion, len(vers))
		for ver, pv := range vers {
			inner[ver] = clonePlanVersion(pv)
		}
		s.versions[planID] = inner
	}
	s.signals = make([]domain.Signal, len(m.signals))
	for i := range m.signals {
		s.signals[i] = cloneSignal(m.signals[i])
	}
	s.outcomes = make([]domain.Outcome, len(m.outcomes))
	for i := range m.outcomes {
		s.outcomes[i] = cloneOutcome(m.outcomes[i])
	}
	return s
}

// adoptLocked replaces m's containers with the staged copy's (commit). Safe to take
// them wholesale: m.mu is held for the whole tx, so no concurrent writer exists.
func (m *MemoryRepository) adoptLocked(s *MemoryRepository) {
	m.players = s.players
	m.goals = s.goals
	m.plans = s.plans
	m.byGoal = s.byGoal
	m.versions = s.versions
	m.signals = s.signals
	m.outcomes = s.outcomes
}

// Players ---------------------------------------------------------------------

func (m *MemoryRepository) CreatePlayer(_ context.Context, p *domain.Player) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.players[p.ID]; ok {
		return ErrConflict
	}
	m.players[p.ID] = clonePlayer(*p)
	return nil
}

func (m *MemoryRepository) GetPlayer(_ context.Context, id string) (domain.Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.players[id]
	if !ok {
		return domain.Player{}, ErrNotFound
	}
	return clonePlayer(p), nil
}

// Goals -----------------------------------------------------------------------

func (m *MemoryRepository) CreateGoal(_ context.Context, g *domain.Goal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.goals[g.ID]; ok {
		return ErrConflict
	}
	m.goals[g.ID] = cloneGoal(*g)
	return nil
}

func (m *MemoryRepository) GetGoal(_ context.Context, id string) (domain.Goal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.goals[id]
	if !ok {
		return domain.Goal{}, ErrNotFound
	}
	return cloneGoal(g), nil
}

func (m *MemoryRepository) UpdateGoalStatus(_ context.Context, id string, expected, status domain.GoalStatus, resolution *domain.Resolution, updatedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.goals[id]
	if !ok {
		return ErrNotFound
	}
	if g.Status != expected {
		return ErrConflict // a concurrent transition moved the goal first
	}
	g.Status = status
	g.Resolution = resolution
	g.UpdatedAt = updatedAt
	m.goals[id] = cloneGoal(g)
	return nil
}

func (m *MemoryRepository) ListGoals(_ context.Context, filter GoalFilter, page Page) ([]domain.Goal, error) {
	page = page.Normalize()
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.Goal, 0, len(m.goals))
	for _, g := range m.goals {
		if !goalMatchesStatus(g, filter.Status) {
			continue
		}
		out = append(out, cloneGoal(g))
	}
	// Newest first, stable by ID for determinism.
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return paginateGoals(out, page), nil
}

// Plans -----------------------------------------------------------------------

func (m *MemoryRepository) CreatePlan(_ context.Context, p *domain.Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.plans[p.ID]; ok {
		return ErrConflict
	}
	m.plans[p.ID] = *p
	m.byGoal[p.GoalID] = p.ID
	if m.versions[p.ID] == nil {
		m.versions[p.ID] = make(map[int]domain.PlanVersion)
	}
	return nil
}

func (m *MemoryRepository) GetPlan(_ context.Context, id string) (domain.Plan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plans[id]
	if !ok {
		return domain.Plan{}, ErrNotFound
	}
	return p, nil
}

func (m *MemoryRepository) GetPlanByGoal(_ context.Context, goalID string) (domain.Plan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	planID, ok := m.byGoal[goalID]
	if !ok {
		return domain.Plan{}, ErrNotFound
	}
	return m.plans[planID], nil
}

// CreatePlanVersion appends an immutable version and advances current_version
// atomically under the write lock.
func (m *MemoryRepository) CreatePlanVersion(_ context.Context, v *domain.PlanVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, ok := m.plans[v.PlanID]
	if !ok {
		return ErrNotFound
	}
	if m.versions[v.PlanID] == nil {
		m.versions[v.PlanID] = make(map[int]domain.PlanVersion)
	}
	if _, exists := m.versions[v.PlanID][v.Version]; exists {
		return ErrConflict // versions are immutable: never overwrite
	}

	m.versions[v.PlanID][v.Version] = clonePlanVersion(*v)
	if v.Version > plan.CurrentVersion {
		plan.CurrentVersion = v.Version
		plan.UpdatedAt = v.CreatedAt
		m.plans[v.PlanID] = plan
	}
	return nil
}

func (m *MemoryRepository) GetPlanVersion(_ context.Context, planID string, version int) (domain.PlanVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	vers, ok := m.versions[planID]
	if !ok {
		return domain.PlanVersion{}, ErrNotFound
	}
	v, ok := vers[version]
	if !ok {
		return domain.PlanVersion{}, ErrNotFound
	}
	return clonePlanVersion(v), nil
}

func (m *MemoryRepository) GetCurrentPlanVersion(_ context.Context, planID string) (domain.PlanVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	plan, ok := m.plans[planID]
	if !ok {
		return domain.PlanVersion{}, ErrNotFound
	}
	v, ok := m.versions[planID][plan.CurrentVersion]
	if !ok {
		return domain.PlanVersion{}, ErrNotFound
	}
	return clonePlanVersion(v), nil
}

func (m *MemoryRepository) ListPlanVersions(_ context.Context, planID string, page Page) ([]domain.PlanVersion, error) {
	page = page.Normalize()
	m.mu.RLock()
	defer m.mu.RUnlock()

	vers, ok := m.versions[planID]
	if !ok {
		if _, planExists := m.plans[planID]; !planExists {
			return nil, ErrNotFound
		}
		return []domain.PlanVersion{}, nil
	}
	out := make([]domain.PlanVersion, 0, len(vers))
	for _, v := range vers {
		out = append(out, clonePlanVersion(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return paginateVersions(out, page), nil
}

// Signals ---------------------------------------------------------------------

func (m *MemoryRepository) CreateSignal(_ context.Context, s *domain.Signal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signals = append(m.signals, cloneSignal(*s))
	return nil
}

func (m *MemoryRepository) GetSignal(_ context.Context, id string) (domain.Signal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.signals {
		if m.signals[i].ID == id {
			return cloneSignal(m.signals[i]), nil
		}
	}
	return domain.Signal{}, ErrNotFound
}

func (m *MemoryRepository) MarkSignalProcessed(_ context.Context, id, status string, resultVersion int, reason, errMsg string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.signals {
		if m.signals[i].ID == id {
			t := at
			m.signals[i].Status = status
			m.signals[i].ResultVersion = resultVersion
			m.signals[i].Reason = reason
			m.signals[i].Error = errMsg
			m.signals[i].ProcessedAt = &t
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryRepository) ListPendingSignals(_ context.Context, limit int) ([]domain.Signal, error) {
	if limit <= 0 {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []domain.Signal
	// Oldest first (insertion order) for FIFO-fair recovery.
	for i := range m.signals {
		// "pending" is the status the service writes on create; "" guards any signal
		// persisted without an explicit status (mirrors the postgres column default).
		if s := m.signals[i].Status; s == "pending" || s == "" {
			out = append(out, cloneSignal(m.signals[i]))
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *MemoryRepository) ListSignals(_ context.Context, goalID string, page Page) ([]domain.Signal, error) {
	page = page.Normalize()
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []domain.Signal
	// Iterate newest first.
	for i := len(m.signals) - 1; i >= 0; i-- {
		if m.signals[i].GoalID == goalID {
			out = append(out, cloneSignal(m.signals[i]))
		}
	}
	return paginateSignals(out, page), nil
}

// Outcomes --------------------------------------------------------------------

func (m *MemoryRepository) CreateOutcome(_ context.Context, o *domain.Outcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, cloneOutcome(*o))
	return nil
}

func (m *MemoryRepository) ListOutcomes(_ context.Context, goalID string, page Page) ([]domain.Outcome, error) {
	page = page.Normalize()
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []domain.Outcome
	for i := len(m.outcomes) - 1; i >= 0; i-- {
		if m.outcomes[i].GoalID == goalID {
			out = append(out, cloneOutcome(m.outcomes[i]))
		}
	}
	return paginateOutcomes(out, page), nil
}

// Operational -----------------------------------------------------------------

func (m *MemoryRepository) Ping(_ context.Context) error { return nil }

func (m *MemoryRepository) Close() {}

// goalMatchesStatus reports whether g passes a status filter. An empty want
// matches everything; a goal's empty stored status is treated as active so legacy
// rows surface under the active filter (mirroring the SQL store's COALESCE).
func goalMatchesStatus(g domain.Goal, want domain.GoalStatus) bool {
	if want == "" {
		return true
	}
	got := g.Status
	if got == "" {
		got = domain.GoalActive
	}
	return got == want
}

// pagination helpers ----------------------------------------------------------

func paginateGoals(in []domain.Goal, p Page) []domain.Goal {
	lo, hi := window(len(in), p)
	return in[lo:hi]
}
func paginateVersions(in []domain.PlanVersion, p Page) []domain.PlanVersion {
	lo, hi := window(len(in), p)
	return in[lo:hi]
}
func paginateSignals(in []domain.Signal, p Page) []domain.Signal {
	lo, hi := window(len(in), p)
	return in[lo:hi]
}
func paginateOutcomes(in []domain.Outcome, p Page) []domain.Outcome {
	lo, hi := window(len(in), p)
	return in[lo:hi]
}

func window(n int, p Page) (int, int) {
	lo := p.Offset
	if lo > n {
		lo = n
	}
	hi := lo + p.Limit
	if hi > n {
		hi = n
	}
	return lo, hi
}
