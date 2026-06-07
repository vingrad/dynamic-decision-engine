package pack

import (
	"strings"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// TestGenericPromptIsEmpty is load-bearing: a non-empty generic template would
// silently change every existing prompt.
func TestGenericPromptIsEmpty(t *testing.T) {
	d, _ := NewRegistry().Get("generic")
	if d.PromptTemplate != "" {
		t.Errorf("generic prompt template must be empty, got %q", d.PromptTemplate)
	}
	if d.Scoring != nil {
		t.Error("generic pack should not carry scoring config")
	}
}

func TestInvestingPromptAndScoring(t *testing.T) {
	d, _ := NewRegistry().Get("investing")
	for _, want := range []string{"thesis", "Not financial advice", "time horizon"} {
		if !strings.Contains(d.PromptTemplate, want) {
			t.Errorf("investing prompt missing %q", want)
		}
	}
	if d.Scoring == nil {
		t.Fatal("investing pack must carry scoring config")
	}
	if d.Scoring.Risk.MaxPositionPct <= 0 {
		t.Error("investing scoring risk budget should be populated")
	}
}

func TestEvaluatorDeltas(t *testing.T) {
	r := NewRegistry()
	cases := map[string]float64{
		"generic":   0.10,
		"investing": 0.05,
		"growth":    0.10,
		"career":    0.10,
	}
	for id, want := range cases {
		d, _ := r.Get(id)
		if d.Eval.ConfidenceDelta != want {
			t.Errorf("%s ConfidenceDelta = %v, want %v", id, d.Eval.ConfidenceDelta, want)
		}
	}
}

func TestValidateSeverities(t *testing.T) {
	r := NewRegistry()

	// A minimal goal (only objective) must never produce a hard error in any pack,
	// so the only blocking validation remains "objective is required" at the service.
	for _, id := range r.IDs() {
		d, _ := r.Get(id)
		for _, iss := range d.Validate(domain.Goal{Objective: "x"}) {
			if iss.Severity == SeverityError {
				t.Errorf("%s returned a hard error for a minimal goal: %+v", id, iss)
			}
		}
	}

	// Investing warns when risk + horizon context is absent.
	inv, _ := r.Get("investing")
	if len(inv.Validate(domain.Goal{Objective: "x"})) == 0 {
		t.Error("investing should warn on a goal lacking risk/horizon context")
	}
	// And is quiet when they are present.
	full := domain.Goal{Objective: "x", Context: domain.Context{
		Constraints: []domain.Constraint{
			{Name: "max 10% dd", Kind: "drawdown_limit"},
			{Name: "3y", Kind: "time_horizon"},
		},
	}}
	if iss := inv.Validate(full); len(iss) != 0 {
		t.Errorf("investing should not warn when risk+horizon present, got %+v", iss)
	}
}
