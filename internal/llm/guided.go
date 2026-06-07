package llm

import "context"

// effectiveSystemPrompt composes a base system prompt with optional domain
// guidance. An empty override returns the base unchanged, which guarantees the
// generic domain produces byte-for-byte the original prompt.
func effectiveSystemPrompt(base, override string) string {
	if override == "" {
		return base
	}
	return base + "\n\n" + override
}

// GuidedPlanner wraps a base planner with a domain's prompt guidance and stamps
// the pack's identity/version into provenance. It lets every text-based domain
// (generic, growth, career, and the qualitative side of investing) reuse a single
// underlying model client while differing only by prompt — the composition that
// keeps multi-domain support cheap.
type GuidedPlanner struct {
	base          Planner
	packID        string
	packVersion   string
	promptVersion string
	template      string // appended to the base system prompt; "" => base unchanged
}

// GuidedConfig configures a GuidedPlanner.
type GuidedConfig struct {
	PackID         string
	PackVersion    string
	PromptVersion  string
	PromptTemplate string
}

// NewGuidedPlanner wraps base with the given pack guidance.
func NewGuidedPlanner(base Planner, cfg GuidedConfig) *GuidedPlanner {
	return &GuidedPlanner{
		base:          base,
		packID:        cfg.PackID,
		packVersion:   cfg.PackVersion,
		promptVersion: cfg.PromptVersion,
		template:      cfg.PromptTemplate,
	}
}

// Name implements Planner, deferring to the base so provenance keeps reporting the
// underlying model/backend (e.g. "anthropic", "mock").
func (g *GuidedPlanner) Name() string { return g.base.Name() }

// GeneratePlan injects the pack's prompt template, delegates to the base planner,
// then records the pack id/version (and prompt version) on the result's provenance.
func (g *GuidedPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	req.SystemPromptOverride = g.template
	res, err := g.base.GeneratePlan(ctx, req)
	if err != nil {
		return PlanResult{}, err
	}
	res.Provenance.PackID = g.packID
	res.Provenance.PackVersion = g.packVersion
	// Record the pack's prompt contract alongside the model's, rather than
	// overwriting it, so provenance keeps both (e.g. "anthropic-v1+investing-v1").
	if g.promptVersion != "" {
		if res.Provenance.PromptVersion != "" {
			res.Provenance.PromptVersion += "+" + g.promptVersion
		} else {
			res.Provenance.PromptVersion = g.promptVersion
		}
	}
	return res, nil
}
