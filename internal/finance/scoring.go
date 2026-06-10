package finance

import (
	"math"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// clamp01 bounds x to [0,1].
func clamp01(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	default:
		return x
	}
}

// round2 keeps scores presentation-stable.
func round2(x float64) float64 { return math.Round(x*100) / 100 }

// Returns computes simple close-to-close returns from chronological bars.
func Returns(bars []marketdata.Bar) []float64 {
	if len(bars) < 2 {
		return nil
	}
	out := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		prev := bars[i-1].Close
		if prev == 0 {
			out = append(out, 0)
			continue
		}
		out = append(out, (bars[i].Close-prev)/prev)
	}
	return out
}

// Volatility is the sample standard deviation of returns (period volatility, not
// annualised — fixtures are small and we keep it transparent).
func Volatility(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	var mean float64
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	var ss float64
	for _, r := range returns {
		d := r - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(returns)-1))
}

// MaxDrawdown is the largest peak-to-trough decline of close prices, as a positive
// fraction in [0,1].
func MaxDrawdown(bars []marketdata.Bar) float64 {
	var peak, maxDD float64
	for _, b := range bars {
		if b.Close > peak {
			peak = b.Close
		}
		if peak > 0 {
			dd := (peak - b.Close) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// ExpectedValue returns the signed expected return of a bet given win probability
// and the win/loss magnitudes (positive fractions, e.g. 0.30 == +30%).
func ExpectedValue(winProb, winFrac, lossFrac float64) float64 {
	winProb = clamp01(winProb)
	return winProb*winFrac - (1-winProb)*lossFrac
}

// EVScore maps a signed expected value into a [0,1] score centred at 0.5.
func EVScore(ev float64) float64 { return clamp01(0.5 + ev) }

// NeutralEVScore is the EV component used when the win probability carries no
// information (flat prior, no signal hint). Scoring the volatility-scaled win/
// loss magnitudes under a flat prior would reward the most volatile asset, so
// the EV component stays neutral until a signal tilts the odds.
const NeutralEVScore = 0.5

// RiskScore maps volatility and drawdown into a [0,1] safety score (higher safer).
func RiskScore(vol, maxDD float64) float64 {
	return clamp01(1 - 0.5*vol - 0.5*maxDD)
}

// LiquidityFit scores how comfortably an intended position fits available
// liquidity. >= ~100x daily dollar volume is treated as fully liquid.
func LiquidityFit(avgDollarVolume, intendedNotional float64) float64 {
	if intendedNotional <= 0 {
		return 1
	}
	if avgDollarVolume <= 0 {
		return 0
	}
	return clamp01((avgDollarVolume / intendedNotional) / 100.0)
}

// HorizonFit scores agreement between the goal horizon and the signal's horizon.
// Either being unknown (<=0) yields a neutral 0.5.
func HorizonFit(goalDays, signalDays int) float64 {
	if goalDays <= 0 || signalDays <= 0 {
		return 0.5
	}
	diff := math.Abs(float64(goalDays - signalDays))
	denom := math.Max(float64(goalDays), float64(signalDays))
	return clamp01(1 - diff/denom)
}

// Composite blends the sub-scores using normalised weights.
func Composite(s ThesisScore, w ScoreWeights) float64 {
	sum := w.EV + w.Risk + w.Liquidity + w.Horizon
	if sum <= 0 {
		w = ScoreWeights{EV: 1, Risk: 1, Liquidity: 1, Horizon: 1}
		sum = 4
	}
	c := (w.EV*s.ExpectedValueScore +
		w.Risk*s.RiskScore +
		w.Liquidity*s.LiquidityFitScore +
		w.Horizon*s.HorizonFitScore) / sum
	return round2(clamp01(c))
}

// CompositeToConfidence converts a composite score into the move confidence. This
// is a transparent transform of the numeric score — NOT a market probability.
func CompositeToConfidence(composite float64) float64 { return clamp01(composite) }

// MapToLevels derives the qualitative impact/effort/risk levels from the score.
func MapToLevels(s ThesisScore) (impact, effort, risk domain.Level) {
	impact = bucket(s.Composite)
	// Risk level is the inverse of the safety score: low safety => high risk.
	risk = bucket(1 - s.RiskScore)
	// Effort tracks illiquidity: a thinly-traded name is harder to build/exit.
	effort = bucket(1 - s.LiquidityFitScore)
	return impact, effort, risk
}

func bucket(x float64) domain.Level {
	switch {
	case x >= 0.66:
		return domain.LevelHigh
	case x >= 0.33:
		return domain.LevelMedium
	default:
		return domain.LevelLow
	}
}

// PositionFractionKelly sizes a position via fractional Kelly, clamped by the risk
// budget. winFrac/lossFrac are positive fractions; lossFrac doubles as the stop
// distance for the per-trade risk cap.
func PositionFractionKelly(winProb, winFrac, lossFrac float64, budget RiskBudget) PositionSize {
	winProb = clamp01(winProb)
	ps := PositionSize{SizingMethod: "fractional_kelly"}
	if lossFrac <= 0 || winFrac <= 0 {
		return ps // not enough information to size
	}
	b := winFrac / lossFrac
	q := 1 - winProb
	kelly := (b*winProb - q) / b
	if kelly < 0 {
		kelly = 0 // negative edge => no position
	}
	frac := budget.KellyFraction * kelly
	frac, ps.BindingCap = applyCaps(frac, lossFrac, budget)
	ps.SuggestedFraction = round2(frac)
	return ps
}

// PositionVolTarget sizes a position to hit a target volatility given the asset's
// volatility, clamped by the risk budget.
func PositionVolTarget(targetVol, assetVol float64, budget RiskBudget) PositionSize {
	ps := PositionSize{SizingMethod: "vol_target"}
	if assetVol <= 0 {
		return ps
	}
	frac := targetVol / assetVol
	frac, ps.BindingCap = applyCaps(frac, math.Max(assetVol, 0.0001), budget)
	ps.SuggestedFraction = round2(frac)
	return ps
}

// applyCaps clamps a raw fraction to the concentration cap and the per-trade risk
// cap (position * stopDistance <= MaxPortfolioRiskPct), reporting which cap bound it.
func applyCaps(frac, stopDistance float64, budget RiskBudget) (float64, string) {
	bound := ""
	if budget.MaxPositionPct > 0 && frac > budget.MaxPositionPct {
		frac = budget.MaxPositionPct
		bound = "max_position_pct"
	}
	if budget.MaxPortfolioRiskPct > 0 && stopDistance > 0 {
		riskCap := budget.MaxPortfolioRiskPct / stopDistance
		if frac > riskCap {
			frac = riskCap
			bound = "max_portfolio_risk_pct"
		}
	}
	if frac < 0 {
		frac = 0
	}
	return frac, bound
}
