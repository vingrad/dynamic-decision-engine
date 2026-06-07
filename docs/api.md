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
Record the result of a move/experiment.

```json
{ "goal_id": "goal_...", "move_title": "Double down on founder network", "result": "partial", "notes": "early signal positive" }
```

`result` is one of `success`, `failure`, `partial`, `inconclusive`.

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
