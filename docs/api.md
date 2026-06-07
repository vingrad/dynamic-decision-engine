# API reference

Base URL: `http://localhost:8080`. All request/response bodies are JSON. Errors
use an RFC 7807-style envelope:

```json
{ "type": "about:blank", "title": "Bad Request", "status": 400, "detail": "objective is required" }
```

List endpoints accept `?limit=` and `?offset=` (defaults: limit 50, max 200).

## Operational

| Method | Path | Description |
| --- | --- | --- |
| GET | `/health`, `/health/live` | Liveness. |
| GET | `/health/ready` | Readiness (pings storage). |
| GET | `/metrics` | Prometheus exposition. |

## Goals

### `POST /v1/goals`
Create a goal with context.

```json
{
  "objective": "Grow to 1000 paying customers",
  "metric": "paying customers",
  "target": "1000",
  "context": {
    "assets": [{ "name": "founder network", "kind": "network" }],
    "constraints": [{ "name": "12 months runway", "kind": "budget" }]
  }
}
```

Returns `201` with the created goal (including its generated `id`).

### `GET /v1/goals/{id}`
Fetch a goal.

### `GET /v1/goals`
List goals (paginated): `{ "goals": [ ... ] }`.

## Plans

### `POST /v1/goals/{id}/plans`
Generate the initial plan (version 1) for a goal and persist it. Returns `201`
with the plan version. `409` if a plan already exists.

### `GET /v1/goals/{id}/plans`
Return the plan and current version for a goal, or `404` if none exists.

### `GET /v1/plans/{id}`
Return the plan head and its current version:
`{ "plan": { ... }, "current_version": { ... } }`.

### `GET /v1/plans/{id}/versions`
List immutable plan versions in ascending order: `{ "versions": [ ... ] }`.

### Ranked move shape
Each entry in a version's `ranked_moves` carries decision-support metadata and its
place in the execution graph:

```json
{
  "rank": 1,
  "key": "double-down:founder-network",
  "title": "Double down on founder network",
  "description": "...",
  "confidence": 0.78,
  "expected_impact": "high",
  "effort": "medium",
  "risk": "low",
  "rationale": "...",
  "experiment": { "title": "...", "duration_days": 7, "success_signals": ["..."], "kill_criteria": ["..."] },
  "fallback_moves": ["..."],
  "depends_on": ["validation-experiment", "neutralise:small-team"],
  "parallel_group": "commit"
}
```

- `depends_on` — keys of moves in the **same version** that must complete before
  this move can start. The moves form a DAG; the engine drops references to
  unknown keys and breaks cycles, so a returned plan is always acyclic. Ordering,
  not priority: a top-ranked move may still depend on a lower-ranked one. Omitted
  when empty.
- `parallel_group` — optional label for moves intended to run together; advisory
  only (`depends_on` governs ordering). Omitted when empty.

A change to either field between versions is treated as a material change and
produces a new version.

## Signals

### `POST /v1/signals`
Store a signal and re-evaluate the goal's current plan. A material change creates
a new immutable version.

```json
{ "goal_id": "goal_...", "kind": "competitive_shift", "description": "competitor launched a free tier" }
```

Returns `200`:

```json
{
  "signal": { "...": "..." },
  "material": true,
  "reason": "confidence on the top move shifted materially",
  "plan_version": { "version": 2, "...": "..." }
}
```

## Outcomes

### `POST /v1/outcomes`
Record the result of a move. The move is referenced by its stable address in the
goal's plan — `plan_version` plus the move's `move_rank` (the `rank` field from
that version's `ranked_moves`). Both are required.

```json
{ "goal_id": "goal_...", "plan_version": 2, "move_rank": 1, "result": "partial", "notes": "early signal positive" }
```

`result` is one of `success`, `failure`, `partial`, `inconclusive`.

The stored outcome carries a `move_title` resolved server-side from
`(plan_version, move_rank)` — it is returned in the response, never accepted as
input. Errors: a missing/unknown `plan_version` or a `move_rank` not present in
that version returns `400`; a goal with no plan yet returns `409`.

## Evaluate (stateless)

### `POST /v1/evaluate`
Generate a plan from a self-contained goal without persisting anything. This is
the same primitive the CLI `evaluate` command uses.

```json
{
  "objective": "Ship the platform",
  "metric": "active tenants",
  "context": { "assets": [{ "name": "strong team" }] },
  "signal_note": "optional: fold a signal into the reasoning"
}
```

Returns `200` with a plan version.
