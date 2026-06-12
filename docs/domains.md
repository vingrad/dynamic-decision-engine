# Domains & Domain Packs

The engine is multi-domain: every goal carries a `domain`, and a **domain pack**
decides how that goal is reasoned about — its prompt guidance, its materiality
threshold, its scoring tunables, its vocabulary and its validation.

Adding a domain is a single descriptor plus one registration; no other code
changes. This is possible because packs are **pure data** (`internal/pack`) and a
composition layer (`internal/wire`) turns them into the running collaborators:

- an `llm.PlannerRouter` that dispatches to a per-domain planner, and
- an `engine.EvaluatorResolver` that selects the per-domain materiality policy.

```
goal.Domain ─► PlannerRouter ─► Guided(Caching(base))  | Caching(FinancePlanner)
            └► EvaluatorResolver ─► ThresholdEvaluator{ConfidenceDelta}
```

## Shipped packs

| Domain      | Materiality Δ | Planner                       | Notes |
|-------------|---------------|-------------------------------|-------|
| `generic`   | 0.10          | base model (mock/anthropic)   | Default. Empty prompt template → identical to pre-multi-domain behaviour. |
| `investing` | 0.05          | numeric **finance** planner   | Thesis framing, calibrated conviction, **not financial advice** disclaimer. |
| `growth`    | 0.10          | base model + growth guidance  | Growth experiments / funnel metrics. |
| `career`    | 0.10          | base model + career guidance  | Positioning bets, bandwidth-aware. |

The canonical stored value for the default domain is the empty string; both `""`
and `"generic"` resolve to the generic pack, and generic goals serialise/hash
exactly as before.

## Selecting a domain

Set `domain` when creating a goal or evaluating:

```bash
dde evaluate --input examples/investing-thesis.json   # "domain": "investing"
```

```http
POST /v1/goals     { "domain": "investing", "objective": "...", "context": {...} }
POST /v1/evaluate  { "domain": "investing", "objective": "...", "context": {...} }
```

Unknown domains are rejected with the list of valid domains. Each pack also runs
soft validation (e.g. investing warns when no risk/horizon context is given);
warnings are logged, they do not block.

## The investing domain

- Moves are framed as falsifiable theses with entry/exit and thesis-invalidation
  kill criteria.
- The optional **numeric finance planner** scores candidate theses (tickers given
  as context assets with `kind: "ticker"`) on expected value, risk
  (volatility/drawdown), liquidity and horizon fit, and suggests an illustrative
  fractional-Kelly position size bounded by a risk budget.
- **Honesty:** confidence is a transparent transform of the composite score, **not**
  a market probability; sizing is illustrative; this is decision support, not a
  trading system. The plan summary always carries: *"Educational decision-support
  only. Not financial advice. Not a recommendation to buy or sell any security."*

### Structured signals

Investing signals carry typed payloads in `signal.payload`, parsed by
`internal/finance`:

| `kind`             | payload fields |
|--------------------|----------------|
| `price_move`       | `pct_change, from_price, to_price, window_days` |
| `earnings`         | `eps_actual, eps_estimate, surprise` |
| `macro`            | `indicator, value, prior` |
| `valuation_change` | `metric, value, fair_value, gap_pct` |
| `thesis_break`     | `reason, broken_level, hard` |

A `thesis_break` for a held ticker zeroes that thesis's confidence, which the
standard materiality evaluator picks up as a material change → a new plan version.

## Market data (point-in-time)

The finance planner reads market data through `internal/marketdata.Provider`.
Every read takes an `asOf` timestamp and returns only data known at or before it —
so backtests cannot peek at the future. The default `OfflineProvider` serves
embedded JSON fixtures (no network, CI-safe). Select with
`DDE_MARKETDATA_PROVIDER=offline|http`.

### Real vendors (BYOK)

`DDE_MARKETDATA_PROVIDER=http` enables real vendors. `DDE_MARKETDATA_VENDOR` is a
comma-separated **chain** tried in order per read — a vendor that can't serve a
request (capability gap, missing ticker, rate limit, outage) falls through to the
next, and the composed chain name (`fmp+stooq`) lands in provenance:

```bash
DDE_MARKETDATA_PROVIDER=http \
DDE_MARKETDATA_VENDOR=fmp,stooq \
DDE_MARKETDATA_API_KEY=your-fmp-key \
dde serve
```

| Vendor | Quotes | Fundamentals | Daily bars | Key | Free tier |
| --- | --- | --- | --- | --- | --- |
| `fmp` (Financial Modeling Prep) | ✅ | ✅ latest-only | ✅ | `DDE_MARKETDATA_API_KEY` | ~250 req/day, US tickers, non-commercial |
| `stooq` | ✅ (bar-derived) | — | ✅ | none | keyless EOD CSV; engine paces 1 req/s; their anti-bot check may block non-browser clients (the chain then degrades) |
| `alphavantage`, `tiingo` | placeholders (selectable, return not-implemented) | | | | |

**Bring your own paid key for production use.** Free tiers are for development
and demos and are typically restricted to non-commercial use by the vendor's
terms. A vendor that needs a key but has none fails at startup (no silent
degradation); use `DDE_MARKETDATA_VENDOR=stooq` for a fully keyless setup.

All vendor reads go through an in-process TTL cache, so the planner's repeated
reads don't burn the daily request budget: quotes default to 15m
(`DDE_MARKETDATA_QUOTE_TTL`), fundamentals to 24h
(`DDE_MARKETDATA_FUNDAMENTALS_TTL`); a TTL of 0 disables that cache. Bar ranges
are split at the start of *yesterday*: the older chunk is immutable history
cached without expiry (with that one-day grace, a vendor that backfills EOD
bars late can't freeze an incomplete series), while the yesterday/today stub
expires on `DDE_MARKETDATA_BARS_TTL` — so refreshing a year-long window
refetches two days, not the year. FMP is additionally paced (250ms between
requests, 240/day budget; cancelled waits are refunded) so a hot loop can't
exhaust the key.

**Point-in-time honesty.** Daily bars satisfy the as-of contract everywhere, and
a past-`asOf` quote is derived from bars (never the live quote endpoint). But
free vendors have no *historical* fundamentals: a live read returns the current
snapshot tagged `"freshness": "latest_only"`, and a past-`asOf` read is refused
(`ErrNotSupported`) rather than served with today's values. The planner already
tolerates missing fundamentals, so nothing breaks — but this is why **backtests
must stay on the offline fixtures** (they do by default: the backtest harness
wires `OfflineProvider` directly and never touches HTTP vendors).

Adding a vendor is one adapter file implementing `Provider` plus one line in the
registry (`internal/marketdata/vendors.go`); the conformance test suite in
`conformance_test.go` runs the shared point-in-time contract against every
implementation.

## External data sources (live context)

A domain can pull **fresh state** (prices, availability, balances, research) into the
goal context right before planning, so the planner reasons over the current world —
not just what was baked into the goal at creation. This is opt-in and off by default.

```
goal.Domain ─► SourceResolver ─► Enricher ─► Source.Fetch ─► ContextDelta
            (folded into goal.Context BEFORE planning → captured by the snapshot)
```

A `Source` is mechanism-agnostic: HTTP/REST, an MCP server, an autonomous AI agent,
or an in-process read-model all implement the same `internal/source.Source`
interface. Non-deterministic sources keep their non-determinism sealed inside
`Fetch`; the engine sees one result and records it (raw payload, fetch time,
identity) in plan provenance under `source_contributions`.

**Determinism is preserved.** Fetched data lands in `Goal.Context`, which is part of
the input snapshot hash — so the same world-state yields the same `input_snapshot_id`
and a different world-state yields a different one. Audit-only fields (raw payload,
timestamp) never enter the hash. With sources disabled the offline/mock path is
byte-for-byte unchanged.

**Failure never blocks a decision.** Each `Fetch` runs under `DDE_SOURCE_TIMEOUT`
with panic recovery; a timeout/error/outage is recorded as a `stale` contribution
(optionally serving a last-good cached value) and the plan is still produced.

### Wiring sources

1. Enable enrichment and point at a sources file:
   ```bash
   DDE_SOURCES_ENABLED=true DDE_SOURCES=examples/sources.yaml DDE_SOURCE_TIMEOUT=5s
   ```
2. Declare which sources a domain consults via `source_kinds` (entries are source
   names from the sources file), e.g. in `examples/domains.yaml`:
   ```yaml
   - id: purchasing
     name: Purchasing
     source_kinds: [pricefeed]
     # ...
   ```
3. Define the sources (`examples/sources.yaml`). An `http` source GETs its endpoint
   (with `goal_id`/`signal_kind` query params) and expects a context-delta JSON body
   (`{"facts": [...], "assets": [...], "constraints": [...]}`):
   ```yaml
   sources:
     - name: pricefeed
       kind: http
       domain: purchasing
       endpoint: http://localhost:8090/pricefeed
       # api_key_env: PRICEFEED_API_KEY   # optional bearer token
   ```

End to end:
```bash
DDE_DOMAINS=examples/domains.yaml \
DDE_SOURCES_ENABLED=true DDE_SOURCES=examples/sources.yaml \
dde evaluate --input examples/purchasing-goal.json
```
The plan's `provenance.source_contributions` lists every source consulted and
whether its data was stale.

A policy file can override a domain's sources (full replace) via a `source_kinds`
list, mirroring `ignore_signal_kinds`; an explicit empty list disables enrichment for
that domain.

> Built-in (Go) sources — HTTP, MCP, AI-agent, read-model/cache — live in
> `internal/source`. The MCP and agent adapters take an injected transport, so the
> non-determinism stays sealed and recorded. A future agentic-planner mode (the model
> calling sources as tools mid-reasoning) reuses the same `Describe()`/`Fetch()`
> contract — see `internal/source/doc.go`.

## Policy (config-as-data)

Pack defaults can be overridden per domain by a policy file (`DDE_POLICY`,
JSON/YAML):

```json
{
  "domains": {
    "investing": {
      "confidence_delta": 0.03,
      "scoring": {
        "weights": { "ev": 0.4, "risk": 0.4, "liquidity": 0.1, "horizon": 0.1 },
        "risk":    { "max_portfolio_risk_pct": 0.01, "max_position_pct": 0.10, "kelly_fraction": 0.2, "max_aggregate_risk_pct": 0.04 }
      }
    }
  }
}
```

A partial `scoring` override is safe: any field left zero keeps its default
(`finance.DefaultScoringConfig`), so overriding one knob never silently zeroes
another. To explicitly disable a risk cap (`max_position_pct`,
`max_portfolio_risk_pct`, `max_aggregate_risk_pct`), set it to a negative
value.

### Per-domain planner strategy

A text domain can override the global reasoning backend (`DDE_PLANNER` /
`DDE_MULTI_*` / `DDE_LLM_MODEL`) via a `planner` block — so one domain can run a
multi-model ensemble while another uses a cheap single model:

```yaml
domains:
  growth:
    planner:
      planner: multi              # mock | anthropic | openai | deepseek | multi
      multi_mode: ensemble        # verify | route | ensemble (when planner: multi)
      multi_providers: [anthropic, openai]
      model: claude-opus-4-8      # optional; overrides the global LLM model
```

Only non-empty fields override the global config (a spec with just `model` keeps
the global strategy). Bad specs fail fast at startup — `multi` needs a valid mode
and ≥2 providers, and `verify` needs a real verifier (not `mock`).

Scope notes:
- Applies to **text** domains only. The `investing` domain uses the numeric finance
  planner, which ignores `planner` — tune it with `scoring` instead. (A `planner`
  override on investing takes effect only if the finance planner declines, e.g.
  offline with no market data.)
- `backtest` constructs its own router and ignores per-domain planner overrides.

## Async replanning

In `serve`, set `DDE_REPLAN_ASYNC=true` to process replanning on a background
worker pool. `POST /v1/signals` then returns **202** with `status: "pending"`;
poll `GET /v1/plans/{id}/versions` for the new version. Bursts of signals for one
plan are coalesced, and a snapshot-keyed plan cache makes duplicate work cheap.
The default is synchronous (inline) so the CLI and existing clients are unchanged.

## Backtesting

`dde backtest --input scenario.json` replays a timeline of signals through the
evaluate/replan loop and reports **decision/replanning quality** (kill
precision/recall, versions created) — not a tradeable strategy return. The
`hypothetical_pnl` figure is illustrative only. Market data is evaluated as of each
event's timestamp (no lookahead).

## Adding a new domain

### From config — no code (`DDE_DOMAINS`)

A text/prompt domain is pure data, so you can add one without recompiling. Point
`DDE_DOMAINS` at a JSON/YAML file (see `examples/domains.yaml`):

```yaml
domains:
  - id: fitness
    name: Fitness
    prompt_template: |        # appended to the base planner prompt
      DOMAIN: FITNESS
      Treat each move as a training experiment ...
    eval:
      confidence_delta: 0.15  # materiality threshold
    vocab:
      asset_kinds: [time, equipment]
    validation:
      warn_unknown_kinds: false
      rules:
        - check: require_metric          # require_metric | require_context | require_any_kind
          field: metric
          message: "no fitness metric set"
          severity: warning
        - check: require_any_kind
          kinds: [schedule, recovery]
          scopes: [constraint]           # asset | constraint; empty = both
          field: context.constraints
          message: "no schedule or recovery constraint"
          severity: warning
```

```bash
DDE_DOMAINS=examples/domains.yaml dde evaluate --input fitness-goal.json
```

`version`/`prompt_version` default to `1`/`<id>-v1`. An optional `prompt_file:
./prompt.txt` (resolved relative to the config file) loads the template from disk
instead of inline. IDs must be unique and may not collide with a built-in
(built-ins are authoritative). Numeric `scoring` is **not** config-loadable — use
the `DDE_POLICY` file (above) for per-domain scoring; config domains are
text/prompt domains.

### Built-in (Go)

For a shipped domain or one needing a numeric planner:

1. Add `internal/pack/<name>.go` returning a `Descriptor` (id, version, prompt
   template, evaluator config, optional scoring, vocabulary, declarative validation).
2. Register it in `NewRegistry()` (`internal/pack/registry.go`).

The router, evaluator resolver, validation and API all pick it up. For a numeric
domain, set `PlannerKind` and register a builder in `internal/wire/builders.go` —
no edit to `BuildPlannerRouter`.
