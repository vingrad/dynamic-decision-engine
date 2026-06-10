# Investing pack: decision-quality roadmap

Forward plan for improving the quality of decisions produced by the investing
domain. It builds on the quick-wins pass (#10), which made per-goal risk
constraints bind sizing, neutralised the flat-prior EV component, and added
decision-quality metrics (Brier score, forward-return attribution, noise
robustness) to the backtest harness. From here on, every scoring change is
justified by those metrics.

Architecture rules hold throughout: packs stay pure data (`internal/pack`),
`internal/finance` stays pure deterministic math, the planner orchestrates I/O
(`internal/llm`), and wire/cmd compose (`internal/wire`, `cmd/dde`).

## Phase 1 — Make the LLM hybrid real (small, bug fix)

`DDE_FINANCE_HYBRID` is parsed in `internal/config/config.go` but never
consumed: `newEngine` in `cmd/dde/commands.go` never sets
`PlannerDeps.FinanceInner`, and the finance route is not wrapped in
`GuidedPlanner`, so the investing pack's `PromptTemplate` (thesis framing,
kill-criteria discipline) never reaches any model.

- Wire `FinanceInner` from the resolved base planner when the flag is set.
- Pass the pack's `PromptTemplate` into `FinanceConfig` so the inner narrator
  receives it as `SystemPromptOverride`, and include the per-thesis
  `score.Explain` lines in the inner request so the narration can reference
  the numbers it annotates. Numbers stay authoritative — the LLM only
  narrates.
- Verify with a recording mock inner planner; no scoring change, so backtest
  metrics must be unchanged.

## Phase 2 — Scenario corpus (small, high leverage)

The Brier score currently averages over two decisions in one scenario. Before
any scoring change, broaden the evidence base:

- Add fixture scenarios: noise-heavy (many immaterial signals), multi-ticker
  rotation, slow-drawdown loser.
- Add a `RunMatrix(scenarios, configs)` helper so scoring-config variants can
  be compared in one deterministic table.

## Phase 3 — Informed win probability (medium, highest quality content)

`marketdata.Provider.Fundamentals()` is implemented on both providers with
point-in-time fixtures (PE/PB/EPS for ACME/GLOBEX) and is never called. Today
`winProb` is 0.5 unless a signal hints otherwise.

- New pure `internal/finance/prior.go`: `WinProbPrior(fundamentals, returns)`
  — valuation tilt from PE/PB vs. neutral bands, earnings quality (EPS > 0),
  6–12-month momentum from bars the planner already fetches. Clamped to
  roughly [0.35, 0.65] and documented as a heuristic prior, per
  `finance/doc.go`'s honesty framing.
- The planner fetches fundamentals per ticker (as-of the sim/real clock),
  blends with signal hints (the signal wins), and falls back to neutral.
- This reintroduces informed EV on initial plans; the `NeutralEVScore` guard
  remains for the truly-uninformed case.
- Acceptance bar: Brier improves on the scenario corpus (ACME's PE drop
  18→14 in the fixtures should lift its prior).

## Phase 4 — Coherent vol/stop/horizon scale (medium)

Two related problems. `Volatility` is per-period (daily, ~0.01–0.02), so the
0.05 `lossFrac` floor almost always dominates — stop distance, EV magnitudes
and Kelly inputs are effectively constants disconnected from the asset. And
`HorizonFit` compares the goal horizon to a signal's *lookback* window, so it
is dead weight at 0.5 on every initial plan.

- `ScaledVol(periodVol, days) = vol * sqrt(days)` over a holding window
  derived from the goal horizon; the floor becomes a true lower bound.
  `winFrac` from the valuation gap when Phase 3 data exists, else the
  configured reward:risk ratio.
- Rebuild `HorizonFit` on vol-implied time-to-target
  (`(winFrac/dailyVol)^2`, a random-walk crossing-time heuristic) vs. the
  goal's stated horizon, falling back to liquidity time-to-build
  (`notional/ADV` days), then neutral 0.5.
- Re-baseline `scoring_test.go` and planner tests; backtest metrics gate the
  change.

## Phase 5 — Outcome-driven confidence calibration (medium/large)

Outcomes are recorded against immutable plan versions (addressed by
`(plan_version, move_rank)`), so (stated confidence, realized result) pairs
are recoverable offline — nothing reads them today.

- Pure `internal/finance/calibrate.go`: `CalibrationSample`,
  `FitCalibration` (binned/isotonic-lite, deterministic), `Curve.Apply`
  (identity when empty).
- Policy carries the curve per domain (`DomainPolicy`); wire threads it into
  `FinanceConfig`; `CompositeToConfidence` applies it.
- New `dde calibrate` command: walk goals → outcomes → resolve recorded
  confidences → fit → emit a policy YAML snippet. Offline, deterministic, no
  schema changes.
- Then a walk-forward backtest variant: fit on the first K events, apply to
  the rest, report calibration error pre/post — the proof the loop helps
  before changing production policy defaults.

## Phase 6 — Portfolio-level risk aggregation (large)

Theses are scored independently; five positions each at the 2% per-trade cap
is 10% aggregate risk with no governor.

- Pure `internal/finance/portfolio.go`: pairwise correlations from aligned
  bar returns, portfolio risk `sqrt(w'Σw)`, proportional scale-down to an
  aggregate cap, new `BindingCap: "portfolio_risk"`.
- The planner runs it as a post-pass over scored theses (it already has the
  bars per ticker). Materiality flows naturally since sizes and confidences
  shift.

## Orthogonal, anytime — real market data

`internal/marketdata`'s HTTP provider is a stub returning
`ErrNotImplemented`. Implementing it gates validating any of the above on
real data, but touches no decision logic; the offline provider remains the
default for tests and backtests.

## Suggested order and gates

| PR | Content | Size | Gate |
|----|---------|------|------|
| 1 | Hybrid wiring fix | S | metrics unchanged |
| 2 | Scenario corpus + RunMatrix | S | n/a (it *is* the gate) |
| 3 | winProb prior (fundamentals/momentum) | M | Brier improves |
| 4 | Vol/stop/horizon coherence | M | no metric regression |
| 5 | Calibration + `dde calibrate` + walk-forward eval | M/L | calibration error drops |
| 6 | Portfolio risk aggregation | L | aggregate risk bounded in backtest |
