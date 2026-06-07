// Package pack defines decision-domain "packs" as pure data. A pack describes how
// a domain shapes planning — its prompt guidance, materiality threshold, scoring
// defaults, vocabulary and validation — without containing any behaviour that
// depends on the engine, the LLM clients or storage.
//
// This deliberate purity is what makes multi-domain support scalable: the wiring
// layer reads these descriptors to build per-domain planners (an llm.PlannerRouter)
// and per-domain evaluators (an engine.EvaluatorResolver), so adding a new domain
// is a single descriptor plus one registration — no edits across the codebase.
//
// Import rule: pack imports only `domain` (and `finance` for the scoring-config
// value types). It must never import `llm` or `engine`.
package pack

import (
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
)

// DefaultDomain is the human-facing key of the default ("generic") domain. The
// canonical *stored* value for the default is the empty string; the registry maps
// both "" and "generic" to the generic descriptor. Keeping the stored default
// empty means generic goals serialise and hash exactly as they did before
// multi-domain support existed.
const DefaultDomain = "generic"

// Vocabulary lists suggested (non-binding) terminology a domain surfaces to
// callers and docs: the asset, constraint and signal kinds that make sense in it.
type Vocabulary struct {
	AssetKinds      []string `json:"asset_kinds"`
	ConstraintKinds []string `json:"constraint_kinds"`
	SignalKinds     []string `json:"signal_kinds"`
}

// EvaluatorConfig is the data form of a materiality policy. The wiring layer turns
// it into an engine.Evaluator (engine.ThresholdEvaluator{ConfidenceDelta}).
type EvaluatorConfig struct {
	ConfidenceDelta float64 `json:"confidence_delta"`
}

// Severity classifies a validation finding.
type Severity string

const (
	SeverityWarning Severity = "warning" // soft: surfaced/logged, does not block
	SeverityError   Severity = "error"   // hard: blocks goal creation
)

// ValidationIssue is a single finding from validating a goal against a domain.
type ValidationIssue struct {
	Field    string   `json:"field"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// Descriptor is the complete data description of a domain. It carries a small
// validate function (pure, no external deps) rather than a behaviour interface so
// the package stays free of engine/LLM dependencies.
type Descriptor struct {
	ID            string // stable key, e.g. "investing"
	Name          string // human-readable, e.g. "Investing"
	Version       string // bump when prompt/policy changes; recorded in provenance
	PromptVersion string // identifies the prompt contract for provenance

	// PromptTemplate is appended to the base system prompt by the guided planner.
	// The generic domain uses "" so its prompt is byte-for-byte the original.
	PromptTemplate string

	Eval    EvaluatorConfig
	Scoring *finance.ScoringConfig // non-nil for quantitative domains (investing)
	Vocab   Vocabulary

	// Validate returns soft/hard findings for a goal. Never nil for built-in packs.
	Validate func(domain.Goal) []ValidationIssue
}
