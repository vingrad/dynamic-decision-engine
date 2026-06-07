// Package finance provides the deterministic numeric core behind the investing
// domain: thesis scoring, position sizing and structured market-signal parsing.
//
// HONEST FRAMING (load-bearing — keep it). This package scores the *quality and
// consistency of investment decisions*; it is NOT calibrated alpha and NOT a
// trading system.
//
//   - A move's confidence is a transparent function of a composite score, not a
//     measured market probability. Do not treat it as one.
//   - Sizing (fractional-Kelly / volatility-target) is an illustrative risk-budget
//     heuristic bounded by hard caps, not investment advice.
//   - Inputs come from market data and the goal's stated risk budget — never from
//     the LLM's self-reported confidence.
//
// All functions here are pure and deterministic so they are unit-testable and so
// the backtest can replay them reproducibly.
package finance
