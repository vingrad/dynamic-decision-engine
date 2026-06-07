package pack

import "github.com/vingrad/dynamic-decision-engine/internal/domain"

const growthPromptTemplate = `DOMAIN: GROWTH

Treat each move as a growth experiment or loop aimed at one primary metric. For
every move:
- Name the funnel stage it targets (acquisition, activation, retention, revenue,
  referral) and the single metric it should move.
- Prefer cheap, reversible bets with short feedback cycles over large commitments.
- Distinguish leading indicators (early signal) from lagging ones (the goal metric).
- Make kill_criteria the failure to move the targeted metric within the window.

Be honest about channel saturation and cost; a move that cannot scale past a small
audience should say so.`

// growthPack covers founder/product growth goals (e.g. the founder-growth example).
func growthPack() Descriptor {
	return Descriptor{
		ID:             "growth",
		Name:           "Growth",
		Version:        "1",
		PromptVersion:  "growth-v1",
		PromptTemplate: growthPromptTemplate,
		Eval:           EvaluatorConfig{ConfidenceDelta: 0.10},
		Vocab: Vocabulary{
			AssetKinds:      []string{"product", "channel", "audience", "data", "brand", "team"},
			ConstraintKinds: []string{"budget", "runway", "team_bandwidth", "channel_saturation", "compliance"},
			SignalKinds:     []string{"metric_change", "channel_performance", "cohort", "competitor", "customer_feedback"},
		},
		Validate: func(g domain.Goal) []ValidationIssue {
			var issues []ValidationIssue
			if g.Metric == "" {
				issues = append(issues, ValidationIssue{
					Field:    "metric",
					Message:  "no growth metric set; experiments cannot be scored against a target",
					Severity: SeverityWarning,
				})
			}
			return issues
		},
	}
}
