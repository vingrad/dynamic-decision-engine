package llm

import (
	"context"
	"os"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func TestMapMoves(t *testing.T) {
	dto := planDTO{
		RankedMoves: []moveDTO{
			{Title: "A", ExpectedImpact: "high", Effort: "weird", Risk: "low", Confidence: 1.4},
			{Title: "B", ExpectedImpact: "", Effort: "medium", Risk: "high", Confidence: -0.2},
		},
	}
	moves := mapMoves(dto)
	if len(moves) != 2 {
		t.Fatalf("expected 2 moves, got %d", len(moves))
	}
	// Ranks are re-numbered 1..N.
	if moves[0].Rank != 1 || moves[1].Rank != 2 {
		t.Errorf("ranks not 1,2: %d,%d", moves[0].Rank, moves[1].Rank)
	}
	// Invalid enums coerce to medium; valid ones pass through.
	if moves[0].Effort != domain.LevelMedium {
		t.Errorf("invalid effort should coerce to medium, got %q", moves[0].Effort)
	}
	if moves[1].ExpectedImpact != domain.LevelMedium {
		t.Errorf("empty impact should coerce to medium, got %q", moves[1].ExpectedImpact)
	}
	if moves[0].ExpectedImpact != domain.LevelHigh {
		t.Errorf("valid impact should pass through, got %q", moves[0].ExpectedImpact)
	}
	// Confidence is clamped to [0,1].
	if moves[0].Confidence != 1 || moves[1].Confidence != 0 {
		t.Errorf("confidence not clamped: %v, %v", moves[0].Confidence, moves[1].Confidence)
	}
}

func TestOpenAIPlannerDefaults(t *testing.T) {
	p := NewOpenAIPlanner(OpenAIConfig{Provider: "deepseek"})
	if p.Name() != "deepseek" {
		t.Errorf("Name() = %q, want deepseek", p.Name())
	}
	if p.model != defaultDeepSeekModel {
		t.Errorf("deepseek default model = %q, want %q", p.model, defaultDeepSeekModel)
	}
	o := NewOpenAIPlanner(OpenAIConfig{Provider: "openai"})
	if o.model != defaultOpenAIModel {
		t.Errorf("openai default model = %q, want %q", o.model, defaultOpenAIModel)
	}
}

// liveOpenAICompatible exercises a real provider; skipped unless its key is set.
func liveOpenAICompatible(t *testing.T, provider, keyEnv string) {
	t.Helper()
	if os.Getenv(keyEnv) == "" {
		t.Skipf("%s not set; skipping live %s test", keyEnv, provider)
	}
	p := NewOpenAIPlanner(OpenAIConfig{Provider: provider, APIKey: os.Getenv(keyEnv)})
	res, err := p.GeneratePlan(context.Background(), PlanRequest{
		Goal: domain.Goal{
			Objective: "Acquire the first 10 paying customers",
			Metric:    "paying customers",
			Context: domain.Context{
				Assets:      []domain.Asset{{Name: "a working prototype"}},
				Constraints: []domain.Constraint{{Name: "no marketing budget"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("live %s request failed: %v", provider, err)
	}
	if len(res.RankedMoves) == 0 {
		t.Fatalf("%s: expected at least one ranked move", provider)
	}
	if res.Provenance.Planner != provider || res.Invocation.TotalTokens == 0 {
		t.Errorf("%s: unexpected provenance/usage: %+v / %+v", provider, res.Provenance, res.Invocation)
	}
}

func TestOpenAILive(t *testing.T)   { liveOpenAICompatible(t, "openai", "OPENAI_API_KEY") }
func TestDeepSeekLive(t *testing.T) { liveOpenAICompatible(t, "deepseek", "DEEPSEEK_API_KEY") }
