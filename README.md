# Dynamic Decision Engine

**A production-oriented Go engine for AI-assisted decision planning, dynamic replanning, and versioned decision state.**

Dynamic Decision Engine helps systems generate, rank, version, and update structured action plans based on goals, context, constraints, signals, and outcomes.

It is built for use cases where a system should not simply ask an LLM “what should we do?”, but instead maintain a traceable, versioned decision process:

* What goal are we optimizing for?
* What context and constraints were known at the time?
* Which possible moves were considered?
* Why was one action path ranked higher than another?
* What changed later?
* Did the plan get updated?
* What outcome did the previous decision produce?

The project focuses on **decision infrastructure**, not chatbot coaching.

---

## Why this exists

Most LLM applications generate recommendations as one-off text.

That is not enough for serious systems.

Real decision systems need:

* structured outputs
* ranked action paths
* fallback moves
* success signals
* kill / pivot criteria
* immutable plan versions
* decision provenance
* auditability
* outcome tracking
* dynamic replanning when new signals arrive

This project explores how to build that as a clean, scalable backend component.

---

## Core idea

```text
Goal
  + Context
  + Assets
  + Constraints
  + Signals
  + Outcomes
        ↓
Decision Engine
        ↓
Ranked Action Plan
        ↓
Versioned Plan State
        ↓
New Signals / Outcomes
        ↓
Replanning
```

Instead of returning a single `next_best_action`, the engine returns ranked action paths with rationale, experiments, fallbacks, and provenance.

---

## Example output

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
        "success_signals": [
          "5+ qualified replies",
          "2+ discovery calls",
          "1 paid pilot conversation"
        ],
        "kill_criteria": [
          "No qualified replies after 50 targeted contacts"
        ]
      },
      "fallback_moves": [
        "Test SaaS support teams with an AI-assisted refund decision workflow",
        "Reposition around approval-controlled AI agent actions"
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

### 1. Plans are versioned

A plan is not overwritten when new information arrives.

New signals create new immutable plan versions.

```text
plan v1 → signal arrives → plan v2
```

This makes the decision process auditable and explainable.

---

### 2. Replanning is signal-driven

The engine is designed to update recommendations when relevant signals arrive.

Examples:

* a customer replies
* an experiment fails
* a new market signal appears
* a constraint changes
* a metric improves or deteriorates
* a user reports a real-world outcome

---

### 3. LLMs are a boundary, not the architecture

The default planner is deterministic and runs without API keys.

That makes the system:

* testable
* reproducible
* usable offline
* suitable for CI
* safe to run out of the box
* private by default — in the default configuration the engine and all decision
  data stay on your own hardware; nothing leaves the machine, which makes it a
  good fit for a self-hosted home-server deployment (see [Quick start](#quick-start))

Want real AI planning? Bring your own key. Real planners ship behind the same
interface for **Anthropic Claude**, **OpenAI**, and **DeepSeek** — select one with
`DDE_PLANNER=anthropic|openai|deepseek` (BYOK) and the matching API key
(`ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `DEEPSEEK_API_KEY`). Each elicits the
structured plan via a forced tool/function call and records token usage and latency
as provenance. (DeepSeek is served through the OpenAI-compatible adapter.) Note that
enabling a cloud provider sends your inputs to that provider's API; the
deterministic mock remains the default so the system runs fully local, offline, and
in CI with no key.

**Local AI inference is planned.** A self-hosted LLM planner (via an
OpenAI-compatible endpoint — specific provider to be decided) is on the
[roadmap](#roadmap), so AI-assisted planning will eventually run entirely on your
own hardware with no cloud dependency. (The OpenAI-compatible adapter already
accepts a custom endpoint via `DDE_LLM_BASE_URL`.)

**Multi-model planning** (`DDE_PLANNER=multi`) composes models by *role*, each
recorded in provenance for auditability:

* **verify** — one provider proposes, a *different* provider critiques (drops weak
  moves, re-calibrates confidence). Best for calibration + auditability.
* **route** — a cheap model handles the common case; escalates to a stronger model
  on low confidence or when a material signal arrives. A cost lever.
* **ensemble** — several providers run in parallel; agreement on the top move
  scales its confidence (divergence lowers it). An uncertainty signal.

```bash
# verify: Claude proposes, GPT reviews
DDE_PLANNER=multi DDE_MULTI_MODE=verify DDE_MULTI_PROVIDERS=anthropic,openai dde serve
# keyless offline demo (ensemble of two mock planners)
DDE_PLANNER=multi DDE_MULTI_MODE=ensemble DDE_MULTI_PROVIDERS=mock,mock \
  dde evaluate --input examples/founder-growth.json
```

---

### 4. Built as infrastructure

The project is designed as a backend system, not a prompt collection.

Implemented architecture principles:

* Go-based core
* clean domain model
* context propagation
* structured logging
* request IDs
* immutable plan versions
* decision provenance
* transactional persistence
* Postgres-backed horizontal scalability
* in-memory mode for tests and local development
* deterministic planner for reproducible tests
* REST API and CLI interface

---

## Core concepts

### Player

The person, team, company, or system pursuing a goal.

### Goal

The objective the system is planning around.

Examples:

* find first paying customers
* choose a technical architecture
* reduce churn
* evaluate a market entry path
* improve career leverage
* prioritize product experiments

### Context

The current known situation: facts, assets, constraints, assumptions, and available options.

### Move

A possible strategic action.

Moves are ranked by expected impact, effort, risk, confidence, and rationale.

### Experiment

A concrete test attached to a move.

Each experiment can define:

* duration
* success signals
* kill criteria
* expected learning

### Signal

A new piece of information that may change the plan.

### Outcome

A real-world result produced by an executed move or experiment.

### Plan Version

An immutable snapshot of a recommendation at a point in time.

### Provenance

Metadata explaining how a plan was generated, from which input snapshot, with which planner, prompt version, and model.

---

## Example use cases

Dynamic Decision Engine is domain-agnostic.

Possible use cases include:

* AI-assisted strategic planning
* founder growth experiments
* product direction decisions
* career path planning
* sales and go-to-market planning
* operational decision support
* AI agent action planning
* policy-constrained recommendation systems
* experiment selection and replanning
* decision audit trails for AI-assisted systems

The core engine is intentionally generic. Domains can be added as separate packages, templates, or adapters.

---

## Quick start

Requirements: Go 1.25+. No database or API keys required — the default store is in-memory and the default planner is deterministic.

### Run a local evaluation

```bash
go run ./cmd/dde evaluate --input examples/founder-growth.json
```

### Start the API

```bash
go run ./cmd/dde serve
```

### Run tests

```bash
go test ./... -race
```

### Run with Postgres

```bash
docker compose up -d postgres

DATABASE_URL=postgres://dde:dde@localhost:5432/dde?sslmode=disable \
  go run ./cmd/dde serve
```

### Run the full stack (API + admin UI + observability)

This is also the one-command **self-hosting** path: anyone can run the whole stack
on a home server, NAS, or mini-PC that has Docker. In the default configuration
all goals, plans, versions, and history stay on that machine — your data never
leaves your hardware.

```bash
docker compose up --build
```

| Service | URL | Notes |
| --- | --- | --- |
| API | http://localhost:8080 | REST API (`/health`, `/metrics`) |
| Admin UI | http://localhost:3000 | Next.js console: goals, plans, versions, provenance |
| Prometheus | http://localhost:9090 | Scrapes the API's `/metrics` |
| Grafana | http://localhost:3001 | Provisioned dashboard (admin / admin) |
| Postgres | localhost:5432 | `dde` / `dde` |

---

## CLI

```bash
dde evaluate --input examples/founder-growth.json
dde signal --input examples/signal-update.json
dde migrate
dde serve
dde version
```

---

## API overview

> 📖 **Full API reference — request/response payloads for every endpoint:** [`docs/api.md`](docs/api.md)

### Stateless evaluation

```http
POST /v1/evaluate
```

Generates a ranked action plan without persisting state.

### Create a goal

```http
POST /v1/goals
```

### Generate an initial plan

```http
POST /v1/goals/{id}/plans
```

### Submit a signal

```http
POST /v1/signals
```

Stores a signal and triggers replanning if the signal is material.

### List plan versions

```http
GET /v1/plans/{id}/versions
```

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

Active development.

The initial core is implemented and tested:

* deterministic planner
* domain model
* CLI
* REST API
* immutable plan versions
* signal-driven replanning
* provenance
* outcome tracking
* table-driven tests with `-race`
* Postgres persistence with ordered migrations
* Prometheus metrics + Grafana dashboards
* a minimal Next.js admin UI

More LLM providers, local / self-hosted inference, authentication, richer scoring, OpenTelemetry, and deeper admin workflows are planned next.

---

## Roadmap

* [x] Core domain model
* [x] Deterministic mock planner
* [x] CLI evaluation flow
* [x] In-memory repository
* [x] Postgres repository
* [x] Ordered migrations
* [x] Immutable plan versions
* [x] Signal-driven replanning
* [x] Outcome tracking
* [x] REST API
* [x] Table-driven tests
* [x] GitHub Actions CI
* [x] Prometheus metrics + Grafana dashboards
* [x] Minimal admin UI
* [x] Application/use-case layer with concurrency-safe replanning
* [x] Pluggable LLM planners: Anthropic Claude, OpenAI, and DeepSeek (structured output via tool/function calls)
* [x] Multi-model strategies: cross-model verify, cost routing, agreement ensemble (provenance-tracked)
* [ ] Local / self-hosted LLM inference (OpenAI-compatible endpoint, provider TBD)
* [ ] Policy constraints
* [ ] Multi-objective scoring
* [ ] OpenTelemetry tracing
* [ ] Authentication / authorization
* [ ] Domain packs and examples

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
