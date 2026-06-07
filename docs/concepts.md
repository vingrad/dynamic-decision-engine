# Core concepts

The engine models decision-making with a small, explicit vocabulary. Every
concept maps to a Go type in `internal/domain`.

| Concept | Description |
| --- | --- |
| **Player** | The person, team, company or system making decisions. Owns goals. |
| **Goal** | The objective to optimise toward, with an optional metric and target. |
| **Context** | The current situation and relevant facts that frame a goal. Holds assets and constraints. |
| **Asset** | A resource, skill, advantage, dataset, relationship or product the player can use. |
| **Constraint** | A limit, risk, rule or boundary the plan must respect (budget, time, geography, policy, …). |
| **Move** | A possible strategic or operational action. In a plan it appears as a **RankedMove** with decision-support metadata. |
| **ActionPath** | A ranked sequence/branch of moves — represented as the ordered `ranked_moves` of a plan version. |
| **DependsOn** | The keys of moves that must complete before a move can start. The moves of a version form a DAG; this is the authoritative execution-ordering constraint, independent of rank. |
| **ParallelGroup** | An optional label grouping moves intended to run together. Advisory display hint; `depends_on` is the source of truth for ordering. |
| **Experiment** | A testable, time-boxed action attached to a move, with success signals and kill/pivot criteria. |
| **Signal** | New information that may require replanning (a market shift, a result, a changed constraint). |
| **Outcome** | The recorded result of a move, addressed by its stable `(plan_version, move_rank)` location in the immutable plan. Closes the learning loop. |
| **Plan** | The mutable head pointing at the current strategy for a goal. |
| **PlanVersion** | An immutable, versioned snapshot of a plan. Append-only. |
| **DecisionProvenance** | The explanation of why a plan/version was generated — reasoning summary, input snapshot, planner, prompt version, model. |
| **ModelInvocation** | Metadata about a reasoning-model call (model, prompt version, token usage, latency). Placeholder values under the mock planner. |

## Key invariants

- **Immutability.** A `PlanVersion` is never updated or deleted. Replanning
  creates a new version (`N+1`); the `Plan.current_version` pointer advances.
  This is enforced both in code and by the database schema
  (`UNIQUE(plan_id, version)`, no update path).
- **Auditability.** Every plan version carries `DecisionProvenance`, so any
  recommendation can be explained and traced back to the exact input snapshot.
- **Determinism (mock planner).** Given the same goal, context and signal note,
  the default planner always produces the same plan — making the system testable
  and reproducible without external services.
- **Acyclic move graph.** Within a plan version, `depends_on` references form a
  directed acyclic graph. The engine sanitises every version it builds — dropping
  references to unknown keys and breaking any cycles — so a stored plan always
  holds a valid DAG, whatever the planner emitted.
- **Structure is material.** A change to the dependency graph or parallel grouping
  counts as a material change to the recommended action path and produces a new
  plan version, the same as a change in the moves or their order.
