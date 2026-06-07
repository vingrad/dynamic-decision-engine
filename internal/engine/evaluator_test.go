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

func TestIsMaterialOnStructureChange(t *testing.T) {
	ev := NewThresholdEvaluator()
	current := []domain.RankedMove{
		{Rank: 1, Key: "double-down", Title: "Double down", Confidence: 0.80, ParallelGroup: "core"},
		{Rank: 2, Key: "validate", Title: "Validate", Confidence: 0.60},
	}

	// Same keys, order and confidence, but the top move gains a dependency: material.
	depChanged := []domain.RankedMove{
		{Rank: 1, Key: "double-down", Title: "Double down", Confidence: 0.80, ParallelGroup: "core", DependsOn: []string{"validate"}},
		{Rank: 2, Key: "validate", Title: "Validate", Confidence: 0.60},
	}
	if material, reason := ev.IsMaterial(current, depChanged); !material {
		t.Error("added dependency should be material")
	} else if reason != "execution dependencies changed" {
		t.Errorf("reason = %q, want %q", reason, "execution dependencies changed")
	}

	// Only the parallel group label changed: material.
	groupChanged := []domain.RankedMove{
		{Rank: 1, Key: "double-down", Title: "Double down", Confidence: 0.80, ParallelGroup: "commit"},
		{Rank: 2, Key: "validate", Title: "Validate", Confidence: 0.60},
	}
	if material, reason := ev.IsMaterial(current, groupChanged); !material {
		t.Error("changed parallel group should be material")
	} else if reason != "parallel grouping changed" {
		t.Errorf("reason = %q, want %q", reason, "parallel grouping changed")
	}

	// Identical structure (dependency order is irrelevant): NOT material.
	current2 := []domain.RankedMove{
		{Rank: 1, Key: "a", Title: "A", Confidence: 0.80, DependsOn: []string{"b", "c"}},
		{Rank: 2, Key: "b", Title: "B", Confidence: 0.60},
		{Rank: 3, Key: "c", Title: "C", Confidence: 0.50},
	}
	reordered := []domain.RankedMove{
		{Rank: 1, Key: "a", Title: "A", Confidence: 0.80, DependsOn: []string{"c", "b"}},
		{Rank: 2, Key: "b", Title: "B", Confidence: 0.60},
		{Rank: 3, Key: "c", Title: "C", Confidence: 0.50},
	}
	if material, reason := ev.IsMaterial(current2, reordered); material {
		t.Errorf("dependency reordering should be immaterial, got material (%s)", reason)
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
