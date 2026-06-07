package domain

// DecisionProvenance explains why a plan or move was generated. It accompanies
// every plan version so that any recommendation is auditable after the fact:
// what reasoning produced it, from which input snapshot, and using which planner,
// prompt version and model.
type DecisionProvenance struct {
	ReasoningSummary string `json:"reasoning_summary"`
	InputSnapshotID  string `json:"input_snapshot_id"`
	Planner          string `json:"planner"`
	PromptVersion    string `json:"prompt_version"`
	Model            string `json:"model"`

	// Strategy is the planning strategy: "single" for one model, or "verify",
	// "route", "ensemble" for multi-model compositions. Empty is treated as single.
	Strategy string `json:"strategy,omitempty"`
	// Contributors records every model that participated and its role, so a
	// multi-model decision is auditable end to end.
	Contributors []ModelContribution `json:"contributors,omitempty"`
	// Notes carries human-readable detail from a composite strategy — verifier
	// flags, agreement summary, or an escalation reason.
	Notes string `json:"notes,omitempty"`
}

// ModelContribution records one model's participation in a (possibly multi-model)
// decision: which planner/model, in what role, and how many tokens it used.
type ModelContribution struct {
	Planner          string `json:"planner"`
	Model            string `json:"model"`
	Role             string `json:"role"` // proposer | verifier | ensemble-member | router-selected
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
}

// ModelInvocation captures metadata about a single call to a reasoning model.
// Token and latency fields are placeholders for the mock planner but become
// meaningful once a real LLM client is wired in behind the planner interface.
type ModelInvocation struct {
	Model            string `json:"model"`
	PromptVersion    string `json:"prompt_version"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	LatencyMS        int64  `json:"latency_ms"`
}
