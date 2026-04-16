package gemini

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/proc"
)

var fakeBackendBin string
var canExec bool

func TestMain(m *testing.M) {
	probe := exec.Command("/bin/sh", "-c", "exit 0")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setsid: true}
	canExec = probe.Run() == nil

	if canExec {
		tmp, err := os.MkdirTemp("", "forge-gemini-test-*")
		if err != nil {
			panic("cannot create temp dir: " + err.Error())
		}
		defer os.RemoveAll(tmp)

		bin := filepath.Join(tmp, "fake-backend")
		out, err := exec.Command("go", "build", "-o", bin, "github.com/arturklasa/forge/cmd/fake-backend").CombinedOutput()
		if err != nil {
			canExec = false
			_ = out
		} else {
			fakeBackendBin = bin
		}
	}

	os.Exit(m.Run())
}

func skipIfNoExec(t *testing.T) {
	t.Helper()
	if !canExec {
		t.Skip("sandbox blocks subprocess execution")
	}
}

// newTestAdapter returns an Adapter backed by fake-backend in gemini-stream-json mode.
// Gemini takes prompt via -p flag; the wrapper script extracts it and pipes to fake-backend stdin.
func newTestAdapter(t *testing.T, scriptFile string) *Adapter {
	t.Helper()
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "gemini")

	wrapperContent := "#!/bin/sh\n" +
		"PROMPT=''\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  if [ \"$1\" = \"-p\" ]; then\n" +
		"    shift; PROMPT=\"$1\"; shift\n" +
		"  else\n" +
		"    shift\n" +
		"  fi\n" +
		"done\n" +
		"printf '%s' \"$PROMPT\" | exec " + fakeBackendBin + " --mode gemini-stream-json --script " + scriptFile + "\n"

	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	return New(WithExecutable(wrapper), WithGracePeriod(2*time.Second))
}

func scriptPath(t *testing.T) string {
	t.Helper()
	root := findProjectRoot(t)
	return filepath.Join(root, "testdata", "trivial.yaml")
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// TestGemini_Success verifies normal Gemini stream-json response parsing.
func TestGemini_Success(t *testing.T) {
	skipIfNoExec(t)
	adapter := newTestAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	if result.Error != nil {
		t.Errorf("unexpected result.Error: %v", result.Error)
	}
	if !strings.Contains(result.FinalText, "Hello World") {
		t.Errorf("FinalText = %q, want to contain 'Hello World'", result.FinalText)
	}
	if result.TokensUsage.Input == 0 && result.TokensUsage.Output == 0 {
		t.Error("expected non-zero token usage")
	}
}

// TestGemini_Events verifies event sequence: init, message, result.
func TestGemini_Events(t *testing.T) {
	skipIfNoExec(t)
	adapter := newTestAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	if len(result.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(result.Events))
	}
	if result.Events[0].Type != "init" {
		t.Errorf("event[0]: want init, got %s", result.Events[0].Type)
	}
	if result.Events[1].Type != "message" {
		t.Errorf("event[1]: want message, got %s", result.Events[1].Type)
	}
	if result.Events[2].Type != "result" {
		t.Errorf("event[2]: want result, got %s", result.Events[2].Type)
	}
}

// TestGemini_Name verifies adapter name.
func TestGemini_Name(t *testing.T) {
	a := New()
	if a.Name() != "gemini" {
		t.Errorf("Name() = %q, want 'gemini'", a.Name())
	}
}

// TestGemini_Capabilities verifies capabilities.
func TestGemini_Capabilities(t *testing.T) {
	a := New()
	caps := a.Capabilities()
	if !caps.StructuredOutput {
		t.Error("expected StructuredOutput=true")
	}
	if caps.NativeSubagents {
		t.Error("expected NativeSubagents=false")
	}
	if caps.SkipPermissionsFlag != "--approval-mode=yolo" {
		t.Errorf("SkipPermissionsFlag = %q, want '--approval-mode=yolo'", caps.SkipPermissionsFlag)
	}
}

// TestGemini_BuildArgs_SkipPermissions verifies --approval-mode=yolo is always included.
func TestGemini_BuildArgs_SkipPermissions(t *testing.T) {
	a := New()
	args := a.buildArgs(backend.IterationOpts{}, "hello")
	found := false
	for _, arg := range args {
		if arg == "--approval-mode=yolo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args %v: missing --approval-mode=yolo", args)
	}
}

// TestGemini_BuildArgs_Model verifies -m flag is included when model set.
func TestGemini_BuildArgs_Model(t *testing.T) {
	a := New()
	args := a.buildArgs(backend.IterationOpts{Model: "gemini-2.5-flash"}, "hello")
	idx := -1
	for i, arg := range args {
		if arg == "-m" {
			idx = i
			break
		}
	}
	if idx == -1 || idx+1 >= len(args) || args[idx+1] != "gemini-2.5-flash" {
		t.Errorf("args %v: missing -m gemini-2.5-flash", args)
	}
}

// TestGemini_TurnLimitExit verifies exit code 53 sets Truncated.
func TestGemini_TurnLimitExit(t *testing.T) {
	result := parseGeminiStreamJSON("", "", proc.Result{ExitCode: 53, Classification: proc.ExitIterationFail})
	if !result.Truncated {
		t.Error("expected Truncated=true for exit 53")
	}
	if result.Error == nil {
		t.Error("expected non-nil Error for exit 53")
	}
}

// TestGemini_InputErrorExit verifies exit code 42 sets an error.
func TestGemini_InputErrorExit(t *testing.T) {
	result := parseGeminiStreamJSON("", "", proc.Result{ExitCode: 42, Classification: proc.ExitIterationFail})
	if result.Error == nil {
		t.Error("expected non-nil Error for exit 42")
	}
	if result.Truncated {
		t.Error("exit 42 should not set Truncated")
	}
}

// TestGemini_Timeout verifies a slow process is terminated after the deadline.
func TestGemini_Timeout(t *testing.T) {
	skipIfNoExec(t)
	if os.Getenv("CI") != "" {
		t.Skip("flaky in CI due to timing")
	}

	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "gemini")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nsleep 600\n"), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	adapter := New(WithExecutable(wrapper), WithGracePeriod(100*time.Millisecond))
	start := time.Now()
	_, _ = adapter.RunIteration(context.Background(), backend.Prompt{Body: "test"}, backend.IterationOpts{
		Timeout: 300 * time.Millisecond,
	})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout test took %v, expected < 5s", elapsed)
	}
}
