package domain

import (
	"reflect"
	"strings"
	"testing"
)

func mv(key string, deps ...string) RankedMove {
	return RankedMove{Key: key, Title: key, DependsOn: deps}
}

func depsByKey(moves []RankedMove) map[string][]string {
	out := make(map[string][]string, len(moves))
	for _, m := range moves {
		out[m.Key] = m.DependsOn
	}
	return out
}

func TestSanitizeMoveGraph(t *testing.T) {
	tests := []struct {
		name  string
		in    []RankedMove
		want  map[string][]string // expected DependsOn per key
		count int                 // expected move count (never changes)
	}{
		{
			name:  "valid DAG is unchanged",
			in:    []RankedMove{mv("a"), mv("b", "a"), mv("c", "a", "b")},
			want:  map[string][]string{"a": nil, "b": {"a"}, "c": {"a", "b"}},
			count: 3,
		},
		{
			name:  "dangling reference dropped",
			in:    []RankedMove{mv("a", "ghost"), mv("b")},
			want:  map[string][]string{"a": nil, "b": nil},
			count: 2,
		},
		{
			name:  "self reference dropped",
			in:    []RankedMove{mv("a", "a")},
			want:  map[string][]string{"a": nil},
			count: 1,
		},
		{
			name:  "duplicate reference within a move dropped",
			in:    []RankedMove{mv("a"), mv("b", "a", "a")},
			want:  map[string][]string{"a": nil, "b": {"a"}},
			count: 2,
		},
		{
			name:  "two-node cycle broken",
			in:    []RankedMove{mv("a", "b"), mv("b", "a")},
			count: 2,
			// one of the two edges is removed; both can't survive
		},
		{
			name:  "three-node cycle broken",
			in:    []RankedMove{mv("a", "c"), mv("b", "a"), mv("c", "b")},
			count: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeMoveGraph(tc.in)
			if len(got) != tc.count {
				t.Fatalf("move count = %d, want %d", len(got), tc.count)
			}
			// The result must always be acyclic.
			if c := findCycle(got); c != "" {
				t.Fatalf("sanitized graph still has a cycle: %s", c)
			}
			if tc.want != nil {
				if g := depsByKey(got); !reflect.DeepEqual(g, tc.want) {
					t.Fatalf("deps = %v, want %v", g, tc.want)
				}
			}
		})
	}
}

func TestSanitizeMoveGraphNil(t *testing.T) {
	if got := SanitizeMoveGraph(nil); got != nil {
		t.Fatalf("SanitizeMoveGraph(nil) = %v, want nil", got)
	}
}

func TestSanitizeMoveGraphDoesNotMutateInput(t *testing.T) {
	in := []RankedMove{mv("a", "a", "ghost"), mv("b")}
	_ = SanitizeMoveGraph(in)
	if !reflect.DeepEqual(in[0].DependsOn, []string{"a", "ghost"}) {
		t.Fatalf("input mutated: %v", in[0].DependsOn)
	}
}

func TestMoveGraphIssues(t *testing.T) {
	moves := []RankedMove{
		mv("a", "a", "ghost"), // self + dangling
		mv("a"),               // duplicate key
		mv("b", "c"),
		mv("c", "b"), // cycle b<->c
	}
	issues := MoveGraphIssues(moves)
	if len(issues) == 0 {
		t.Fatal("expected issues, got none")
	}
	want := map[string]bool{"duplicate": false, "itself": false, "unknown": false, "cycle": false}
	for _, is := range issues {
		for k := range want {
			if strings.Contains(is, k) {
				want[k] = true
			}
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("expected an issue mentioning %q; got %v", k, issues)
		}
	}
}

func TestMoveGraphIssuesCleanGraph(t *testing.T) {
	moves := []RankedMove{mv("a"), mv("b", "a")}
	if issues := MoveGraphIssues(moves); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}
