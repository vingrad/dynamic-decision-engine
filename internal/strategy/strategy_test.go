package strategy

import (
	"errors"
	"math"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func goalWith(constraints ...domain.Constraint) domain.Goal {
	return domain.Goal{
		ID:        "goal-1",
		Objective: "grow the portfolio",
		Context:   domain.Context{Constraints: constraints},
	}
}

func move(key string, conf float64, impact, effort, risk domain.Level) domain.RankedMove {
	return domain.RankedMove{
		Key: key, Title: key, Confidence: conf, RawConfidence: conf,
		ExpectedImpact: impact, Effort: effort, Risk: risk,
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestUtilityHandComputed(t *testing.T) {
	g := goalWith() // default risk aversion 0.6

	// Single move: v = 0.8*0.9 − 0.6*0.5 − 0.25*0.2 = 0.72 − 0.30 − 0.05 = 0.37.
	u, _ := Utility(g, []domain.RankedMove{
		move("a", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
	})
	if !approx(u, 0.37) {
		t.Errorf("single-move utility = %v, want 0.37", u)
	}

	// Two moves: decay 1, 0.7 normalized.
	// v1 = 0.37 (above); v2 = 0.5*0.5 − 0.6*0.2 − 0.25*0.5 = 0.25 − 0.12 − 0.125 = 0.005
	// U = (1*0.37 + 0.7*0.005) / 1.7 = 0.3735/1.7 = 0.2197...→ round4 0.2197
	u, _ = Utility(g, []domain.RankedMove{
		move("a", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
		move("b", 0.5, domain.LevelMedium, domain.LevelMedium, domain.LevelLow),
	})
	if !approx(u, 0.2197) {
		t.Errorf("two-move utility = %v, want 0.2197", u)
	}
}

func TestUtilityRiskAversion(t *testing.T) {
	moves := []domain.RankedMove{
		move("a", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelHigh),
	}
	uCons, _ := Utility(goalWith(domain.Constraint{Kind: "risk_tolerance", Name: "conservative"}), moves)
	uDef, _ := Utility(goalWith(), moves)
	uAggr, _ := Utility(goalWith(domain.Constraint{Kind: "risk_tolerance", Name: "aggressive"}), moves)
	if !(uCons < uDef && uDef < uAggr) {
		t.Errorf("risk aversion ordering broken: conservative %v, default %v, aggressive %v", uCons, uDef, uAggr)
	}
}

func TestHardFilter(t *testing.T) {
	cases := []struct {
		name     string
		goal     domain.Goal
		moves    []domain.RankedMove
		filtered bool
	}{
		{"no moves", goalWith(), nil, true},
		{"all zero confidence", goalWith(), []domain.RankedMove{
			move("a", 0, domain.LevelLow, domain.LevelLow, domain.LevelHigh),
		}, true},
		{"viable default goal", goalWith(), []domain.RankedMove{
			move("a", 0.6, domain.LevelHigh, domain.LevelLow, domain.LevelHigh),
		}, false},
		{"conservative tolerance rejects high-risk top", goalWith(domain.Constraint{Kind: "risk_tolerance", Name: "conservative"}), []domain.RankedMove{
			move("a", 0.6, domain.LevelHigh, domain.LevelLow, domain.LevelHigh),
		}, true},
		{"tight drawdown limit rejects high-risk top", goalWith(domain.Constraint{Kind: "drawdown_limit", Name: "10%"}), []domain.RankedMove{
			move("a", 0.6, domain.LevelHigh, domain.LevelLow, domain.LevelHigh),
		}, true},
		{"loose drawdown limit allows high-risk top", goalWith(domain.Constraint{Kind: "drawdown_limit", Name: "25%"}), []domain.RankedMove{
			move("a", 0.6, domain.LevelHigh, domain.LevelLow, domain.LevelHigh),
		}, false},
		{"conservative tolerance keeps medium-risk top", goalWith(domain.Constraint{Kind: "risk_tolerance", Name: "conservative"}), []domain.RankedMove{
			move("a", 0.6, domain.LevelHigh, domain.LevelLow, domain.LevelMedium),
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, filtered := HardFilter(tc.goal, tc.moves)
			if filtered != tc.filtered {
				t.Errorf("filtered = %v (%q), want %v", filtered, reason, tc.filtered)
			}
			if filtered && reason == "" {
				t.Error("filtered candidates must carry a reason")
			}
		})
	}
}

func TestSelectPicksHighestWeightedUtility(t *testing.T) {
	g := goalWith()
	cands := []Candidate{
		{ID: "value", Moves: []domain.RankedMove{move("thesis:ACME", 0.5, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}},
		{ID: "momentum", Moves: []domain.RankedMove{move("thesis:ACME", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelMedium)}},
	}
	sel, err := Select(g, cands, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Scored[sel.Winner].ID != "momentum" {
		t.Errorf("winner = %q, want momentum", sel.Scored[sel.Winner].ID)
	}
	if len(sel.Scored) != 2 {
		t.Fatalf("scored must be parallel to candidates, got %d", len(sel.Scored))
	}
	for _, s := range sel.Scored {
		if s.Reason == "" {
			t.Errorf("candidate %q has no audit reason", s.ID)
		}
	}
}

func TestSelectWeights(t *testing.T) {
	g := goalWith()
	same := []domain.RankedMove{move("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}
	cands := []Candidate{
		{ID: "value", Moves: same},
		{ID: "momentum", Moves: same},
	}

	// Plain weight flips the winner.
	sel, err := Select(g, cands, Options{Weights: map[string]float64{"momentum": 1.3}})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Scored[sel.Winner].ID != "momentum" {
		t.Errorf("weighted winner = %q, want momentum", sel.Scored[sel.Winner].ID)
	}

	// Regime-specific entry beats the plain entry.
	sel, err = Select(g, cands, Options{
		Regime:  "trend",
		Weights: map[string]float64{"value": 1.2, "value@trend": 0.5, "momentum": 1.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Scored[sel.Winner].ID != "momentum" {
		t.Errorf("regime-weighted winner = %q, want momentum", sel.Scored[sel.Winner].ID)
	}
}

func TestSelectTieBreaks(t *testing.T) {
	g := goalWith()

	// Identical utility, higher raw confidence wins... but identical moves tie
	// all the way down, so canonical (input) order decides.
	same := []domain.RankedMove{move("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}
	sel, err := Select(g, []Candidate{{ID: "b-strategy", Moves: same}, {ID: "a-strategy", Moves: same}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "b-strategy" {
		t.Errorf("full tie must keep canonical input order, got %q", got)
	}

	// Same utility by construction differs only in raw confidence after
	// calibration flattened the stated confidence.
	a := move("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)
	b := a
	a.RawConfidence = 0.61
	b.RawConfidence = 0.66
	sel, err = Select(g, []Candidate{
		{ID: "first", Moves: []domain.RankedMove{a}},
		{ID: "second", Moves: []domain.RankedMove{b}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "second" {
		t.Errorf("raw-confidence tie-break winner = %q, want second", got)
	}
}

func TestSelectDeterministicUnderPermutation(t *testing.T) {
	g := goalWith()
	mk := func(id string, conf float64) Candidate {
		return Candidate{ID: id, Moves: []domain.RankedMove{move("thesis:"+id, conf, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}}
	}
	// Distinct utilities: the winner must be identical under any input order.
	perm1 := []Candidate{mk("a", 0.4), mk("b", 0.9), mk("c", 0.6)}
	perm2 := []Candidate{mk("c", 0.6), mk("a", 0.4), mk("b", 0.9)}
	s1, err1 := Select(g, perm1, Options{})
	s2, err2 := Select(g, perm2, Options{})
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if s1.Scored[s1.Winner].ID != "b" || s2.Scored[s2.Winner].ID != "b" {
		t.Errorf("permutation changed the winner: %q vs %q", s1.Scored[s1.Winner].ID, s2.Scored[s2.Winner].ID)
	}
}

func TestSelectHysteresis(t *testing.T) {
	g := goalWith()
	inc := []domain.RankedMove{move("thesis:ACME", 0.60, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}
	chal := []domain.RankedMove{move("thesis:GLOBEX", 0.63, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}
	cands := []Candidate{{ID: "value", Moves: inc}, {ID: "momentum", Moves: chal}}

	// Challenger ahead, but within the margin: incumbent holds.
	sel, err := Select(g, cands, Options{Incumbent: "value", IncumbentMargin: 0.05})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "value" {
		t.Errorf("incumbent should hold within margin, winner = %q", got)
	}

	// No margin: the challenger takes it.
	sel, err = Select(g, cands, Options{Incumbent: "value"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "momentum" {
		t.Errorf("without margin the better candidate wins, got %q", got)
	}

	// A filtered incumbent holds nothing.
	brokenInc := []domain.RankedMove{move("thesis:ACME", 0, domain.LevelLow, domain.LevelLow, domain.LevelHigh)}
	sel, err = Select(g, []Candidate{{ID: "value", Moves: brokenInc}, {ID: "momentum", Moves: chal}},
		Options{Incumbent: "value", IncumbentMargin: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "momentum" {
		t.Errorf("filtered incumbent must not hold, winner = %q", got)
	}
}

func TestSelectErrorAndFallback(t *testing.T) {
	g := goalWith(domain.Constraint{Kind: "risk_tolerance", Name: "conservative"})

	// A child error is recorded as filtered, and cannot win.
	ok := []domain.RankedMove{move("thesis:ACME", 0.6, domain.LevelMedium, domain.LevelLow, domain.LevelMedium)}
	sel, err := Select(g, []Candidate{
		{ID: "value", Err: errors.New("provider down")},
		{ID: "momentum", Moves: ok},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "momentum" {
		t.Errorf("winner = %q, want momentum", got)
	}
	if !sel.Scored[0].Filtered {
		t.Error("errored candidate must be marked filtered")
	}

	// All candidates inadmissible: degrade to best inadmissible, never empty.
	risky := []domain.RankedMove{move("thesis:ACME", 0.8, domain.LevelHigh, domain.LevelLow, domain.LevelHigh)}
	riskier := []domain.RankedMove{move("thesis:GLOBEX", 0.4, domain.LevelLow, domain.LevelLow, domain.LevelHigh)}
	sel, err = Select(g, []Candidate{{ID: "value", Moves: risky}, {ID: "momentum", Moves: riskier}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sel.Scored[sel.Winner].ID; got != "value" {
		t.Errorf("degraded winner = %q, want value (higher utility)", got)
	}

	// Empty candidate list is the only hard error.
	if _, err := Select(g, nil, Options{}); err == nil {
		t.Error("empty candidate list must error")
	}
}

func TestDisagreementPenalty(t *testing.T) {
	mk := func(id, top string, filtered bool) Scored {
		return Scored{ID: id, TopMoveKey: top, Filtered: filtered}
	}
	cases := []struct {
		name   string
		scored []Scored
		want   float64
	}{
		{"single admissible", []Scored{mk("a", "x", false), mk("b", "y", true)}, 0},
		{"full agreement", []Scored{mk("a", "x", false), mk("b", "x", false)}, 0},
		{"majority agreement", []Scored{mk("a", "x", false), mk("b", "x", false), mk("c", "y", false)}, 0.05},
		{"minority agreement", []Scored{mk("a", "x", false), mk("b", "y", false), mk("c", "z", false)}, 0.10},
		{"filtered do not vote", []Scored{mk("a", "x", false), mk("b", "y", true), mk("c", "y", true)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DisagreementPenalty(tc.scored, "x"); got != tc.want {
				t.Errorf("penalty = %v, want %v", got, tc.want)
			}
		})
	}
}
