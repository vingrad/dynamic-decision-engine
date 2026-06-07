package llm

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

func financeTestPlanner(t *testing.T) *FinancePlanner {
	t.Helper()
	prov, err := marketdata.NewOfflineProvider()
	if err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	return NewFinancePlanner(FinanceConfig{
		Provider:    prov,
		Now:         func() time.Time { return asOf },
		PackVersion: "1",
	})
}

func investingGoal() domain.Goal {
	return domain.Goal{
		Domain:    "investing",
		Objective: "Build a thesis-driven equity position",
		Context: domain.Context{
			Assets: []domain.Asset{
				{Name: "ACME", Kind: "ticker"},
				{Name: "GLOBEX", Kind: "ticker"},
			},
		},
	}
}

func TestFinancePlannerScoresAndRanks(t *testing.T) {
	p := financeTestPlanner(t)
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RankedMoves) != 2 {
		t.Fatalf("expected 2 scored theses, got %d", len(res.RankedMoves))
	}
	// Ranks are contiguous and confidence is non-increasing.
	for i, m := range res.RankedMoves {
		if m.Rank != i+1 {
			t.Errorf("move %d has rank %d", i, m.Rank)
		}
		if i > 0 && res.RankedMoves[i-1].Confidence < m.Confidence {
			t.Error("confidence should be non-increasing with rank")
		}
	}
	if res.Provenance.Planner != "finance" || res.Provenance.PackID != "investing" {
		t.Errorf("unexpected provenance: %+v", res.Provenance)
	}
	if !strings.Contains(res.Summary, "Not financial advice") {
		t.Errorf("summary must carry the disclaimer: %q", res.Summary)
	}
}

func TestFinancePlannerDeterministic(t *testing.T) {
	p := financeTestPlanner(t)
	a, _ := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	b, _ := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	if len(a.RankedMoves) != len(b.RankedMoves) {
		t.Fatal("nondeterministic move count")
	}
	for i := range a.RankedMoves {
		if a.RankedMoves[i].Title != b.RankedMoves[i].Title || a.RankedMoves[i].Confidence != b.RankedMoves[i].Confidence {
			t.Errorf("nondeterministic output at %d", i)
		}
	}
}

func TestFinancePlannerThesisBreak(t *testing.T) {
	p := financeTestPlanner(t)
	res, err := p.GeneratePlan(context.Background(), PlanRequest{
		Goal: investingGoal(),
		SignalPayload: map[string]any{
			"kind": "thesis_break", "ticker": "ACME", "reason": "core customer lost", "hard": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The broken ACME thesis should be driven to zero confidence and ranked last.
	var acme *domain.RankedMove
	for i := range res.RankedMoves {
		if res.RankedMoves[i].Title == "Thesis: ACME" {
			acme = &res.RankedMoves[i]
		}
	}
	if acme == nil {
		t.Fatal("ACME thesis missing")
	}
	if acme.Confidence != 0 || acme.Risk != domain.LevelHigh {
		t.Errorf("thesis break should zero confidence and raise risk, got conf=%v risk=%v", acme.Confidence, acme.Risk)
	}
	if res.RankedMoves[len(res.RankedMoves)-1].Title != "Thesis: ACME" {
		t.Error("broken thesis should rank last")
	}
}

func TestFinancePlannerUntargetedThesisBreak(t *testing.T) {
	p := financeTestPlanner(t)
	res, err := p.GeneratePlan(context.Background(), PlanRequest{
		Goal: investingGoal(), // two tickers: ACME, GLOBEX
		SignalPayload: map[string]any{
			"kind": "thesis_break", "reason": "regime change", "hard": true,
		}, // no ticker => untargeted
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.RankedMoves {
		if m.Confidence != 0 {
			t.Errorf("untargeted thesis_break should zero all theses, %q has conf %v", m.Title, m.Confidence)
		}
	}
}

func TestFinancePlannerUsesHorizon(t *testing.T) {
	p := financeTestPlanner(t)
	// A goal with a stated horizon and a signal carrying a matching window should
	// score horizon fit above the neutral 0.5 (proving it is no longer hardcoded).
	g := investingGoal()
	g.Context.Constraints = append(g.Context.Constraints, domain.Constraint{Name: "30 day horizon", Kind: "time_horizon"})
	res, err := p.GeneratePlan(context.Background(), PlanRequest{
		Goal:          g,
		SignalKind:    "price_move",
		SignalPayload: map[string]any{"kind": "price_move", "ticker": "ACME", "pct_change": -0.1, "window_days": 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RankedMoves[0].Rationale, "horizon=1.00") {
		t.Errorf("expected perfect horizon fit (1.00) for matching 30d horizon/window, got rationale: %q", res.RankedMoves[0].Rationale)
	}
}

func TestFinancePlannerConcurrent(t *testing.T) {
	p := financeTestPlanner(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestFinancePlannerNoTickers(t *testing.T) {
	p := financeTestPlanner(t)
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Domain: "investing", Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RankedMoves) != 1 || res.RankedMoves[0].Title != "Insufficient market data" {
		t.Errorf("expected insufficient-data move, got %+v", res.RankedMoves)
	}
}
