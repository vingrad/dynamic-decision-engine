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
		Weights:         ScoreWeights{EV: 0.40, Risk: 0.30, Liquidity: 0.15, Horizon: 0.15},
		Risk:            RiskBudget{MaxPortfolioRiskPct: 0.02, MaxPositionPct: 0.20, KellyFraction: 0.25},
		RewardRiskRatio: 2.0,
	}
}
