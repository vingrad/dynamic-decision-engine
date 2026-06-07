package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vingrad/dynamic-decision-engine/internal/api"
	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/backtest"
	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/logging"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
	"github.com/vingrad/dynamic-decision-engine/internal/wire"
)

// newPlanner selects the reasoning backend named in config. It is the base
// (text/LLM) planner the domain router uses for generic/growth/career; the
// investing domain uses the numeric finance planner automatically. The mock is
// the default and runs with no external dependencies.
func newPlanner(cfg config.Config) llm.Planner {
	if cfg.Planner == "multi" {
		return newMultiPlanner(cfg)
	}
	return buildProvider(cfg.Planner, cfg)
}

// buildProvider constructs a single-provider planner by name. "mock" is included
// so multi-model compositions can be exercised offline without API keys.
func buildProvider(name string, cfg config.Config) llm.Planner {
	switch name {
	case "anthropic":
		return llm.NewAnthropicPlanner(llm.AnthropicConfig{
			APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
			Model:     cfg.LLMModel,
			MaxTokens: cfg.LLMMaxTokens,
		})
	case "openai":
		return llm.NewOpenAIPlanner(llm.OpenAIConfig{
			Provider:  "openai",
			APIKey:    os.Getenv("OPENAI_API_KEY"),
			Model:     cfg.LLMModel,
			BaseURL:   cfg.LLMBaseURL,
			MaxTokens: cfg.LLMMaxTokens,
		})
	case "deepseek":
		return llm.NewOpenAIPlanner(llm.OpenAIConfig{
			Provider:  "deepseek",
			APIKey:    os.Getenv("DEEPSEEK_API_KEY"),
			Model:     cfg.LLMModel,
			BaseURL:   cfg.LLMBaseURL,
			MaxTokens: cfg.LLMMaxTokens,
		})
	default: // "mock", "finance", or anything else -> deterministic mock base
		return llm.NewMockPlanner()
	}
}

// newMultiPlanner composes a multi-model planner from cfg.MultiProviders per the
// selected mode. Config validation guarantees a valid mode and ≥2 providers.
func newMultiPlanner(cfg config.Config) llm.Planner {
	sub := make([]llm.Planner, len(cfg.MultiProviders))
	for i, name := range cfg.MultiProviders {
		sub[i] = buildProvider(name, cfg)
	}
	switch cfg.MultiMode {
	case "route":
		return llm.NewRouterPlanner(sub[0], sub[1], cfg.MultiConfidenceThreshold, cfg.MultiEscalateOnSignal)
	case "ensemble":
		return llm.NewEnsemblePlanner(sub...)
	default: // "verify"
		verifier, ok := buildProvider(cfg.MultiProviders[1], cfg).(llm.PlanVerifier)
		if !ok {
			// The mock planner can't verify; fall back to the proposer alone.
			return sub[0]
		}
		return llm.NewVerifyPlanner(sub[0], verifier)
	}
}

// newProvider builds the market-data provider for the finance planner. Offline
// (embedded fixtures, no network) is the default; "http" selects the vendor stub.
func newProvider(cfg config.Config) (marketdata.Provider, error) {
	if cfg.MarketDataProvider == "http" {
		return marketdata.NewHTTPProvider(marketdata.HTTPConfig{
			APIKey: os.Getenv("DDE_MARKETDATA_API_KEY"),
			Vendor: cfg.MarketDataVendor,
		}), nil
	}
	return marketdata.NewOfflineProvider()
}

// marketDataSources wraps a market-data provider as the wire DataSources registry
// entry numeric domains look up. Returns nil when no provider is configured so the
// finance builder declines and investing falls back to the guided text planner.
func marketDataSources(provider marketdata.Provider) map[string]wire.DataSource {
	if provider == nil {
		return nil
	}
	return map[string]wire.DataSource{"marketdata": provider}
}

// buildRegistry assembles the domain pack registry: the built-in packs plus any
// config-defined domains loaded from cfg.DomainsPath (DDE_DOMAINS).
func buildRegistry(cfg config.Config) (*pack.Registry, error) {
	extra, err := pack.LoadDescriptors(cfg.DomainsPath)
	if err != nil {
		return nil, err
	}
	return pack.NewRegistryFrom(extra...), nil
}

// newEngine assembles the multi-domain engine: a per-domain planner router (with
// optional plan cache and the finance planner for investing) plus a per-domain
// evaluator resolver, both built from the pack registry overlaid with policy. The
// router's base (for non-investing domains) is the configured text planner, which
// may itself be a multi-model composition.
func newEngine(cfg config.Config, reg *pack.Registry, pol policy.Policy, cacheObs llm.CacheObserver) (*engine.Engine, error) {
	provider, err := newProvider(cfg)
	if err != nil {
		return nil, err
	}
	var cache, financeCache llm.PlanCache
	if cfg.PlanCacheSize > 0 {
		// Text/LLM plans are deterministic -> no expiry. Finance plans depend on
		// as-of market data -> TTL expiry so they refresh.
		cache = llm.NewMemoryCache(cfg.PlanCacheSize)
		if cfg.PlanCacheTTL > 0 {
			financeCache = llm.NewMemoryCacheTTL(cfg.PlanCacheSize, cfg.PlanCacheTTL, nil)
		}
	}
	router := wire.BuildPlannerRouter(reg, pol, wire.PlannerDeps{
		Base:         newPlanner(cfg),
		DataSources:  marketDataSources(provider),
		Cache:        cache,
		FinanceCache: financeCache,
		CacheObs:     cacheObs,
	})
	return engine.New(router,
		engine.WithEvaluatorResolver(wire.NewEvaluatorResolver(reg, pol)),
		engine.WithGateResolver(wire.NewGateResolver(reg, pol)),
	), nil
}

// newServeCommand runs the REST API server.
func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the REST API server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			log := logging.New(cfg.LogLevel, cfg.LogFormat)

			// Cancel on SIGINT/SIGTERM for graceful shutdown.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			repo, err := storage.Open(ctx, storage.Options{
				DatabaseURL: cfg.DatabaseURL,
				MaxConns:    cfg.DBMaxConns,
			}, log)
			if err != nil {
				return err
			}
			defer repo.Close()

			reg, err := buildRegistry(cfg)
			if err != nil {
				return err
			}
			pol, err := policy.Load(cfg.PolicyFile)
			if err != nil {
				return err
			}
			metrics := api.NewMetrics()
			eng, err := newEngine(cfg, reg, pol, metrics)
			if err != nil {
				return err
			}

			opts := []app.Option{app.WithMetrics(metrics), app.WithLogger(log), app.WithRegistry(reg)}
			if cfg.ReplanAsync {
				opts = append(opts,
					app.WithReplanQueue(app.NewMemoryQueue(cfg.ReplanWorkers, 1024, log, app.WithQueueTimeout(cfg.ReplanTimeout))),
					app.WithReplanRetries(cfg.ReplanMaxRetries),
				)
			}
			svc := app.New(repo, eng, opts...)
			// Re-enqueue replans left pending by a previous crash (async only — the
			// in-memory queue loses scheduled work on restart; inline never does).
			if cfg.ReplanAsync {
				go func() {
					n, err := svc.RecoverPending(ctx)
					if err != nil {
						log.Error("replan recovery failed", "err", err)
						return
					}
					if n > 0 {
						log.Info("recovered pending replans", "count", n)
					}
				}()
			}
			// Drain in-flight async replanning on shutdown.
			defer func() {
				sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = svc.Shutdown(sctx)
			}()

			srv := api.New(cfg, log, svc, metrics)
			return srv.Run(ctx)
		},
	}
}

// newMigrateCommand applies pending database migrations and exits.
func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations and exit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.DatabaseURL == "" {
				return fmt.Errorf("migrate requires DATABASE_URL to be set")
			}
			log := logging.New(cfg.LogLevel, cfg.LogFormat)

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			// Opening the postgres store runs migrations as part of connect.
			repo, err := storage.NewPostgres(ctx, storage.Options{
				DatabaseURL: cfg.DatabaseURL,
				MaxConns:    cfg.DBMaxConns,
			}, log)
			if err != nil {
				return err
			}
			repo.Close()
			fmt.Println("migrations applied")
			return nil
		},
	}
}

// evaluateInput is the file format consumed by `dde evaluate`.
type evaluateInput struct {
	Domain     string         `json:"domain"`
	Objective  string         `json:"objective"`
	Metric     string         `json:"metric"`
	Target     string         `json:"target"`
	Context    domain.Context `json:"context"`
	SignalNote string         `json:"signal_note"`
}

// newEvaluateCommand runs the planner against an input file and prints the plan.
func newEvaluateCommand() *cobra.Command {
	var input string
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Generate a ranked plan from a goal+context JSON file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var in evaluateInput
			if err := readJSONFile(input, &in); err != nil {
				return err
			}
			svc, err := newMemoryService()
			if err != nil {
				return err
			}
			version, err := svc.Evaluate(cmd.Context(), app.EvaluateInput{
				Domain:     in.Domain,
				Objective:  in.Objective,
				Metric:     in.Metric,
				Target:     in.Target,
				Context:    in.Context,
				SignalNote: in.SignalNote,
			})
			if err != nil {
				return err
			}
			return printJSON(version)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "path to a goal+context JSON file (required)")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

// signalInput is the file format consumed by `dde signal`: a self-contained goal
// plus a signal to apply to it, so replanning can be demonstrated offline.
type signalInput struct {
	Goal struct {
		Domain    string         `json:"domain"`
		Objective string         `json:"objective"`
		Metric    string         `json:"metric"`
		Target    string         `json:"target"`
		Context   domain.Context `json:"context"`
	} `json:"goal"`
	Signal struct {
		Kind        string         `json:"kind"`
		Description string         `json:"description"`
		Payload     map[string]any `json:"payload"`
	} `json:"signal"`
}

// newSignalCommand applies a signal to a freshly generated plan and reports
// whether it materially changes the recommended action path.
func newSignalCommand() *cobra.Command {
	var input string
	cmd := &cobra.Command{
		Use:   "signal",
		Short: "Apply a signal to a plan and show the replanning decision",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var in signalInput
			if err := readJSONFile(input, &in); err != nil {
				return err
			}
			if in.Goal.Objective == "" {
				return fmt.Errorf("input %q: goal.objective is required", input)
			}

			// Drive the same use-cases the API uses: create the goal, generate
			// its initial plan, then apply the signal through ApplySignal.
			ctx := cmd.Context()
			svc, err := newMemoryService()
			if err != nil {
				return err
			}
			goal, err := svc.CreateGoal(ctx, app.CreateGoalInput{
				Domain:    in.Goal.Domain,
				Objective: in.Goal.Objective,
				Metric:    in.Goal.Metric,
				Target:    in.Goal.Target,
				Context:   in.Goal.Context,
			})
			if err != nil {
				return err
			}
			previous, err := svc.GeneratePlan(ctx, goal.ID)
			if err != nil {
				return err
			}
			result, err := svc.ApplySignal(ctx, app.SignalInput{
				GoalID:      goal.ID,
				Kind:        in.Signal.Kind,
				Description: in.Signal.Description,
				Payload:     in.Signal.Payload,
			})
			if err != nil {
				return err
			}

			out := map[string]any{
				"signal":           result.Signal.Note(),
				"material":         result.Material,
				"reason":           result.Reason,
				"previous_version": previous,
			}
			if result.Material {
				out["new_version"] = result.PlanVersion
			}
			return printJSON(out)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "path to a goal+signal JSON file (required)")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

// newBacktestCommand replays a scenario of market signals through the engine and
// reports decision-quality metrics. It measures replanning/decision quality, not a
// tradeable strategy return. Runs fully offline.
func newBacktestCommand() *cobra.Command {
	var input string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "Replay a signal timeline and report decision-quality metrics (offline)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var sc backtest.Scenario
			if err := readJSONFile(input, &sc); err != nil {
				return err
			}
			var provOpts []marketdata.OfflineOption
			if sc.FixtureDir != "" {
				provOpts = append(provOpts, marketdata.WithFixtureDir(sc.FixtureDir))
			}
			provider, err := marketdata.NewOfflineProvider(provOpts...)
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			pol, err := policy.Load(cfg.PolicyFile)
			if err != nil {
				return err
			}
			reg, err := buildRegistry(cfg)
			if err != nil {
				return err
			}
			h := backtest.New(reg, pol, provider)
			rep, err := h.Run(cmd.Context(), sc)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(rep)
			}
			rep.Render(os.Stdout)
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "path to a backtest scenario JSON file (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

// newMemoryService builds an in-memory, offline service for the CLI commands
// (evaluate, signal), reusing the production wiring — registry, planner router
// (incl. the finance planner on offline fixtures) and per-domain evaluators — with
// the synchronous inline replan queue. It honours DDE_PLANNER (defaulting to the
// deterministic mock), so the same commands can drive a real provider with a key.
func newMemoryService() (*app.Service, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	reg, err := buildRegistry(cfg)
	if err != nil {
		return nil, err
	}
	pol, err := policy.Load(cfg.PolicyFile)
	if err != nil {
		return nil, err
	}
	eng, err := newEngine(cfg, reg, pol, nil)
	if err != nil {
		return nil, err
	}
	return app.New(storage.NewMemory(), eng, app.WithRegistry(reg)), nil
}

// newVersionCommand prints build information.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("dde %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}

// readJSONFile reads and decodes a JSON file into dst.
func readJSONFile(path string, dst any) error {
	if path == "" {
		return fmt.Errorf("--input is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// printJSON writes v as indented JSON to stdout.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
