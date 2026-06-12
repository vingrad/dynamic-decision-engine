package domain

import (
	"encoding/json"
	"time"
)

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

	// PackID and PackVersion identify the domain pack whose prompt/policy shaped
	// this plan, so a historical recommendation can be reconstructed even after
	// packs evolve. Empty for the generic domain / when no pack was applied.
	PackID      string `json:"pack_id,omitempty"`
	PackVersion string `json:"pack_version,omitempty"`

	// Strategy is the planning strategy: "single" for one model, "verify",
	// "route", "ensemble" for multi-model compositions, or "selector" when named
	// domain strategies competed and one was selected. Empty is treated as single.
	Strategy string `json:"strategy,omitempty"`
	// Contributors records every model that participated and its role, so a
	// multi-model decision is auditable end to end.
	Contributors []ModelContribution `json:"contributors,omitempty"`
	// Notes carries human-readable detail from a composite strategy — verifier
	// flags, agreement summary, or an escalation reason.
	Notes string `json:"notes,omitempty"`

	// SelectedStrategy names the domain strategy whose candidate plan won the
	// selection (e.g. "momentum"). Set only when Strategy == "selector".
	SelectedStrategy string `json:"selected_strategy,omitempty"`
	// Regime is the market-regime label in effect when strategies were gated
	// ("trend", "range", "high_vol"); empty when unknown or not classified. It is
	// recorded even when gating changed nothing, so per-regime outcome analysis
	// stays possible after the fact.
	Regime string `json:"regime,omitempty"`
	// StrategyCandidates is the audit trail of the strategy competition: every
	// candidate that ran, its utility score, and why the losers lost (or were
	// filtered). Present only when Strategy == "selector".
	StrategyCandidates []StrategyCandidate `json:"strategy_candidates,omitempty"`

	// SourceContributions records every external data source consulted before
	// planning: its identity, when it was fetched, the verbatim payload, and
	// whether the data was stale. The fetched data itself is folded into the goal
	// Context (and so into the input snapshot); this field is the audit trail of
	// where it came from. Empty when no sources were wired (offline / mock).
	SourceContributions []SourceContribution `json:"source_contributions,omitempty"`
}

// StrategyCandidate is the audit record for one strategy's candidate plan in a
// selection: its identity, its goal-derived utility (and the outcome weight
// applied), its top recommendation, and — when it lost or was filtered — why.
type StrategyCandidate struct {
	StrategyID    string  `json:"strategy_id"`
	Planner       string  `json:"planner"`
	UtilityScore  float64 `json:"utility_score"`
	Weight        float64 `json:"weight,omitempty"`
	TopMoveKey    string  `json:"top_move_key,omitempty"`
	TopConfidence float64 `json:"top_confidence,omitempty"`
	Filtered      bool    `json:"filtered,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

// SourceContribution is the audit record for one external data source consulted
// during a decision. Raw and FetchedAt exist for audit only and never enter the
// input snapshot hash, so they cannot affect reproducibility.
type SourceContribution struct {
	SourceName string          `json:"source_name"`
	FetchedAt  time.Time       `json:"fetched_at"`
	Stale      bool            `json:"stale,omitempty"`
	Err        string          `json:"error,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
	// DeltaSummary is a short human-readable note of what was folded into Context
	// (e.g. "+3 facts, +1 constraint"); the actual delta lives in the goal Context.
	DeltaSummary string `json:"delta_summary,omitempty"`
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
