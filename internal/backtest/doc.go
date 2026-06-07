// Package backtest replays a timeline of market signals through the engine's
// evaluate/replan loop and reports decision-quality metrics.
//
// HONEST FRAMING (load-bearing). This measures the QUALITY OF DECISIONS AND
// REPLANNING — how well kill-criteria and materiality track an analyst's labels —
// NOT the return of a tradeable strategy. The HypotheticalPnL field is an
// illustrative figure derived from offline fixtures, not a backtested strategy
// result. Point-in-time market data is enforced (each event is evaluated as of its
// own timestamp) so the replay cannot peek at the future.
package backtest
