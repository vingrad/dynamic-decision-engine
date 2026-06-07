package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

// fakeEnricher appends fixed facts to the goal Context and returns canned audit
// records, standing in for a real source pipeline without any I/O.
type fakeEnricher struct {
	facts    []string
	contribs []domain.SourceContribution
}

func (f fakeEnricher) Enrich(_ context.Context, goal domain.Goal, _ string, _ map[string]any) (domain.Goal, []domain.SourceContribution) {
	if len(f.facts) > 0 {
		goal.Context.Facts = append(append([]string(nil), goal.Context.Facts...), f.facts...)
	}
	return goal, f.contribs
}

// fixedResolver always returns the same Enricher (or nil) regardless of domain.
type fixedResolver struct{ e Enricher }

func (r fixedResolver) SourcesFor(string) Enricher { return r.e }

// TestEnrichmentNoOpPreservesSnapshot proves the offline guarantee: neither a nil
// resolver nor a resolver whose enricher leaves Context untouched changes the input
// snapshot, so the mock planner stays byte-for-byte deterministic.
func TestEnrichmentNoOpPreservesSnapshot(t *testing.T) {
	ctx := context.Background()

	baseline, err := New(llm.NewMockPlanner()).GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}

	noop := New(llm.NewMockPlanner(), WithSourceResolver(fixedResolver{e: fakeEnricher{}}))
	got, err := noop.GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}
	if got.InputSnapshotID != baseline.InputSnapshotID {
		t.Errorf("no-op enricher changed snapshot: got %q want %q", got.InputSnapshotID, baseline.InputSnapshotID)
	}
	if len(got.Provenance.SourceContributions) != 0 {
		t.Errorf("expected no contributions, got %d", len(got.Provenance.SourceContributions))
	}

	// A resolver that returns a nil Enricher must also be a clean no-op.
	nilEnricher := New(llm.NewMockPlanner(), WithSourceResolver(fixedResolver{e: nil}))
	got2, err := nilEnricher.GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}
	if got2.InputSnapshotID != baseline.InputSnapshotID {
		t.Errorf("nil-enricher resolver changed snapshot: got %q want %q", got2.InputSnapshotID, baseline.InputSnapshotID)
	}
}

// TestEnrichmentChangesSnapshot proves a different world-state (different fetched
// facts) yields a different snapshot id, and that the audit record is recorded.
func TestEnrichmentChangesSnapshot(t *testing.T) {
	ctx := context.Background()

	baseline, err := New(llm.NewMockPlanner()).GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}

	contrib := domain.SourceContribution{
		SourceName:   "fake",
		FetchedAt:    time.Unix(1, 0).UTC(),
		Raw:          json.RawMessage(`{"price":42}`),
		DeltaSummary: "+1 facts",
	}
	enriched := New(llm.NewMockPlanner(), WithSourceResolver(fixedResolver{e: fakeEnricher{
		facts:    []string{"current price is 42"},
		contribs: []domain.SourceContribution{contrib},
	}}))
	got, err := enriched.GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}
	if got.InputSnapshotID == baseline.InputSnapshotID {
		t.Errorf("enrichment did not change snapshot id (%q)", got.InputSnapshotID)
	}
	if len(got.Provenance.SourceContributions) != 1 || got.Provenance.SourceContributions[0].SourceName != "fake" {
		t.Fatalf("expected the fake contribution to be recorded, got %+v", got.Provenance.SourceContributions)
	}
}

// TestEnrichmentDeterministic proves the same world-state always hashes to the same
// snapshot id across runs.
func TestEnrichmentDeterministic(t *testing.T) {
	ctx := context.Background()
	mk := func() *Engine {
		return New(llm.NewMockPlanner(), WithSourceResolver(fixedResolver{e: fakeEnricher{
			facts: []string{"a", "b"},
		}}))
	}
	first, err := mk().GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}
	second, err := mk().GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}
	if first.InputSnapshotID != second.InputSnapshotID {
		t.Errorf("same world-state hashed differently: %q vs %q", first.InputSnapshotID, second.InputSnapshotID)
	}
}

// TestReplanRecordsContributions proves enrichment also runs on the replan path and
// its audit records land on the candidate version.
func TestReplanRecordsContributions(t *testing.T) {
	ctx := context.Background()
	e := New(llm.NewMockPlanner(), WithSourceResolver(fixedResolver{e: fakeEnricher{
		facts:    []string{"fresh signal context"},
		contribs: []domain.SourceContribution{{SourceName: "fake"}},
	}}))

	current, err := e.GenerateInitialPlan(ctx, testGoal())
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Replan(ctx, testGoal(), current, "competitor: launched free tier", "competitor", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidate.Provenance.SourceContributions) != 1 {
		t.Fatalf("expected 1 contribution on replan candidate, got %d", len(res.Candidate.Provenance.SourceContributions))
	}
}
