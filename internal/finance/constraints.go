package finance

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

var (
	percentRe      = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	conservativeRe = regexp.MustCompile(`\b(conservative|cautious|low)\b`)
	aggressiveRe   = regexp.MustCompile(`\b(aggressive|high)\b`)
)

// EffectiveRiskBudget tightens a base risk budget with the goal's stated
// drawdown_limit and risk_tolerance constraints, so per-goal risk preferences
// actually bind position sizing. Constraints that don't parse leave the base
// unchanged. The returned note names what bound, for the move rationale.
func EffectiveRiskBudget(base RiskBudget, constraints []domain.Constraint) (RiskBudget, string) {
	out := base
	var notes []string
	for _, c := range constraints {
		switch strings.ToLower(strings.TrimSpace(c.Kind)) {
		case "drawdown_limit":
			dd, ok := parsePercent(c.Name)
			if !ok {
				dd, ok = parsePercent(c.Description)
			}
			if !ok || dd <= 0 || dd >= 1 {
				continue
			}
			// A total loss of a single position must not exceed the stated
			// drawdown limit, a run of five stopped-out trades must stay
			// within it, and the whole book's correlated capital at risk must
			// not exceed it either.
			if out.MaxPositionPct == 0 || dd < out.MaxPositionPct {
				out.MaxPositionPct = dd
			}
			if perTrade := dd / 5; out.MaxPortfolioRiskPct == 0 || perTrade < out.MaxPortfolioRiskPct {
				out.MaxPortfolioRiskPct = perTrade
			}
			if out.MaxAggregateRiskPct <= 0 || dd < out.MaxAggregateRiskPct {
				out.MaxAggregateRiskPct = dd
			}
			notes = append(notes, fmt.Sprintf("drawdown_limit %.0f%%", dd*100))
		case "risk_tolerance":
			text := strings.ToLower(c.Name + " " + c.Description)
			switch {
			case conservativeRe.MatchString(text):
				out.KellyFraction *= 0.5
				out.MaxPositionPct *= 0.5
				notes = append(notes, "risk_tolerance conservative")
			case aggressiveRe.MatchString(text):
				// Aggressive loosens the Kelly multiplier only; the hard caps
				// (concentration, per-trade risk) remain guardrails.
				out.KellyFraction *= 1.5
				notes = append(notes, "risk_tolerance aggressive")
			}
			// Moderate or unrecognised wording: the base budget already
			// encodes a moderate stance.
		}
	}
	return out, strings.Join(notes, ", ")
}

// parsePercent extracts the first "N%" in s as a fraction (10% -> 0.10).
func parsePercent(s string) (float64, bool) {
	m := percentRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return n / 100, true
}
