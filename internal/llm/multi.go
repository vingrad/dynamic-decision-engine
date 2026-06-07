package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// This file composes single-model planners into multi-model strategies, all of
// which themselves implement Planner so the engine is unaware of the difference.
// Every contributing model is recorded in provenance for auditability.

// --- Verify: propose with one model, critique with another -------------------

// VerifyPlanner has a proposer generate a plan, then a (preferably different
// provider) verifier critique it: dropping weak moves and re-calibrating
// confidence. If verification fails, it degrades to the unverified proposal.
type VerifyPlanner struct {
	proposer Planner
	verifier PlanVerifier
}

// NewVerifyPlanner composes a proposer and a verifier.
func NewVerifyPlanner(proposer Planner, verifier PlanVerifier) *VerifyPlanner {
	return &VerifyPlanner{proposer: proposer, verifier: verifier}
}

// Name implements Planner.
func (*VerifyPlanner) Name() string { return "multi:verify" }

// GeneratePlan implements Planner.
func (p *VerifyPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	res, err := p.proposer.GeneratePlan(ctx, req)
	if err != nil {
		return PlanResult{}, err
	}
	proposerContrib := contribOf(res, "proposer")
	res.Provenance.Strategy = "verify"

	verdict, vinv, verr := p.verifier.VerifyPlan(ctx, req.Goal, res)
	if verr != nil {
		// Degrade gracefully: keep the unverified proposal, note why.
		res.Provenance.Notes = "verification skipped: " + verr.Error()
		res.Provenance.Contributors = []domain.ModelContribution{proposerContrib}
		return res, nil
	}

	original := res.RankedMoves
	res, issues := applyVerdict(res, verdict)
	if len(res.RankedMoves) == 0 {
		// Don't return an empty plan if the verifier rejected everything.
		res.RankedMoves = original
		issues = append(issues, "verifier rejected all moves; kept the original proposal")
	}

	res.Provenance.Notes = composeNotes(verdict.OverallNote, issues)
	res.Provenance.Contributors = []domain.ModelContribution{
		proposerContrib,
		{
			Planner:          p.verifier.VerifierName(),
			Model:            vinv.Model,
			Role:             "verifier",
			PromptTokens:     vinv.PromptTokens,
			CompletionTokens: vinv.CompletionTokens,
		},
	}
	return res, nil
}

// --- Route: cheap model first, escalate to a stronger one when it matters -----

// RouterPlanner runs a cheap planner by default and escalates to a stronger one
// when a material signal arrives or the cheap plan's top-move confidence is below
// a threshold.
type RouterPlanner struct {
	cheap            Planner
	strong           Planner
	threshold        float64
	escalateOnSignal bool
}

// NewRouterPlanner composes a cheap and a strong planner.
func NewRouterPlanner(cheap, strong Planner, threshold float64, escalateOnSignal bool) *RouterPlanner {
	return &RouterPlanner{cheap: cheap, strong: strong, threshold: threshold, escalateOnSignal: escalateOnSignal}
}

// Name implements Planner.
func (*RouterPlanner) Name() string { return "multi:route" }

// GeneratePlan implements Planner.
func (p *RouterPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	// A material signal goes straight to the strong model.
	if p.escalateOnSignal && req.SignalNote != "" {
		res, err := p.strong.GeneratePlan(ctx, req)
		if err != nil {
			return PlanResult{}, err
		}
		return annotateRoute(res, "routed to strong model: a new signal triggered re-planning"), nil
	}

	res, err := p.cheap.GeneratePlan(ctx, req)
	if err != nil {
		// Cheap model failed; fall back to the strong one.
		res, err = p.strong.GeneratePlan(ctx, req)
		if err != nil {
			return PlanResult{}, err
		}
		return annotateRoute(res, "routed to strong model: cheap model failed"), nil
	}

	if topConfidence(res) < p.threshold {
		strongRes, serr := p.strong.GeneratePlan(ctx, req)
		if serr == nil {
			return annotateRoute(strongRes, fmt.Sprintf(
				"escalated to strong model: top-move confidence %.2f below threshold %.2f",
				topConfidence(res), p.threshold)), nil
		}
		// Strong model failed; keep the cheap result with a note.
		return annotateRoute(res, "kept cheap model: escalation attempt failed"), nil
	}

	return annotateRoute(res, "handled by cheap model: confidence above threshold"), nil
}

func annotateRoute(res PlanResult, note string) PlanResult {
	res.Provenance.Strategy = "route"
	res.Provenance.Notes = note
	res.Provenance.Contributors = []domain.ModelContribution{contribOf(res, "router-selected")}
	return res
}

// --- Ensemble: run several models, use agreement as a confidence signal -------

// EnsemblePlanner runs several planners in parallel on the same goal and uses
// inter-model agreement on the top move as an uncertainty signal: it scales the
// top-move confidence by the fraction of models that agree.
type EnsemblePlanner struct {
	planners []Planner
}

// NewEnsemblePlanner composes two or more planners.
func NewEnsemblePlanner(planners ...Planner) *EnsemblePlanner {
	return &EnsemblePlanner{planners: planners}
}

// Name implements Planner.
func (*EnsemblePlanner) Name() string { return "multi:ensemble" }

// GeneratePlan implements Planner.
func (p *EnsemblePlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	if len(p.planners) == 0 {
		return PlanResult{}, fmt.Errorf("llm: ensemble has no planners")
	}

	results := make([]PlanResult, len(p.planners))
	errs := make([]error, len(p.planners))
	var wg sync.WaitGroup
	for i, pl := range p.planners {
		wg.Add(1)
		go func(i int, pl Planner) {
			defer wg.Done()
			results[i], errs[i] = pl.GeneratePlan(ctx, req)
		}(i, pl)
	}
	wg.Wait()

	var ok []PlanResult
	var lastErr error
	for i := range results {
		if errs[i] != nil {
			lastErr = errs[i]
			continue
		}
		ok = append(ok, results[i])
	}
	if len(ok) == 0 {
		return PlanResult{}, fmt.Errorf("llm: all ensemble members failed: %w", lastErr)
	}

	primary := ok[0]
	if len(primary.RankedMoves) == 0 {
		return primary, nil
	}

	// Agreement = fraction of members whose top move matches the primary's.
	topTitle := primary.RankedMoves[0].Title
	agree := 0
	contributors := make([]domain.ModelContribution, 0, len(ok))
	for _, r := range ok {
		contributors = append(contributors, contribOf(r, "ensemble-member"))
		if len(r.RankedMoves) > 0 && r.RankedMoves[0].Title == topTitle {
			agree++
		}
	}
	ratio := float64(agree) / float64(len(ok))

	// Scale top-move confidence by agreement: unanimous leaves it unchanged, a
	// split reduces it. Honest uncertainty rather than an artificial boost.
	primary.RankedMoves[0].Confidence = clampConfidence(primary.RankedMoves[0].Confidence * ratio)

	primary.Provenance.Strategy = "ensemble"
	primary.Provenance.Notes = fmt.Sprintf(
		"%d/%d models agreed on the top move %q (agreement %.0f%%); top-move confidence scaled by agreement.",
		agree, len(ok), topTitle, ratio*100)
	primary.Provenance.Contributors = contributors
	return primary, nil
}

// --- helpers ----------------------------------------------------------------

func topConfidence(res PlanResult) float64 {
	if len(res.RankedMoves) == 0 {
		return 0
	}
	return res.RankedMoves[0].Confidence
}

func contribOf(res PlanResult, role string) domain.ModelContribution {
	return domain.ModelContribution{
		Planner:          res.Provenance.Planner,
		Model:            res.Provenance.Model,
		Role:             role,
		PromptTokens:     res.Invocation.PromptTokens,
		CompletionTokens: res.Invocation.CompletionTokens,
	}
}

func composeNotes(overall string, issues []string) string {
	if len(issues) == 0 {
		return overall
	}
	joined := joinIssues(issues)
	if overall == "" {
		return joined
	}
	return overall + " | " + joined
}
