package finance

import "testing"

func TestParseMarketSignalValuation(t *testing.T) {
	ms, err := ParseMarketSignal("valuation_change", map[string]any{
		"ticker": "ACME", "metric": "pe", "value": 14.0, "fair_value": 20.0, "gap_pct": 0.30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ms.Ticker != "ACME" || ms.Valuation == nil || ms.Valuation.GapPct != 0.30 {
		t.Fatalf("bad parse: %+v", ms)
	}
	wp, ok := ms.WinProbHint()
	if !ok || wp <= 0.5 {
		t.Errorf("undervalued should tilt win prob above 0.5, got %v ok=%v", wp, ok)
	}
}

func TestParseMarketSignalThesisBreak(t *testing.T) {
	ms, err := ParseMarketSignal("thesis_break", map[string]any{
		"ticker": "ACME", "reason": "core customer lost", "hard": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ms.IsThesisBreak() || ms.ThesisBreak == nil || !ms.ThesisBreak.Hard {
		t.Fatalf("bad thesis_break parse: %+v", ms)
	}
}

func TestParseMarketSignalUnknown(t *testing.T) {
	if _, err := ParseMarketSignal("nonsense", map[string]any{}); err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestWinProbHintNone(t *testing.T) {
	ms := MarketSignal{Kind: KindMacro, Macro: &MacroPayload{Indicator: "cpi"}}
	if _, ok := ms.WinProbHint(); ok {
		t.Error("macro signal should not yield a win-prob hint")
	}
}
