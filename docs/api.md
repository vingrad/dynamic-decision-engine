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
| * | `/mcp` | MCP server (streamable HTTP) sharing the live service — see [`mcp.md`](mcp.md). |

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

Returns `201` with the created goal (including its generated `id`). New goals
start with `status: "active"`.

### `GET /v1/goals/{id}`
Fetch a goal.

### `GET /v1/goals`
List goals (paginated): `{ "goals": [ ... ] }`. Accepts an optional
`?status=active|on_hold|resolved|abandoned` filter; an unrecognised value is
`400`.

### `PATCH /v1/goals/{id}/status`
Transition a goal's lifecycle status. Body:

```json
{ "status": "resolved", "resolution_result": "success", "resolution_notes": "shipped and hit target" }
```

`status` is one of `active`, `on_hold`, `resolved`, `abandoned`. Moving to a
terminal status (`resolved`/`abandoned`) **requires** a `resolution_result`
(`success|failure|partial|inconclusive`); the resolution is rejected for
non-terminal transitions. Transitions out of a terminal status are `400`. The
update is a compare-and-swap on the current status, so a losing concurrent
transition gets `409`. Returns `200` with the updated goal.

## Plans

### `POST /v1/goals/{id}/plans`
Generate the initial plan (version 1) for a goal and persist it. Returns `201`
with the plan version. `409` if a plan already exists, or if the goal is not
`active` (only active goals generate plans).

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

Only `active` goals accept signals; an `on_hold`, `resolved` or `abandoned` goal
returns `409` (and a goal with no plan yet also returns `409`).

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

## Webhooks

The engine can POST domain events to a single HTTP endpoint as they happen, so
external systems (Slack bridges, n8n/Zapier flows, agent triggers) react to
decisions without polling. Webhooks are off by default and enabled by setting a
URL.

| Variable | Default | Description |
| --- | --- | --- |
| `DDE_WEBHOOK_URL` | _(empty = disabled)_ | Endpoint events are POSTed to. |
| `DDE_WEBHOOK_SECRET` | _(empty)_ | When set, each body is signed with HMAC-SHA256 in `X-DDE-Signature`. |
| `DDE_WEBHOOK_TIMEOUT` | `5s` | Per-attempt delivery timeout. |
| `DDE_WEBHOOK_RETRIES` | `3` | Extra attempts after a failed delivery. |
| `DDE_WEBHOOK_EVENTS` | _(empty = all)_ | Comma-separated event-type filter. |

### Event types

| Type | Fired when |
| --- | --- |
| `goal.created` | A goal is persisted. |
| `plan.created` | The initial plan (version 1) is generated for a goal. |
| `signal.received` | A signal is stored; `status` is `applied`/`unchanged` (inline replanning) or `pending` (async). |
| `replan.completed` | Replanning finished: `applied` (new version), `unchanged` (immaterial) or `failed`. |
| `outcome.recorded` | An outcome is recorded for a move. |
| `goal.status_changed` | A goal's lifecycle status transitioned; payload carries `previous_status`. |

With the default inline (synchronous) replan queue, one signal produces both a
`signal.received` and a `replan.completed` event; filter with
`DDE_WEBHOOK_EVENTS` if you only want one.

### Delivery format

Each delivery is a JSON envelope:

```json
{
  "id": "evt_u7ctmqbm3f6fhdux",
  "type": "replan.completed",
  "created_at": "2026-06-10T19:38:30Z",
  "payload": {
    "goal_id": "goal_ikjtghtpm265ij6n",
    "plan_id": "plan_x2x3hwc2ujadkkqi",
    "signal_id": "sig_4nrqkm66g3qhd3xr",
    "status": "applied",
    "reason": "confidence shifted materially",
    "version": { "plan_id": "plan_x2x3hwc2ujadkkqi", "version": 2, "...": "..." }
  }
}
```

Headers: `Content-Type: application/json`, `X-DDE-Event` (the type),
`X-DDE-Delivery` (the event id), and — when a secret is configured —
`X-DDE-Signature: sha256=<hex>`, the HMAC-SHA256 of the raw request body keyed
with the secret. Verify it by recomputing:

```python
import hashlib, hmac
expected = "sha256=" + hmac.new(secret.encode(), raw_body, hashlib.sha256).hexdigest()
ok = hmac.compare_digest(expected, request.headers["X-DDE-Signature"])
```

### Delivery semantics

Delivery is **best-effort, at-most-once**: events are queued in memory and
POSTed by background workers. Transport errors, `429` and `5xx` responses are
retried with exponential backoff and jitter; other `4xx` responses are treated
as receiver misconfiguration and not retried. If the queue overflows (receiver
down for long) or the process crashes, events are dropped — the store remains
the source of truth, so receivers needing a complete picture should reconcile
via the REST API. On graceful shutdown queued events are drained before exit.
Deliveries are observable via the `dde_webhook_deliveries_total{event,result}`
metric (`success|failure|dropped`) and warn-level logs.

The webhook notifier runs in the long-lived transports (`dde serve`,
`dde mcp`); one-shot CLI commands (`dde evaluate`, `dde signal`) do not emit
events.
