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

// DomainPolicy overrides a single domain's tunables. Pointer/optional fields mean
// "leave the pack default" when absent.
type DomainPolicy struct {
	ConfidenceDelta *float64               `json:"confidence_delta,omitempty" yaml:"confidence_delta"`
	Scoring         *finance.ScoringConfig `json:"scoring,omitempty" yaml:"scoring"`
	// IgnoreSignalKinds, when non-nil, replaces the pack's list of signal kinds the
	// domain never replans on. A nil pointer leaves the pack default; an explicit
	// empty list means "replan on every kind".
	IgnoreSignalKinds *[]string `json:"ignore_signal_kinds,omitempty" yaml:"ignore_signal_kinds"`
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
	return p, nil
}
