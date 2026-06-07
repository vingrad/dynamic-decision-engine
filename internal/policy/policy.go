// Package policy loads per-domain operational tunables (materiality thresholds and
// finance scoring parameters) from an optional file, so they can be changed as
// data without recompiling. Domain packs supply defaults; a Policy overlays them.
//
// This is the data-driven half of the roadmap's "policy constraints" and
// "multi-objective scoring": the pack descriptor says what a domain *is*, the
// policy file says how a particular deployment wants it *tuned*.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vingrad/dynamic-decision-engine/internal/finance"
)

// PlannerSpec overrides the reasoning backend for a single (text) domain, mirroring
// the global DDE_PLANNER/DDE_MULTI_*/DDE_LLM_MODEL knobs. Empty fields fall back to
// the global configuration. It applies only to text domains; a domain that uses a
// numeric planner (e.g. investing's finance planner) ignores it.
type PlannerSpec struct {
	Planner        string   `json:"planner,omitempty" yaml:"planner"`                 // mock|anthropic|openai|deepseek|multi
	MultiMode      string   `json:"multi_mode,omitempty" yaml:"multi_mode"`           // verify|route|ensemble (when Planner==multi)
	MultiProviders []string `json:"multi_providers,omitempty" yaml:"multi_providers"` // ordered providers for the multi planner
	Model          string   `json:"model,omitempty" yaml:"model"`                     // overrides the global LLM model id
}

// DomainPolicy overrides a single domain's tunables. Pointer/optional fields mean
// "leave the pack default" when absent.
type DomainPolicy struct {
	ConfidenceDelta *float64               `json:"confidence_delta,omitempty" yaml:"confidence_delta"`
	Scoring         *finance.ScoringConfig `json:"scoring,omitempty" yaml:"scoring"`
	// IgnoreSignalKinds, when non-nil, replaces the pack's list of signal kinds the
	// domain never replans on. A nil pointer leaves the pack default; an explicit
	// empty list means "replan on every kind".
	IgnoreSignalKinds *[]string `json:"ignore_signal_kinds,omitempty" yaml:"ignore_signal_kinds"`
	// Planner, when non-nil, overrides the reasoning backend for this (text) domain.
	Planner *PlannerSpec `json:"planner,omitempty" yaml:"planner"`
}

// Policy is the full set of per-domain overrides, keyed by domain id.
type Policy struct {
	Domains map[string]DomainPolicy `json:"domains" yaml:"domains"`
}

// For returns the override for a domain, if any.
func (p Policy) For(domainKey string) (DomainPolicy, bool) {
	dp, ok := p.Domains[domainKey]
	return dp, ok
}

// Load reads a policy file (JSON or YAML by extension). An empty path returns an
// empty policy (all defaults), so policy is strictly opt-in.
func Load(path string) (Policy, error) {
	if path == "" {
		return Policy{Domains: map[string]DomainPolicy{}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("policy: read %s: %w", path, err)
	}
	var p Policy
	if strings.HasSuffix(path, ".json") {
		err = json.Unmarshal(data, &p)
	} else {
		err = yaml.Unmarshal(data, &p)
	}
	if err != nil {
		return Policy{}, fmt.Errorf("policy: parse %s: %w", path, err)
	}
	if p.Domains == nil {
		p.Domains = map[string]DomainPolicy{}
	}
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("policy: %s: %w", path, err)
	}
	return p, nil
}

// singleBackends are the planner backends valid both globally and as a per-domain
// override or multi member. "multi" composes these; "finance" is rejected per
// domain (it aliases mock for a text planner, which is confusing).
var singleBackends = map[string]bool{
	"mock": true, "anthropic": true, "openai": true, "deepseek": true,
}

// Validate checks per-domain planner specs so a bad override fails fast at startup
// rather than panicking (a multi spec with <2 providers indexes out of range) or
// silently degrading (a verify spec whose verifier can't verify).
func (p Policy) Validate() error {
	for id, dp := range p.Domains {
		if dp.Planner == nil {
			continue
		}
		if err := dp.Planner.validate(); err != nil {
			return fmt.Errorf("domain %q planner: %w", id, err)
		}
	}
	return nil
}

func (s PlannerSpec) validate() error {
	if s.Planner == "" {
		return nil // empty spec falls back to the global planner
	}
	if s.Planner != "multi" {
		if !singleBackends[s.Planner] {
			return fmt.Errorf("unknown backend %q (want mock|anthropic|openai|deepseek|multi)", s.Planner)
		}
		return nil
	}
	switch s.MultiMode {
	case "verify", "route", "ensemble":
	default:
		return fmt.Errorf("invalid multi_mode %q (want verify|route|ensemble)", s.MultiMode)
	}
	if len(s.MultiProviders) < 2 {
		return fmt.Errorf("multi needs at least 2 multi_providers")
	}
	for _, m := range s.MultiProviders {
		if !singleBackends[m] {
			return fmt.Errorf("invalid multi_providers entry %q", m)
		}
	}
	// The verifier (second provider) must be able to verify; mock cannot.
	if s.MultiMode == "verify" && s.MultiProviders[1] == "mock" {
		return fmt.Errorf("multi_mode verify needs a real verifier; mock cannot verify")
	}
	return nil
}
