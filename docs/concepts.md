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
| **Experiment** | A testable, time-boxed action attached to a move, with success signals and kill/pivot criteria. |
| **Signal** | New information that may require replanning (a market shift, a result, a changed constraint). |
| **Outcome** | The recorded result of a move or experiment. Closes the learning loop. |
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
