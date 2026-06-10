package main

import (
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/vingrad/dynamic-decision-engine/internal/api"
	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/logging"
	mcpserver "github.com/vingrad/dynamic-decision-engine/internal/mcp"
)

// newMCPCommand serves the engine as an MCP server over stdio, so MCP clients
// (Claude Code/Desktop, agent runtimes) can drive the decision loop as tools.
// It assembles the same production service as serve — same DDE_* config, store
// (in-memory by default, Postgres via DATABASE_URL), planner and webhooks.
//
// Stdout carries JSON-RPC only: logging.New writes to stderr, and nothing in
// this path may print to stdout.
func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the engine as an MCP server over stdio",
		Long: "mcp exposes the engine's use-cases (evaluate, create_goal, generate_plan, " +
			"submit_signal, record_outcome, ...) as Model Context Protocol tools over stdio. " +
			"Configuration matches serve: DDE_* environment variables select the store, " +
			"planner and webhooks.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			log := logging.New(cfg.LogLevel, cfg.LogFormat)

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// Metrics are recorded but not exported in stdio mode (no HTTP listener).
			metrics := api.NewMetrics()
			svc, cleanup, err := buildService(ctx, cfg, log, metrics)
			if err != nil {
				return err
			}
			defer cleanup()

			srv := mcpserver.New(svc, version)
			return srv.Run(ctx, &mcp.StdioTransport{})
		},
	}
}
