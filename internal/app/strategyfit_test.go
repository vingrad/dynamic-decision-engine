package app

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// selectorStubPlanner emits plans stamped the way the strategy selector does,
// so StrategySamples' attribution can be tested without market data.
type selectorStubPlanner struct{ strategy, regime string }

func (p *selectorStubPlanner) Name() string { return "multi:selector" }

func (p *selectorStubPlanner) GeneratePlan(_ context.Context, req llm.PlanRequest) (llm.PlanResult, error) {
	return llm.PlanResult{
		Summary: "stub",
		RankedMoves: []domain.RankedMove{
			{Rank: 1, Key: "thesis:ACME", Title: "Thesis: ACME", Confidence: 0.6,
				ExpectedImpact: domain.LevelMedium, Effort: domain.LevelLow, Risk: domain.LevelMedium,
				Experiment: domain.Experiment{Title: "t", DurationDays: 1}},
		},
		Provenance: domain.DecisionProvenance{
			ReasoningSummary: "stub",
			Planner:          "finance:" + p.strategy,
			Strategy:         "selector",
			SelectedStrategy: p.strategy,
			Regime:           p.regime,
		},
	}, nil
}

func TestStrategySamples(t *testing.T) {
	stub := &selectorStubPlanner{strategy: "momentum", regime: "trend"}
	s := New(storage.NewMemory(), engine.New(stub), WithRegistry(pack.NewRegistry()))
	ctx := context.Background()

	g, err := s.CreateGoal(ctx, CreateGoalInput{
		Domain:    "investing",
		Objective: "Build a thesis-driven position",
		Context: domain.Context{
			Assets: []domain.Asset{{Name: "ACME", Kind: "ticker"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.GeneratePlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}

	record := func(result domain.OutcomeResult) {
		t.Helper()
		if _, err := s.RecordOutcome(ctx, OutcomeInput{
			GoalID: g.ID, PlanVersion: v1.Version, MoveRank: 1, Result: result,
		}); err != nil {
			t.Fatal(err)
		}
	}
	record(domain.OutcomeSuccess)
	record(domain.OutcomeFailure)
	record(domain.OutcomePartial) // no binary label -> skipped

	samples, err := s.StrategySamples(ctx, "investing")
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 decisive samples, got %d: %+v", len(samples), samples)
	}
	for _, sm := range samples {
		if sm.Strategy != "momentum" || sm.Regime != finance.Regime("trend") {
			t.Errorf("sample attribution wrong: %+v", sm)
		}
	}
	wins := 0
	for _, sm := range samples {
		if sm.Success {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("expected exactly one success sample, got %d", wins)
	}
}

// TestStrategySamplesSkipsPreSelectorPlans: plans whose provenance carries no
// selected strategy (the entire pre-selector history) contribute nothing.
func TestStrategySamplesSkipsPreSelectorPlans(t *testing.T) {
	s := newTestServiceWithRegistry() // mock planner: no SelectedStrategy
	ctx := context.Background()

	g, err := s.CreateGoal(ctx, CreateGoalInput{
		Domain:    "investing",
		Objective: "Build a thesis-driven position",
		Context:   domain.Context{Assets: []domain.Asset{{Name: "ACME", Kind: "ticker"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.GeneratePlan(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordOutcome(ctx, OutcomeInput{
		GoalID: g.ID, PlanVersion: v1.Version, MoveRank: 1, Result: domain.OutcomeSuccess,
	}); err != nil {
		t.Fatal(err)
	}

	samples, err := s.StrategySamples(ctx, "investing")
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 {
		t.Errorf("pre-selector plans must contribute nothing, got %+v", samples)
	}
}
