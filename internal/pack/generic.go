package pack

// genericPack is the default domain. Its prompt template is intentionally empty so
// that the planner's base system prompt is used unchanged — generic goals behave
// byte-for-byte as they did before multi-domain support existed.
func genericPack() Descriptor {
	return Descriptor{
		ID:             DefaultDomain,
		Name:           "Generic",
		Version:        "1",
		PromptVersion:  "generic-v1",
		PromptTemplate: "", // load-bearing: empty -> base system prompt unchanged
		Eval:           EvaluatorConfig{ConfidenceDelta: 0.10},
		Vocab: Vocabulary{
			AssetKinds:      []string{"skill", "data", "network", "product", "capital"},
			ConstraintKinds: []string{"budget", "time", "geography", "policy", "risk"},
			SignalKinds:     []string{"competitor", "customer", "internal", "external"},
		},
		Validation: Validation{Rules: []ValidationRule{
			{
				Check:    CheckRequireContext,
				Field:    "context",
				Message:  "no assets or constraints provided; plans will be generic",
				Severity: SeverityWarning,
			},
		}},
	}
}
