package domain

import "testing"

func TestGoalStatusValid(t *testing.T) {
	for _, s := range []GoalStatus{GoalActive, GoalOnHold, GoalResolved, GoalAbandoned} {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if GoalStatus("done").Valid() || GoalStatus("").Valid() {
		t.Error("unexpected valid status")
	}
}

func TestGoalStatusTerminal(t *testing.T) {
	terminal := map[GoalStatus]bool{
		GoalActive:    false,
		GoalOnHold:    false,
		GoalResolved:  true,
		GoalAbandoned: true,
	}
	for s, want := range terminal {
		if got := s.Terminal(); got != want {
			t.Errorf("GoalStatus(%q).Terminal() = %v, want %v", s, got, want)
		}
	}
}

func TestGoalStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		from, to GoalStatus
		want     bool
	}{
		{GoalActive, GoalOnHold, true},
		{GoalActive, GoalResolved, true},
		{GoalActive, GoalAbandoned, true},
		{GoalOnHold, GoalActive, true},
		{GoalOnHold, GoalResolved, true},
		{GoalActive, GoalActive, false},   // no-op
		{GoalResolved, GoalActive, false}, // terminal is final
		{GoalAbandoned, GoalOnHold, false},
		{GoalActive, GoalStatus("nope"), false}, // invalid target
	}
	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
			t.Errorf("%q.CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}
