// Package strategy holds the pure, deterministic math that picks one candidate
// plan out of several competing strategies' proposals. It knows nothing about
// planners, models or market data: it scores []domain.RankedMove against the
// goal that framed them, filters candidates the goal's hard constraints rule
// out, and breaks ties deterministically. Everything here must stay free of
// I/O and map-iteration ordering so a selection is reproducible byte-for-byte.
package strategy

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Level scores map the qualitative low/medium/high vocabulary onto [0, 1] for
// the utility blend. Unknown levels read as medium.
const (
	levelScoreLow    = 0.2
	levelScoreMedium = 0.5
	levelScoreHigh   = 0.9
)

// Risk-aversion weights derived from the goal's risk_tolerance constraint: how
// hard a move's risk level subtracts from its utility.
const (
	riskAversionConservative = 1.0
	riskAversionDefault      = 0.6
	riskAversionAggressive   = 0.3
)

// effortPenalty is the fixed weight on a move's effort level — effort matters,
// but never as much as risk.
const effortPenalty = 0.25

// rankDecay geometrically discounts moves below the top recommendation: the
// top move dominates a plan's utility but a strong bench still counts.
const rankDecay = 0.7

// conservativeDrawdownLimit is the drawdown_limit fraction at or below which a
// goal is treated as conservative for the hard filter (10%).
const conservativeDrawdownLimit = 0.10

// Keyword parses mirror internal/finance/constraints.go so the selector reads
// risk_tolerance wording exactly the way sizing does. Duplicated (three small
// regexes) rather than imported to keep this package dependent on domain only.
var (
	conservativeRe = regexp.MustCompile(`\b(conservative|cautious|low)\b`)
	aggressiveRe   = regexp.MustCompile(`\b(aggressive|high)\b`)
	percentRe      = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
)

func levelScore(l domain.Level) float64 {
	switch l {
	case domain.LevelLow:
		return levelScoreLow
	case domain.LevelHigh:
		return levelScoreHigh
	default:
		return levelScoreMedium
	}
}

// riskAversion derives the λ_risk utility weight from the goal's stated
// risk_tolerance constraint; absent or unrecognised wording reads as moderate.
func riskAversion(goal domain.Goal) float64 {
	for _, c := range goal.Context.Constraints {
		if !strings.EqualFold(strings.TrimSpace(c.Kind), "risk_tolerance") {
			continue
		}
		text := strings.ToLower(c.Name + " " + c.Description)
		switch {
		case conservativeRe.MatchString(text):
			return riskAversionConservative
		case aggressiveRe.MatchString(text):
			return riskAversionAggressive
		}
	}
	return riskAversionDefault
}

// conservativeGoal reports whether the goal's constraints read conservative:
// an explicitly conservative risk_tolerance, or a drawdown_limit at or below
// conservativeDrawdownLimit.
func conservativeGoal(goal domain.Goal) bool {
	for _, c := range goal.Context.Constraints {
		switch strings.ToLower(strings.TrimSpace(c.Kind)) {
		case "risk_tolerance":
			text := strings.ToLower(c.Name + " " + c.Description)
			if conservativeRe.MatchString(text) {
				return true
			}
		case "drawdown_limit":
			dd, ok := parsePercent(c.Name)
			if !ok {
				dd, ok = parsePercent(c.Description)
			}
			if ok && dd > 0 && dd <= conservativeDrawdownLimit {
				return true
			}
		}
	}
	return false
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

// HardFilter applies the goal's hard gates to one candidate's moves. It
// returns filtered=true with a human-readable reason when the candidate is
// inadmissible: it proposes nothing viable, or its top recommendation carries
// a risk level the goal's stated tolerance rules out. Sizing-level constraints
// are already enforced inside each strategy's planner; this is the
// selector-level backstop on the plan's shape.
func HardFilter(goal domain.Goal, moves []domain.RankedMove) (reason string, filtered bool) {
	if len(moves) == 0 {
		return "no moves proposed", true
	}
	viable := false
	for _, m := range moves {
		if m.Confidence > 0 {
			viable = true
			break
		}
	}
	if !viable {
		return "no viable moves (every move has confidence 0)", true
	}
	if conservativeGoal(goal) && moves[0].Risk == domain.LevelHigh {
		return "top move risk \"high\" exceeds the goal's conservative risk tolerance", true
	}
	return "", false
}

// Utility scores a candidate's moves against the goal: per-move value is
// confidence-weighted impact minus risk (weighted by the goal's risk aversion)
// minus effort, blended down the ranking with geometric decay so the top move
// dominates without the bench being ignored. The result is rounded to 4
// decimals so downstream comparisons and tie-breaks are stable. The returned
// explain string names the inputs for provenance.
func Utility(goal domain.Goal, moves []domain.RankedMove) (float64, string) {
	if len(moves) == 0 {
		return 0, "no moves"
	}
	lambdaRisk := riskAversion(goal)

	var sum, weights float64
	g := 1.0
	for _, m := range moves {
		v := m.Confidence*levelScore(m.ExpectedImpact) -
			lambdaRisk*levelScore(m.Risk) -
			effortPenalty*levelScore(m.Effort)
		sum += g * v
		weights += g
		g *= rankDecay
	}
	u := round4(sum / weights)
	explain := fmt.Sprintf("utility %.4f over %d moves (risk aversion %.2f, top move %q conf %.2f)",
		u, len(moves), lambdaRisk, topKey(moves), moves[0].Confidence)
	return u, explain
}

// topKey returns the stable identity of a move list's top recommendation,
// falling back to the title when no key was minted.
func topKey(moves []domain.RankedMove) string {
	if len(moves) == 0 {
		return ""
	}
	if moves[0].Key != "" {
		return moves[0].Key
	}
	return moves[0].Title
}

// round4 stabilises utilities at 4 decimals so equality comparisons (and so
// the tie-break chain) never hinge on sub-noise float drift.
func round4(x float64) float64 {
	return math.Round(x*10000) / 10000
}
