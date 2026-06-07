package pack

const careerPromptTemplate = `DOMAIN: CAREER

Treat each move as a career-positioning bet. For every move:
- Identify the leverage it builds: skill, visibility, network, or reputation.
- Favour moves whose value compounds and that preserve optionality.
- Respect limited bandwidth: time-box effort to what fits alongside current work.
- Make kill_criteria the absence of a concrete signal (interview, recognition,
  demonstrable skill) within the window.

Be realistic about timelines; senior transitions usually take quarters, not weeks.`

// careerPack covers career-strategy goals (e.g. the career-strategy example).
func careerPack() Descriptor {
	return Descriptor{
		ID:             "career",
		Name:           "Career",
		Version:        "1",
		PromptVersion:  "career-v1",
		PromptTemplate: careerPromptTemplate,
		Eval:           EvaluatorConfig{ConfidenceDelta: 0.10},
		Vocab: Vocabulary{
			AssetKinds:      []string{"skill", "network", "reputation", "experience", "credential"},
			ConstraintKinds: []string{"time", "geography", "compensation_floor", "risk_tolerance", "family"},
			SignalKinds:     []string{"interview_outcome", "feedback", "market_demand", "internal_signal", "offer"},
		},
		Validation: Validation{Rules: []ValidationRule{
			{
				Check:    CheckRequireAnyKind,
				Kinds:    []string{"time"},
				Scopes:   []KindScope{ScopeConstraint},
				Field:    "context.constraints",
				Message:  "no time constraint; career moves are bandwidth-bound and should state available time",
				Severity: SeverityWarning,
			},
		}},
	}
}
