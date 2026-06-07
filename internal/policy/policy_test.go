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
