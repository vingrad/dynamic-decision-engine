package llm

import (
	"context"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// recordingPlanner records the last request it saw and counts calls. It returns a
// fixed result so tests can observe routing/caching/guidance behaviour.
type recordingPlanner struct {
	name  string
	last  PlanRequest
	calls int
	err   error
}

func (r *recordingPlanner) Name() string { return r.name }

func (r *recordingPlanner) GeneratePlan(_ context.Context, req PlanRequest) (PlanResult, error) {
	r.calls++
	r.last = req
	if r.err != nil {
		return PlanResult{}, r.err
	}
	return PlanResult{
		Summary:     "ok",
		RankedMoves: []domain.RankedMove{{Rank: 1, Title: "m", Confidence: 0.5}},
		Provenance:  domain.DecisionProvenance{Planner: r.name},
	}, nil
}

func TestEffectiveSystemPrompt(t *testing.T) {
	if got := effectiveSystemPrompt("base", ""); got != "base" {
		t.Errorf("empty override should return base unchanged, got %q", got)
	}
	if got := effectiveSystemPrompt("base", "extra"); got != "base\n\nextra" {
		t.Errorf("override should append, got %q", got)
	}
}

func TestGuidedPlannerStampsProvenanceAndPrompt(t *testing.T) {
	base := &recordingPlanner{name: "mock"}
	g := NewGuidedPlanner(base, GuidedConfig{
		PackID: "investing", PackVersion: "1", PromptVersion: "investing-v1", PromptTemplate: "THESIS",
	})
	res, err := g.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Domain: "investing", Objective: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if base.last.SystemPromptOverride != "THESIS" {
		t.Errorf("guided planner should set override, got %q", base.last.SystemPromptOverride)
	}
	if res.Provenance.PackID != "investing" || res.Provenance.PackVersion != "1" {
		t.Errorf("provenance pack id/version not stamped: %+v", res.Provenance)
	}
	if res.Provenance.PromptVersion != "investing-v1" {
		t.Errorf("prompt version not stamped: %q", res.Provenance.PromptVersion)
	}
	if g.Name() != "mock" {
		t.Errorf("guided Name should defer to base, got %q", g.Name())
	}
}

func TestRouterDispatch(t *testing.T) {
	def := &recordingPlanner{name: "def"}
	inv := &recordingPlanner{name: "inv"}
	r := NewPlannerRouter(def, map[string]Planner{"investing": inv})

	// Known domain -> routed planner.
	if _, err := r.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Domain: "investing", Objective: "x"}}); err != nil {
		t.Fatal(err)
	}
	if inv.calls != 1 || def.calls != 0 {
		t.Errorf("investing should route to inv: inv=%d def=%d", inv.calls, def.calls)
	}
	// Empty domain -> default.
	if _, err := r.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Objective: "x"}}); err != nil {
		t.Fatal(err)
	}
	// Unknown domain -> default.
	if _, err := r.GeneratePlan(context.Background(), PlanRequest{Goal: domain.Goal{Domain: "nope", Objective: "x"}}); err != nil {
		t.Fatal(err)
	}
	if def.calls != 2 {
		t.Errorf("empty and unknown domains should hit default twice, got %d", def.calls)
	}
}

type countingObs struct{ hits, misses int }

func (o *countingObs) PlanCacheHit(string)  { o.hits++ }
func (o *countingObs) PlanCacheMiss(string) { o.misses++ }

func TestCachingPlanner(t *testing.T) {
	base := &recordingPlanner{name: "mock"}
	obs := &countingObs{}
	c := NewCachingPlanner(base, NewMemoryCache(8), obs)
	ctx := context.Background()
	req := PlanRequest{Goal: domain.Goal{Domain: "investing", Objective: "x"}, SystemPromptOverride: "THESIS"}

	if _, err := c.GeneratePlan(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GeneratePlan(ctx, req); err != nil {
		t.Fatal(err)
	}
	if base.calls != 1 {
		t.Errorf("second identical request should hit cache, base called %d times", base.calls)
	}
	if obs.hits != 1 || obs.misses != 1 {
		t.Errorf("expected 1 hit / 1 miss, got %d / %d", obs.hits, obs.misses)
	}

	// A different prompt override must NOT collide (different pack version/prompt).
	req2 := req
	req2.SystemPromptOverride = "OTHER"
	if _, err := c.GeneratePlan(ctx, req2); err != nil {
		t.Fatal(err)
	}
	if base.calls != 2 {
		t.Errorf("different override should miss cache, base called %d times", base.calls)
	}
}

func TestMemoryCacheTTLExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewMemoryCacheTTL(8, 60*time.Second, func() time.Time { return now })
	c.Put("k", PlanResult{Summary: "v"})

	if _, ok := c.Get("k"); !ok {
		t.Fatal("entry should be present before TTL")
	}
	now = now.Add(59 * time.Second)
	if _, ok := c.Get("k"); !ok {
		t.Error("entry should still be present just before TTL")
	}
	now = now.Add(2 * time.Second) // now 61s past Put
	if _, ok := c.Get("k"); ok {
		t.Error("entry should have expired after TTL")
	}
}

func TestMemoryCacheNoTTLNeverExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewMemoryCacheTTL(8, 0, func() time.Time { return now })
	c.Put("k", PlanResult{Summary: "v"})
	now = now.Add(1000 * time.Hour)
	if _, ok := c.Get("k"); !ok {
		t.Error("ttl=0 entries must never expire")
	}
}

func TestMemoryCacheEviction(t *testing.T) {
	c := NewMemoryCache(2)
	c.Put("a", PlanResult{Summary: "a"})
	c.Put("b", PlanResult{Summary: "b"})
	_, _ = c.Get("a")                    // make "a" most-recently used
	c.Put("c", PlanResult{Summary: "c"}) // evicts "b" (LRU)
	if _, ok := c.Get("b"); ok {
		t.Error("b should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("a should still be present")
	}
}

func TestMockIgnoresOverride(t *testing.T) {
	// The mock's output must not change with a domain prompt override (determinism
	// must not vary by domain).
	p := NewMockPlanner()
	g := domain.Goal{Objective: "Reach 1000 customers", Metric: "customers"}
	a, _ := p.GeneratePlan(context.Background(), PlanRequest{Goal: g})
	b, _ := p.GeneratePlan(context.Background(), PlanRequest{Goal: g, SystemPromptOverride: "DOMAIN: INVESTING"})
	if a.RankedMoves[0].Confidence != b.RankedMoves[0].Confidence {
		t.Error("mock confidence should not depend on the prompt override")
	}
}
