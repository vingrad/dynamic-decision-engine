package llm

import (
	"context"
	"os"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func TestLevelCoercion(t *testing.T) {
	cases := map[string]domain.Level{
		"low":     domain.LevelLow,
		"medium":  domain.LevelMedium,
		"high":    domain.LevelHigh,
		"":        domain.LevelMedium, // invalid -> default
		"extreme": domain.LevelMedium, // invalid -> default
	}
	for in, want := range cases {
		if got := level(in); got != want {
			t.Errorf("level(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampConfidence(t *testing.T) {
	cases := map[float64]float64{-0.5: 0, 0: 0, 0.42: 0.42, 1: 1, 1.7: 1}
	for in, want := range cases {
		if got := clampConfidence(in); got != want {
			t.Errorf("clampConfidence(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestPlanInputSchemaShape(t *testing.T) {
	schema := planInputSchema()
	if _, ok := schema.Properties.(map[string]any)["ranked_moves"]; !ok {
		t.Fatal("schema missing ranked_moves")
	}
	if len(schema.Required) == 0 {
		t.Fatal("schema should declare required fields")
	}
}

// TestAnthropicLive is an end-to-end smoke test against the real API. It is
// skipped unless ANTHROPIC_API_KEY is set, so unit runs and CI stay offline.
func TestAnthropicLive(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Anthropic test")
	}
	p := NewAnthropicPlanner(AnthropicConfig{})
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
		t.Fatalf("live request failed: %v", err)
	}
	if len(res.RankedMoves) == 0 {
		t.Fatal("expected at least one ranked move")
	}
	if res.Provenance.Planner != "anthropic" || res.Invocation.TotalTokens == 0 {
		t.Errorf("unexpected provenance/usage: %+v / %+v", res.Provenance, res.Invocation)
	}
}
