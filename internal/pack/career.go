package pack

const careerPromptTemplate = `DOMAIN: CAREER

Treat each move as a career-positioning bet. For every move:
- Identify the leverage it builds: skill, visibility, network, or reputation.
- Favour moves whose value compounds and that preserve optionality.
- Respect limited bandwidth: time-box effort to what fits alongside current work.
- Make kill_criteria the absence of a concrete signal (interview, recognition,
  demonstrable skill) within the window.

Be realistic about timelines; senior transitions usually take quarters, not weeks.`

// Career strategy lenses: prompt variants that reframe move generation. They
// compete only when policy enables selection (SelectionDefaultOn is false —
// no validation gates exist for this domain's competition yet, and each
// competing lens is a full model call).
const (
	careerMasteryTemplate = `STRATEGY LENS: COMPOUNDING MASTERY

Bias every move toward DEPTH in one valuable, durable skill. Treat scattered
effort as the default failure mode. A move that broadens must justify why it
beats deepening the strongest existing skill. State explicitly which skill
each move compounds and the demonstrable artifact (talk, project, credential)
that proves the depth gained.`

	careerOptionalityTemplate = `STRATEGY LENS: OPTIONALITY

Bias every move toward keeping FUTURE PATHS open: transferable skills,
multiple live opportunities, low switching costs. Treat any move that locks
years into one employer, stack or city as suspect until the lock-in is paid
for. State explicitly which options each move creates or preserves and what
it would cost to change course afterwards.`

	careerNetworkTemplate = `STRATEGY LENS: NETWORK LEVERAGE

Bias every move toward RELATIONSHIPS and VISIBILITY: who will know this work
exists, and who can act on it. Treat skill-building without an audience as
incomplete. A move done alone must justify why it beats the same effort done
with or in front of the right people. State explicitly which relationship or
audience each move builds and the concrete signal (introduction, invitation,
referral) that proves it worked.`
)

// careerPack covers career-strategy goals (e.g. the career-strategy example).
func careerPack() Descriptor {
	return Descriptor{
		ID:             "career",
		Name:           "Career",
		Version:        "1",
		PromptVersion:  "career-v1",
		PromptTemplate: careerPromptTemplate,
		Eval:           EvaluatorConfig{ConfidenceDelta: 0.10},
		Strategies: []StrategyDescriptor{
			{ID: "mastery", Name: "Compounding mastery", PromptTemplate: careerMasteryTemplate},
			{ID: "optionality", Name: "Optionality", PromptTemplate: careerOptionalityTemplate},
			{ID: "network", Name: "Network leverage", PromptTemplate: careerNetworkTemplate},
		},
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
