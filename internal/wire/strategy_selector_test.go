package wire

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// textPack returns a text-domain descriptor with prompt-variant strategies.
func textPack(defaultOn bool, strategies ...pack.StrategyDescriptor) pack.Descriptor {
	return pack.Descriptor{
		ID:                 "advice",
		Name:               "Advice",
		Version:            "1",
		PromptVersion:      "advice-v1",
		PromptTemplate:     "DOMAIN: ADVICE",
		Strategies:         strategies,
		SelectionDefaultOn: defaultOn,
	}
}

func textStrategy(id, template string, regimes ...string) pack.StrategyDescriptor {
	return pack.StrategyDescriptor{ID: id, Name: id, PromptTemplate: template, Regimes: regimes}
}

func adviceGoal() domain.Goal {
	return domain.Goal{Domain: "advice", Objective: "decide the next quarter"}
}

// overrideRecorder counts GeneratePlan calls per SystemPromptOverride, so a
// test can prove each prompt variant reached the base exactly once (distinct
// cache keys) rather than aliasing onto one entry.
type overrideRecorder struct {
	mu    sync.Mutex
	calls map[string]int
}

func (r *overrideRecorder) Name() string { return "recorder" }

func (r *overrideRecorder) GeneratePlan(_ context.Context, req llm.PlanRequest) (llm.PlanResult, error) {
	r.mu.Lock()
	r.calls[req.SystemPromptOverride]++
	r.mu.Unlock()
	return llm.PlanResult{
		Summary: "plan",
		RankedMoves: []domain.RankedMove{{
			Rank: 1, Key: "act", Title: "Act", Confidence: 0.6, RawConfidence: 0.6,
			ExpectedImpact: domain.LevelMedium, Effort: domain.LevelLow, Risk: domain.LevelMedium,
		}},
		Provenance: domain.DecisionProvenance{Planner: "recorder", Model: "none"},
	}, nil
}

func TestTextDomainStrategySelector(t *testing.T) {
	reg := pack.NewRegistryFrom(textPack(true,
		textStrategy("bold", "LENS: BOLD"),
		textStrategy("safe", "LENS: SAFE"),
	))
	base := &overrideRecorder{calls: map[string]int{}}
	router := mustRouter(t, reg, policy.Policy{}, PlannerDeps{Base: base, Cache: llm.NewMemoryCache(16)})

	res, err := router.GeneratePlan(context.Background(), llm.PlanRequest{Goal: adviceGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.Strategy != "selector" || len(res.Provenance.StrategyCandidates) != 2 {
		t.Fatalf("expected a 2-candidate text competition, got %+v", res.Provenance)
	}
	// Identical mock-style outputs tie all the way down: canonical order wins,
	// and full agreement on the top key means no disagreement haircut.
	if res.Provenance.SelectedStrategy != "bold" {
		t.Errorf("tie must keep canonical order, got %q", res.Provenance.SelectedStrategy)
	}
	if res.RankedMoves[0].Confidence != 0.6 {
		t.Errorf("agreeing variants must not be penalized, got %v", res.RankedMoves[0].Confidence)
	}
	// The winner's provenance names its variant.
	if !strings.Contains(res.Provenance.PromptVersion, "+bold") {
		t.Errorf("prompt version should carry the variant, got %q", res.Provenance.PromptVersion)
	}

	// Each variant's combined template reached the base exactly once — with a
	// shared cache, aliasing would have collapsed them into one call.
	if len(base.calls) != 2 {
		t.Fatalf("expected 2 distinct overrides at the base, got %v", base.calls)
	}
	for override, n := range base.calls {
		if n != 1 {
			t.Errorf("override %q called %d times, want 1 (cache aliasing?)", override, n)
		}
		if !strings.Contains(override, "DOMAIN: ADVICE") || !strings.Contains(override, "LENS:") {
			t.Errorf("override must combine pack and strategy templates, got %q", override)
		}
	}
}

func TestTextDomainSelectionDefaultOff(t *testing.T) {
	reg := pack.NewRegistryFrom(textPack(false,
		textStrategy("bold", "LENS: BOLD"),
		textStrategy("safe", "LENS: SAFE"),
	))
	base := &overrideRecorder{calls: map[string]int{}}
	router := mustRouter(t, reg, policy.Policy{}, PlannerDeps{Base: base})

	// Default off: a plain guided planner serves, no competition.
	res, err := router.GeneratePlan(context.Background(), llm.PlanRequest{Goal: adviceGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.Strategy == "selector" || len(res.Provenance.StrategyCandidates) != 0 {
		t.Errorf("default-off domain must not compete: %+v", res.Provenance)
	}

	// Policy opt-in turns the competition on.
	on := true
	pol := policy.Policy{Domains: map[string]policy.DomainPolicy{
		"advice": {Strategy: &policy.StrategySelection{Enabled: &on}},
	}}
	router = mustRouter(t, reg, pol, PlannerDeps{Base: base})
	res, err = router.GeneratePlan(context.Background(), llm.PlanRequest{Goal: adviceGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.Strategy != "selector" {
		t.Errorf("policy opt-in must enable the competition: %+v", res.Provenance)
	}
}

func TestStrategySelectorBuildErrors(t *testing.T) {
	cases := []struct {
		name string
		d    pack.Descriptor
	}{
		{"text strategy with regime label", textPack(true,
			textStrategy("bold", "LENS: BOLD", "trend"),
			textStrategy("safe", "LENS: SAFE"),
		)},
		{"text strategy without template", textPack(true,
			textStrategy("bold", ""),
			textStrategy("safe", "LENS: SAFE"),
		)},
		{"duplicate templates", textPack(true,
			textStrategy("bold", "LENS: SAME"),
			textStrategy("safe", "LENS: SAME"),
		)},
		{"duplicate ids", textPack(true,
			textStrategy("bold", "LENS: A"),
			textStrategy("bold", "LENS: B"),
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := pack.NewRegistryFrom(tc.d)
			if _, err := BuildPlannerRouter(reg, policy.Policy{}, PlannerDeps{Base: llm.NewMockPlanner()}); err == nil {
				t.Error("invalid strategy declaration must fail the build")
			}
		})
	}
}

// TestFinanceKitDeclineRule: a finance-kind domain with strategies but no
// market data must fall back to the plain guided text planner — never into
// the text-variant competition (its strategies carry numeric tuning that
// prompt children cannot interpret).
func TestFinanceKitDeclineRule(t *testing.T) {
	router := mustRouter(t, pack.NewRegistry(), policy.Policy{}, PlannerDeps{
		Base: llm.NewMockPlanner(),
		// no DataSources: marketdata absent
	})
	res, err := router.GeneratePlan(context.Background(), llm.PlanRequest{Goal: domain.Goal{
		Domain: "investing", Objective: "grow",
		Context: domain.Context{Assets: []domain.Asset{{Name: "ACME", Kind: "ticker"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Provenance.Strategy == "selector" {
		t.Fatalf("kit-unavailable domain must not compete: %+v", res.Provenance)
	}
	if res.Provenance.Planner != "mock" || res.Provenance.PackID != "investing" {
		t.Errorf("expected the guided text fallback exactly as without strategies: %+v", res.Provenance)
	}
}

// TestBuiltInTextPacksDefaultOff: growth and career declare strategy lenses
// but must remain byte-for-byte single-planner by default — no validation
// gates have earned their default-on flip. The policy opt-in turns the
// competition on with a deterministic canonical-order winner under the mock.
func TestBuiltInTextPacksDefaultOff(t *testing.T) {
	cases := []struct {
		domainID   string
		wantWinner string // canonical first strategy
	}{
		{"growth", "expand"},
		{"career", "mastery"},
	}
	for _, tc := range cases {
		t.Run(tc.domainID, func(t *testing.T) {
			goal := domain.Goal{Domain: tc.domainID, Objective: "advance"}

			router := mustRouter(t, pack.NewRegistry(), policy.Policy{}, PlannerDeps{Base: llm.NewMockPlanner()})
			res, err := router.GeneratePlan(context.Background(), llm.PlanRequest{Goal: goal})
			if err != nil {
				t.Fatal(err)
			}
			if res.Provenance.Strategy == "selector" {
				t.Fatalf("%s must not compete by default: %+v", tc.domainID, res.Provenance)
			}

			on := true
			pol := policy.Policy{Domains: map[string]policy.DomainPolicy{
				tc.domainID: {Strategy: &policy.StrategySelection{Enabled: &on}},
			}}
			router = mustRouter(t, pack.NewRegistry(), pol, PlannerDeps{Base: llm.NewMockPlanner()})
			res, err = router.GeneratePlan(context.Background(), llm.PlanRequest{Goal: goal})
			if err != nil {
				t.Fatal(err)
			}
			if res.Provenance.Strategy != "selector" || len(res.Provenance.StrategyCandidates) != 3 {
				t.Fatalf("opt-in must compete 3 lenses: %+v", res.Provenance)
			}
			// The mock ignores prompt overrides, so all variants tie and the
			// canonical first lens wins deterministically with no penalty.
			if res.Provenance.SelectedStrategy != tc.wantWinner {
				t.Errorf("winner = %q, want canonical %q", res.Provenance.SelectedStrategy, tc.wantWinner)
			}
			if res.Provenance.Comparator != "utility" {
				t.Errorf("default comparator = %q, want utility", res.Provenance.Comparator)
			}
		})
	}
}
