package pack

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

// Growth strategy lenses: prompt variants that reframe move generation. They
// compete only when policy enables selection (SelectionDefaultOn is false —
// unlike investing, no backtest gates have validated this domain's
// competition yet, and each competing lens is a full model call).
const (
	growthExpandTemplate = `STRATEGY LENS: EXPANSION-FIRST

Bias every move toward NEW demand: untapped channels, new segments, new
geographies. Treat current channels as near saturation unless the context
proves otherwise. A move that only optimises an existing funnel stage must
justify why expansion would not beat it. State explicitly what new audience
each move reaches and the cheapest test that proves the channel works.`

	growthRetainTemplate = `STRATEGY LENS: RETENTION-EFFICIENCY

Bias every move toward keeping and deepening EXISTING demand: activation,
retention, expansion revenue, and unit economics. Treat acquisition spend as
suspect until churn and payback are understood. A move that buys new traffic
must justify why fixing the leaky bucket would not beat it. State explicitly
which cohort each move improves and the retention metric that proves it.`

	growthExperimentTemplate = `STRATEGY LENS: EXPERIMENT-DRIVEN

Bias every move toward LEARNING RATE: the portfolio should maximise validated
information per week, not any single bet. Prefer several small, parallel,
reversible tests over one committed push, even when the committed push has
higher expected value on paper. State explicitly the hypothesis each move
tests, what result would falsify it, and what the next experiment is in
either outcome.`
)

// growthPack covers founder/product growth goals (e.g. the founder-growth example).
func growthPack() Descriptor {
	return Descriptor{
		ID:             "growth",
		Name:           "Growth",
		Version:        "1",
		PromptVersion:  "growth-v1",
		PromptTemplate: growthPromptTemplate,
		Eval:           EvaluatorConfig{ConfidenceDelta: 0.10},
		Strategies: []StrategyDescriptor{
			{ID: "expand", Name: "Expansion-first", PromptTemplate: growthExpandTemplate},
			{ID: "retain", Name: "Retention-efficiency", PromptTemplate: growthRetainTemplate},
			{ID: "experiment", Name: "Experiment-driven", PromptTemplate: growthExperimentTemplate},
		},
		Vocab: Vocabulary{
			AssetKinds:      []string{"product", "channel", "audience", "data", "brand", "team"},
			ConstraintKinds: []string{"budget", "runway", "team_bandwidth", "channel_saturation", "compliance"},
			SignalKinds:     []string{"metric_change", "channel_performance", "cohort", "competitor", "customer_feedback"},
		},
		Validation: Validation{Rules: []ValidationRule{
			{
				Check:    CheckRequireMetric,
				Field:    "metric",
				Message:  "no growth metric set; experiments cannot be scored against a target",
				Severity: SeverityWarning,
			},
		}},
	}
}
