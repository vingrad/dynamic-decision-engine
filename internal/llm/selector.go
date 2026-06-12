package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/strategy"
)

// SelectorPlanner makes named domain strategies compete: every eligible child
// generates a full candidate plan, the pure selection math in
// internal/strategy scores each candidate against the goal, and the winner's
// plan is returned with the whole competition recorded in provenance. The
// engine is unaware of the difference — like every composite in multi.go,
// this is just another Planner.

// StrategyChild is one competing strategy: a stable ID, the planner that
// embodies it, and the market regimes it declares itself applicable to (empty
// means always eligible).
type StrategyChild struct {
	ID      string
	Planner Planner
	Regimes []string
}

// RegimeFn classifies the market regime for a goal at plan time ("trend",
// "range", "high_vol"; "" means unknown — and unknown gates nothing). Nil
// disables regime gating entirely.
type RegimeFn func(ctx context.Context, goal domain.Goal) (string, error)

// defaultIncumbentMargin is the hysteresis a challenger must clear over the
// incumbent strategy. It is deliberately at least the investing pack's
// materiality ConfidenceDelta so a winner flip is never triggered by sub-
// materiality noise.
const defaultIncumbentMargin = 0.05

// SelectorConfig assembles a SelectorPlanner.
type SelectorConfig struct {
	Children []StrategyChild
	// Weights are outcome-fit multipliers ("id" or "id@regime" keys); empty is
	// the identity.
	Weights map[string]float64
	// Regime, when non-nil, classifies the market regime to gate children and
	// stamp provenance.
	Regime RegimeFn
	// IncumbentMargin overrides the hysteresis margin; zero means the default.
	IncumbentMargin float64
	// Inner, when non-nil, narrates the WINNING plan only (hybrid mode) — one
	// model call per decision regardless of how many strategies competed.
	// Children must be built without their own narrator.
	Inner Planner
	// PromptTemplate is the pack's domain guidance, handed to the narrator as
	// its system prompt override.
	PromptTemplate string
}

// SelectorPlanner implements Planner over a set of competing strategy children.
type SelectorPlanner struct {
	children []StrategyChild
	weights  map[string]float64
	regime   RegimeFn
	margin   float64
	inner    Planner
	prompt   string
}

// NewSelectorPlanner validates and constructs a selector. It requires at
// least two children (one strategy has nothing to compete with) and unique
// child IDs (the ID is the audit identity).
func NewSelectorPlanner(cfg SelectorConfig) (*SelectorPlanner, error) {
	if len(cfg.Children) < 2 {
		return nil, fmt.Errorf("llm: selector needs at least 2 strategy children, got %d", len(cfg.Children))
	}
	seen := make(map[string]bool, len(cfg.Children))
	for _, c := range cfg.Children {
		if c.ID == "" || c.Planner == nil {
			return nil, fmt.Errorf("llm: selector child needs an ID and a planner")
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("llm: duplicate selector child ID %q", c.ID)
		}
		seen[c.ID] = true
	}
	margin := cfg.IncumbentMargin
	if margin == 0 {
		margin = defaultIncumbentMargin
	}
	return &SelectorPlanner{
		children: cfg.Children,
		weights:  cfg.Weights,
		regime:   cfg.Regime,
		margin:   margin,
		inner:    cfg.Inner,
		prompt:   cfg.PromptTemplate,
	}, nil
}

// Name implements Planner.
func (*SelectorPlanner) Name() string { return "multi:selector" }

// GeneratePlan implements Planner.
func (p *SelectorPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	// Classify the regime first; a failed or unknown classification gates
	// nothing (honesty: never filter on a label we couldn't compute).
	regime := ""
	if p.regime != nil {
		if r, err := p.regime(ctx, req.Goal); err == nil {
			regime = r
		}
	}

	eligible := p.eligibleChildren(regime)
	gateNote := ""
	if len(eligible) == 0 {
		// Gating must narrow the field, never empty it.
		eligible = p.children
		gateNote = fmt.Sprintf("regime %q gated out every strategy; competing all instead", regime)
	}

	// Children never see the incumbent: it exists only for selection
	// hysteresis, and keeping it out of child requests keeps it out of child
	// cache keys.
	childReq := req
	childReq.CurrentStrategy = ""

	results := make([]PlanResult, len(eligible))
	errs := make([]error, len(eligible))
	var wg sync.WaitGroup
	for i, c := range eligible {
		wg.Add(1)
		go func(i int, c StrategyChild) {
			defer wg.Done()
			results[i], errs[i] = c.Planner.GeneratePlan(ctx, childReq)
		}(i, c)
	}
	wg.Wait()

	cands := make([]strategy.Candidate, len(eligible))
	var lastErr error
	failed := 0
	for i := range eligible {
		cands[i] = strategy.Candidate{
			ID:          eligible[i].ID,
			PlannerName: eligible[i].Planner.Name(),
			Moves:       results[i].RankedMoves,
			Err:         errs[i],
		}
		if errs[i] != nil {
			failed++
			lastErr = errs[i]
		}
	}
	if failed == len(eligible) {
		return PlanResult{}, fmt.Errorf("llm: all %d strategy children failed: %w", failed, lastErr)
	}

	sel, err := strategy.Select(req.Goal, cands, strategy.Options{
		Weights:         p.weights,
		Regime:          regime,
		Incumbent:       req.CurrentStrategy,
		IncumbentMargin: p.margin,
	})
	if err != nil {
		return PlanResult{}, fmt.Errorf("llm: strategy selection: %w", err)
	}

	winner := results[sel.Winner]
	winnerID := sel.Scored[sel.Winner].ID

	// Disagreement among admissible strategies is honest uncertainty about the
	// decision, quantized to material steps so it can never churn plans on
	// sub-threshold noise. The moves are COPIED before the haircut: the winner
	// may be a cache hit whose slice shares its backing array with the child's
	// plan-cache entry, and an in-place write would corrupt the cached result
	// (compounding the penalty on every subsequent hit).
	penalty := strategy.DisagreementPenalty(sel.Scored, sel.Scored[sel.Winner].TopMoveKey)
	if penalty > 0 {
		moves := append([]domain.RankedMove(nil), winner.RankedMoves...)
		for i := range moves {
			moves[i].Confidence = clampConfidence(moves[i].Confidence - penalty)
		}
		winner.RankedMoves = moves
	}

	contributors := p.buildContributors(eligible, results, errs, sel.Winner)

	// Hybrid narration runs once, on the winner only — the competition never
	// multiplies model cost.
	if p.inner != nil {
		override := req.SystemPromptOverride
		if override == "" {
			override = p.prompt
		}
		r, nerr := p.inner.GeneratePlan(ctx, PlanRequest{
			Goal:                 req.Goal,
			SignalNote:           narrationNote(req.SignalNote, winner.RankedMoves),
			SystemPromptOverride: override,
		})
		if nerr == nil && r.Summary != "" {
			winner.Provenance.ReasoningSummary = r.Summary + " " + winner.Provenance.ReasoningSummary
			contributors = append(contributors, domain.ModelContribution{
				Planner:          p.inner.Name(),
				Model:            r.Invocation.Model,
				Role:             "narrator",
				PromptTokens:     r.Invocation.PromptTokens,
				CompletionTokens: r.Invocation.CompletionTokens,
			})
		}
	}

	winner.Provenance.Strategy = "selector"
	winner.Provenance.SelectedStrategy = winnerID
	winner.Provenance.Regime = regime
	winner.Provenance.StrategyCandidates = buildCandidateAudit(sel.Scored, cands)
	winner.Provenance.Contributors = contributors
	winner.Provenance.Notes = composeSelectorNotes(sel.Reason, gateNote, penalty)
	return winner, nil
}

// eligibleChildren filters children by declared regime applicability. An
// unknown regime ("") and children with no declared regimes always pass.
func (p *SelectorPlanner) eligibleChildren(regime string) []StrategyChild {
	if regime == "" {
		return p.children
	}
	var out []StrategyChild
	for _, c := range p.children {
		if len(c.Regimes) == 0 || containsString(c.Regimes, regime) {
			out = append(out, c)
		}
	}
	return out
}

// buildContributors records every child that produced a plan, marking the
// winner's role distinctly so the competition is auditable end to end.
func (p *SelectorPlanner) buildContributors(eligible []StrategyChild, results []PlanResult, errs []error, winner int) []domain.ModelContribution {
	contributors := make([]domain.ModelContribution, 0, len(eligible))
	for i := range eligible {
		if errs[i] != nil {
			continue
		}
		role := "strategy-candidate"
		if i == winner {
			role = "strategy-selected"
		}
		contributors = append(contributors, contribOf(results[i], role))
	}
	return contributors
}

// buildCandidateAudit maps the selection's scored records into provenance.
func buildCandidateAudit(scored []strategy.Scored, cands []strategy.Candidate) []domain.StrategyCandidate {
	out := make([]domain.StrategyCandidate, len(scored))
	for i, s := range scored {
		out[i] = domain.StrategyCandidate{
			StrategyID:    s.ID,
			Planner:       cands[i].PlannerName,
			UtilityScore:  s.Utility,
			Weight:        s.Weight,
			TopMoveKey:    s.TopMoveKey,
			TopConfidence: s.TopConfidence,
			Filtered:      s.Filtered,
			Reason:        s.Reason,
		}
	}
	return out
}

func composeSelectorNotes(reason, gateNote string, penalty float64) string {
	parts := []string{reason}
	if gateNote != "" {
		parts = append(parts, gateNote)
	}
	if penalty > 0 {
		parts = append(parts, fmt.Sprintf("strategy disagreement: confidence reduced by %.2f", penalty))
	}
	return strings.Join(parts, " | ")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
