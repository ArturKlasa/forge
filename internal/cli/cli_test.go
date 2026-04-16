package cli_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/arturklasa/forge/internal/cli"
)

// execute runs the forge command with the given args and returns stdout, stderr, and any error.
func execute(args ...string) (stdout string, stderr string, err error) {
	root := cli.NewRootCmd()
	cli.RegisterCommands(root)

	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// TestHelpCommands verifies that --help works for every (sub)command and exits 0.
func TestHelpCommands(t *testing.T) {
	helpCases := [][]string{
		{"--help"},
		{"plan", "--help"},
		{"status", "--help"},
		{"stop", "--help"},
		{"resume", "--help"},
		{"history", "--help"},
		{"show", "--help"},
		{"clean", "--help"},
		{"backend", "--help"},
		{"backend", "set", "--help"},
		{"config", "--help"},
		{"config", "get", "--help"},
		{"config", "set", "--help"},
		{"config", "unset", "--help"},
		{"config", "edit", "--help"},
		{"doctor", "--help"},
	}

	for _, args := range helpCases {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			out, _, err := execute(args...)
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if len(out) == 0 {
				t.Error("expected non-empty help output")
			}
		})
	}
}

// TestVersion verifies that --version prints "forge 0.0.1".
func TestVersion(t *testing.T) {
	out, _, err := execute("--version")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(out, "forge 0.0.1") {
		t.Errorf("expected version output to contain 'forge 0.0.1', got: %q", out)
	}
}

// TestUnimplementedStubs verifies every stub returns a non-zero exit and the
// "not implemented yet" message.
func TestUnimplementedStubs(t *testing.T) {
	cases := []struct {
		args []string
		step int
	}{
		{[]string{"status"}, 4},
		{[]string{"stop"}, 24},
		{[]string{"resume"}, 24},
		{[]string{"history"}, 24},
		{[]string{"show", "fake-run-id"}, 24},
		{[]string{"clean"}, 24},
		{[]string{"backend", "set", "claude"}, 3},
		{[]string{"config", "get", "brain"}, 3},
		{[]string{"config", "set", "brain", "claude"}, 3},
		{[]string{"config", "unset", "brain"}, 3},
		{[]string{"config", "edit"}, 3},
		{[]string{"doctor"}, 24},
		{[]string{"plan", "do something"}, 11},
		{[]string{"some task description"}, 12},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			_, _, err := execute(tc.args...)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), "not implemented yet") {
				t.Errorf("expected 'not implemented yet' in error, got: %q", err.Error())
			}
			wantStep := fmt.Sprintf("step %d", tc.step)
			if !strings.Contains(err.Error(), wantStep) {
				t.Errorf("expected step reference %q in error, got: %q", wantStep, err.Error())
			}
		})
	}
}
