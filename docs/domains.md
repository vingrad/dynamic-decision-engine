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
embedded JSON fixtures (no network, CI-safe); a real HTTP vendor is stubbed behind
the same interface. Select with `DDE_MARKETDATA_PROVIDER=offline|http`.

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
        "risk":    { "max_portfolio_risk_pct": 0.01, "max_position_pct": 0.10, "kelly_fraction": 0.2 }
      }
    }
  }
}
```

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
