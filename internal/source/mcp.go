package source

import (
	"context"
	"encoding/json"
	"fmt"
)

// MCPInvoker performs one MCP tools/call and returns the raw JSON result. It is
// injected so this package carries no MCP client dependency and stays unit-testable;
// the wiring layer supplies a real transport. Any non-determinism the MCP server
// introduces (it may itself call a model) is fully contained behind this call.
type MCPInvoker func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error)

// MCPConfig configures an MCPSource.
type MCPConfig struct {
	Name        string
	Domain      string
	Tool        string         // MCP tool name to call
	InputSchema map[string]any // the tool's JSON schema; surfaced via Describe for Phase-2
	Invoke      MCPInvoker
	// ArgsFor builds the tool arguments from a query; nil sends no arguments.
	ArgsFor func(Query) map[string]any
}

// MCPSource exposes an MCP tool as a Source. The whole MCP exchange runs inside
// Fetch; the engine sees a single Result. The tool is expected to return a
// ContextDelta-shaped JSON document; the verbatim response is kept as Raw for audit.
// Any transport failure yields a stale Result so the decision proceeds.
type MCPSource struct {
	name        string
	domain      string
	tool        string
	inputSchema map[string]any
	invoke      MCPInvoker
	argsFor     func(Query) map[string]any
}

// NewMCPSource builds an MCPSource.
func NewMCPSource(cfg MCPConfig) *MCPSource {
	name := cfg.Name
	if name == "" {
		name = "mcp:" + cfg.Tool
	}
	return &MCPSource{
		name:        name,
		domain:      cfg.Domain,
		tool:        cfg.Tool,
		inputSchema: cfg.InputSchema,
		invoke:      cfg.Invoke,
		argsFor:     cfg.ArgsFor,
	}
}

// Describe implements Source. InputSchema mirrors the MCP tool contract, which is
// also what a future agentic planner needs to expose this source as a tool.
func (s *MCPSource) Describe() Descriptor {
	return Descriptor{
		Name:        s.name,
		Domain:      s.domain,
		Description: "MCP tool " + s.tool,
		InputSchema: s.inputSchema,
	}
}

// Fetch implements Source.
func (s *MCPSource) Fetch(ctx context.Context, q Query) (Result, error) {
	if s.invoke == nil {
		return Result{SourceName: s.name, Stale: true, Err: "mcp: no invoker configured"}, nil
	}
	var args map[string]any
	if s.argsFor != nil {
		args = s.argsFor(q)
	}
	raw, err := s.invoke(ctx, s.tool, args)
	if err != nil {
		return Result{SourceName: s.name, Stale: true, Err: fmt.Sprintf("mcp call: %v", err)}, nil
	}
	var wd wireDelta
	if err := json.Unmarshal(raw, &wd); err != nil {
		// Keep the raw payload for audit even when we can't map it to a delta.
		return Result{SourceName: s.name, Raw: raw, Stale: true, Err: fmt.Sprintf("mcp decode: %v", err)}, nil
	}
	return Result{
		SourceName: s.name,
		Delta:      ContextDelta{Facts: wd.Facts, Assets: wd.Assets, Constraints: wd.Constraints},
		Raw:        raw,
	}, nil
}
