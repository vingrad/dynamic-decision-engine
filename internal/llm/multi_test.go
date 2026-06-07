package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// fakePlanner is a deterministic in-memory Planner for composite tests.
type fakePlanner struct {
	name  string
	model string
	moves []domain.RankedMove
	err   error
}

func (f *fakePlanner) Name() string { return f.name }

func (f *fakePlanner) GeneratePlan(_ context.Context, _ PlanRequest) (PlanResult, error) {
	if f.err != nil {
		return PlanResult{}, f.err
	}
	return PlanResult{
		Summary:     "summary from " + f.name,
		RankedMoves: append([]domain.RankedMove(nil), f.moves...),
		Provenance:  domain.DecisionProvenance{Planner: f.name, Model: f.model},
		Invocation:  domain.ModelInvocation{Model: f.model, PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

// fakeVerifier returns a fixed verdict.
type fakeVerifier struct {
	name    string
	verdict Verdict
	err     error
}

func (f *fakeVerifier) VerifierName() string { return f.name }

func (f *fakeVerifier) VerifyPlan(_ context.Context, _ domain.Goal, _ PlanResult) (Verdict, domain.ModelInvocation, error) {
	if f.err != nil {
		return Verdict{}, domain.ModelInvocation{}, f.err
	}
	return f.verdict, domain.ModelInvocation{Model: "verifier-model", PromptTokens: 7, CompletionTokens: 3}, nil
}

func move(title string, conf float64) domain.RankedMove {
	return domain.RankedMove{Title: title, Confidence: conf, ExpectedImpact: domain.LevelHigh, Effort: domain.LevelMedium, Risk: domain.LevelLow}
}

func TestVerifyPlannerAppliesVerdict(t *testing.T) {
	proposer := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{
		move("A", 0.9), move("B", 0.8), move("C", 0.7),
	}}
	adj := 0.5
	verifier := &fakeVerifier{name: "openai", verdict: Verdict{
		OverallNote: "looks mostly fine",
		Moves: []MoveVerdict{
			{Title: "A", Keep: true, AdjustedConfidence: &adj},
			{Title: "B", Keep: false, Issues: []string{"unsupported by context"}},
			{Title: "C", Keep: true},
		},
	}}

	res, err := NewVerifyPlanner(proposer, verifier).GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RankedMoves) != 2 {
		t.Fatalf("expected B dropped → 2 moves, got %d", len(res.RankedMoves))
	}
	if res.RankedMoves[0].Title != "A" || res.RankedMoves[0].Confidence != 0.5 {
		t.Errorf("A confidence should be adjusted to 0.5, got %v", res.RankedMoves[0].Confidence)
	}
	if res.RankedMoves[1].Rank != 2 {
		t.Errorf("surviving moves should be re-ranked, got rank %d", res.RankedMoves[1].Rank)
	}
	if res.Provenance.Strategy != "verify" || len(res.Provenance.Contributors) != 2 {
		t.Errorf("expected verify strategy + 2 contributors, got %q / %d", res.Provenance.Strategy, len(res.Provenance.Contributors))
	}
	if res.Provenance.Notes == "" {
		t.Error("expected verifier notes")
	}
}

func TestVerifyPlannerDegradesOnVerifierError(t *testing.T) {
	proposer := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{move("A", 0.9)}}
	verifier := &fakeVerifier{name: "openai", err: errors.New("boom")}
	res, err := NewVerifyPlanner(proposer, verifier).GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RankedMoves) != 1 {
		t.Fatalf("expected proposal kept, got %d moves", len(res.RankedMoves))
	}
	if res.Provenance.Strategy != "verify" || res.Provenance.Notes == "" {
		t.Errorf("expected degraded note, got %q / %q", res.Provenance.Strategy, res.Provenance.Notes)
	}
}

func TestRouterEscalatesOnLowConfidence(t *testing.T) {
	cheap := &fakePlanner{name: "deepseek", model: "deepseek-chat", moves: []domain.RankedMove{move("A", 0.3)}}
	strong := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{move("Z", 0.85)}}
	r := NewRouterPlanner(cheap, strong, 0.6, true)

	res, err := r.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RankedMoves[0].Title != "Z" {
		t.Errorf("low cheap confidence should escalate to strong, got %q", res.RankedMoves[0].Title)
	}
	if res.Provenance.Strategy != "route" {
		t.Errorf("expected route strategy, got %q", res.Provenance.Strategy)
	}
}

func TestRouterUsesCheapWhenConfident(t *testing.T) {
	cheap := &fakePlanner{name: "deepseek", model: "deepseek-chat", moves: []domain.RankedMove{move("A", 0.9)}}
	strong := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{move("Z", 0.85)}}
	res, err := NewRouterPlanner(cheap, strong, 0.6, true).GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RankedMoves[0].Title != "A" {
		t.Errorf("confident cheap plan should be kept, got %q", res.RankedMoves[0].Title)
	}
}

func TestRouterEscalatesOnSignal(t *testing.T) {
	cheap := &fakePlanner{name: "deepseek", model: "deepseek-chat", moves: []domain.RankedMove{move("A", 0.99)}}
	strong := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{move("Z", 0.85)}}
	res, err := NewRouterPlanner(cheap, strong, 0.6, true).GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}, SignalNote: "competitor: launched"})
	if err != nil {
		t.Fatal(err)
	}
	if res.RankedMoves[0].Title != "Z" {
		t.Errorf("a signal should route to strong, got %q", res.RankedMoves[0].Title)
	}
}

func TestEnsembleAgreementScalesConfidence(t *testing.T) {
	// 2 of 3 agree on top move "A".
	a := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{move("A", 0.9)}}
	b := &fakePlanner{name: "openai", model: "gpt", moves: []domain.RankedMove{move("A", 0.8)}}
	c := &fakePlanner{name: "deepseek", model: "deepseek-chat", moves: []domain.RankedMove{move("B", 0.7)}}

	res, err := NewEnsemblePlanner(a, b, c).GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RankedMoves[0].Title != "A" {
		t.Fatalf("primary should be first planner's top move A, got %q", res.RankedMoves[0].Title)
	}
	// agreement ratio 2/3 → 0.9 * 0.666... ≈ 0.6
	got := res.RankedMoves[0].Confidence
	if got < 0.59 || got > 0.61 {
		t.Errorf("confidence should scale by 2/3 agreement (~0.6), got %v", got)
	}
	if res.Provenance.Strategy != "ensemble" || len(res.Provenance.Contributors) != 3 {
		t.Errorf("expected ensemble strategy + 3 contributors, got %q / %d", res.Provenance.Strategy, len(res.Provenance.Contributors))
	}
}

func TestEnsembleDropsFailures(t *testing.T) {
	a := &fakePlanner{name: "anthropic", model: "claude", moves: []domain.RankedMove{move("A", 0.9)}}
	bad := &fakePlanner{name: "openai", err: errors.New("down")}
	res, err := NewEnsemblePlanner(a, bad).GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	// Only one member succeeded → unanimous among survivors, confidence unchanged.
	if len(res.Provenance.Contributors) != 1 {
		t.Errorf("expected 1 surviving contributor, got %d", len(res.Provenance.Contributors))
	}
	if res.RankedMoves[0].Confidence != 0.9 {
		t.Errorf("single survivor should keep confidence 0.9, got %v", res.RankedMoves[0].Confidence)
	}
}

func TestVerifySchemaShape(t *testing.T) {
	properties, required := verifySchema()
	if _, ok := properties["moves"]; !ok {
		t.Fatal("verify schema missing moves")
	}
	if len(required) == 0 {
		t.Fatal("verify schema should declare required fields")
	}
}
