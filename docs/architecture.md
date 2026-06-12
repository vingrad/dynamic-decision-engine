# Architecture

The system is organised in clean layers with a strict dependency direction.
Domain logic does not depend on transport or persistence; the LLM is a pluggable
boundary.

```
            ┌──────────────────────────────────────────────┐
            │                  cmd/dde (CLI)                 │
            │   serve · migrate · evaluate · signal · ver.   │
            └───────────────┬───────────────┬───────────────┘
                            │               │
                  ┌─────────▼──────┐  ┌──────▼─────────┐
                  │  internal/api  │  │ internal/engine│
                  │  (chi, REST)   │─▶│  planner /     │
                  │  handlers/dto  │  │  evaluator /   │
                  │  middleware    │  │  replanner     │
                  └───────┬────────┘  └──────┬─────────┘
                          │                  │
              ┌───────────▼─────┐   ┌────────▼─────────┐
              │ internal/storage│   │   internal/llm   │
              │ Repository:     │   │  Planner iface:  │
              │  memory │ pgx   │   │  mock │ openai*   │
              └───────────┬─────┘   └──────────────────┘
                          │
                  ┌───────▼────────┐
                  │ internal/domain│  (pure types + invariants)
                  └────────────────┘

  * openai is a placeholder implementing the interface; not wired to a real API.
```

## Layers

- **`internal/domain`** — pure data types and invariants (`Goal`, `Plan`,
  `PlanVersion`, `RankedMove`, `Signal`, `Outcome`, `DecisionProvenance`, …). No
  I/O. The JSON tags define the public contract.
- **`internal/llm`** — the reasoning boundary. `Planner` is the interface; the
  default `MockPlanner` is deterministic; `OpenAIPlanner` is a placeholder.
- **`internal/engine`** — orchestration: generate an initial plan, evaluate
  materiality of a signal, and replan. Pure logic; knows nothing about HTTP or
  storage. The `Evaluator` encodes the materiality policy.
- **`internal/storage`** — the persistence boundary. `Repository` has two
  implementations: an in-memory store (default, zero-infra) and a PostgreSQL
  store (`pgx`). Plan versions are append-only everywhere.
- **`internal/api`** — transport only: chi router, middleware (request IDs,
  structured logging, recovery, timeout, CORS, Prometheus), handlers, DTOs and a
  consistent RFC 7807 error envelope.
- **`internal/config`, `internal/logging`** — configuration (defaults → file →
  env) and the `slog` logger.

## Replanning loop

```
 new Signal ──▶ load Goal + current PlanVersion
            ──▶ engine.Replan (fold signal note into planner input)
            ──▶ Evaluator.IsMaterial(current, candidate)?
                  ├─ no  ──▶ keep current version (audit the decision)
                  └─ yes ──▶ append immutable PlanVersion N+1 (atomic)
```

The write that appends a version and advances `current_version` is atomic: a
single locked section in the memory store and a single transaction in Postgres.

## Strategy selection

Any domain pack may declare named **strategies** — competing lenses on the
same goal. The machinery is domain-neutral and assembled from data:

```
 pack.Descriptor.Strategies ──▶ wire.buildStrategySelector
      │                              │
      │   PlannerKind kit?           ├─ kit children (finance: parameter lenses
      │   (StrategyKit registry)     │   + market-regime classifier with a
      │                              │   closed label set)
      │   no kit (text domain)       ├─ prompt-variant children
      │                              │   Guided(Caching(base), pack + lens template)
      │                              ▼
      │                        llm.SelectorPlanner
      │                           1. classify context (regime) → gate lenses
      │                           2. run eligible children in parallel
      │                           3. optional comparator review (verify)
      │                           4. strategy.Select: filter → utility → tie-break
      │                              (+ incumbent hysteresis, disagreement penalty
      │                               quantized to the domain's ConfidenceDelta)
      │                           5. winner + full competition into provenance
      └─ SelectionDefaultOn: the default is PACK data and must be earned
         (investing: on, via backtest gates; growth/career: off, policy opt-in)
```

Key rules: a kind domain whose data dependency is missing declines to its
plain fallback (never into mis-parameterized text children); invalid strategy
configuration (unknown regime labels, duplicate/empty prompt templates,
strategies on the generic domain) fails the build instead of degrading;
outcome weights (`dde strategy-fit`) are fitted per strategy and regime and
only from decisions under the same comparator.

## Scalability posture

- **Stateless API nodes.** All state lives in Postgres, so the API scales
  horizontally behind a load balancer.
- **Bounded work.** List endpoints are paginated; request bodies are size-capped;
  requests have a configurable timeout.
- **Operational visibility.** Liveness/readiness endpoints, Prometheus metrics
  (request rate/latency/errors, plan-version and replan counters), structured
  logs with request IDs.
- **Graceful shutdown.** In-flight requests drain on SIGINT/SIGTERM.

## Monitoring topology

```
  admin UI (Next.js, :3000)  ──REST──▶  api (:8080) ──▶ postgres
                                            │ /metrics
                                            ▼
                                     prometheus (:9090) ──▶ grafana (:3001)
```

The admin UI handles **domain** inspection (goals, plans, versions, provenance);
Prometheus + Grafana handle **operational** monitoring. The two are deliberately
separate.
