package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	forgelog "github.com/arturklasa/forge/internal/log"
	"github.com/arturklasa/forge/internal/cli"
)

func init() {
	forgelog.Init(forgelog.Config{})
}

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

// executeInDir runs the forge command with --path set to a temp directory.
func executeInDir(dir string, args ...string) (stdout string, stderr string, err error) {
	allArgs := append([]string{"--path", dir}, args...)
	return execute(allArgs...)
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

// TestUnimplementedStubs verifies every remaining stub returns a non-zero exit
// and the "not implemented yet" message with the expected step reference.
func TestUnimplementedStubs(t *testing.T) {
	type testCase struct {
		args  []string
		step  int
		inDir bool // run with a temp workdir
	}
	cases := []testCase{
		{[]string{"stop"}, 24, false},
		{[]string{"resume"}, 24, false},
		{[]string{"history"}, 24, false},
		{[]string{"show", "fake-run-id"}, 24, false},
		{[]string{"clean"}, 24, false},
		// doctor is partially implemented in step 6 (git checks)
		// plan is implemented in step 10 — removed from stubs list
		// "some task description" now runs the loop engine (step 12) — no longer a stub
	}

	for _, tc := range cases {
		tc := tc
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var err error
			if tc.inDir {
				_, _, err = executeInDir(t.TempDir(), tc.args...)
			} else {
				_, _, err = execute(tc.args...)
			}
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

// TestStatusCommand verifies forge status output with and without an active run.
func TestStatusCommand(t *testing.T) {
	dir := t.TempDir()

	// No run yet — expect "No active run."
	out, _, err := executeInDir(dir, "status")
	if err != nil {
		t.Fatalf("status (empty): %v", err)
	}
	if !strings.Contains(out, "No active run.") {
		t.Errorf("expected 'No active run.', got: %q", out)
	}

	// Create a test run.
	_, _, err = executeInDir(dir, "test-utility", "create-test-run", "test-2026-04-16-001")
	if err != nil {
		t.Fatalf("create-test-run: %v", err)
	}

	// Now status should show the run.
	out, _, err = executeInDir(dir, "status")
	if err != nil {
		t.Fatalf("status (with run): %v", err)
	}
	if !strings.Contains(out, "test-2026-04-16-001") {
		t.Errorf("expected run ID in output, got: %q", out)
	}
	if !strings.Contains(out, "RUNNING") {
		t.Errorf("expected RUNNING in output, got: %q", out)
	}
}

// newCleanGitRepo creates a temp git repo with a clean working tree on a feature branch.
func newCleanGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@example.com")
	gitRun("config", "user.name", "Test")
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", "README.md")
	gitRun("commit", "-m", "init")
	// Switch to feature branch so main is not checked out (protected branch test lives in planphase package).
	gitRun("checkout", "-b", "feature/test")
	return dir
}

// TestPlanCommand verifies that `forge plan` detects intent correctly.
func TestPlanCommand(t *testing.T) {
	// Chain detection: plan detects a review:fix chain and runs it with --yes.
	t.Run("chain_detection", func(t *testing.T) {
		dir := t.TempDir()
		out, _, err := executeInDir(dir, "plan", "--yes", "Review and fix the auth module")
		if err != nil {
			t.Fatalf("plan chain: unexpected error: %v", err)
		}
		// The chain engine prints a stage header for each stage.
		if !strings.Contains(out, "Stage") {
			t.Errorf("expected stage output in chain run, got: %q", out)
		}
	})

	// Single-path detection via a clean git repo + --yes to skip confirmation.
	singlePathTests := []struct {
		args    []string
		wantOut string
	}{
		{[]string{"plan", "--yes", "Fix the login redirect bug"}, "Path:"},
		{[]string{"plan", "--yes", "Create a hello-world Go CLI"}, "Path:"},
		{[]string{"plan", "--yes", "--mode", "create", "Fix the login redirect bug"}, "Path:"},
	}
	for _, tt := range singlePathTests {
		tt := tt
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			dir := newCleanGitRepo(t)
			allArgs := append([]string{"--path", dir}, tt.args...)
			out, _, err := execute(allArgs...)
			if err != nil {
				t.Fatalf("plan: unexpected error: %v (output: %q)", err, out)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("expected %q in output, got: %q", tt.wantOut, out)
			}
		})
	}

	// Verify 'fmt' usage avoids "imported and not used".
	_ = fmt.Sprintf
}

// TestConfigCommands verifies that config get/set/unset/edit commands work correctly.
func TestConfigCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoDir := t.TempDir()

	// forge config set backend.default gemini
	_, _, err := executeInDir(repoDir, "config", "set", "backend.default", "gemini")
	if err != nil {
		t.Fatalf("config set: %v", err)
	}

	// forge config get backend.default → "gemini"
	out, _, err := executeInDir(repoDir, "config", "get", "backend.default")
	if err != nil {
		t.Fatalf("config get: %v", err)
	}
	if !strings.Contains(out, "gemini") {
		t.Errorf("config get: expected 'gemini', got %q", out)
	}

	// forge config (no subcommand) → merged YAML containing backend
	out, _, err = executeInDir(repoDir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.Contains(out, "backend") {
		t.Errorf("config merged output should contain 'backend', got: %q", out)
	}

	// forge config unset backend.default → default (claude) should win again
	_, _, err = executeInDir(repoDir, "config", "unset", "backend.default")
	if err != nil {
		t.Fatalf("config unset: %v", err)
	}
	out, _, err = executeInDir(repoDir, "config", "get", "backend.default")
	if err != nil {
		t.Fatalf("config get after unset: %v", err)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("after unset, expected default 'claude', got %q", out)
	}

	// forge config edit with EDITOR=true (no-op binary)
	t.Setenv("EDITOR", "true")
	editorPath := findTrueBinary(t)
	if editorPath != "" {
		t.Setenv("EDITOR", editorPath)
		_, _, err = executeInDir(repoDir, "config", "edit")
		if err != nil {
			t.Errorf("config edit with EDITOR=true: %v", err)
		}
	}

	// forge backend set claude
	_, _, err = executeInDir(repoDir, "backend", "set", "claude")
	if err != nil {
		t.Fatalf("backend set claude: %v", err)
	}

	// forge backend set invalid → error
	_, _, err = executeInDir(repoDir, "backend", "set", "invalid")
	if err == nil {
		t.Error("backend set invalid: expected error, got nil")
	}
}

// findTrueBinary returns the path to the `true` binary, or "" if not found.
func findTrueBinary(t *testing.T) string {
	t.Helper()
	candidates := []string{"/bin/true", "/usr/bin/true"}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Write a small shell script as a fallback.
	tmp := filepath.Join(t.TempDir(), "true")
	if err := os.WriteFile(tmp, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		return ""
	}
	return tmp
}
