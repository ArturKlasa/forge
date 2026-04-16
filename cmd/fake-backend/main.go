package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var mode string
	var script string

	cmd := &cobra.Command{
		Use:   "fake-backend",
		Short: "Canned-response test backend for Forge integration tests",
		Long: `fake-backend reads a script file (.csv or .yaml) that maps prompt patterns
to canned responses, then emits those responses in the requested mode.

Prompt is read from stdin (text/stream-json modes) or received via JSON-RPC
session/prompt (acp mode).

Script CSV format:
  pattern,response

Script YAML format:
  - pattern: "hello"
    response: "world"
    exit_code: 0
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if script == "" {
				return fmt.Errorf("--script is required")
			}
			entries, err := LoadScript(script)
			if err != nil {
				return fmt.Errorf("loading script: %w", err)
			}

			var exitCode int
			switch mode {
			case "text":
				exitCode = runText(os.Stdin, os.Stdout, entries)
			case "stream-json":
				exitCode = runStreamJSON(os.Stdin, os.Stdout, entries)
			case "acp":
				exitCode = runACP(os.Stdin, os.Stdout, entries)
			case "gemini-stream-json":
				exitCode = runGeminiStreamJSON(os.Stdin, os.Stdout, entries)
			case "kiro-text":
				exitCode = runKiroText(os.Stdin, os.Stdout, entries)
			default:
				return fmt.Errorf("unknown mode %q (want text, stream-json, acp, gemini-stream-json, or kiro-text)", mode)
			}

			os.Exit(exitCode)
			return nil
		},
	}

	cmd.Flags().StringVarP(&mode, "mode", "m", "text", "output mode: text, stream-json, acp, gemini-stream-json, or kiro-text")
	cmd.Flags().StringVarP(&script, "script", "s", "", "path to script file (.csv or .yaml)")

	return cmd
}
