package domain

import (
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	id := NewID("plan")
	if !strings.HasPrefix(id, "plan_") {
		t.Fatalf("expected plan_ prefix, got %q", id)
	}
	if id == NewID("plan") {
		t.Fatal("two IDs should not collide")
	}
	if got := strings.TrimPrefix(id, "plan_"); got == "" {
		t.Fatal("expected a non-empty random suffix")
	}
}

func TestLevelValid(t *testing.T) {
	tests := []struct {
		level Level
		want  bool
	}{
		{LevelLow, true},
		{LevelMedium, true},
		{LevelHigh, true},
		{Level("critical"), false},
		{Level(""), false},
	}
	for _, tt := range tests {
		if got := tt.level.Valid(); got != tt.want {
			t.Errorf("Level(%q).Valid() = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestPlayerKindValid(t *testing.T) {
	for _, k := range []PlayerKind{PlayerPerson, PlayerTeam, PlayerCompany, PlayerSystem} {
		if !k.Valid() {
			t.Errorf("expected %q to be valid", k)
		}
	}
	if PlayerKind("alien").Valid() {
		t.Error("unexpected valid kind")
	}
}

func TestOutcomeResultValid(t *testing.T) {
	for _, r := range []OutcomeResult{OutcomeSuccess, OutcomeFailure, OutcomePartial, OutcomeInconclusive} {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}
	if OutcomeResult("maybe").Valid() {
		t.Error("unexpected valid result")
	}
}

func TestSignalNote(t *testing.T) {
	s := Signal{Kind: "market", Description: "demand dropped"}
	if got := s.Note(); got != "market: demand dropped" {
		t.Errorf("Note() = %q", got)
	}
	s2 := Signal{Kind: "market"}
	if got := s2.Note(); got != "market" {
		t.Errorf("Note() = %q", got)
	}
}
