package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// demoScenario is a self-contained story the demo walks through: a goal, a
// signal that should move the plan, and the outcome later observed in the
// real world.
type demoScenario struct {
	tagline       string // one line under the banner describing the scenario
	goal          app.CreateGoalInput
	signalKind    string
	signalDesc    string
	signalPayload map[string]any
	signalNote    string // one line explaining why this signal matters
	outcome       app.OutcomeInput
	outcomeNote   string // one line tying the outcome back to the plan
}

// demoScenarios maps the demo's positional argument to its scenario. "growth"
// is the default; "investing" shows the numeric finance planner reacting to a
// thesis break.
func demoScenarios() map[string]demoScenario {
	return map[string]demoScenario{
		"growth": {
			tagline: "scenario: an early-stage SaaS deciding how to grow",
			goal: app.CreateGoalInput{
				Domain:    "growth",
				Objective: "Grow the product to 1,000 paying customers within two quarters",
				Metric:    "paying customers",
				Target:    "1000",
				Context: domain.Context{
					Situation: "Early-stage B2B SaaS with 120 paying customers, mostly inbound. Small team, limited runway.",
					Assets: []domain.Asset{
						{Name: "founder network in target vertical", Kind: "network"},
						{Name: "high net revenue retention", Kind: "product"},
					},
					Constraints: []domain.Constraint{
						{Name: "limited engineering capacity", Kind: "time"},
						{Name: "12 months runway", Kind: "budget"},
					},
				},
			},
			signalKind: "competitive_shift",
			signalDesc: "A well-funded competitor just launched a free tier targeting the same vertical, slowing inbound signups.",
			signalNote: "The engine judges materiality itself — it does not blindly regenerate on every ping:",
			outcome: app.OutcomeInput{
				PlanVersion:     1,
				MoveRank:        1,
				Result:          domain.OutcomeFailure,
				ObservedSignals: []string{"no movement in paying customers after the full window"},
				Notes:           "Kill criterion hit: inbound slowed after the competitor's free-tier launch.",
			},
			outcomeNote: "Later replans never orphan this record; 'dde calibrate' uses outcomes like it to test stated confidence against reality.",
		},
		"investing": {
			tagline: "scenario: a thesis-driven position whose thesis breaks (offline fixtures; educational, not financial advice)",
			goal: app.CreateGoalInput{
				Domain:    "investing",
				Objective: "Build a thesis-driven position in ACME over the next year",
				Metric:    "calibrated thesis conviction",
				Target:    "a sized position with explicit invalidation criteria",
				Context: domain.Context{
					Situation: "ACME trades at a discount to normalised earnings after a transient demand air-pocket.",
					Facts: []string{
						"Balance sheet is net-cash",
						"Management has a credible margin-recovery plan",
					},
					Assets: []domain.Asset{
						{Name: "ACME", Kind: "ticker", Description: "Primary thesis instrument"},
						{Name: "sector research edge", Kind: "edge"},
						{Name: "investable capital", Kind: "capital"},
					},
					Constraints: []domain.Constraint{
						{Name: "max 10% drawdown on this position", Kind: "drawdown_limit"},
						{Name: "moderate risk tolerance", Kind: "risk_tolerance"},
						{Name: "2 year horizon", Kind: "time_horizon"},
					},
				},
			},
			signalKind: "thesis_break",
			signalDesc: "ACME's largest customer terminated its contract, breaking the demand-recovery thesis.",
			signalPayload: map[string]any{
				"ticker": "ACME",
				"reason": "largest customer terminated contract",
				"hard":   true,
			},
			signalNote: "A hard thesis break is exactly the signal the position's kill criteria exist for:",
			outcome: app.OutcomeInput{
				PlanVersion:     1,
				MoveRank:        1,
				Result:          domain.OutcomeFailure,
				ObservedSignals: []string{"thesis invalidation event: largest customer terminated contract"},
				Notes:           "Position exited per the kill criterion; loss contained within the drawdown limit.",
			},
			outcomeNote: "The exit is on the record against the exact sized move that was held, not a vague memory of 'the ACME idea'.",
		},
	}
}

// newDemoCommand walks the full decision loop — goal → plan v1 → signal →
// materiality decision → plan v2 → outcome — on a built-in scenario, entirely
// in memory, and narrates each step. It is the fastest way to see what the
// engine does that a one-off LLM answer cannot: nothing to configure, no input
// files, no API keys (the planner honours DDE_PLANNER, so the same demo can be
// re-run against a real provider).
func newDemoCommand() *cobra.Command {
	return &cobra.Command{
		Use:       "demo [growth|investing]",
		Short:     "Walk the full decision loop on a built-in scenario (offline, no flags)",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"growth", "investing"},
		RunE: func(cmd *cobra.Command, args []string) error {
			key := "growth"
			if len(args) == 1 {
				key = args[0]
			}
			sc, ok := demoScenarios()[key]
			if !ok {
				return fmt.Errorf("unknown scenario %q (available: growth, investing)", key)
			}
			return runDemo(cmd, sc)
		},
	}
}

func runDemo(cmd *cobra.Command, sc demoScenario) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	svc, err := newMemoryService()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, `
────────────────────────────────────────────────────────────────────
 dde demo — the full decision loop, offline, deterministic
────────────────────────────────────────────────────────────────────
 %s

 An LLM answer is a chat reply: the world changes, the answer does
 not, and nobody remembers why it was trusted. This engine keeps a
 decision as a versioned, auditable object instead. Watch the loop:
`, sc.tagline)

	// [1/5] Goal.
	goal, err := svc.CreateGoal(ctx, sc.goal)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n[1/5] GOAL — the objective and what is known at decision time\n      %q\n", goal.Objective)
	fmt.Fprintf(out, "      assets:      %s\n", joinNames(len(sc.goal.Context.Assets), func(i int) string { return sc.goal.Context.Assets[i].Name }))
	fmt.Fprintf(out, "      constraints: %s\n", joinNames(len(sc.goal.Context.Constraints), func(i int) string { return sc.goal.Context.Constraints[i].Name }))

	// [2/5] Plan v1.
	v1, err := svc.GeneratePlan(ctx, goal.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "\n[2/5] PLAN v1 — ranked moves, not a paragraph of advice")
	printDemoMoves(out, v1.RankedMoves)
	if len(v1.RankedMoves) > 0 {
		top := v1.RankedMoves[0]
		if len(top.Experiment.KillCriteria) > 0 {
			fmt.Fprintf(out, "      move #1 ships with an experiment: %q\n", top.Experiment.Title)
			fmt.Fprintf(out, "      and a kill criterion a machine can check: %q\n", top.Experiment.KillCriteria[0])
		}
	}

	// [3/5] Signal → materiality decision.
	sig, err := svc.ApplySignal(ctx, app.SignalInput{
		GoalID:      goal.ID,
		Kind:        sc.signalKind,
		Description: sc.signalDesc,
		Payload:     sc.signalPayload,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n[3/5] SIGNAL — the world changed\n")
	fmt.Fprintf(out, "      %s: %s\n\n", sc.signalKind, wrapIndent(sc.signalDesc, 60, "      "))
	fmt.Fprintf(out, "      %s\n", wrapIndent(sc.signalNote, 60, "      "))
	fmt.Fprintf(out, "      material: %v — %s\n", sig.Material, sig.Reason)

	// [4/5] Plan v2, diffed against v1.
	if sig.Material {
		fmt.Fprintln(out, "\n[4/5] PLAN v2 — appended, v1 is kept, the diff is explainable")
		printDemoDiff(out, v1.RankedMoves, sig.PlanVersion.RankedMoves)
		fmt.Fprintf(out, "      provenance: planner=%s snapshot=%s (what the planner saw is reproducible)\n",
			sig.PlanVersion.Provenance.Planner, sig.PlanVersion.InputSnapshotID)
	} else {
		fmt.Fprintln(out, "\n[4/5] PLAN v2 — not created: the signal was immaterial, and that decision is recorded too")
	}

	// [5/5] Outcome against the immutable v1 address.
	in := sc.outcome
	in.GoalID = goal.ID
	outcome, err := svc.RecordOutcome(ctx, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n[5/5] OUTCOME — reality is recorded against the exact move acted on\n")
	fmt.Fprintf(out, "      (plan v%d, move #%d) %q → %s\n", outcome.PlanVersion, outcome.MoveRank, outcome.MoveTitle, outcome.Result)
	fmt.Fprintf(out, "      %s\n", wrapIndent(sc.outcomeNote, 60, "      "))

	fmt.Fprintln(out, `
────────────────────────────────────────────────────────────────────
 That was the product: goal → plan v1 → signal → materiality →
 plan v2 → outcome. Versioned, explained, auditable.

 Next:
   dde demo investing                                  same loop, numeric planner
   dde evaluate "your goal in one sentence"            your own goal
   dde serve                                           REST API (+ /mcp)
   DDE_PLANNER=anthropic dde demo                      same loop, real LLM
────────────────────────────────────────────────────────────────────`)
	return nil
}

// printDemoMoves renders ranked moves as a compact two-line list.
func printDemoMoves(out io.Writer, moves []domain.RankedMove) {
	for _, m := range moves {
		fmt.Fprintf(out, "      #%d %s\n", m.Rank, m.Title)
		fmt.Fprintf(out, "         confidence %.2f · impact %s · effort %s · risk %s\n",
			m.Confidence, m.ExpectedImpact, m.Effort, m.Risk)
	}
}

// printDemoDiff renders v2 moves annotated with what changed relative to v1,
// matching moves by their stable key (normalised title when the key is empty).
func printDemoDiff(out io.Writer, v1, v2 []domain.RankedMove) {
	prev := make(map[string]domain.RankedMove, len(v1))
	for _, m := range v1 {
		prev[demoMoveKey(m)] = m
	}
	seen := make(map[string]bool, len(v2))
	for _, m := range v2 {
		k := demoMoveKey(m)
		seen[k] = true
		old, ok := prev[k]
		switch {
		case !ok:
			fmt.Fprintf(out, "      #%d %s  (new)\n", m.Rank, m.Title)
		case old.Rank != m.Rank:
			fmt.Fprintf(out, "      #%d %s  (was #%d, confidence %.2f → %.2f)\n",
				m.Rank, m.Title, old.Rank, old.Confidence, m.Confidence)
		default:
			fmt.Fprintf(out, "      #%d %s  (confidence %.2f → %.2f)\n",
				m.Rank, m.Title, old.Confidence, m.Confidence)
		}
	}
	for _, m := range v1 {
		if !seen[demoMoveKey(m)] {
			fmt.Fprintf(out, "      –  %s  (dropped from v1)\n", m.Title)
		}
	}
}

// demoMoveKey mirrors how materiality matches moves across versions: the
// semantic Key when present, otherwise the normalised title.
func demoMoveKey(m domain.RankedMove) string {
	if m.Key != "" {
		return m.Key
	}
	return strings.ToLower(strings.TrimSpace(m.Title))
}

// joinNames renders n names produced by get as a " · "-separated list.
func joinNames(n int, get func(int) string) string {
	names := make([]string, n)
	for i := range names {
		names[i] = get(i)
	}
	return strings.Join(names, " · ")
}

// wrapIndent word-wraps s at width, prefixing continuation lines with indent.
func wrapIndent(s string, width int, indent string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	line := 0
	for i, w := range words {
		if i > 0 {
			if line+1+len(w) > width {
				b.WriteString("\n" + indent)
				line = 0
			} else {
				b.WriteString(" ")
				line++
			}
		}
		b.WriteString(w)
		line += len(w)
	}
	return b.String()
}
