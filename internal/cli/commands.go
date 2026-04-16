package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/arturklasa/forge/internal/config"
	forgelog "github.com/arturklasa/forge/internal/log"
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
	)
}

// newPlanCmd returns the `plan` subcommand stub.
func newPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan <task>",
		Short: "Generate a plan for a task without executing it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 11)")
		},
	}
}

// newStatusCmd returns the `status` subcommand stub.
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of the current or specified run",
		RunE: func(cmd *cobra.Command, args []string) error {
			forgelog.G().Info("status requested", "implemented", false)
			return fmt.Errorf("not implemented yet (scheduled for step 4)")
		},
	}
	cmd.Flags().Bool("verbose", false, "Show detailed status")
	cmd.Flags().String("run", "", "Run ID to query")
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

// newDoctorCmd returns the `doctor` subcommand stub.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose forge installation and dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 24)")
		},
	}
}
