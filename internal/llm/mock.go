package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// mockPromptVersion identifies the heuristics version used by the mock planner.
const mockPromptVersion = "mock-v1"

// signalUncertaintyPenalty is the confidence reduction applied to the prior
// recommendation when a new signal arrives. It is large enough to exceed the
// default materiality threshold, so a signal reliably triggers a new plan
// version — making the dynamic replanning loop observable end to end.
const signalUncertaintyPenalty = 0.15

// MockPlanner is a deterministic, dependency-free planner. Given the same goal,
// context and signal note it always produces the same ranked moves, which makes
// the whole system testable and runnable without external services.
//
// It is not a model: it applies a small set of transparent heuristics
// (double-down on the strongest asset, neutralise the binding constraint, run a
// cheap validation) and derives confidence deterministically from a hash of the
// decision-relevant inputs.
type MockPlanner struct{}

// NewMockPlanner returns a ready-to-use deterministic planner.
func NewMockPlanner() *MockPlanner { return &MockPlanner{} }

// Name implements Planner.
func (*MockPlanner) Name() string { return "mock" }

// GeneratePlan implements Planner.
func (*MockPlanner) GeneratePlan(_ context.Context, req PlanRequest) (PlanResult, error) {
	g := req.Goal
	if g.Objective == "" {
		return PlanResult{}, fmt.Errorf("llm: goal objective is required")
	}

	// The snapshot ID is a faithful audit fingerprint of exactly what was reasoned
	// over (goal, context and any signal note), shared with every other planner.
	snapshotID := inputSnapshotID(g, req.SignalNote)

	// The confidence baseline, however, is derived only from the goal and context
	// (excluding the signal note) so that an initial plan and a re-plan for the
	// same goal share a stable baseline. A new signal then applies an explicit
	// uncertainty penalty on top of that baseline — modelling the intuition that
	// fresh information reduces confidence in the prior recommendation. This keeps
	// the mock fully deterministic while making replanning observable.
	baseSeed := hashSnapshot(snapshotInput{
		Objective: g.Objective,
		Metric:    g.Metric,
		Target:    g.Target,
		Context:   g.Context,
	})
	base := 0.70 + float64(baseSeed[0]%16)/100.0 // 0.70 .. 0.85
	if req.SignalNote != "" {
		base -= signalUncertaintyPenalty
	}

	asset := firstAssetName(g.Context.Assets, "your strongest current asset")
	constraint := firstConstraintName(g.Context.Constraints, "your most binding constraint")
	metric := g.Metric
	if metric == "" {
		metric = "the goal metric"
	}

	moves := []domain.RankedMove{
		{
			Rank:           1,
			Key:            "double-down:" + asset,
			Title:          "Double down on " + asset,
			Description:    fmt.Sprintf("Concentrate effort and resources on %s to make measurable progress toward: %s.", asset, g.Objective),
			Confidence:     round2(base),
			ExpectedImpact: domain.LevelHigh,
			Effort:         domain.LevelMedium,
			Risk:           domain.LevelLow,
			Rationale:      fmt.Sprintf("This is the lowest-risk path to impact: it exploits an existing advantage (%s) rather than building a new capability, so it can move %s quickly.", asset, metric),
			Experiment: domain.Experiment{
				Title:        "Focused 7-day push leveraging " + asset,
				DurationDays: 7,
				SuccessSignals: []string{
					"Measurable upward movement in " + metric,
					"Positive qualitative feedback from the first cohort",
				},
				KillCriteria: []string{
					"No movement in " + metric + " after the full window",
					"Effort required exceeds the available budget or time",
				},
			},
			FallbackMoves: []string{"Reallocate effort to the second-strongest asset"},
			// Commit only after the cheap validation and the constraint work land,
			// so the top recommendation is the join point of the parallel front.
			DependsOn:     []string{"validation-experiment", "neutralise:" + constraint},
			ParallelGroup: "commit",
		},
		{
			Rank:           2,
			Key:            "neutralise:" + constraint,
			Title:          "Neutralise " + constraint,
			Description:    fmt.Sprintf("Reduce or remove the limiting effect of %s so that higher-impact moves become viable.", constraint),
			Confidence:     round2(base - 0.12),
			ExpectedImpact: domain.LevelMedium,
			Effort:         domain.LevelMedium,
			Risk:           domain.LevelMedium,
			Rationale:      fmt.Sprintf("Progress is currently gated by %s. Relaxing it unlocks options that are otherwise blocked, at moderate cost and risk.", constraint),
			Experiment: domain.Experiment{
				Title:        "Two-week effort to relax " + constraint,
				DurationDays: 14,
				SuccessSignals: []string{
					"The constraint no longer blocks the top-ranked move",
					"Lead time or cost associated with " + constraint + " drops measurably",
				},
				KillCriteria: []string{
					"The constraint proves structural and cannot be relaxed in the window",
					"Cost of relaxing it exceeds the value it unlocks",
				},
			},
			FallbackMoves: []string{"Design the plan to work within " + constraint + " instead of removing it"},
			// Independent of the validation experiment: the two can run concurrently.
			ParallelGroup: "discover",
		},
		{
			Rank:           3,
			Key:            "validation-experiment",
			Title:          "Run a low-cost validation experiment",
			Description:    fmt.Sprintf("Test the riskiest assumption behind %q with a small, time-boxed experiment before committing further.", g.Objective),
			Confidence:     round2(base - 0.22),
			ExpectedImpact: domain.LevelMedium,
			Effort:         domain.LevelLow,
			Risk:           domain.LevelLow,
			Rationale:      "When the path is uncertain, buying information cheaply is higher expected value than committing to a single bet. This keeps optionality open.",
			Experiment: domain.Experiment{
				Title:        "Five-day assumption test",
				DurationDays: 5,
				SuccessSignals: []string{
					"The core assumption holds under a real, if small, test",
					"Clear signal on which direction to scale next",
				},
				KillCriteria: []string{
					"The assumption fails the test",
					"Results are inconclusive after the window — pivot the question",
				},
			},
			FallbackMoves: []string{"Escalate to a slightly larger experiment if the first is inconclusive"},
			// No prerequisites: runs immediately, in parallel with the constraint work.
			ParallelGroup: "discover",
		},
	}

	summary := fmt.Sprintf(
		"Three ranked action paths toward %q, prioritising exploitation of %s while de-risking %s.",
		g.Objective, asset, constraint,
	)
	reasoning := fmt.Sprintf(
		"Ranked by expected impact against risk and effort. The top path exploits an existing asset (%s) for fast, low-risk movement; the second removes the binding constraint (%s); the third buys information cheaply where the path is uncertain.",
		asset, constraint,
	)
	if req.SignalNote != "" {
		reasoning += fmt.Sprintf(" Re-evaluated in response to a new signal (%s).", req.SignalNote)
	}

	prov := domain.DecisionProvenance{
		ReasoningSummary: reasoning,
		InputSnapshotID:  snapshotID,
		Planner:          "mock",
		PromptVersion:    mockPromptVersion,
		Model:            "none",
	}

	return PlanResult{
		Summary:     summary,
		RankedMoves: moves,
		Provenance:  prov,
		Invocation: domain.ModelInvocation{
			Model:         "none",
			PromptVersion: mockPromptVersion,
			// Token/latency fields stay zero for the mock; they become meaningful
			// once a real model client is wired in.
		},
	}, nil
}

// hashSnapshot returns a stable SHA-256 over the canonical JSON of the decision
// inputs. JSON encoding of the snapshot struct is deterministic because Go
// marshals struct fields in declaration order and maps by sorted key.
func hashSnapshot(s snapshotInput) [32]byte {
	b, _ := json.Marshal(s)
	return sha256.Sum256(b)
}

func firstAssetName(assets []domain.Asset, fallback string) string {
	for _, a := range assets {
		if a.Name != "" {
			return a.Name
		}
	}
	return fallback
}

func firstConstraintName(constraints []domain.Constraint, fallback string) string {
	for _, c := range constraints {
		if c.Name != "" {
			return c.Name
		}
	}
	return fallback
}

// round2 rounds to two decimal places so confidence values are presentation-ready
// and stable.
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
