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
// Import rule: pack may use `domain`, stdlib serialization and YAML (for the
// optional config loader in load.go), and `finance` for built-in scoring defaults.
// It must never import `llm`, `engine`, or `wire` — domain-specific behaviour
// rides on the opaque Scoring field and is interpreted by the wiring layer.
package pack

import (
	"strconv"
	"strings"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
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
	AssetKinds      []string `json:"asset_kinds" yaml:"asset_kinds"`
	ConstraintKinds []string `json:"constraint_kinds" yaml:"constraint_kinds"`
	SignalKinds     []string `json:"signal_kinds" yaml:"signal_kinds"`
}

// EvaluatorConfig is the data form of a materiality policy. The wiring layer turns
// it into an engine.Evaluator (engine.ThresholdEvaluator{ConfidenceDelta}).
type EvaluatorConfig struct {
	ConfidenceDelta float64 `json:"confidence_delta" yaml:"confidence_delta"`

	// IgnoreSignalKinds lists signal kinds the domain never replans on. A signal of
	// one of these kinds is short-circuited by the engine's replan gate before any
	// (expensive) plan regeneration. Empty means every kind triggers a replan.
	IgnoreSignalKinds []string `json:"ignore_signal_kinds,omitempty" yaml:"ignore_signal_kinds,omitempty"`
}

// Severity classifies a validation finding.
type Severity string

const (
	SeverityWarning Severity = "warning" // soft: surfaced/logged, does not block
	SeverityError   Severity = "error"   // hard: blocks goal creation
)

// ValidationIssue is a single finding from validating a goal against a domain.
type ValidationIssue struct {
	Field    string   `json:"field" yaml:"field"`
	Message  string   `json:"message" yaml:"message"`
	Severity Severity `json:"severity" yaml:"severity"`
}

// Descriptor is the complete data description of a domain. It carries a small
// validate function (pure, no external deps) rather than a behaviour interface so
// the package stays free of engine/LLM dependencies.
type Descriptor struct {
	ID            string `json:"id" yaml:"id"`                         // stable key, e.g. "investing"
	Name          string `json:"name" yaml:"name"`                     // human-readable, e.g. "Investing"
	Version       string `json:"version" yaml:"version"`               // bump when prompt/policy changes; recorded in provenance
	PromptVersion string `json:"prompt_version" yaml:"prompt_version"` // identifies the prompt contract for provenance

	// PromptTemplate is appended to the base system prompt by the guided planner.
	// The generic domain uses "" so its prompt is byte-for-byte the original.
	PromptTemplate string `json:"prompt_template" yaml:"prompt_template"`

	// PlannerKind selects how the domain reasons. The empty value means the
	// guided text planner (base prompt + PromptTemplate). A named kind (e.g.
	// "finance") selects a numeric planner registered in the wiring layer. Kept a
	// free string so this package never enumerates planner implementations.
	PlannerKind string `json:"planner_kind,omitempty" yaml:"planner_kind,omitempty"`

	// SourceKinds names the external-data sources this domain consults before
	// planning, in fold order. Entries are keys into the wired source registry
	// (the source analogue of how PlannerKind selects a planner builder). Kept a
	// free string list so this package never enumerates source implementations and
	// config-defined domains can declare sources. Empty means no enrichment.
	SourceKinds []string `json:"source_kinds,omitempty" yaml:"source_kinds,omitempty"`

	Eval EvaluatorConfig `json:"eval" yaml:"eval"`
	// Scoring carries opaque, domain-specific scoring config consumed by the
	// planner builder for this domain's PlannerKind (e.g. *finance.ScoringConfig
	// for "finance"). nil for domains without numeric scoring. It is not loadable
	// from config (a map cannot become a typed scoring struct); numeric tuning is
	// done per-domain via the policy file instead.
	Scoring any        `json:"-" yaml:"-"`
	Vocab   Vocabulary `json:"vocab" yaml:"vocab"`

	// Validation is the declarative validation policy for the domain: a set of
	// shape rules plus an optional vocabulary check. It is pure data so a domain
	// (including one loaded from config) needs no code. Evaluate it via Validate.
	Validation Validation `json:"validation" yaml:"validation"`
}

// KindScope names where a kind-based rule looks for a kind.
type KindScope string

const (
	ScopeAsset      KindScope = "asset"
	ScopeConstraint KindScope = "constraint"
)

// ValidationCheck is the predicate a rule asserts. The rule emits its issue when
// the predicate is NOT satisfied.
type ValidationCheck string

const (
	CheckRequireMetric  ValidationCheck = "require_metric"   // the goal sets a metric
	CheckRequireContext ValidationCheck = "require_context"  // the goal has assets or constraints
	CheckRequireAnyKind ValidationCheck = "require_any_kind" // any of Kinds appears in Scopes
)

// ValidationRule is one declarative validation check, replacing the per-pack
// validate functions. When its predicate is unsatisfied it yields a ValidationIssue
// carrying Field/Message/Severity.
type ValidationRule struct {
	Check    ValidationCheck `json:"check" yaml:"check"`
	Kinds    []string        `json:"kinds,omitempty" yaml:"kinds,omitempty"`   // for require_any_kind
	Scopes   []KindScope     `json:"scopes,omitempty" yaml:"scopes,omitempty"` // for require_any_kind; empty = both
	Field    string          `json:"field" yaml:"field"`
	Message  string          `json:"message" yaml:"message"`
	Severity Severity        `json:"severity" yaml:"severity"`
}

// Validation is a domain's declarative validation policy.
type Validation struct {
	Rules []ValidationRule `json:"rules,omitempty" yaml:"rules,omitempty"`
	// WarnUnknownKinds, when true, warns for any asset/constraint whose Kind is not
	// in the domain's Vocab. Off by default: Vocab is suggested, not binding.
	WarnUnknownKinds bool `json:"warn_unknown_kinds,omitempty" yaml:"warn_unknown_kinds,omitempty"`
}

// Validate returns the soft/hard findings for a goal under this domain's policy.
// It is safe on a zero Validation (no rules → no issues) and never nil-panics, so
// config-defined descriptors validate without bespoke code.
func (d Descriptor) Validate(g domain.Goal) []ValidationIssue {
	var issues []ValidationIssue
	for _, r := range d.Validation.Rules {
		if !r.satisfied(g) {
			issues = append(issues, ValidationIssue{Field: r.Field, Message: r.Message, Severity: r.Severity})
		}
	}
	if d.Validation.WarnUnknownKinds {
		issues = append(issues, d.unknownKindIssues(g)...)
	}
	return issues
}

// satisfied reports whether the rule's predicate holds for the goal. An unknown
// check is treated as satisfied so a malformed rule never blocks goal creation.
func (r ValidationRule) satisfied(g domain.Goal) bool {
	switch r.Check {
	case CheckRequireMetric:
		return g.Metric != ""
	case CheckRequireContext:
		return len(g.Context.Assets) > 0 || len(g.Context.Constraints) > 0
	case CheckRequireAnyKind:
		return anyKindPresent(g, r.Kinds, r.Scopes)
	default:
		return true
	}
}

// anyKindPresent reports whether any of kinds appears (case-insensitive) as an
// asset or constraint Kind within the given scopes. Empty scopes means both.
func anyKindPresent(g domain.Goal, kinds []string, scopes []KindScope) bool {
	checkAssets := len(scopes) == 0 || containsScope(scopes, ScopeAsset)
	checkConstraints := len(scopes) == 0 || containsScope(scopes, ScopeConstraint)
	for _, want := range kinds {
		if checkAssets {
			for _, a := range g.Context.Assets {
				if strings.EqualFold(a.Kind, want) {
					return true
				}
			}
		}
		if checkConstraints {
			for _, c := range g.Context.Constraints {
				if strings.EqualFold(c.Kind, want) {
					return true
				}
			}
		}
	}
	return false
}

func containsScope(scopes []KindScope, s KindScope) bool {
	for _, x := range scopes {
		if x == s {
			return true
		}
	}
	return false
}

// unknownKindIssues warns for each asset/constraint whose non-empty Kind is not in
// the domain's Vocab. Skipped for a category whose Vocab list is empty (undeclared).
func (d Descriptor) unknownKindIssues(g domain.Goal) []ValidationIssue {
	var issues []ValidationIssue
	for _, a := range g.Context.Assets {
		if a.Kind != "" && len(d.Vocab.AssetKinds) > 0 && !containsFold(d.Vocab.AssetKinds, a.Kind) {
			issues = append(issues, ValidationIssue{
				Field:    "context.assets",
				Message:  "unrecognised asset kind " + strconv.Quote(a.Kind) + " for this domain",
				Severity: SeverityWarning,
			})
		}
	}
	for _, c := range g.Context.Constraints {
		if c.Kind != "" && len(d.Vocab.ConstraintKinds) > 0 && !containsFold(d.Vocab.ConstraintKinds, c.Kind) {
			issues = append(issues, ValidationIssue{
				Field:    "context.constraints",
				Message:  "unrecognised constraint kind " + strconv.Quote(c.Kind) + " for this domain",
				Severity: SeverityWarning,
			})
		}
	}
	return issues
}

func containsFold(list []string, want string) bool {
	for _, x := range list {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}
