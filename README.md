# Dynamic Decision Engine

**A Go engine that turns AI recommendations into versioned, auditable, self-updating decisions.**

Ask an LLM "what should we do?" and you get a paragraph of advice that is
stale the moment the world moves, and forgotten the moment the tab closes.
This engine keeps the *decision* instead: what was known at the time, which
options were considered, why #1 beat #2, what changed later, and whether it
actually worked.

---

## See it in 30 seconds

Requirements: Go 1.25+. No database, no API keys, fully offline.

```bash
go run ./cmd/dde demo
```

One command walks the entire decision loop on a built-in scenario and
narrates each step:

```text
[1/5] GOAL — the objective and what is known at decision time
      "Grow the product to 1,000 paying customers within two quarters"
      assets:      founder network in target vertical · high net revenue retention
      constraints: limited engineering capacity · 12 months runway

[2/5] PLAN v1 — ranked moves, not a paragraph of advice
      #1 Double down on founder network in target vertical
         confidence 0.76 · impact high · effort medium · risk low
      ...
      and a kill criterion a machine can check: "No movement in paying
      customers after the full window"

[3/5] SIGNAL — the world changed
      competitive_shift: A well-funded competitor just launched a free tier...
      material: true — confidence on the top move shifted materially

[4/5] PLAN v2 — appended, v1 is kept, the diff is explainable
      #1 Double down on founder network...  (confidence 0.76 → 0.61)
      provenance: planner=mock snapshot=snap_... (reproducible)

[5/5] OUTCOME — reality is recorded against the exact move acted on
      (plan v1, move #1) "Double down on founder network..." → failure
```

That loop — **goal → plan v1 → signal → materiality decision → plan v2 →
outcome** — is the whole product. Everything else in this README builds on it.

Variants:

```bash
go run ./cmd/dde demo investing      # a thesis break drops a sized position's confidence 0.75 → 0.00
go run ./cmd/dde evaluate "find the first 10 paying customers in 3 months"   # your own goal, plain text
DDE_PLANNER=anthropic go run ./cmd/dde demo   # the same loop with a real LLM (BYOK)
```

---

## The four concepts that matter

| Concept | What it is |
| --- | --- |
| **Goal** | The objective plus what is known at decision time: situation, facts, assets, constraints. The durable identity everything else keys off. |
| **Plan version** | An immutable, ranked set of moves — each with confidence, impact/effort/risk, rationale, and an experiment with success signals and kill criteria. Never overwritten: v1 → v2 → … |
| **Signal** | New information pushed in. The engine judges whether it is *material*; if yes, it replans into a new version, if no, the reason is recorded. |
| **Outcome** | A real-world result recorded against a move's stable address `(plan_version, move_rank)`, so confidence can be tested against reality. |

Everything else — players, experiments, provenance, domains, strategies,
data sources — hangs off these four. Full vocabulary and invariants:
[`docs/concepts.md`](docs/concepts.md).

```mermaid
flowchart TD
    G["🎯 Goal<br/>context · assets · constraints"] --> E["Decision Engine"]
    E --> P["Ranked action plan<br/>moves · experiments · fallbacks · provenance"]
    P --> V[("Immutable plan versions<br/>v1 → v2 → …")]
    S["📡 Signal<br/>new information"] --> M{"material?"}
    M -- "yes — replan" --> E
    M -- "no — version stands, audited" --> V
    O["✅ Outcome<br/>recorded result"] --> V
    V -. "world changes" .-> S
```

---

## Why this exists

Most LLM applications treat a recommendation as the end of the conversation.
That breaks down the moment a decision matters:

* **No accountability.** When the plan turns out wrong, there is no record of
  what was known at decision time, which options were considered, or why one
  was ranked above another.
* **No reaction to change.** The world moves — a customer replies, an
  experiment fails, a price drops — but the one-off answer doesn't, and stale
  advice keeps getting followed.
* **No learning.** Outcomes are never recorded against the recommendation that
  produced them, so confidence is never tested against reality.

A real decision system treats a recommendation as a versioned, auditable
object — and replans when a material signal arrives. This project builds
exactly that, as a clean, production-oriented backend component. If you're
building agents, decision-support tools, or anything where *"why did it
recommend that?"* must have an answer, this is the missing layer.

---

## Example output

Every plan version is structured data, not prose:

```json
{
  "plan_id": "plan_7K2M9Q",
  "version": 1,
  "summary": "Test a narrow market segment before expanding positioning.",
  "ranked_moves": [
    {
      "rank": 1,
      "title": "Test AI automation agencies as the first segment",
      "confidence": 0.82,
      "expected_impact": "high",
      "effort": "medium",
      "risk": "low",
      "rationale": "This segment already understands AI agents, has short feedback cycles, and can validate the decision-infrastructure use case quickly.",
      "experiment": {
        "title": "Contact 30 AI automation agency founders in 7 days",
        "duration_days": 7,
        "success_signals": ["5+ qualified replies", "2+ discovery calls"],
        "kill_criteria": ["No qualified replies after 50 targeted contacts"]
      },
      "fallback_moves": [
        "Test SaaS support teams with an AI-assisted refund decision workflow"
      ]
    }
  ],
  "provenance": {
    "planner": "mock",
    "model": "deterministic",
    "prompt_version": "mock-v1",
    "input_snapshot_id": "snap_F3A91P",
    "reasoning_summary": "The top move was selected because it maximizes learning speed while keeping execution cost low."
  }
}
```

---

## What makes it different

### Plans are versioned, replanning is signal-driven

A plan is never overwritten. New material signals create new immutable plan
versions (`v1 → v2`); immaterial signals are recorded with the reason no new
version was created. Per-domain materiality thresholds decide what justifies
a new version, so consumers see meaningful updates only.

### LLMs are a boundary, not the architecture

The default planner is deterministic and runs without API keys — testable,
reproducible, offline, CI-friendly, private by default. Real planners ship
behind the same interface for **Anthropic Claude**, **OpenAI** and
**DeepSeek** (BYOK), and **multi-model** composition adds cross-provider
verify / cost routing / agreement ensembles, all recorded in provenance.

> 📖 Providers, env switches, multi-model modes: [`docs/planners.md`](docs/planners.md)

### Decisions can pull live data

The engine can fetch fresh state (prices, availability, balances, research)
right before planning and fold it into the goal context. Sources sit behind
one mechanism-agnostic interface — HTTP/REST, MCP servers, AI agents,
in-process read-models — every source consulted lands in the input snapshot
and provenance, a source outage degrades instead of blocking, and the whole
mechanism is off by default so the offline path stays byte-for-byte
reproducible.

The same BYOK posture applies to **market data** for the investing domain:
offline fixtures by default, real vendors (Financial Modeling Prep + keyless
Stooq fallback) one env switch away, behind a TTL cache and rate limiting.
Backtests always run on offline fixtures so they stay reproducible and
lookahead-free.

> 📖 Sources, market data, vendor chains: [`docs/domains.md`](docs/domains.md)

### Domain packs

Pure-data descriptors supply a domain's vocabulary, prompt guidance, scoring
and validation. Four ship today — `generic`, `investing` (numeric
deterministic scorer over point-in-time market data), `growth`, `career` —
and new domains load from a config file, no code.

### A measurable learning loop

Outcomes are recorded against the exact move acted on; `dde calibrate` fits
stated confidence to observed reality, and the backtest harness scores
decision quality (Brier score, kill precision/recall, noise robustness)
before a scoring change ships.

### Built as infrastructure

Go core, clean domain model, context propagation, structured logging, request
IDs, transactional persistence, Postgres-backed horizontal scalability,
in-memory mode for tests and local development, REST API + CLI + MCP.

---

## Use cases

### AI agents that act

An agent needs more than a chat reply — it needs a plan it can execute
mechanically, abort safely, and answer for later. The engine gives it an MCP
server built in (`dde mcp` or `/mcp`), machine-actionable moves (confidence,
explicit dependency DAG, fallbacks, kill criteria it can check mechanically),
signal-driven replanning instead of re-prompting from scratch, and webhooks
(`replan.completed`, `outcome.recorded`, …) to trigger downstream automation.

### Decision-support products

A product that recommends actions to humans — growth experiments, operations,
investing, sales — must explain its ranking, stay current as data changes,
and not spam users with churn. The engine provides ranked alternatives with
rationale (so the UI can show *why #1 beat #2*), live context at decision
time, materiality thresholds, and a measurable learning loop.

### Anything that must answer "why did it recommend that?"

After the fact — an incident review, a compliance question, a postmortem —
you must reconstruct what was known, what was considered, and why one path
ranked above another. Input snapshots, full provenance (planner, model,
prompt version, token usage, every contributor and data source), immutable
history, and stable outcome addressing make that reconstruction mechanical.

---

## Quick start

### Run it (no database, no keys)

```bash
go run ./cmd/dde demo                     # the full loop, narrated
go run ./cmd/dde demo investing           # same loop, numeric finance planner
go run ./cmd/dde evaluate "your goal in one sentence"
go run ./cmd/dde evaluate --input examples/founder-growth.json   # structured goal+context
go run ./cmd/dde serve                    # REST API + streamable HTTP MCP
go test ./... -race
```

### Run with Postgres

```bash
docker compose -f docker-compose.dev.yml up -d postgres

DATABASE_URL=postgres://dde:dde@localhost:5432/dde?sslmode=disable \
  go run ./cmd/dde serve
```

### Run the full stack (API + admin UI + observability)

This is also the **self-hosting** path: anyone can run the whole stack
on a home server, NAS, or mini-PC that has Docker. In the default configuration
all goals, plans, versions, and history stay on that machine — your data never
leaves your hardware.

```bash
docker compose -f docker-compose.dev.yml up --build
```

A [Caddy](https://caddyserver.com/) reverse proxy fronts the stack as a single
origin. **Open the app at http://localhost** — the browser talks to the API on
the same origin (relative `/v1/*`), routed by Caddy. The individual service ports
below are also published locally for debugging, but open the UI via
**http://localhost** (not `:3000`): browser write actions issue same-origin
requests and only reach the API through the proxy.

| Service | URL | Notes |
| --- | --- | --- |
| **App (use this)** | **http://localhost** | Admin UI + API behind one origin (Caddy) |
| API | http://localhost:8080 | REST API direct (`/health`, `/metrics`) — for curl/debugging |
| Admin UI | http://localhost:3000 | Next.js server direct — SSR/debug only; browser writes need the proxy |
| Prometheus | http://localhost:9090 | Scrapes the API's `/metrics` |
| Grafana | http://localhost/grafana | Provisioned dashboard (admin / admin), via the proxy |
| Postgres | localhost:5432 | `dde` / `dde` |

### Publish a public demo (Hetzner VPS, BYOK)

A separate compose file runs the stack as a public, HTTPS demo where visitors
bring their own LLM key. See **[docs/deploy.md](docs/deploy.md)** for the full
guide. In short:

```bash
cp .env.prod.example .env.prod   # set DDE_DOMAIN + strong passwords
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build
```

---

## CLI

```bash
dde demo [growth|investing]         # walk the full decision loop, narrated
dde evaluate "goal in plain text"   # ranked plan from a one-sentence goal
dde evaluate --input goal.json      # ranked plan from structured goal+context
dde signal --input signal.json      # apply a signal, see the replanning decision
dde backtest --input scenario.json  # replay a signal timeline, score decision quality
dde calibrate                       # fit confidence to recorded outcomes
dde migrate                         # apply database migrations
dde serve                           # REST API (+ /mcp)
dde mcp                             # MCP server over stdio
dde version
```

---

## API overview

> 📖 **Full API reference — request/response payloads for every endpoint:** [`docs/api.md`](docs/api.md)

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/evaluate` | Stateless: generate a ranked plan without persisting |
| `POST /v1/goals` | Create a goal |
| `POST /v1/goals/{id}/plans` | Generate the initial plan |
| `POST /v1/signals` | Submit a signal; replans if material |
| `GET /v1/plans/{id}/versions` | List immutable plan versions |
| `POST /v1/outcomes` | Record a result against `(plan_version, move_rank)` |

---

## MCP server

The engine is also an **MCP server**: every use-case above is exposed as a
Model Context Protocol tool, so MCP-capable agents (Claude Code, Claude
Desktop, custom runtimes) can create goals, generate ranked plans, submit
signals, record outcomes and audit the version history with zero integration
code.

```bash
dde mcp        # stdio — point Claude Code/Desktop at {"command": "dde", "args": ["mcp"]}
dde serve      # also mounts streamable HTTP MCP at http://localhost:8080/mcp
```

> 📖 **Tool reference, transports and agent-loop guidance:** [`docs/mcp.md`](docs/mcp.md)

---

## Webhooks

The engine can push domain events (`goal.created`, `plan.created`,
`signal.received`, `replan.completed`, `outcome.recorded`,
`goal.status_changed`) to an HTTP endpoint as they happen — Slack bridges,
n8n/Zapier flows and agent triggers react to decisions without polling.
Off by default; enable with:

```bash
DDE_WEBHOOK_URL=https://example.com/hook \
DDE_WEBHOOK_SECRET=s3cret \
dde serve
```

Deliveries are best-effort with retries and an HMAC-SHA256 signature header
(`X-DDE-Signature`) for receiver-side verification.

> 📖 **Event types, envelope format, signature verification and delivery semantics:** [`docs/api.md`](docs/api.md#webhooks)

---

## Architecture

```text
cmd/dde
  ↓
internal/api
  ↓
internal/engine
  ↓
internal/llm
  ↓
internal/storage
  ↓
internal/domain
```

The domain layer is pure and does not depend on transport, storage, or LLM providers.

The LLM/planner layer is a replaceable boundary.

Storage supports both in-memory mode and Postgres.

For the full layering, replanning loop, and monitoring topology, see [`docs/architecture.md`](docs/architecture.md) and [`docs/concepts.md`](docs/concepts.md).

---

## Project status

Active development. Implemented and tested: the full decision loop
(deterministic planner, immutable versions, signal-driven replanning,
provenance, outcome tracking), CLI + REST API + MCP server (stdio and
streamable HTTP), BYOK planners (Anthropic, OpenAI, DeepSeek) and multi-model
strategies, domain packs (generic, investing, growth, career) with the
investing numeric scorer, point-in-time market data with real vendors behind
BYOK, backtesting and confidence calibration, external data sources,
webhooks, Postgres persistence with ordered migrations, Prometheus metrics +
Grafana dashboards, a minimal Next.js admin UI, table-driven tests with
`-race`, GitHub Actions CI.

Planned next: local / self-hosted LLM inference (OpenAI-compatible endpoint),
OpenTelemetry tracing, authentication / authorization.

---

## Design principles

* Deterministic by default
* LLMs behind interfaces
* No hidden state
* Immutable decision history
* Explicit provenance
* Structured outputs over free-form text
* Small core, extensible edges
* Production-oriented Go code
* Testable without external services

---

## Disclaimer

This software is provided "as is", without warranty of any kind. The Dynamic Decision
Engine generates plans, rankings, and recommendations — including output produced by
large language models — that may be incomplete, biased, or incorrect. It is a
decision-support tool, not a substitute for human judgment.

You are solely responsible for any decision made or action taken based on its output.
Use it at your own risk. The authors and contributors accept no liability for any loss
or damage arising from its use. See the [LICENSE](LICENSE) for the full warranty and
liability terms.

## License

GNU Affero General Public License v3.0 (AGPL-3.0) — see [`LICENSE`](LICENSE).

The AGPL is chosen deliberately: if you run a modified version of this engine as
a network service, you must make your modified source available to its users.
