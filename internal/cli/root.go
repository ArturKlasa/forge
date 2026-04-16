package cli

import (
	"fmt"
	"os"
	"time"

	claudebackend "github.com/arturklasa/forge/internal/backend/claude"
	forgegit "github.com/arturklasa/forge/internal/git"
	forgelog "github.com/arturklasa/forge/internal/log"
	"github.com/arturklasa/forge/internal/loopengine"
	"github.com/arturklasa/forge/internal/oneshot"
	"github.com/arturklasa/forge/internal/planphase"
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

			task := args[0]
			pf := cmd.Root().PersistentFlags()

			workDir, _ := pf.GetString("path")
			if workDir == "" {
				var err error
				workDir, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			yesFlag, _ := pf.GetBool("yes")
			timeoutSec, _ := pf.GetInt("timeout")

			mgr := state.NewManager(workDir)
			if err := mgr.Init(); err != nil {
				return fmt.Errorf("state init: %w", err)
			}

			gitHelper := forgegit.New(workDir)

			// Run plan phase.
			planResult, err := planphase.Run(cmd.Context(), planphase.Options{
				Task:         task,
				WorkDir:      workDir,
				ForceYes:     yesFlag,
				Output:       cmd.OutOrStdout(),
				StateManager: mgr,
				GitHelper:    gitHelper,
			})
			if err != nil {
				return err
			}

			if planResult.Action != planphase.ActionGo {
				return nil // aborted or chain (chain handled in step 23)
			}

			be := claudebackend.New()

			// One-shot paths skip the loop engine entirely.
			if oneshot.IsOneShotPath(planResult.Path) {
				_, err = oneshot.Run(cmd.Context(), oneshot.Options{
					Task:    task,
					Path:    planResult.Path,
					RunDir:  planResult.RunDir,
					Backend: be,
					Output:  cmd.OutOrStdout(),
				})
				return err
			}

			// Acquire lock before starting the loop.
			l, err := forgelock.Acquire(mgr.ForgeDir(), planResult.RunDir.ID)
			if err != nil {
				return err
			}
			defer l.Release()

			var maxDuration time.Duration
			if timeoutSec > 0 {
				maxDuration = time.Duration(timeoutSec) * time.Second
			}

			_, err = loopengine.Run(cmd.Context(), loopengine.Options{
				RunDir:        planResult.RunDir,
				Backend:       be,
				GitHelper:     gitHelper,
				StateManager:  mgr,
				MaxIterations: 100,
				MaxDuration:   maxDuration,
				Path:          string(planResult.Path),
				Output:        cmd.OutOrStdout(),
			})
			return err
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
