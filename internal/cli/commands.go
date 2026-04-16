package cli

import (
	"fmt"

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

// newBackendSetCmd returns the `backend set` subcommand stub.
func newBackendSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Set the active backend driver",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 3)")
		},
	}
}

// newConfigCmd returns the `config` command with its subcommands.
func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage forge configuration",
		// No RunE — cobra will print usage when invoked without a subcommand.
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
			return fmt.Errorf("not implemented yet (scheduled for step 3)")
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 3)")
		},
	}
}

func newConfigUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 3)")
		},
	}
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the configuration file in an editor",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (scheduled for step 3)")
		},
	}
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
