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
	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/logging"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// newPlanner selects the reasoning backend named in config. The mock is the
// default and runs with no external dependencies.
func newPlanner(cfg config.Config) llm.Planner {
	switch cfg.Planner {
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
	default:
		return llm.NewMockPlanner()
	}
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

			eng := engine.New(newPlanner(cfg))
			metrics := api.NewMetrics()
			svc := app.New(repo, eng, app.WithMetrics(metrics), app.WithLogger(log))
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
		Objective string         `json:"objective"`
		Metric    string         `json:"metric"`
		Target    string         `json:"target"`
		Context   domain.Context `json:"context"`
	} `json:"goal"`
	Signal struct {
		Kind        string `json:"kind"`
		Description string `json:"description"`
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

// newMemoryService builds an in-memory service for the offline CLI commands
// (evaluate, signal), reusing the production use-cases. The planner honours
// DDE_PLANNER (defaulting to the deterministic mock), so the same commands can
// drive a real provider when a key is configured.
func newMemoryService() (*app.Service, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return app.New(storage.NewMemory(), engine.New(newPlanner(cfg))), nil
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
