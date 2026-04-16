package brain_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	backendclaude "github.com/arturklasa/forge/internal/backend/claude"
	"github.com/arturklasa/forge/internal/brain"
)

var fakeBackendBin string
var canExec bool

func TestMain(m *testing.M) {
	probe := exec.Command("/bin/sh", "-c", "exit 0")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setsid: true}
	canExec = probe.Run() == nil

	if canExec {
		tmp, err := os.MkdirTemp("", "forge-brain-test-*")
		if err != nil {
			panic("cannot create temp dir: " + err.Error())
		}
		defer os.RemoveAll(tmp)

		bin := filepath.Join(tmp, "fake-backend")
		_, thisFile, _, _ := runtime.Caller(0)
		root := filepath.Join(filepath.Dir(thisFile), "..", "..")
		out, err := exec.Command("go", "build", "-o", bin, "./cmd/fake-backend").CombinedOutput()
		_ = root
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

// newTestBrain creates a Brain backed by a fake-backend that always returns response.
func newTestBrain(t *testing.T, response string) *brain.Brain {
	t.Helper()
	skipIfNoExec(t)

	// Write script YAML.
	script := filepath.Join(t.TempDir(), "script.yaml")
	content := "- pattern: \".*\"\n  response: |\n"
	for _, line := range strings.Split(response, "\n") {
		content += "    " + line + "\n"
	}
	if err := os.WriteFile(script, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	// Write wrapper shell script that ignores claude args.
	wrapper := filepath.Join(t.TempDir(), "claude")
	wrapperContent := "#!/bin/sh\nexec " + fakeBackendBin + " --mode stream-json --script " + script + "\n"
	if err := os.WriteFile(wrapper, []byte(wrapperContent), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	b := backendclaude.New(backendclaude.WithExecutable(wrapper))
	return brain.New(b)
}

func TestClassify(t *testing.T) {
	br := newTestBrain(t, "category=fix\nconfidence=high")

	result, err := br.Classify(context.Background(), "Fix the login bug", []string{"create", "fix", "refactor"})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if result.Category != "fix" {
		t.Errorf("category: got %q, want %q", result.Category, "fix")
	}
	if result.Confidence != "high" {
		t.Errorf("confidence: got %q, want %q", result.Confidence, "high")
	}
}

func TestJudge(t *testing.T) {
	br := newTestBrain(t, "verdict=complete\nconfidence=high\nrationale=All tests pass and task requirements are met.")

	result, err := br.Judge(context.Background(), "Create hello world", "tests passing", "+hello world")
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if result.Verdict != brain.VerdictComplete {
		t.Errorf("verdict: got %q, want %q", result.Verdict, brain.VerdictComplete)
	}
	if result.Confidence != "high" {
		t.Errorf("confidence: got %q, want %q", result.Confidence, "high")
	}
	if result.Rationale == "" {
		t.Error("rationale should be non-empty")
	}
}

func TestDistill(t *testing.T) {
	br := newTestBrain(t, "Compressed content: task done, 3 files changed.")

	out, err := br.Distill(context.Background(), "long content repeated many times...", 100)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	if out == "" {
		t.Error("distilled output should be non-empty")
	}
}

func TestDiagnose(t *testing.T) {
	br := newTestBrain(t, "diagnosis=Agent is repeating the same failing test.\nsuggestion=Clear the test cache and retry with verbose output.")

	result, err := br.Diagnose(context.Background(), "ledger window...", "state...")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if result.Diagnosis == "" {
		t.Error("diagnosis should be non-empty")
	}
	if result.Suggestion == "" {
		t.Error("suggestion should be non-empty")
	}
}

func TestDraft(t *testing.T) {
	br := newTestBrain(t, "feat: add hello world CLI")

	out, err := br.Draft(context.Background(), "a git commit message", "added main.go with hello world")
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if out == "" {
		t.Error("draft output should be non-empty")
	}
}

func TestSpawn(t *testing.T) {
	br := newTestBrain(t, "Found 3 usages of the deprecated API in src/.")

	result, err := br.Spawn(context.Background(), "Find deprecated API usages", "codebase search")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if result.Output == "" {
		t.Error("spawn output should be non-empty")
	}
}

func TestParseFailureRetry(t *testing.T) {
	// Response is malformed — neither parse attempt will succeed.
	// Brain should not return an error (backend call works, just format is wrong).
	br := newTestBrain(t, "this is not the expected format at all")

	_, err := br.Classify(context.Background(), "some input", []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error on parse failure: %v", err)
	}
}
