// Package mcpserver exposes the decision engine's use-cases as Model Context
// Protocol tools, so MCP-capable agents (Claude Code/Desktop, custom agent
// runtimes) can drive the full decision loop: create goals, generate ranked
// plans, submit signals, record outcomes and inspect the immutable version
// history. Like the REST API it is a thin transport adapter over app.Service —
// tool semantics, validation and error mapping mirror the HTTP endpoints.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
)

// New builds an MCP server with the engine's tool set registered. The same
// server value can serve a single stdio session (Run) or many concurrent HTTP
// sessions (mcp.NewStreamableHTTPHandler).
func New(svc *app.Service, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "dde",
		Title:   "Dynamic Decision Engine",
		Version: version,
	}, nil)
	addTools(s, svc)
	return s
}
