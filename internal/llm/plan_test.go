package llm

import (
	"strings"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func TestPlanUserPayloadIncludesSignalData(t *testing.T) {
	req := PlanRequest{
		Goal:          domain.Goal{Objective: "Grow revenue"},
		SignalNote:    "price_move: AAPL -8%",
		SignalKind:    "price_move",
		SignalPayload: map[string]any{"ticker": "AAPL", "pct": -8.0},
		CurrentMoves: []domain.RankedMove{
			{Rank: 1, Key: "thesis:AAPL", Title: "Thesis: AAPL"},
		},
	}
	out, err := planUserPayload(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"new_signal_data", "\"ticker\"", "new_signal_kind", "current_plan", "thesis:AAPL"} {
		if !strings.Contains(out, want) {
			t.Errorf("payload missing %q:\n%s", want, out)
		}
	}
}

func TestPlanUserPayloadOmitsEmptySignalData(t *testing.T) {
	// An initial plan (no signal) must not emit the replan-only fields.
	out, err := planUserPayload(PlanRequest{Goal: domain.Goal{Objective: "Grow revenue"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"new_signal_data", "new_signal_kind", "current_plan"} {
		if strings.Contains(out, absent) {
			t.Errorf("initial-plan payload should omit %q:\n%s", absent, out)
		}
	}
}

func TestMapMovesFillsKeyFromSlugWhenAbsent(t *testing.T) {
	dto := planDTO{RankedMoves: []moveDTO{
		{Title: "Expand Paid-Search!"},                   // no key -> slug
		{Key: "fix-onboarding", Title: "Fix onboarding"}, // explicit key preserved
	}}
	moves := mapMoves(dto)
	if moves[0].Key != "expand-paid-search" {
		t.Errorf("expected slug key, got %q", moves[0].Key)
	}
	if moves[1].Key != "fix-onboarding" {
		t.Errorf("expected explicit key preserved, got %q", moves[1].Key)
	}
}
