# Core concepts

The engine models decision-making with a small, explicit vocabulary. Every
concept maps to a Go type in `internal/domain`.

| Concept | Description |
| --- | --- |
| **Player** | The person, team, company or system making decisions. Owns goals. |
| **Goal** | The objective to optimise toward, with an optional metric and target. Also the durable identity ("case file") of a decision: all plans, signals and outcomes key off the goal id. |
| **GoalStatus** | The goal's lifecycle state: `active` (default), `on_hold`, `resolved`, `abandoned`. `resolved`/`abandoned` are terminal. Lifecycle is metadata about the decision; it does not influence the input snapshot or the planner. |
| **Resolution** | How a goal concluded — a `result` (reusing the Outcome vocabulary), `notes` and `resolved_at`. Present only once the goal is in a terminal status. |
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
| **DecisionProvenance** | The explanation of why a plan/version was generated — reasoning summary, input snapshot, planner, prompt version, model. When strategies competed it also records the winner, the comparator mode, the context label ("regime") and every candidate's score. |
| **ModelInvocation** | Metadata about a reasoning-model call (model, prompt version, token usage, latency). Placeholder values under the mock planner. |
| **Strategy** | A named lens a domain can plan through (investing: `value`/`momentum`/`defensive` parameter lenses; growth: `expand`/`retain`/`experiment` prompt lenses). Declared as pure data on the pack descriptor. |
| **Strategy selection** | When a domain declares strategies (and selection is enabled), every lens produces a full candidate plan in parallel and a selector picks the winner: hard-constraint filter → goal-derived utility → deterministic tie-break, with hysteresis so near-ties never flap the winner. The whole competition is recorded in provenance (`strategy_candidates`). |
| **Comparator** | How competing candidates are compared: `utility` (pure math over each candidate's stated numbers) or `verify` (one independent reviewer critiques every candidate first, all-or-nothing, then utility arbitrates — for text lenses whose self-reported confidences share no yardstick). |
| **Regime / context label** | A coarse, descriptive classification of the decision context that gates which strategies compete (investing: `trend`/`range`/`high_vol` from trailing price action). Unknown gates nothing. Recorded in provenance for per-regime outcome analysis (`dde strategy-fit`). |

## Key invariants

- **Immutability.** A `PlanVersion` is never updated or deleted. Replanning
  creates a new version (`N+1`); the `Plan.current_version` pointer advances.
  This is enforced both in code and by the database schema
  (`UNIQUE(plan_id, version)`, no update path).
- **Auditability.** Every plan version carries `DecisionProvenance`, so any
  recommendation can be explained and traced back to the exact input snapshot.
- **Goal lifecycle is the only goal mutation.** A goal's substantive fields
  (objective, metric, target, context) are never updated in place; the sole
  mutation path is a `GoalStatus` transition (with its `Resolution` and
  `updated_at`). Transitions out of a terminal status are rejected, and lifecycle
  never feeds the planner, so it cannot change an `input_snapshot_id`.
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
