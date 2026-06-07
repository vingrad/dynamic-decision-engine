package source

import (
	"context"
	"encoding/json"
	"fmt"
)

// Researcher runs an autonomous agent loop (multiple model/tool turns) and returns
// the structured context it gathered plus a raw transcript for audit. It is injected
// so this package carries no LLM-client dependency. This is the strongest quarantine
// case: a Researcher is non-deterministic per call, but the engine sees only the one
// Result it produces, and reproducibility comes from the recorded snapshot — not from
// re-running the agent.
type Researcher func(ctx context.Context, q Query) (ContextDelta, json.RawMessage, error)

// AgentConfig configures an AgentSource.
type AgentConfig struct {
	Name        string
	Domain      string
	Description string
	Run         Researcher
}

// AgentSource exposes an autonomous research agent as a Source. The entire agent loop
// runs inside Fetch; whatever facts it converges on are folded into Context and its
// transcript is recorded as Raw. A failed run yields a stale Result so the decision
// still proceeds with whatever other sources provided.
type AgentSource struct {
	name        string
	domain      string
	description string
	run         Researcher
}

// NewAgentSource builds an AgentSource.
func NewAgentSource(cfg AgentConfig) *AgentSource {
	name := cfg.Name
	if name == "" {
		name = "agent"
	}
	desc := cfg.Description
	if desc == "" {
		desc = "autonomous research agent"
	}
	return &AgentSource{name: name, domain: cfg.Domain, description: desc, run: cfg.Run}
}

// Describe implements Source.
func (s *AgentSource) Describe() Descriptor {
	return Descriptor{Name: s.name, Domain: s.domain, Description: s.description}
}

// Fetch implements Source.
func (s *AgentSource) Fetch(ctx context.Context, q Query) (Result, error) {
	if s.run == nil {
		return Result{SourceName: s.name, Stale: true, Err: "agent: no researcher configured"}, nil
	}
	delta, transcript, err := s.run(ctx, q)
	if err != nil {
		return Result{SourceName: s.name, Raw: transcript, Stale: true, Err: fmt.Sprintf("agent run: %v", err)}, nil
	}
	return Result{SourceName: s.name, Delta: delta, Raw: transcript}, nil
}
