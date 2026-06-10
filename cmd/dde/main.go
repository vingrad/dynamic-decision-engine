// Command dde is the CLI and server entrypoint for the dynamic-decision-engine.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Build information, injected at build time via -ldflags. See the Makefile.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:   "dde",
		Short: "dynamic-decision-engine: ranked, versioned, dynamically-replanned action plans",
		Long: "dde generates, ranks, versions and dynamically updates structured action plans " +
			"from goals, context, constraints, signals and outcomes.",
		SilenceUsage: true,
	}

	root.AddCommand(
		newServeCommand(),
		newMCPCommand(),
		newMigrateCommand(),
		newEvaluateCommand(),
		newSignalCommand(),
		newBacktestCommand(),
		newCalibrateCommand(),
		newVersionCommand(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
