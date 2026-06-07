package domain

import (
	"fmt"
	"sort"
	"strings"
)

// The moves of a plan version form a dependency DAG: a move's DependsOn lists the
// Keys of other moves in the same version that must complete first. These helpers
// keep that graph well-formed. Cross-move references are resolved by Key, scoped to
// a single version — they are not the (version, rank) addressing used for outcomes.

// SanitizeMoveGraph returns a copy of moves whose DependsOn references always form
// a valid DAG. It drops references that are empty, self-referential, point at a key
// no move carries, or are duplicated within a single move's list; and it breaks any
// cycles by removing the edge that closes them. It never reorders or drops moves.
//
// This mirrors the engine's existing "sanitise, don't fail" stance (cf.
// clampConfidence): planner output is coerced into a usable shape rather than
// rejected, so a malformed graph from a model can never corrupt a stored version.
func SanitizeMoveGraph(moves []RankedMove) []RankedMove {
	if moves == nil {
		return nil
	}

	out := make([]RankedMove, len(moves))
	copy(out, moves)

	// Keys that some move carries (valid dependency targets) and where they live.
	keyToIndices := make(map[string][]int, len(out))
	for i := range out {
		if out[i].Key != "" {
			keyToIndices[out[i].Key] = append(keyToIndices[out[i].Key], i)
		}
	}

	// Pass 1: drop empty, self, dangling and duplicate references.
	for i := range out {
		src := moves[i].DependsOn
		if len(src) == 0 {
			out[i].DependsOn = nil
			continue
		}
		own := out[i].Key
		seen := make(map[string]bool, len(src))
		kept := make([]string, 0, len(src))
		for _, dep := range src {
			if dep == "" || dep == own || seen[dep] {
				continue
			}
			if _, ok := keyToIndices[dep]; !ok {
				continue
			}
			seen[dep] = true
			kept = append(kept, dep)
		}
		if len(kept) == 0 {
			kept = nil
		}
		out[i].DependsOn = kept
	}

	// Pass 2: break cycles. DFS over moves (nodes) following DependsOn edges; an
	// edge to a node currently on the recursion stack is a back edge and is removed.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, len(out))
	remove := make([]map[string]bool, len(out))

	var visit func(i int)
	visit = func(i int) {
		color[i] = gray
		for _, dep := range out[i].DependsOn {
			if remove[i][dep] {
				continue
			}
			backEdge := false
			for _, j := range keyToIndices[dep] {
				switch color[j] {
				case gray:
					backEdge = true
				case white:
					visit(j)
				}
				if backEdge {
					break
				}
			}
			if backEdge {
				if remove[i] == nil {
					remove[i] = make(map[string]bool)
				}
				remove[i][dep] = true
			}
		}
		color[i] = black
	}
	for i := range out {
		if color[i] == white {
			visit(i)
		}
	}

	// Apply removals from the cycle-breaking pass.
	for i := range out {
		if len(remove[i]) == 0 {
			continue
		}
		kept := out[i].DependsOn[:0]
		for _, dep := range out[i].DependsOn {
			if !remove[i][dep] {
				kept = append(kept, dep)
			}
		}
		if len(kept) == 0 {
			kept = nil
		}
		out[i].DependsOn = kept
	}

	return out
}

// MoveGraphIssues reports problems with the move dependency graph without mutating
// it: duplicate keys (which make Key-based references ambiguous), self-references,
// dangling references to absent keys, and cycles. It returns a stable, sorted list
// of human-readable messages, empty when the graph is well-formed. Intended for
// tests and optional warning logs; SanitizeMoveGraph is what actually repairs.
func MoveGraphIssues(moves []RankedMove) []string {
	var issues []string

	keyCount := make(map[string]int, len(moves))
	for _, m := range moves {
		if m.Key != "" {
			keyCount[m.Key]++
		}
	}
	for key, n := range keyCount {
		if n > 1 {
			issues = append(issues, fmt.Sprintf("duplicate move key %q (%d moves)", key, n))
		}
	}

	for _, m := range moves {
		for _, dep := range m.DependsOn {
			switch {
			case dep == "":
				issues = append(issues, fmt.Sprintf("move %q has an empty dependency", m.Key))
			case dep == m.Key:
				issues = append(issues, fmt.Sprintf("move %q depends on itself", m.Key))
			case keyCount[dep] == 0:
				issues = append(issues, fmt.Sprintf("move %q depends on unknown key %q", m.Key, dep))
			}
		}
	}

	if cycle := findCycle(moves); cycle != "" {
		issues = append(issues, "dependency cycle: "+cycle)
	}

	sort.Strings(issues)
	return issues
}

// findCycle returns a "a -> b -> a" description of one dependency cycle, or "" when
// the graph (restricted to edges between existing keys) is acyclic.
func findCycle(moves []RankedMove) string {
	keyToIndices := make(map[string][]int, len(moves))
	for i, m := range moves {
		if m.Key != "" {
			keyToIndices[m.Key] = append(keyToIndices[m.Key], i)
		}
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, len(moves))
	var stack []string

	var dfs func(i int) string
	dfs = func(i int) string {
		color[i] = gray
		stack = append(stack, moves[i].Key)
		for _, dep := range moves[i].DependsOn {
			if dep == "" || dep == moves[i].Key {
				continue
			}
			for _, j := range keyToIndices[dep] {
				if color[j] == gray {
					return strings.Join(append(stack, dep), " -> ")
				}
				if color[j] == white {
					if c := dfs(j); c != "" {
						return c
					}
				}
			}
		}
		color[i] = black
		stack = stack[:len(stack)-1]
		return ""
	}
	for i := range moves {
		if color[i] == white {
			if c := dfs(i); c != "" {
				return c
			}
		}
	}
	return ""
}
