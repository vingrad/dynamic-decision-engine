package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmptyPath(t *testing.T) {
	p, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Domains) != 0 {
		t.Errorf("empty path should yield empty policy, got %v", p.Domains)
	}
}

func TestLoadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	body := `{"domains":{"investing":{"confidence_delta":0.03,"scoring":{"weights":{"ev":1,"risk":1,"liquidity":1,"horizon":1},"risk":{"max_portfolio_risk_pct":0.01,"max_position_pct":0.10,"kelly_fraction":0.20}}}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dp, ok := p.For("investing")
	if !ok {
		t.Fatal("expected investing policy")
	}
	if dp.ConfidenceDelta == nil || *dp.ConfidenceDelta != 0.03 {
		t.Errorf("confidence_delta not parsed: %+v", dp.ConfidenceDelta)
	}
	if dp.Scoring == nil || dp.Scoring.Risk.MaxPositionPct != 0.10 {
		t.Errorf("scoring not parsed: %+v", dp.Scoring)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/no/such/policy.json"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadPlannerSpec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	body := "domains:\n  growth:\n    planner:\n      planner: multi\n      multi_mode: ensemble\n      multi_providers: [anthropic, openai]\n      model: claude-x\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dp, _ := p.For("growth")
	if dp.Planner == nil {
		t.Fatal("expected planner spec")
	}
	if dp.Planner.Planner != "multi" || dp.Planner.MultiMode != "ensemble" ||
		len(dp.Planner.MultiProviders) != 2 || dp.Planner.Model != "claude-x" {
		t.Errorf("planner spec not parsed: %+v", dp.Planner)
	}
}

func TestPolicyValidateRejects(t *testing.T) {
	cases := map[string]PlannerSpec{
		"unknown backend":      {Planner: "bogus"},
		"finance per-domain":   {Planner: "finance"},
		"multi one provider":   {Planner: "multi", MultiMode: "ensemble", MultiProviders: []string{"anthropic"}},
		"multi bad mode":       {Planner: "multi", MultiMode: "nope", MultiProviders: []string{"anthropic", "openai"}},
		"verify mock verifier": {Planner: "multi", MultiMode: "verify", MultiProviders: []string{"anthropic", "mock"}},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			p := Policy{Domains: map[string]DomainPolicy{"x": {Planner: &spec}}}
			if err := p.Validate(); err == nil {
				t.Errorf("expected validation error for %q", name)
			}
		})
	}

	// A valid spec passes.
	ok := Policy{Domains: map[string]DomainPolicy{"x": {Planner: &PlannerSpec{
		Planner: "multi", MultiMode: "verify", MultiProviders: []string{"anthropic", "openai"},
	}}}}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid spec rejected: %v", err)
	}
}

func TestLoadRejectsBadPlanner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	body := "domains:\n  growth:\n    planner:\n      planner: multi\n      multi_mode: route\n      multi_providers: [anthropic]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load should fail on an invalid planner spec")
	}
}
