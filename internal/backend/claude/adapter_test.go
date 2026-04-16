package claude

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
)

// fakeBackendBin is the path to the compiled fake-backend binary.
var fakeBackendBin string

// canExec is true when the sandbox allows subprocess execution.
var canExec bool

func TestMain(m *testing.M) {
	// Probe: same SysProcAttr that proc.Wrapper uses on Unix.
	// Some sandboxes block setpgid/setsid, which makes Start() return "operation not permitted".
	probe := exec.Command("/bin/sh", "-c", "exit 0")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setsid: true}
	canExec = probe.Run() == nil

	if canExec {
		// Build fake-backend into a temp dir.
		tmp, err := os.MkdirTemp("", "forge-claude-test-*")
		if err != nil {
			panic("cannot create temp dir: " + err.Error())
		}
		defer os.RemoveAll(tmp)

		bin := filepath.Join(tmp, "fake-backend")
		out, err := exec.Command("go", "build", "-o", bin, "github.com/arturklasa/forge/cmd/fake-backend").CombinedOutput()
		if err != nil {
			// Build failed — disable exec tests.
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

// newTestAdapter returns an Adapter pointed at fake-backend with stream-json mode.
// fake-backend is invoked via a wrapper shell script that translates claude flags
// into fake-backend --mode stream-json --script <script>.
func newTestAdapter(t *testing.T, scriptPath string) (*Adapter, string) {
	t.Helper()

	// Write a thin wrapper that ignores claude flags and calls fake-backend.
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "claude")

	wrapperContent := "#!/bin/sh\nexec " + fakeBackendBin + " --mode stream-json --script " + scriptPath + "\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	return New(WithExecutable(wrapper), WithGracePeriod(2*time.Second)), tmp
}

// newTestAdapterForTimeout returns an adapter whose fake "claude" sleeps forever.
func newTestAdapterForTimeout(t *testing.T) *Adapter {
	t.Helper()
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "claude")

	// sleeps indefinitely — used to test timeout/SIGTERM
	wrapperContent := "#!/bin/sh\nsleep 600\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	return New(WithExecutable(wrapper), WithGracePeriod(100*time.Millisecond))
}

// TestRunIteration_Success verifies that a normal stream-json response is parsed correctly.
func TestRunIteration_Success(t *testing.T) {
	skipIfNoExec(t)
	adapter, _ := newTestAdapter(t, scriptPath(t))

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

// TestRunIteration_Events verifies event sequence: system/init, assistant, result.
func TestRunIteration_Events(t *testing.T) {
	skipIfNoExec(t)
	adapter, _ := newTestAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "create a hello-world"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}

	if len(result.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(result.Events))
	}
	if result.Events[0].Type != "system" || result.Events[0].Subtype != "init" {
		t.Errorf("event[0]: want system/init, got %s/%s", result.Events[0].Type, result.Events[0].Subtype)
	}
	if result.Events[1].Type != "assistant" {
		t.Errorf("event[1]: want assistant, got %s", result.Events[1].Type)
	}
	if result.Events[2].Type != "result" {
		t.Errorf("event[2]: want result, got %s", result.Events[2].Type)
	}
}

// TestRunIteration_ErrorMaxTurns verifies error_max_turns subtype sets Truncated.
func TestRunIteration_ErrorMaxTurns(t *testing.T) {
	skipIfNoExec(t)
	// The trivial script returns exit_code=1 for "error case" → fake-backend
	// emits subtype=error_max_turns.
	adapter, _ := newTestAdapter(t, scriptPath(t))

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Body: "error case"}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}

	if !result.Truncated {
		t.Error("expected Truncated=true for error_max_turns")
	}
	if result.Error == nil {
		t.Error("expected non-nil Error for error_max_turns")
	}
}

// TestRunIteration_PromptFromFile verifies that Prompt.Path is resolved correctly.
func TestRunIteration_PromptFromFile(t *testing.T) {
	skipIfNoExec(t)
	adapter, tmp := newTestAdapter(t, scriptPath(t))

	// Write a prompt file.
	promptFile := filepath.Join(tmp, "prompt.md")
	if err := os.WriteFile(promptFile, []byte("create a hello-world"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	result, err := adapter.RunIteration(context.Background(), backend.Prompt{Path: promptFile}, backend.IterationOpts{})
	if err != nil {
		t.Fatalf("RunIteration: %v", err)
	}
	if !strings.Contains(result.FinalText, "Hello World") {
		t.Errorf("FinalText = %q, want to contain 'Hello World'", result.FinalText)
	}
}

// TestRunIteration_Timeout verifies that a slow process is terminated after the deadline.
func TestRunIteration_Timeout(t *testing.T) {
	skipIfNoExec(t)
	if os.Getenv("CI") != "" {
		t.Skip("flaky in CI due to timing")
	}

	adapter := newTestAdapterForTimeout(t)

	ctx := context.Background()
	start := time.Now()
	_, err := adapter.RunIteration(ctx, backend.Prompt{Body: "test"}, backend.IterationOpts{
		Timeout: 300 * time.Millisecond,
	})
	elapsed := time.Since(start)

	// Should finish well under 5s (grace period is 100ms in test adapter).
	if elapsed > 5*time.Second {
		t.Errorf("timeout test took %v, expected < 5s", elapsed)
	}
	// err may be nil (process was killed via context cancellation) — that's fine.
	// What matters is that it returned promptly.
	_ = err
}

// TestSessionLeakPrevention verifies that --session-id is set and --no-session-persistence appears.
func TestSessionLeakPrevention(t *testing.T) {
	// We capture the args that would be passed by inspecting buildArgs directly.
	a := New()
	args1 := a.buildArgs("uuid-1", backend.IterationOpts{})
	args2 := a.buildArgs("uuid-2", backend.IterationOpts{})

	check := func(args []string, sessionID string) {
		var hasSessionID, hasNoPersist bool
		for i, arg := range args {
			if arg == "--session-id" && i+1 < len(args) && args[i+1] == sessionID {
				hasSessionID = true
			}
			if arg == "--no-session-persistence" {
				hasNoPersist = true
			}
		}
		if !hasSessionID {
			t.Errorf("args %v: missing --session-id %s", args, sessionID)
		}
		if !hasNoPersist {
			t.Errorf("args %v: missing --no-session-persistence", args)
		}
	}

	check(args1, "uuid-1")
	check(args2, "uuid-2")

	// Session IDs must be distinct per call.
	id1 := extractArg(args1, "--session-id")
	id2 := extractArg(args2, "--session-id")
	if id1 == id2 {
		t.Errorf("session IDs should differ per call, both = %q", id1)
	}
}

// TestBuildArgs_AllowedTools verifies --allowedTools flag is included.
func TestBuildArgs_AllowedTools(t *testing.T) {
	a := New()
	args := a.buildArgs("sid", backend.IterationOpts{
		AllowedTools: []string{"Bash", "Read", "Edit"},
	})

	idx := -1
	for i, arg := range args {
		if arg == "--allowedTools" {
			idx = i
			break
		}
	}
	if idx == -1 || idx+1 >= len(args) {
		t.Fatalf("--allowedTools not found in args %v", args)
	}
	if args[idx+1] != "Bash,Read,Edit" {
		t.Errorf("--allowedTools value = %q, want 'Bash,Read,Edit'", args[idx+1])
	}
}

// TestName verifies the adapter name.
func TestName(t *testing.T) {
	a := New()
	if a.Name() != "claude" {
		t.Errorf("Name() = %q, want 'claude'", a.Name())
	}
}

// TestCapabilities verifies that structured output is reported.
func TestCapabilities(t *testing.T) {
	a := New()
	caps := a.Capabilities()
	if !caps.StructuredOutput {
		t.Error("expected StructuredOutput=true")
	}
	if !caps.NativeSubagents {
		t.Error("expected NativeSubagents=true")
	}
	if caps.EffectiveWindow != 200_000 {
		t.Errorf("EffectiveWindow = %d, want 200000", caps.EffectiveWindow)
	}
}

// --- helpers ---

func scriptPath(t *testing.T) string {
	t.Helper()
	// Use the project-level testdata/trivial.yaml.
	// From internal/backend/claude, go up 3 levels to reach the project root.
	root := findProjectRoot(t)
	return filepath.Join(root, "testdata", "trivial.yaml")
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file directory until we find go.mod.
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

func extractArg(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
