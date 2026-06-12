package strategy

import (
	"fmt"
	"strings"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Candidate is one strategy's proposed plan, as handed to Select. A non-nil
// Err marks a strategy whose planner failed; it stays in the audit trail as a
// filtered candidate but can never win.
type Candidate struct {
	ID          string
	PlannerName string
	Moves       []domain.RankedMove
	Err         error
}

// Scored is the audit record for one candidate: its utility, the outcome
// weight applied, whether the hard filter removed it and why. The slice
// returned by Select is parallel to the input candidates so provenance can
// show every competitor, not just the winner.
type Scored struct {
	ID            string
	Utility       float64 // pre-weight, rounded to 4 decimals
	Weight        float64 // outcome weight applied (1.0 when none)
	Weighted      float64 // Utility × Weight, rounded to 4 decimals
	TopMoveKey    string
	TopConfidence float64
	Filtered      bool
	Reason        string // filter reason, or the utility explain string
}

// Options tunes a selection.
type Options struct {
	// Weights are outcome-fit multipliers keyed by strategy ID, with an optional
	// regime-specific entry "id@regime" that wins over the plain "id" key.
	// Missing keys mean 1.0 — an empty map never changes behaviour.
	Weights map[string]float64
	// Regime is the market regime label used for weight lookup ("" = unknown).
	Regime string
	// Incumbent is the strategy that won the current plan version ("" = none).
	// When it survives the filter, a challenger must beat its weighted utility
	// by more than IncumbentMargin — hysteresis against materiality flapping.
	Incumbent string
	// IncumbentMargin is the strict margin a challenger must clear. Callers set
	// it explicitly; zero means no hysteresis.
	IncumbentMargin float64
}

// Selection is the outcome of one competition.
type Selection struct {
	// Winner indexes the winning candidate in the input slice.
	Winner int
	// Scored is parallel to the input candidates.
	Scored []Scored
	// Reason is a short, audit-friendly account of why the winner won.
	Reason string
}

// Select filters, scores and picks one candidate. The input order is the
// canonical strategy declaration order and serves as the last-resort
// tie-break, so callers must pass candidates in a fixed order — never one
// derived from map iteration. It errors only on an empty candidate list: when
// every candidate is filtered it degrades to utility argmax over all of them
// (noting the degraded mode in Reason) rather than returning nothing.
func Select(goal domain.Goal, cands []Candidate, opts Options) (Selection, error) {
	if len(cands) == 0 {
		return Selection{}, fmt.Errorf("strategy: no candidates to select from")
	}

	scored := make([]Scored, len(cands))
	for i, c := range cands {
		s := Scored{ID: c.ID, Weight: weightFor(opts, c.ID)}
		if c.Err != nil {
			s.Filtered = true
			s.Reason = "planner error: " + c.Err.Error()
		} else {
			// Utility is computed even for filtered candidates: it feeds the
			// all-filtered fallback and makes the audit trail comparable.
			u, explain := Utility(goal, c.Moves)
			s.Utility = u
			s.Weighted = applyWeight(u, s.Weight)
			s.TopMoveKey = topKey(c.Moves)
			if len(c.Moves) > 0 {
				s.TopConfidence = c.Moves[0].Confidence
			}
			if reason, filtered := HardFilter(goal, c.Moves); filtered {
				s.Filtered = true
				s.Reason = reason
			} else {
				s.Reason = explain
			}
		}
		scored[i] = s
	}

	winner := pick(scored, cands, false)
	degraded := winner < 0
	if degraded {
		// Every candidate was filtered. The least-bad plan still beats no plan:
		// pick by the same chain ignoring filters. Candidates whose planner
		// errored stay excluded — they have no plan to return — unless nothing
		// else exists at all.
		winner = pick(scored, cands, true)
	}

	reason := fmt.Sprintf("selected %q: weighted utility %.4f", scored[winner].ID, scored[winner].Weighted)
	if degraded {
		reason = fmt.Sprintf("no admissible candidate; degraded to best inadmissible: %q (%s)",
			scored[winner].ID, scored[winner].Reason)
	} else if inc := incumbentIndex(scored, opts.Incumbent); inc >= 0 && inc != winner {
		// Hysteresis: the incumbent holds unless the challenger clears the margin.
		challenger := winner
		if scored[challenger].Weighted <= scored[inc].Weighted+opts.IncumbentMargin {
			winner = inc
			reason = fmt.Sprintf("incumbent %q held: challenger %q margin %.4f within hysteresis %.4f",
				scored[inc].ID, scored[challenger].ID, scored[challenger].Weighted-scored[inc].Weighted, opts.IncumbentMargin)
		} else {
			reason = fmt.Sprintf("selected %q: weighted utility %.4f beats incumbent %q by more than %.4f",
				scored[winner].ID, scored[winner].Weighted, scored[inc].ID, opts.IncumbentMargin)
		}
	}
	if rejected := filteredIDs(scored); len(rejected) > 0 && !degraded {
		reason += "; filtered: " + strings.Join(rejected, ", ")
	}

	return Selection{Winner: winner, Scored: scored, Reason: reason}, nil
}

// applyWeight scales a utility by an outcome weight so that a weight above 1
// always FAVOURS the strategy and below 1 always penalises it, regardless of
// the utility's sign — a plain product would invert the meaning on negative
// utilities (a 1.3 weight making a −0.05 plan read worse, not better).
func applyWeight(u, w float64) float64 {
	if u < 0 {
		return round4(u / w)
	}
	return round4(u * w)
}

// weightFor resolves the outcome weight for a strategy: the regime-specific
// "id@regime" entry wins over the plain "id" entry; absent keys mean 1.0.
func weightFor(opts Options, id string) float64 {
	if opts.Regime != "" {
		if w, ok := opts.Weights[id+"@"+opts.Regime]; ok && w > 0 {
			return w
		}
	}
	if w, ok := opts.Weights[id]; ok && w > 0 {
		return w
	}
	return 1.0
}

// pick returns the index of the best candidate by the deterministic chain:
// higher weighted utility → higher top-move raw confidence → earlier canonical
// (input) order. With includeFiltered=false it considers only admissible
// candidates and returns -1 when none exist. Candidates with a planner error
// have no plan to return, so even the degraded pass skips them; only when
// every candidate errored does it fall back to the first.
func pick(scored []Scored, cands []Candidate, includeFiltered bool) int {
	best := -1
	for i := range scored {
		if cands[i].Err != nil {
			continue
		}
		if !includeFiltered && scored[i].Filtered {
			continue
		}
		if best < 0 || better(scored, cands, i, best) {
			best = i
		}
	}
	if best < 0 && includeFiltered {
		return 0
	}
	return best
}

// better reports whether candidate i strictly beats candidate j. Equal on
// every key falls through to false, keeping the earlier (canonical-order)
// candidate — the last-resort tie-break.
func better(scored []Scored, cands []Candidate, i, j int) bool {
	if scored[i].Weighted != scored[j].Weighted {
		return scored[i].Weighted > scored[j].Weighted
	}
	if ri, rj := topRawConfidence(cands[i].Moves), topRawConfidence(cands[j].Moves); ri != rj {
		return ri > rj
	}
	return false
}

// topRawConfidence reads the top move's pre-calibration confidence, falling
// back to the stated confidence for plans that predate RawConfidence.
func topRawConfidence(moves []domain.RankedMove) float64 {
	if len(moves) == 0 {
		return 0
	}
	if moves[0].RawConfidence > 0 {
		return moves[0].RawConfidence
	}
	return moves[0].Confidence
}

// incumbentIndex finds the incumbent among admissible candidates; -1 when the
// incumbent is unset, unknown, or was filtered out (a filtered incumbent holds
// nothing).
func incumbentIndex(scored []Scored, incumbent string) int {
	if incumbent == "" {
		return -1
	}
	for i := range scored {
		if scored[i].ID == incumbent && !scored[i].Filtered {
			return i
		}
	}
	return -1
}

func filteredIDs(scored []Scored) []string {
	var out []string
	for _, s := range scored {
		if s.Filtered {
			out = append(out, fmt.Sprintf("%s (%s)", s.ID, s.Reason))
		}
	}
	return out
}

// ReWeigh picks the winning index among RECORDED candidates under a new set
// of outcome weights — the offline evaluation primitive behind the strategy
// walk-forward: it re-runs the weight mapping over a past competition without
// re-running any planner. Filtered candidates stay out; ties break on the
// candidate's recorded top confidence, then on recorded (canonical) order.
// Returns -1 when no admissible candidate was recorded.
func ReWeigh(cands []domain.StrategyCandidate, weights map[string]float64, regime string) int {
	opts := Options{Weights: weights, Regime: regime}
	best := -1
	var bestW float64
	for i, c := range cands {
		if c.Filtered {
			continue
		}
		w := applyWeight(c.UtilityScore, weightFor(opts, c.StrategyID))
		switch {
		case best < 0, w > bestW:
			best, bestW = i, w
		case w == bestW && c.TopConfidence > cands[best].TopConfidence:
			best = i
		}
	}
	return best
}

// DisagreementPenalty converts strategy disagreement into a confidence
// haircut, quantized to steps of 0.05 — the investing pack's materiality
// ConfidenceDelta — so disagreement either leaves stated confidence alone or
// moves it by a deliberately material step; sub-threshold churn from a
// flickering minority opinion is impossible by construction. Agreement is the
// share of admissible candidates whose top move matches the winner's.
func DisagreementPenalty(scored []Scored, winnerTopKey string) float64 {
	admissible, agree := 0, 0
	for _, s := range scored {
		if s.Filtered {
			continue
		}
		admissible++
		if s.TopMoveKey == winnerTopKey {
			agree++
		}
	}
	if admissible < 2 || agree == admissible {
		return 0
	}
	if float64(agree)/float64(admissible) >= 0.5 {
		return 0.05
	}
	return 0.10
}
