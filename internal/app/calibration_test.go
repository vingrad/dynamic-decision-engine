package app

import (
	"context"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func TestCalibrationSamples(t *testing.T) {
	s := newTestServiceWithRegistry()
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

	record := func(rank int, result domain.OutcomeResult) {
		t.Helper()
		if _, err := s.RecordOutcome(ctx, OutcomeInput{
			GoalID: g.ID, PlanVersion: v1.Version, MoveRank: rank, Result: result,
		}); err != nil {
			t.Fatal(err)
		}
	}
	record(1, domain.OutcomeSuccess)
	record(2, domain.OutcomeFailure)
	record(1, domain.OutcomePartial) // no binary label -> skipped

	// A goal in another domain must not contribute.
	other := makeGoal(t, s)
	if _, err := s.GeneratePlan(ctx, other.ID); err != nil {
		t.Fatal(err)
	}

	samples, err := s.CalibrationSamples(ctx, "investing")
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 decisive samples, got %d: %+v", len(samples), samples)
	}
	moveConf := func(rank int) float64 {
		for _, m := range v1.RankedMoves {
			if m.Rank == rank {
				return m.Confidence
			}
		}
		t.Fatalf("rank %d missing", rank)
		return 0
	}
	// Listing order is not guaranteed; assert the set of samples.
	bySuccess := map[bool]float64{}
	for _, s := range samples {
		bySuccess[s.Success] = s.Confidence
	}
	if bySuccess[true] != moveConf(1) {
		t.Errorf("success sample confidence = %v, want rank-1 confidence %v", bySuccess[true], moveConf(1))
	}
	if bySuccess[false] != moveConf(2) {
		t.Errorf("failure sample confidence = %v, want rank-2 confidence %v", bySuccess[false], moveConf(2))
	}
}
