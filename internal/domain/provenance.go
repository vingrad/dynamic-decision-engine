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
