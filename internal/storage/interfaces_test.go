package storage

// Compile-time assertions that both concrete stores satisfy every segregated role
// interface as well as the composed Repository. These fail the build (not just a
// test run) if a method drifts out of a role interface.
var (
	_ Repository   = (*MemoryRepository)(nil)
	_ Repository   = (*PostgresRepository)(nil)
	_ PlayerStore  = (*MemoryRepository)(nil)
	_ GoalStore    = (*MemoryRepository)(nil)
	_ PlanStore    = (*MemoryRepository)(nil)
	_ SignalStore  = (*MemoryRepository)(nil)
	_ OutcomeStore = (*MemoryRepository)(nil)
	_ Pinger       = (*MemoryRepository)(nil)
	_ Closer       = (*MemoryRepository)(nil)
	_ PlayerStore  = (*PostgresRepository)(nil)
	_ GoalStore    = (*PostgresRepository)(nil)
	_ PlanStore    = (*PostgresRepository)(nil)
	_ SignalStore  = (*PostgresRepository)(nil)
	_ OutcomeStore = (*PostgresRepository)(nil)
	_ Pinger       = (*PostgresRepository)(nil)
	_ Closer       = (*PostgresRepository)(nil)
)
