package cli

import (
	"fmt"
	"os"

	forgelog "github.com/arturklasa/forge/internal/log"
	"github.com/arturklasa/forge/internal/state"
	forgelock "github.com/arturklasa/forge/internal/state/lock"
	"github.com/arturklasa/forge/internal/version"
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root cobra command for forge.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "forge [task]",
		Short: "Forge — AI coding task orchestrator",
		Long: `Forge orchestrates long-running AI coding tasks by driving AI CLIs
(Claude Code / Kiro / Gemini CLI) in automated loops.

Run a task:
  forge "add unit tests for the auth package"

Use a subcommand:
  forge plan "add unit tests for the auth package"
  forge status
  forge history`,
		Version:      version.Version,
		SilenceUsage: true,
		// SilenceErrors is set to true; main.go prints the error.
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		// PersistentPreRunE initialises the global logger from flags before any subcommand runs.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			pf := cmd.Root().PersistentFlags()

			verbose, _ := pf.GetBool("verbose")
			quiet, _ := pf.GetBool("quiet")
			jsonMode, _ := pf.GetBool("json")

			forgelog.Init(forgelog.Config{
				Verbose: verbose,
				Quiet:   quiet,
				JSON:    jsonMode,
			})
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			workDir, _ := cmd.Root().PersistentFlags().GetString("path")
			if workDir == "" {
				var err error
				workDir, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			mgr := state.NewManager(workDir)
			if err := mgr.Init(); err != nil {
				return fmt.Errorf("state init: %w", err)
			}

			runID := "task-stub"
			l, err := forgelock.Acquire(mgr.ForgeDir(), runID)
			if err != nil {
				return err
			}
			defer l.Release()

			return fmt.Errorf("not implemented yet (scheduled for step 12)")
		},
	}

	// Override the version template to match the required output format.
	root.SetVersionTemplate("forge " + version.Version + "\n")

	// -----------------------------------------------------------------
	// Global flags
	// -----------------------------------------------------------------
	pf := root.PersistentFlags()

	pf.BoolP("verbose", "v", false, "Enable verbose output")
	pf.BoolP("quiet", "q", false, "Suppress non-essential output")
	pf.Bool("json", false, "Output in JSON format")
	pf.BoolP("yes", "y", false, "Auto-confirm all prompts")
	pf.String("auto-resolve", "", "Auto-resolve strategy (e.g. 'rebase')")
	pf.Int("timeout", 0, "Global timeout in seconds (0 = no timeout)")
	pf.String("path", "", "Working directory path override")
	pf.String("branch", "", "Target branch name")
	pf.Bool("no-branch", false, "Disable automatic branch creation")
	pf.String("brain", "", "AI backend brain to use")
	pf.String("backend", "", "Backend driver (claude|kiro|gemini)")
	pf.String("chain", "", "Prompt chain definition")
	pf.Int("subagents", 0, "Number of parallel subagents")

	return root
}
