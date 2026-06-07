package engine

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func TestIsMaterialKeyedOnStableKey(t *testing.T) {
	ev := NewThresholdEvaluator()
	current := []domain.RankedMove{
		{Rank: 1, Key: "expand-paid-search", Title: "Expand paid search", Confidence: 0.80},
		{Rank: 2, Key: "fix-onboarding", Title: "Fix onboarding", Confidence: 0.60},
	}

	// Same keys, top move reworded and confidence unchanged: NOT material.
	reworded := []domain.RankedMove{
		{Rank: 1, Key: "expand-paid-search", Title: "Scale up paid acquisition", Confidence: 0.80},
		{Rank: 2, Key: "fix-onboarding", Title: "Fix onboarding", Confidence: 0.60},
	}
	if material, reason := ev.IsMaterial(current, reworded); material {
		t.Errorf("reworded title with same key should be immaterial, got material (%s)", reason)
	}

	// Different top key: material.
	changed := []domain.RankedMove{
		{Rank: 1, Key: "raise-prices", Title: "Raise prices", Confidence: 0.80},
		{Rank: 2, Key: "fix-onboarding", Title: "Fix onboarding", Confidence: 0.60},
	}
	if material, _ := ev.IsMaterial(current, changed); !material {
		t.Error("different top-move key should be material")
	}
}

func TestIsMaterialSlugFallbackWhenNoKey(t *testing.T) {
	ev := NewThresholdEvaluator()
	current := []domain.RankedMove{{Rank: 1, Title: "Expand Paid-Search!", Confidence: 0.80}}
	// Same move, only punctuation/casing/spacing differ: slug normalises to equal.
	candidate := []domain.RankedMove{{Rank: 1, Title: "expand   paid search", Confidence: 0.80}}
	if material, reason := ev.IsMaterial(current, candidate); material {
		t.Errorf("formatting-only title change should be immaterial via slug, got material (%s)", reason)
	}
}

func TestSlugNormalisation(t *testing.T) {
	cases := map[string]string{
		"Expand Paid-Search!": "expand-paid-search",
		"  Hello, World  ":    "hello-world",
		"AAPL/MSFT":           "aapl-msft",
		"":                    "",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
