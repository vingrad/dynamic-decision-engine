package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// strategyMove builds a keyed move so selection and disagreement work on
// stable identities, the way real planners emit them.
func strategyMove(key string, conf float64, impact, effort, risk domain.Level) domain.RankedMove {
	return domain.RankedMove{
		Rank: 1, Key: key, Title: key, Confidence: conf, RawConfidence: conf,
		ExpectedImpact: impact, Effort: effort, Risk: risk,
	}
}

func selectorGoal() domain.Goal {
	return domain.Goal{ID: "g1", Domain: "investing", Objective: "grow the portfolio"}
}

func TestNewSelectorPlannerValidation(t *testing.T) {
	one := []StrategyChild{{ID: "value", Planner: &fakePlanner{name: "finance:value"}}}
	if _, err := NewSelectorPlanner(SelectorConfig{Children: one}); err == nil {
		t.Error("one child must be rejected")
	}
	dup := []StrategyChild{
		{ID: "value", Planner: &fakePlanner{name: "a"}},
		{ID: "value", Planner: &fakePlanner{name: "b"}},
	}
	if _, err := NewSelectorPlanner(SelectorConfig{Children: dup}); err == nil {
		t.Error("duplicate IDs must be rejected")
	}
}

func TestSelectorPicksWinnerAndStampsProvenance(t *testing.T) {
	weak := &fakePlanner{name: "finance:value", model: "none", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.4, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	strong := &fakePlanner{name: "finance:momentum", model: "none", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	}}
	p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
		{ID: "value", Planner: weak},
		{ID: "momentum", Planner: strong},
	}})
	if err != nil {
		t.Fatal(err)
	}

	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.Strategy != "selector" {
		t.Errorf("Strategy = %q, want selector", res.Provenance.Strategy)
	}
	if res.Provenance.SelectedStrategy != "momentum" {
		t.Errorf("SelectedStrategy = %q, want momentum", res.Provenance.SelectedStrategy)
	}
	if len(res.Provenance.StrategyCandidates) != 2 {
		t.Fatalf("expected 2 strategy candidates in provenance, got %d", len(res.Provenance.StrategyCandidates))
	}
	for _, c := range res.Provenance.StrategyCandidates {
		if c.StrategyID == "" || c.Planner == "" || c.Reason == "" {
			t.Errorf("candidate audit incomplete: %+v", c)
		}
	}
	roles := map[string]int{}
	for _, c := range res.Provenance.Contributors {
		roles[c.Role]++
	}
	if roles["strategy-selected"] != 1 || roles["strategy-candidate"] != 1 {
		t.Errorf("contributor roles = %v, want one selected + one candidate", roles)
	}
	// Both children agree on the top move key → no disagreement penalty.
	if got := res.RankedMoves[0].Confidence; got != 0.8 {
		t.Errorf("agreed competition must not haircut confidence, got %v", got)
	}
}

func TestSelectorDisagreementPenalty(t *testing.T) {
	a := &fakePlanner{name: "finance:value", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	}}
	b := &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:GLOBEX", 0.4, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
		{ID: "value", Planner: a},
		{ID: "momentum", Planner: b},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	// Two admissible candidates, split top keys → agreement 1/2 → penalty 0.05.
	if got := res.RankedMoves[0].Confidence; got != 0.75 {
		t.Errorf("split competition should haircut 0.05: confidence = %v, want 0.75", got)
	}
	if res.RankedMoves[0].RawConfidence != 0.8 {
		t.Errorf("RawConfidence must stay untouched, got %v", res.RankedMoves[0].RawConfidence)
	}
}

func TestSelectorChildErrorIsFilteredCandidate(t *testing.T) {
	bad := &fakePlanner{name: "finance:value", err: errors.New("provider down")}
	good := &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
		{ID: "value", Planner: bad},
		{ID: "momentum", Planner: good},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.SelectedStrategy != "momentum" {
		t.Errorf("winner = %q, want momentum", res.Provenance.SelectedStrategy)
	}
	var errored *domain.StrategyCandidate
	for i := range res.Provenance.StrategyCandidates {
		if res.Provenance.StrategyCandidates[i].StrategyID == "value" {
			errored = &res.Provenance.StrategyCandidates[i]
		}
	}
	if errored == nil || !errored.Filtered {
		t.Errorf("errored child must appear as a filtered candidate: %+v", errored)
	}
}

func TestSelectorAllChildrenFailed(t *testing.T) {
	p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
		{ID: "value", Planner: &fakePlanner{name: "a", err: errors.New("down")}},
		{ID: "momentum", Planner: &fakePlanner{name: "b", err: errors.New("also down")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()}); err == nil {
		t.Error("all children failing must surface an error")
	}
}

func TestSelectorRegimeGating(t *testing.T) {
	trendOnly := &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.9, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	}}
	always := &fakePlanner{name: "finance:defensive", moves: []domain.RankedMove{
		strategyMove("thesis:GLOBEX", 0.5, domain.LevelMedium, domain.LevelLow, domain.LevelLow),
	}}
	mk := func(regime string) *SelectorPlanner {
		p, err := NewSelectorPlanner(SelectorConfig{
			Children: []StrategyChild{
				{ID: "momentum", Planner: trendOnly, Regimes: []string{"trend"}},
				{ID: "defensive", Planner: always},
			},
			Regime: func(context.Context, domain.Goal) (string, error) { return regime, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	// In a high_vol regime momentum is gated out: only defensive competes.
	res, err := mk("high_vol").GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.SelectedStrategy != "defensive" || len(res.Provenance.StrategyCandidates) != 1 {
		t.Errorf("high_vol should gate momentum out: winner %q, %d candidates",
			res.Provenance.SelectedStrategy, len(res.Provenance.StrategyCandidates))
	}
	if res.Provenance.Regime != "high_vol" {
		t.Errorf("Regime = %q, want high_vol", res.Provenance.Regime)
	}

	// Unknown regime gates nothing.
	res, err = mk("").GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Provenance.StrategyCandidates) != 2 {
		t.Errorf("unknown regime must not gate, got %d candidates", len(res.Provenance.StrategyCandidates))
	}
}

func TestSelectorRegimeGateNeverEmptiesField(t *testing.T) {
	a := &fakePlanner{name: "finance:value", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	b := &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.7, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	p, err := NewSelectorPlanner(SelectorConfig{
		Children: []StrategyChild{
			{ID: "value", Planner: a, Regimes: []string{"range"}},
			{ID: "momentum", Planner: b, Regimes: []string{"trend"}},
		},
		Regime: func(context.Context, domain.Goal) (string, error) { return "high_vol", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Provenance.StrategyCandidates) != 2 {
		t.Errorf("a gate that empties the field must fall back to all children, got %d", len(res.Provenance.StrategyCandidates))
	}
}

func TestSelectorHysteresisHoldsIncumbent(t *testing.T) {
	inc := &fakePlanner{name: "finance:value", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.60, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	chal := &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.64, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
		{ID: "value", Planner: inc},
		{ID: "momentum", Planner: chal},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// On a replan with the incumbent set, a marginally better challenger holds.
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal(), CurrentStrategy: "value"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.SelectedStrategy != "value" {
		t.Errorf("incumbent should hold within the default margin, got %q", res.Provenance.SelectedStrategy)
	}

	// Initial plan (no incumbent): the better candidate wins outright.
	res, err = p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.SelectedStrategy != "momentum" {
		t.Errorf("without an incumbent the better candidate wins, got %q", res.Provenance.SelectedStrategy)
	}
}

// capturePlanner captures the requests it receives.
type capturePlanner struct {
	fakePlanner
	gotStrategy []string
	gotOverride []string
}

func (r *capturePlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	r.gotStrategy = append(r.gotStrategy, req.CurrentStrategy)
	r.gotOverride = append(r.gotOverride, req.SystemPromptOverride)
	return r.fakePlanner.GeneratePlan(ctx, req)
}

func TestSelectorChildrenNeverSeeIncumbentAndNarratorRunsOnce(t *testing.T) {
	child1 := &capturePlanner{fakePlanner: fakePlanner{name: "finance:value", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}}
	child2 := &capturePlanner{fakePlanner: fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.7, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}}
	narrator := &capturePlanner{fakePlanner: fakePlanner{name: "mock", model: "mock-1", moves: nil}}

	p, err := NewSelectorPlanner(SelectorConfig{
		Children: []StrategyChild{
			{ID: "value", Planner: child1},
			{ID: "momentum", Planner: child2},
		},
		Inner:          narrator,
		PromptTemplate: "DOMAIN: INVESTING",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal(), CurrentStrategy: "value"})
	if err != nil {
		t.Fatal(err)
	}

	for _, got := range append(child1.gotStrategy, child2.gotStrategy...) {
		if got != "" {
			t.Errorf("children must never see the incumbent, got %q", got)
		}
	}
	if n := len(narrator.gotStrategy); n != 1 {
		t.Fatalf("narrator must run exactly once, ran %d times", n)
	}
	if narrator.gotOverride[0] != "DOMAIN: INVESTING" {
		t.Errorf("narrator must receive the pack template, got %q", narrator.gotOverride[0])
	}
	var narrated int
	for _, c := range res.Provenance.Contributors {
		if c.Role == "narrator" {
			narrated++
		}
	}
	if narrated != 1 {
		t.Errorf("expected one narrator contribution, got %d", narrated)
	}
}

func TestSelectorDeterministicAcrossRuns(t *testing.T) {
	mk := func() *SelectorPlanner {
		p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
			{ID: "value", Planner: &fakePlanner{name: "finance:value", moves: []domain.RankedMove{
				strategyMove("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
			}}},
			{ID: "momentum", Planner: &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
				strategyMove("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
			}}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Identical candidates tie all the way down; canonical order must decide,
	// regardless of goroutine completion order, on every run.
	for i := 0; i < 50; i++ {
		res, err := mk().GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
		if err != nil {
			t.Fatal(err)
		}
		if res.Provenance.SelectedStrategy != "value" {
			t.Fatalf("run %d: winner %q, want canonical-order value", i, res.Provenance.SelectedStrategy)
		}
	}
}

// TestSelectorPenaltyDoesNotCorruptChildCache: the disagreement haircut must
// never write into a cached child result. With cached children and a split
// competition, repeated identical requests must ship the SAME confidence —
// an in-place penalty would compound through the shared backing array on
// every cache hit.
func TestSelectorPenaltyDoesNotCorruptChildCache(t *testing.T) {
	a := &fakePlanner{name: "finance:value", moves: []domain.RankedMove{
		strategyMove("thesis:ACME", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	}}
	b := &fakePlanner{name: "finance:momentum", moves: []domain.RankedMove{
		strategyMove("thesis:GLOBEX", 0.4, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	cache := NewMemoryCache(16)
	p, err := NewSelectorPlanner(SelectorConfig{Children: []StrategyChild{
		{ID: "value", Planner: NewCachingPlanner(a, cache, nil)},
		{ID: "momentum", Planner: NewCachingPlanner(b, cache, nil)},
	}})
	if err != nil {
		t.Fatal(err)
	}

	req := PlanRequest{Goal: selectorGoal()}
	for i := 0; i < 3; i++ {
		res, err := p.GeneratePlan(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		// Split top keys → penalty 0.05 → 0.75, on EVERY request identically.
		if got := res.RankedMoves[0].Confidence; got != 0.75 {
			t.Fatalf("request %d: confidence = %v, want 0.75 (penalty must not compound through the cache)", i+1, got)
		}
	}
	// The cached child entry itself must be untouched.
	if cached, ok := cache.Get(cacheKey("finance:value", req)); !ok {
		t.Fatal("expected a cached child entry")
	} else if got := cached.RankedMoves[0].Confidence; got != 0.8 {
		t.Errorf("cached child confidence = %v, want pristine 0.8", got)
	}
}

// scriptedVerifier returns canned verdicts in call order, or an error after
// failAfter successful calls (0 = never fail).
type scriptedVerifier struct {
	verdicts  []Verdict
	failAfter int
	calls     int
}

func (s *scriptedVerifier) VerifierName() string { return "scripted" }

func (s *scriptedVerifier) VerifyPlan(_ context.Context, _ domain.Goal, _ PlanResult) (Verdict, domain.ModelInvocation, error) {
	s.calls++
	if s.failAfter > 0 && s.calls > s.failAfter {
		return Verdict{}, domain.ModelInvocation{}, errors.New("verifier down")
	}
	v := s.verdicts[0]
	if len(s.verdicts) > 1 {
		s.verdicts = s.verdicts[1:]
	}
	return v, domain.ModelInvocation{Model: "scripted-1"}, nil
}

func TestSelectorVerifyComparatorAdjustsWinner(t *testing.T) {
	// Bold self-reports 0.9; the reviewer cuts it to 0.3. Safe stays at 0.6.
	bold := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act-bold", 0.9, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	}}
	safe := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act-safe", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelLow),
	}}
	adj := 0.3
	verifier := &scriptedVerifier{verdicts: []Verdict{
		{Moves: []MoveVerdict{{Title: "act-bold", Keep: true, AdjustedConfidence: &adj}}},
		{Moves: []MoveVerdict{{Title: "act-safe", Keep: true}}},
	}}
	p, err := NewSelectorPlanner(SelectorConfig{
		Children: []StrategyChild{
			{ID: "bold", Planner: bold},
			{ID: "safe", Planner: safe},
		},
		Reviewer: NewVerifyReviewer(verifier),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.SelectedStrategy != "safe" {
		t.Errorf("review should flip the winner to safe, got %q", res.Provenance.SelectedStrategy)
	}
	if res.Provenance.Comparator != "verify" {
		t.Errorf("Comparator = %q, want verify", res.Provenance.Comparator)
	}
	var verifierContribs int
	for _, c := range res.Provenance.Contributors {
		if c.Role == "candidate-verifier" {
			verifierContribs++
		}
	}
	if verifierContribs != 2 {
		t.Errorf("expected 2 candidate-verifier contributions, got %d", verifierContribs)
	}
}

func TestSelectorVerifyComparatorAllOrNothing(t *testing.T) {
	bold := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act-bold", 0.9, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	}}
	safe := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act-safe", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelLow),
	}}
	adj := 0.3
	// First candidate reviews fine, second errors: ALL adjustments discarded.
	verifier := &scriptedVerifier{
		verdicts:  []Verdict{{Moves: []MoveVerdict{{Title: "act-bold", Keep: true, AdjustedConfidence: &adj}}}},
		failAfter: 1,
	}
	p, err := NewSelectorPlanner(SelectorConfig{
		Children: []StrategyChild{
			{ID: "bold", Planner: bold},
			{ID: "safe", Planner: safe},
		},
		Reviewer: NewVerifyReviewer(verifier),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	// Raw numbers stand: bold's 0.9 (minus the 0.05 disagreement haircut on
	// split top keys) wins, and provenance says why review was skipped.
	if res.Provenance.SelectedStrategy != "bold" {
		t.Errorf("degraded competition must use raw numbers: winner %q", res.Provenance.SelectedStrategy)
	}
	if res.Provenance.Comparator != "utility" {
		t.Errorf("degraded Comparator = %q, want utility", res.Provenance.Comparator)
	}
	if !strings.Contains(res.Provenance.Notes, "degraded to utility") {
		t.Errorf("notes must record the degradation, got %q", res.Provenance.Notes)
	}
}

func TestSelectorVerifyComparatorMissingVerifier(t *testing.T) {
	a := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	b := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act", 0.7, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	p, err := NewSelectorPlanner(SelectorConfig{
		Children: []StrategyChild{{ID: "a", Planner: a}, {ID: "b", Planner: b}},
		Reviewer: NewVerifyReviewer(nil), // policy asked for verify; no capable client
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.Comparator != "utility" || !strings.Contains(res.Provenance.Notes, "no verifier-capable client") {
		t.Errorf("missing verifier must degrade with the reason recorded: %+v", res.Provenance)
	}
}

// TestSelectorVerifyRejectingAllMovesFiltersCandidate: a candidate whose moves
// are all dropped by the reviewer becomes inadmissible rather than restored.
func TestSelectorVerifyRejectingAllMovesFiltersCandidate(t *testing.T) {
	junk := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act-junk", 0.95, domain.LevelHigh, domain.LevelLow, domain.LevelLow),
	}}
	solid := &fakePlanner{name: "base", moves: []domain.RankedMove{
		strategyMove("act-solid", 0.5, domain.LevelMedium, domain.LevelLow, domain.LevelMedium),
	}}
	verifier := &scriptedVerifier{verdicts: []Verdict{
		{Moves: []MoveVerdict{{Title: "act-junk", Keep: false, Issues: []string{"unsupported"}}}},
		{Moves: []MoveVerdict{{Title: "act-solid", Keep: true}}},
	}}
	p, err := NewSelectorPlanner(SelectorConfig{
		Children: []StrategyChild{
			{ID: "junk", Planner: junk},
			{ID: "solid", Planner: solid},
		},
		Reviewer: NewVerifyReviewer(verifier),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: selectorGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.SelectedStrategy != "solid" {
		t.Errorf("winner = %q, want solid", res.Provenance.SelectedStrategy)
	}
	for _, c := range res.Provenance.StrategyCandidates {
		if c.StrategyID == "junk" && !c.Filtered {
			t.Errorf("fully rejected candidate must be filtered: %+v", c)
		}
	}
}
