package llm

import (
	"context"
	"fmt"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// A CandidateReviewer makes competing candidates' stated numbers more
// comparable BEFORE the pure utility math arbitrates. Text-domain prompt
// variants self-report confidences that share no yardstick; a reviewer
// replaces N self-assessments with N assessments by one consistent reviewer.
// The contract is ALL-OR-NOTHING: the returned slice is parallel to the
// input, and any error means the selector must use the raw candidates for
// every competitor — partially reviewed competitions (adjusted confidences
// compared against raw ones) are strictly worse than unreviewed ones.
//
// "verify" (below) is the per-candidate critique built on the existing
// PlanVerifier capability. A future "judge" reviewer — one model call
// comparing all candidates side by side — slots in behind this same
// interface; its policy enum value is reserved.
type CandidateReviewer interface {
	// Name identifies the comparator mode in provenance (e.g. "verify").
	Name() string
	// ReviewCandidates returns adjusted candidate results, parallel to cands.
	ReviewCandidates(ctx context.Context, goal domain.Goal, cands []PlanResult) ([]PlanResult, []domain.ModelContribution, error)
}

// verifyReviewer critiques every candidate with one PlanVerifier: weak or
// unsupported moves are dropped and miscalibrated confidences adjusted via
// the same verdict semantics the VerifyPlanner uses. A candidate whose moves
// are ALL rejected stays empty — the hard filter then reads it as
// inadmissible, which is exactly what "the reviewer believes none of it"
// should mean in a competition.
type verifyReviewer struct {
	verifier PlanVerifier
}

// NewVerifyReviewer wraps a verifier-capable client as a CandidateReviewer.
// A nil verifier is allowed and fails at review time — the selector's
// all-or-nothing degradation then records WHY the competition ran unreviewed
// instead of the build silently downgrading the comparator.
func NewVerifyReviewer(verifier PlanVerifier) CandidateReviewer {
	return &verifyReviewer{verifier: verifier}
}

// Name implements CandidateReviewer.
func (*verifyReviewer) Name() string { return "verify" }

// ReviewCandidates implements CandidateReviewer.
func (r *verifyReviewer) ReviewCandidates(ctx context.Context, goal domain.Goal, cands []PlanResult) ([]PlanResult, []domain.ModelContribution, error) {
	if r.verifier == nil {
		return nil, nil, fmt.Errorf("llm: comparator verify: no verifier-capable client configured")
	}
	out := make([]PlanResult, len(cands))
	contribs := make([]domain.ModelContribution, 0, len(cands))
	for i, c := range cands {
		verdict, vinv, err := r.verifier.VerifyPlan(ctx, goal, c)
		if err != nil {
			return nil, nil, fmt.Errorf("llm: comparator verify: candidate %d: %w", i, err)
		}
		adjusted, _ := applyVerdict(c, verdict)
		out[i] = adjusted
		contribs = append(contribs, domain.ModelContribution{
			Planner:          r.verifier.VerifierName(),
			Model:            vinv.Model,
			Role:             "candidate-verifier",
			PromptTokens:     vinv.PromptTokens,
			CompletionTokens: vinv.CompletionTokens,
		})
	}
	return out, contribs, nil
}
