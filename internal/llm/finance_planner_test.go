package llm

import (
	"context"
	"regexp"
	"strconv"
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
	// Theses are independent positions: same parallel group, no dependencies.
	for _, m := range res.RankedMoves {
		if m.ParallelGroup != "portfolio" {
			t.Errorf("thesis %q parallel group = %q, want %q", m.Key, m.ParallelGroup, "portfolio")
		}
		if len(m.DependsOn) != 0 {
			t.Errorf("thesis %q should have no dependencies, got %v", m.Key, m.DependsOn)
		}
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
	// Horizon fit compares the goal's stated horizon against the vol-implied time
	// for the thesis to traverse its upside. ACME's fixture volatility implies
	// roughly a year to a 2:1 move, so a 1-year goal fits nearly perfectly and a
	// 30-day goal fits poorly.
	yearGoal := investingGoal()
	yearGoal.Context.Constraints = append(yearGoal.Context.Constraints, domain.Constraint{Name: "1 year horizon", Kind: "time_horizon"})
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: yearGoal})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RankedMoves[0].Rationale, "horizon=0.99") {
		t.Errorf("1-year goal should fit ACME's vol-implied horizon (~1y), got rationale: %q", res.RankedMoves[0].Rationale)
	}

	shortGoal := investingGoal()
	shortGoal.Context.Constraints = append(shortGoal.Context.Constraints, domain.Constraint{Name: "30 day horizon", Kind: "time_horizon"})
	short, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: shortGoal})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(short.RankedMoves[0].Rationale, "horizon=0.25") {
		t.Errorf("30-day goal should fit poorly (~0.25), got rationale: %q", short.RankedMoves[0].Rationale)
	}

	// Without a stated horizon the component is neutral.
	plain, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.RankedMoves[0].Rationale, "horizon=0.50") {
		t.Errorf("no stated horizon should be neutral, got rationale: %q", plain.RankedMoves[0].Rationale)
	}
}

var suggestedSizeRe = regexp.MustCompile(`suggested size (\d+(?:\.\d+)?)% of equity`)

func suggestedSize(t *testing.T, m domain.RankedMove) float64 {
	t.Helper()
	match := suggestedSizeRe.FindStringSubmatch(m.Rationale)
	if match == nil {
		t.Fatalf("no suggested size in rationale: %q", m.Rationale)
	}
	size, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return size
}

func TestFinancePlannerHonorsRiskConstraints(t *testing.T) {
	p := financeTestPlanner(t)
	base, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	if err != nil {
		t.Fatal(err)
	}

	g := investingGoal()
	g.Context.Constraints = []domain.Constraint{
		{Name: "max 10% drawdown", Kind: "drawdown_limit"},
		{Name: "conservative risk tolerance", Kind: "risk_tolerance"},
	}
	constrained, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: g})
	if err != nil {
		t.Fatal(err)
	}

	for i, m := range constrained.RankedMoves {
		if !strings.Contains(m.Rationale, "risk budget per goal constraints: drawdown_limit 10%, risk_tolerance conservative") {
			t.Errorf("rationale should name the binding constraints: %q", m.Rationale)
		}
		baseSize := suggestedSize(t, base.RankedMoves[i])
		gotSize := suggestedSize(t, m)
		if gotSize >= baseSize {
			t.Errorf("%s: constrained size %v%% should be below unconstrained %v%%", m.Key, gotSize, baseSize)
		}
	}
}

// winProbOf extracts the "(p=X.XX)" win probability from a move rationale.
func winProbOf(t *testing.T, m domain.RankedMove) string {
	t.Helper()
	match := regexp.MustCompile(`\(p=(\d\.\d\d)\)`).FindStringSubmatch(m.Rationale)
	if match == nil {
		t.Fatalf("no win probability in rationale: %q", m.Rationale)
	}
	return match[1]
}

func TestFinancePlannerPriorInformsEV(t *testing.T) {
	p := financeTestPlanner(t)
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	if err != nil {
		t.Fatal(err)
	}
	// With no signal, the fundamentals prior tilts the odds: ACME (PE 14, positive
	// EPS) above the base rate, GLOBEX (PE 30, PB 5) below it.
	probs := map[string]string{}
	for _, m := range res.RankedMoves {
		probs[m.Key] = winProbOf(t, m)
	}
	if probs["thesis:ACME"] != "0.54" {
		t.Errorf("ACME prior = %s, want 0.54 (cheap PE +0.03, positive EPS +0.01)", probs["thesis:ACME"])
	}
	if probs["thesis:GLOBEX"] != "0.47" {
		t.Errorf("GLOBEX prior = %s, want 0.47 (rich PE -0.02, rich PB -0.02, positive EPS +0.01)", probs["thesis:GLOBEX"])
	}
}

func TestFinancePlannerNeutralEVWithoutInformation(t *testing.T) {
	p := financeTestPlanner(t)
	// A ticker with no fundamentals and no price history carries no information:
	// the EV component must stay neutral rather than rewarding volatility.
	g := investingGoal()
	g.Context.Assets = []domain.Asset{{Name: "ZZZ", Kind: "ticker"}}
	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: g})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RankedMoves[0].Rationale, "ev=0.50 (p=0.50)") {
		t.Errorf("uninformed thesis should have neutral EV, rationale: %q", res.RankedMoves[0].Rationale)
	}
}

func TestFinancePlannerHintAppliesOnlyToNamedTicker(t *testing.T) {
	p := financeTestPlanner(t)
	base, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal()})
	if err != nil {
		t.Fatal(err)
	}
	hinted, err := p.GeneratePlan(context.Background(), PlanRequest{
		Goal:       investingGoal(), // ACME and GLOBEX
		SignalKind: "valuation_change",
		SignalPayload: map[string]any{
			"kind": "valuation_change", "ticker": "ACME", "metric": "pe",
			"value": 14.0, "fair_value": 20.0, "gap_pct": 0.30,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	prob := func(res PlanResult, key string) string {
		t.Helper()
		for _, m := range res.RankedMoves {
			if m.Key == key {
				return winProbOf(t, m)
			}
		}
		t.Fatalf("move %q missing", key)
		return ""
	}
	// The hint moves ACME's win probability (averaged with its prior) but must
	// not leak onto GLOBEX, whose prior stays as-is.
	if prob(hinted, "thesis:ACME") == prob(base, "thesis:ACME") {
		t.Error("ACME win probability should move on its valuation hint")
	}
	if got, want := prob(hinted, "thesis:GLOBEX"), prob(base, "thesis:GLOBEX"); got != want {
		t.Errorf("GLOBEX must not inherit ACME's hint: %s vs %s", got, want)
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

// narrationRecorder captures the request it receives and returns a fixed summary.
type narrationRecorder struct {
	req PlanRequest
}

func (r *narrationRecorder) Name() string { return "recording" }

func (r *narrationRecorder) GeneratePlan(_ context.Context, req PlanRequest) (PlanResult, error) {
	r.req = req
	return PlanResult{
		Summary:    "Narrated thesis colour.",
		Invocation: domain.ModelInvocation{Model: "test-model"},
	}, nil
}

func TestFinancePlannerHybridNarration(t *testing.T) {
	prov, err := marketdata.NewOfflineProvider()
	if err != nil {
		t.Fatal(err)
	}
	inner := &narrationRecorder{}
	asOf := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	p := NewFinancePlanner(FinanceConfig{
		Provider:       prov,
		Inner:          inner,
		Now:            func() time.Time { return asOf },
		PackVersion:    "1",
		PromptTemplate: "DOMAIN: INVESTING test guidance",
	})

	res, err := p.GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal(), SignalNote: "quarterly review"})
	if err != nil {
		t.Fatal(err)
	}

	// The narrator receives the pack guidance and the authoritative scores.
	if inner.req.SystemPromptOverride != "DOMAIN: INVESTING test guidance" {
		t.Errorf("inner should receive the pack template, got %q", inner.req.SystemPromptOverride)
	}
	if !strings.Contains(inner.req.SignalNote, "quarterly review") {
		t.Errorf("inner note should carry the signal note: %q", inner.req.SignalNote)
	}
	if !strings.Contains(inner.req.SignalNote, "Thesis: ACME") || !strings.Contains(inner.req.SignalNote, "ev=") {
		t.Errorf("inner note should carry the numeric scores: %q", inner.req.SignalNote)
	}

	// Narration lands in reasoning; provenance records the narrator.
	if !strings.Contains(res.Provenance.ReasoningSummary, "Narrated thesis colour.") {
		t.Errorf("narration missing from reasoning: %q", res.Provenance.ReasoningSummary)
	}
	if len(res.Provenance.Contributors) != 1 || res.Provenance.Contributors[0].Role != "narrator" || res.Provenance.Contributors[0].Model != "test-model" {
		t.Errorf("expected a narrator contributor, got %+v", res.Provenance.Contributors)
	}

	// Numbers are untouched by the narrator: same moves as the pure numeric run.
	numeric, err := financeTestPlanner(t).GeneratePlan(context.Background(), PlanRequest{Goal: investingGoal(), SignalNote: "quarterly review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RankedMoves) != len(numeric.RankedMoves) {
		t.Fatal("hybrid must not change the move set")
	}
	for i := range res.RankedMoves {
		if res.RankedMoves[i].Confidence != numeric.RankedMoves[i].Confidence || res.RankedMoves[i].Title != numeric.RankedMoves[i].Title {
			t.Errorf("hybrid changed numbers at %d: %+v vs %+v", i, res.RankedMoves[i], numeric.RankedMoves[i])
		}
	}
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
