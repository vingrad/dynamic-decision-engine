package finance

// Strategies are named heuristic LENSES over the same transparent scoring math
// — prior component weights, composite weights, reward:risk assumption and a
// risk-budget scale. They are not calibrated alpha and must never be read as
// trading systems; per the package doc, this remains decision support. Which
// lens wins for a given goal is decided elsewhere (the strategy selector); this
// file only defines what a lens is, as pure data.

// RiskScale multiplies the base risk budget. The hard-cap multipliers
// (PerTradeRisk, Aggregate) are clamped to (0, 1] by Apply — a strategy may
// only TIGHTEN caps, never loosen them, mirroring EffectiveRiskBudget's
// "aggressive loosens Kelly only" rule. Kelly may scale either way; the caps
// remain guardrails around it. Zero fields mean 1.0 (no change).
type RiskScale struct {
	Kelly        float64 `json:"kelly,omitempty"`
	PerTradeRisk float64 `json:"per_trade_risk,omitempty"`
	Aggregate    float64 `json:"aggregate,omitempty"`
}

// StrategyParams is one named lens over the scoring math. Zero-valued fields
// leave the base configuration untouched, so a partial override tunes one knob
// without dragging the rest to zero.
type StrategyParams struct {
	Name            string       `json:"name"`
	Prior           PriorWeights `json:"prior"`
	Weights         ScoreWeights `json:"weights"`
	RewardRiskRatio float64      `json:"reward_risk_ratio,omitempty"`
	RiskScale       RiskScale    `json:"risk_scale,omitempty"`
}

// Apply overlays the strategy onto a base config: it replaces Weights and
// RewardRiskRatio when set and multiplies the risk budget by RiskScale (caps
// clamped to tighten-only). Order of operations in the planner is
//
//	pack/policy base -> strategy.Apply -> EffectiveRiskBudget(goal constraints)
//
// so the goal's own constraints always have the last (tightening) word.
func (s StrategyParams) Apply(base ScoringConfig) ScoringConfig {
	out := base.Normalize()
	if s.Weights != (ScoreWeights{}) {
		out.Weights = s.Weights
	}
	if s.RewardRiskRatio > 0 {
		out.RewardRiskRatio = s.RewardRiskRatio
	}
	if k := s.RiskScale.Kelly; k > 0 {
		out.Risk.KellyFraction *= k
	}
	if v := tightenOnly(s.RiskScale.PerTradeRisk); v < 1 {
		out.Risk.MaxPortfolioRiskPct *= v
		out.Risk.MaxPositionPct *= v
	}
	if v := tightenOnly(s.RiskScale.Aggregate); v < 1 && out.Risk.MaxAggregateRiskPct > 0 {
		// A negative aggregate cap is the explicit "disabled" spelling; scaling
		// it would be meaningless, so only a positive cap tightens.
		out.Risk.MaxAggregateRiskPct *= v
	}
	return out
}

// Merge overlays the SET fields of an override onto the receiver, leaving
// zero-valued fields of the override alone — the same partial-override
// semantics every policy knob follows. A policy tuning one knob of a lens
// (e.g. its Kelly scale) must not silently strip the lens's other parameters:
// without this, an unset Prior would normalize to the neutral blend and erase
// the lens's identity. Name is identity, never overridable.
func (s StrategyParams) Merge(o StrategyParams) StrategyParams {
	if o.Prior != (PriorWeights{}) {
		s.Prior = o.Prior
	}
	if o.Weights != (ScoreWeights{}) {
		s.Weights = o.Weights
	}
	if o.RewardRiskRatio > 0 {
		s.RewardRiskRatio = o.RewardRiskRatio
	}
	if o.RiskScale.Kelly > 0 {
		s.RiskScale.Kelly = o.RiskScale.Kelly
	}
	if o.RiskScale.PerTradeRisk > 0 {
		s.RiskScale.PerTradeRisk = o.RiskScale.PerTradeRisk
	}
	if o.RiskScale.Aggregate > 0 {
		s.RiskScale.Aggregate = o.RiskScale.Aggregate
	}
	return s
}

// tightenOnly maps a cap multiplier onto (0, 1]: zero (unset) and anything
// above 1 become 1 (no change); only a genuine tightening passes through.
func tightenOnly(v float64) float64 {
	if v <= 0 || v >= 1 {
		return 1
	}
	return v
}

// DefaultStrategySet returns the investing pack's standard lenses in their
// canonical, fixed order — that order is the last-resort tie-break during
// selection, so it must never depend on map iteration.
//
//   - value buys weakness: the valuation tilt dominates, momentum is nearly
//     ignored, targets are larger (2.5 reward:risk) and held longer.
//   - momentum rides strength and pays for its exits: the momentum tilt
//     dominates, liquidity weighs more, and the 1.8 reward:risk keeps targets
//     tight.
//   - defensive preserves capital: quality-heavy prior, risk-heavy composite,
//     halved Kelly and tightened caps. It is the always-admissible fallback —
//     its pack descriptor declares no regime restrictions.
func DefaultStrategySet() []StrategyParams {
	return []StrategyParams{
		{
			Name:            "value",
			Prior:           PriorWeights{Valuation: 1.6, Quality: 1.0, Momentum: 0.4},
			Weights:         ScoreWeights{EV: 0.45, Risk: 0.25, Liquidity: 0.15, Horizon: 0.15},
			RewardRiskRatio: 2.5,
		},
		{
			Name:            "momentum",
			Prior:           PriorWeights{Valuation: 0.3, Quality: 0.7, Momentum: 1.8},
			Weights:         ScoreWeights{EV: 0.50, Risk: 0.20, Liquidity: 0.20, Horizon: 0.10},
			RewardRiskRatio: 1.8,
		},
		{
			Name:            "defensive",
			Prior:           PriorWeights{Valuation: 0.8, Quality: 1.6, Momentum: 0.6},
			Weights:         ScoreWeights{EV: 0.25, Risk: 0.45, Liquidity: 0.15, Horizon: 0.15},
			RewardRiskRatio: 2.0,
			RiskScale:       RiskScale{Kelly: 0.5, PerTradeRisk: 0.75, Aggregate: 0.75},
		},
	}
}
