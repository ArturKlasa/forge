package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"context"
	"strings"

	"github.com/arturklasa/forge/internal/backend"
	claudebackend "github.com/arturklasa/forge/internal/backend/claude"
	"github.com/arturklasa/forge/internal/config"
	forgegit "github.com/arturklasa/forge/internal/git"
	forgelog "github.com/arturklasa/forge/internal/log"
	"github.com/arturklasa/forge/internal/loopengine"
	"github.com/arturklasa/forge/internal/planphase"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
	forgelock "github.com/arturklasa/forge/internal/state/lock"
	"github.com/spf13/cobra"
)

// RegisterCommands attaches all subcommand stubs to the root command.
func RegisterCommands(root *cobra.Command) {
	root.AddCommand(
		newPlanCmd(),
		newStatusCmd(),
		newStopCmd(),
		newResumeCmd(),
		newHistoryCmd(),
		newShowCmd(),
		newCleanCmd(),
		newBackendCmd(),
		newConfigCmd(),
		newDoctorCmd(),
		newTestUtilityCmd(),
	)
}

// newPlanCmd returns the `plan` subcommand.
func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <task>",
		Short: "Research, draft a plan, and confirm before running",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := args[0]

			workDir, _ := cmd.Root().PersistentFlags().GetString("path")
			if workDir == "" {
				var err error
				workDir, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			modeFlag, _ := cmd.Flags().GetString("mode")
			yesFlag, _ := cmd.Root().PersistentFlags().GetBool("yes")

			opts := planphase.Options{
				Task:         task,
				WorkDir:      workDir,
				ForceYes:     yesFlag,
				Output:       cmd.OutOrStdout(),
				StateManager: state.NewManager(workDir),
				GitHelper:    forgegit.New(workDir),
			}
			if modeFlag != "" {
				opts.PathOverride = router.Path(modeFlag)
			}

			res, err := planphase.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			switch res.Action {
			case planphase.ActionChain:
				fmt.Fprintf(out, "Detected: %s chain\n", res.ChainKey)
				for i, p := range res.Chain {
					fmt.Fprintf(out, "Stages: %d/%s\n", i+1, strings.Title(string(p)))
				}
				fmt.Fprintln(out, "(Composite chaining implemented in step 23)")
				return nil
			case planphase.ActionAbort:
				return nil
			default:
				// ActionGo: plan phase accepted, start the loop.
				fmt.Fprintf(out, "Plan accepted. Run ID: %s\n", res.RunDir.ID)
				l, lockErr := forgelock.Acquire(state.NewManager(workDir).ForgeDir(), res.RunDir.ID)
				if lockErr != nil {
					return lockErr
				}
				defer l.Release()
				be := claudebackend.New()
				_, loopErr := loopengine.Run(cmd.Context(), loopengine.Options{
					RunDir:        res.RunDir,
					Backend:       be,
					GitHelper:     forgegit.New(workDir),
					StateManager:  state.NewManager(workDir),
					MaxIterations: 100,
					Path:          string(res.Path),
					Output:        out,
				})
				return loopErr
			}
		},
	}
	cmd.Flags().String("mode", "", "Force a specific mode (create|add|fix|refactor|upgrade|test|review|document|explain|research)")
	return cmd
}

// newStatusCmd returns the `status` subcommand.
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of the current or specified run",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			current, err := mgr.CurrentRun()
			if err != nil {
				return fmt.Errorf("read current run: %w", err)
			}
			if current == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No active run.")
				return nil
			}

			mk, err := mgr.ReadMarker(current)
			if err != nil {
				return fmt.Errorf("read marker: %w", err)
			}

			elapsed := time.Since(current.StartedAt).Round(time.Second)
			fmt.Fprintf(cmd.OutOrStdout(), "Run:   %s\nState: %s (started %s ago)\n",
				current.ID, mk, elapsed)
			return nil
		},
	}
	cmd.Flags().Bool("verbose", false, "Show detailed status")
	cmd.Flags().String("run", "", "Run ID to query")
	return cmd
}

// newTestUtilityCmd returns the `test-utility` command used during development.
// It will be removed before v1 ship.
func newTestUtilityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "test-utility",
		Short:  "Development utilities (to be removed before v1 ship)",
		Hidden: true,
	}

	createTestRun := &cobra.Command{
		Use:   "create-test-run [id]",
		Short: "Create a test run directory with RUNNING marker",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, _ := cmd.Root().PersistentFlags().GetString("path")
			if workDir == "" {
				var err error
				workDir, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			id := fmt.Sprintf("test-%s-001", time.Now().UTC().Format("2006-01-02"))
			if len(args) == 1 {
				id = args[0]
			}

			mgr := state.NewManager(workDir)
			if err := mgr.Init(); err != nil {
				return fmt.Errorf("state init: %w", err)
			}

			rd, err := mgr.CreateRun(id)
			if err != nil {
				return fmt.Errorf("create run: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created run: %s\nPath: %s\n", rd.ID, rd.Path)
			return nil
		},
	}
	holdLock := &cobra.Command{
		Use:   "hold-lock [run-id]",
		Short: "Acquire the run lock and hold it until SIGINT/SIGTERM (for demo/testing)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, _ := cmd.Root().PersistentFlags().GetString("path")
			if workDir == "" {
				var err error
				workDir, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			runID := fmt.Sprintf("demo-%s", time.Now().UTC().Format("2006-01-02-150405"))
			if len(args) == 1 {
				runID = args[0]
			}

			mgr := state.NewManager(workDir)
			if err := mgr.Init(); err != nil {
				return fmt.Errorf("state init: %w", err)
			}

			l, err := forgelock.Acquire(mgr.ForgeDir(), runID)
			if err != nil {
				return err
			}
			defer l.Release()

			fmt.Fprintf(cmd.OutOrStdout(), "(running... lock held for run %s; press Ctrl-C to release)\n", runID)

			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
			<-ch
			fmt.Fprintln(cmd.OutOrStdout(), "\nReleasing lock.")
			return nil
		},
	}
	probeBackend := &cobra.Command{
		Use:   "probe-backend <backend-name> <prompt-file>",
		Short: "Send a prompt through a backend adapter and print the result (for testing)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			backendName := args[0]
			promptFile := args[1]

			var b backend.Backend
			switch backendName {
			case "claude":
				b = claudebackend.New()
			default:
				return fmt.Errorf("unknown backend %q (supported: claude)", backendName)
			}

			result, err := b.RunIteration(context.Background(), backend.Prompt{Path: promptFile}, backend.IterationOpts{})
			if err != nil {
				return fmt.Errorf("run iteration: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Response: %s\n", result.FinalText)
			fmt.Fprintf(cmd.OutOrStdout(), "Tokens: %d in / %d out\n", result.TokensUsage.Input, result.TokensUsage.Output)
			exitLabel := "success"
			if result.Error != nil {
				exitLabel = result.Error.Error()
			} else if result.Truncated {
				exitLabel = "truncated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Exit: %s\n", exitLabel)
			return result.Error
		},
	}

	cmd.AddCommand(createTestRun)
	cmd.AddCommand(holdLock)
	cmd.AddCommand(probeBackend)
	return cmd
}

// newStopCmd returns the `stop` subcommand stub.
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the currently running task",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 24)")
		},
	}
}

// newResumeCmd returns the `resume` subcommand stub.
func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [run-id]",
		Short: "Resume a paused or interrupted run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 24)")
		},
	}
}

// newHistoryCmd returns the `history` subcommand stub.
func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List past runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 24)")
		},
	}
	cmd.Flags().Bool("full", false, "Show full history without truncation")
	return cmd
}

// newShowCmd returns the `show` subcommand stub.
func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Show details of a specific run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 24)")
		},
	}
	cmd.Flags().Int("iter", 0, "Show a specific iteration number")
	return cmd
}

// newCleanCmd returns the `clean` subcommand stub.
func newCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Remove stale run state and temporary files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 24)")
		},
	}
}

// newBackendCmd returns the `backend` command with its `set` subcommand.
func newBackendCmd() *cobra.Command {
	backendCmd := &cobra.Command{
		Use:   "backend",
		Short: "Manage forge backend drivers",
		// No RunE — cobra will print usage when invoked without a subcommand.
	}
	backendCmd.AddCommand(newBackendSetCmd())
	return backendCmd
}

// newBackendSetCmd sets the default backend in the global config.
func newBackendSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Set the active backend driver (claude|gemini|kiro)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			backend := args[0]
			switch backend {
			case "claude", "gemini", "kiro":
			default:
				return fmt.Errorf("unknown backend %q: must be claude, gemini, or kiro", backend)
			}
			if err := m.SetKey("backend.default", backend, true); err != nil {
				return err
			}
			forgelog.G().Info("backend set", "backend", backend)
			return nil
		},
	}
}

// loadConfig loads the Forge configuration for the current working directory.
func loadConfig(cmd *cobra.Command) (*config.Manager, error) {
	path, _ := cmd.Root().PersistentFlags().GetString("path")
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
		}
	}
	return config.Load(path)
}

// newConfigCmd returns the `config` command with its subcommands.
// When invoked without a subcommand it prints the merged effective configuration.
func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config [get|set|unset|edit]",
		Short: "Manage forge configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			b, err := m.MarshalYAML()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
	configCmd.AddCommand(
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigUnsetCmd(),
		newConfigEditCmd(),
	)
	return configCmd
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			key := args[0]
			if !m.Exists(key) {
				return fmt.Errorf("key not found: %s", key)
			}
			fmt.Fprintln(cmd.OutOrStdout(), m.GetString(key))
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			global, _ := cmd.Flags().GetBool("global")
			m, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			if err := m.SetKey(args[0], args[1], global); err != nil {
				return err
			}
			forgelog.G().Info("config set", "key", args[0], "value", args[1], "global", global)
			return nil
		},
	}
	cmd.Flags().Bool("global", false, "Write to global config (~/.config/forge/config.yml)")
	return cmd
}

func newConfigUnsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			global, _ := cmd.Flags().GetBool("global")
			m, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			if err := m.UnsetKey(args[0], global); err != nil {
				return err
			}
			forgelog.G().Info("config unset", "key", args[0], "global", global)
			return nil
		},
	}
	cmd.Flags().Bool("global", false, "Write to global config (~/.config/forge/config.yml)")
	return cmd
}

func newConfigEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Open the configuration file in an editor",
		RunE: func(cmd *cobra.Command, args []string) error {
			global, _ := cmd.Flags().GetBool("global")
			m, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			target := m.RepoPath
			if global {
				target = m.GlobalPath
			}
			// Ensure the file exists so the editor has something to open.
			if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
				if mkErr := os.MkdirAll(target[:len(target)-len("/config.yml")], 0o750); mkErr != nil {
					return fmt.Errorf("creating config dir: %w", mkErr)
				}
				if wErr := os.WriteFile(target, []byte{}, 0o640); wErr != nil {
					return fmt.Errorf("creating config file: %w", wErr)
				}
			}
			edCmd := exec.Command(editor, target) //nolint:gosec
			edCmd.Stdin = os.Stdin
			edCmd.Stdout = os.Stdout
			edCmd.Stderr = os.Stderr
			return edCmd.Run()
		},
	}
	cmd.Flags().Bool("global", false, "Edit global config (~/.config/forge/config.yml)")
	return cmd
}

// newDoctorCmd returns the `doctor` subcommand (partially wired in step 6: git checks).
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose forge installation and dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctorGitChecks(cmd)
		},
	}
}

// runDoctorGitChecks performs git-related diagnostics for forge doctor.
func runDoctorGitChecks(cmd *cobra.Command) error {
	ctx := cmd.Context()

	// git version
	v, err := forgegit.Version(ctx)
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "git: MISSING (install git and retry)")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "git: OK (version %s)\n", v)
	}

	// repo check
	workDir, _ := cmd.Root().PersistentFlags().GetString("path")
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	g := forgegit.New(workDir)
	if !g.IsRepo(ctx) {
		fmt.Fprintln(cmd.OutOrStdout(), "repo: not a git repository")
		return nil
	}

	sha, branch, err := g.HEAD(ctx)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "repo: ERROR (%v)\n", err)
	} else {
		short := sha
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Fprintf(cmd.OutOrStdout(), "repo: OK (HEAD=%s on %s)\n", short, branch)
	}

	// protected-branch detection
	branches, source := g.DetectProtectedBranches(ctx, nil)
	if len(branches) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "protected branches: none detected")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "protected branches: %s (detected via %s)\n",
			joinBranches(branches), source)
	}

	return nil
}

func joinBranches(branches []string) string {
	result := ""
	for i, b := range branches {
		if i > 0 {
			result += ", "
		}
		result += b
	}
	return result
}
