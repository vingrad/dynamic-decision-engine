# MCP server

The engine exposes its use-cases as [Model Context Protocol](https://modelcontextprotocol.io)
tools, so MCP-capable agents (Claude Code, Claude Desktop, custom agent
runtimes) can drive the full decision loop — create goals, generate ranked
plans, report signals, record outcomes and inspect the immutable version
history — with zero integration code. The MCP layer is a thin transport adapter
over the same application service the REST API uses: identical semantics,
validation and error mapping.

## Transports

### stdio: `dde mcp`

The zero-config path for local agents. The command assembles the same
production service as `dde serve` — same `DDE_*` environment configuration,
in-memory store by default, Postgres via `DATABASE_URL`, deterministic mock
planner by default (BYOK for a real one). Logs go to stderr; stdout carries
JSON-RPC only.

Example client configuration (Claude Code/Desktop `mcpServers` entry):

```json
{
  "mcpServers": {
    "dde": {
      "command": "dde",
      "args": ["mcp"],
      "env": { "DDE_PLANNER": "mock" }
    }
  }
}
```

With no `DATABASE_URL` each session gets a fresh in-memory store; point it at
Postgres to share durable state with a running `dde serve`.

### Streamable HTTP: `/mcp` on the API server

`dde serve` mounts an MCP streamable-HTTP endpoint at `http://localhost:8080/mcp`,
sharing the live service — goals created over REST are visible to MCP tools and
vice versa. Like the rest of the API, `/mcp` is currently **unauthenticated**;
do not expose it beyond a trusted network until authentication lands.

Try either transport with the MCP inspector:

```bash
npx @modelcontextprotocol/inspector ./dde mcp     # stdio
npx @modelcontextprotocol/inspector http://localhost:8080/mcp  # HTTP (serve running)
```

## Tools

| Tool | Arguments | Returns | REST equivalent |
| --- | --- | --- | --- |
| `evaluate` | `objective` (req), `domain`, `metric`, `target`, `context`, `signal_note` | plan version (not persisted) | `POST /v1/evaluate` |
| `create_goal` | `objective` (req), `player_id`, `domain`, `metric`, `target`, `context` | goal | `POST /v1/goals` |
| `get_goal` | `goal_id` (req) | goal | `GET /v1/goals/{id}` |
| `list_goals` | `status`, `limit`, `offset` | `{goals: [...]}` | `GET /v1/goals` |
| `update_goal_status` | `goal_id`, `status` (req), `resolution_result`, `resolution_notes` | goal | `PATCH /v1/goals/{id}/status` |
| `generate_plan` | `goal_id` (req) | plan version 1 | `POST /v1/goals/{id}/plans` |
| `get_plan` | exactly one of `plan_id` \| `goal_id` | `{plan, current_version}` | `GET /v1/plans/{id}` |
| `list_plan_versions` | `plan_id` (req), `limit`, `offset` | `{versions: [...]}` | `GET /v1/plans/{id}/versions` |
| `submit_signal` | `goal_id`, `kind` (req), `description`, `payload` | `{signal, status, material, reason, plan_version}` | `POST /v1/signals` |
| `record_outcome` | `goal_id`, `plan_version`, `move_rank`, `result` (req), `observed_signals`, `notes` | outcome | `POST /v1/outcomes` |

The typical agent loop: `create_goal` → `generate_plan` → act → `submit_signal`
when something changes (a material signal appends a new immutable version) →
`record_outcome` for executed moves → `update_goal_status` when the goal
concludes.

## Errors

Service errors surface as MCP tool errors (`isError: true`) with messages
mirroring the REST status mapping: `invalid input: …` (400), `not found` (404)
and `conflict: …` (409, e.g. a second `generate_plan` for the same goal, or a
signal for a goal with no plan). Internal failures return `internal error`
without detail.

## Notes

- Webhooks (see [`api.md`](api.md#webhooks)) work in both transports: `dde mcp`
  is a long-running process and emits events like `serve`.
- In stdio mode metrics are recorded but not exported (no HTTP listener).
- Provenance survives the transport: plans generated through MCP carry the same
  planner/model/snapshot provenance as REST-generated ones.
