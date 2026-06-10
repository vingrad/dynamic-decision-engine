package finance

// This file holds only the *tunable inputs* to the finance scorer (plain data,
// no behaviour and no dependencies) so that other packages — notably the domain
// pack descriptors and the policy loader — can carry and override them without
// importing the scoring logic (or pulling in market data). The scoring functions
// and their result types live in score.go / scoring.go.

// ScoreWeights weights the components of the composite thesis score. Only the
// relative magnitudes matter; the scorer normalises by their sum.
type ScoreWeights struct {
	EV        float64 `json:"ev"`        // expected value
	Risk      float64 `json:"risk"`      // risk/drawdown safety
	Liquidity float64 `json:"liquidity"` // liquidity fit
	Horizon   float64 `json:"horizon"`   // time-horizon fit
}

// RiskBudget bounds position sizing. Fractions are of account equity.
type RiskBudget struct {
	MaxPortfolioRiskPct float64 `json:"max_portfolio_risk_pct"` // capital at risk per position, e.g. 0.02
	MaxPositionPct      float64 `json:"max_position_pct"`       // concentration cap, e.g. 0.20
	KellyFraction       float64 `json:"kelly_fraction"`         // fractional-Kelly multiplier, e.g. 0.25
	AccountEquity       float64 `json:"account_equity"`         // optional; 0 == unknown (liquidity uses a fallback notional)
	// MaxAggregateRiskPct caps the correlation-aware capital at risk across all
	// concurrent theses (see AggregateRisk). Zero means "use the default" (a
	// partial override must not silently drop the cap); a NEGATIVE value
	// explicitly disables the aggregate cap.
	MaxAggregateRiskPct float64 `json:"max_aggregate_risk_pct"`
}

// ScoringConfig bundles the tunable scorer inputs. Domain packs provide defaults;
// the policy file (DDE_POLICY) may override them per domain.
type ScoringConfig struct {
	Weights         ScoreWeights `json:"weights"`
	Risk            RiskBudget   `json:"risk"`
	RewardRiskRatio float64      `json:"reward_risk_ratio"` // assumed win:loss magnitude ratio, e.g. 2.0
}

// DefaultScoringConfig returns conservative, transparent defaults suitable for
// the investing pack. They are deliberately cautious — this is decision support,
// not a tuned trading strategy.
func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		Weights: ScoreWeights{EV: 0.40, Risk: 0.30, Liquidity: 0.15, Horizon: 0.15},
		// Aggregate cap: roughly three per-trade budgets' worth of correlated
		// capital at risk across the whole book.
		Risk:            RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.20, KellyFraction: 0.25, MaxAggregateRiskPct: 0.06},
		RewardRiskRatio: 2.0,
	}
}

// Normalize fills the fields a partial config (e.g. a policy override that sets
// one knob) leaves zero with the defaults, so overriding one parameter never
// silently zeroes the others — a KellyFraction of 0 would size every position
// to nothing, and a MaxAggregateRiskPct of 0 would drop the portfolio cap for
// exactly the operators who tuned their risk. Negative cap values are kept:
// they are the explicit "disable this cap" spelling. A fully zero config
// normalizes to DefaultScoringConfig.
func (c ScoringConfig) Normalize() ScoringConfig {
	def := DefaultScoringConfig()
	if c.Weights == (ScoreWeights{}) {
		c.Weights = def.Weights
	}
	if c.RewardRiskRatio <= 0 {
		c.RewardRiskRatio = def.RewardRiskRatio
	}
	if c.Risk.KellyFraction == 0 {
		c.Risk.KellyFraction = def.Risk.KellyFraction
	}
	if c.Risk.MaxPositionPct == 0 {
		c.Risk.MaxPositionPct = def.Risk.MaxPositionPct
	}
	if c.Risk.MaxPortfolioRiskPct == 0 {
		c.Risk.MaxPortfolioRiskPct = def.Risk.MaxPortfolioRiskPct
	}
	if c.Risk.MaxAggregateRiskPct == 0 {
		c.Risk.MaxAggregateRiskPct = def.Risk.MaxAggregateRiskPct
	}
	return c
}
