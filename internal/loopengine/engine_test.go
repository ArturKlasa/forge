package loopengine

import (
	"context"
	"os"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	claudebackend "github.com/arturklasa/forge/internal/backend/claude"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/state"
)

var (
	fakeBackendBin string
	canExec        bool
)

func TestMain(m *testing.M) {
	probe := exec.Command("/bin/sh", "-c", "exit 0")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setsid: true}
	canExec = probe.Run() == nil

	if canExec {
		tmp, err := os.MkdirTemp("", "forge-loop-test-*")
		if err != nil {
			panic("cannot create temp dir: " + err.Error())
		}
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

// newWrapperAdapter returns a Claude adapter backed by fake-backend.
func newWrapperAdapter(t *testing.T, scriptPath string) *claudebackend.Adapter {
	t.Helper()
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "claude")
	content := "#!/bin/sh\nexec " + fakeBackendBin + " --mode stream-json --script " + scriptPath + "\n"
	if err := os.WriteFile(wrapper, []byte(content), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	return claudebackend.New(
		claudebackend.WithExecutable(wrapper),
		claudebackend.WithGracePeriod(2*time.Second),
	)
}

// initGitRepo creates a temporary git repo and returns the dir + Git helper.
func initGitRepo(t *testing.T) (string, *forgegit.Git) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	placeholder := filepath.Join(dir, ".gitkeep")
	if err := os.WriteFile(placeholder, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".gitkeep")
	run("commit", "-m", "init")
	return dir, forgegit.New(dir)
}

// makeRunDir creates a RunDir with minimal stub artifact files.
func makeRunDir(t *testing.T, workDir string) *state.RunDir {
	t.Helper()
	mgr := state.NewManager(workDir)
	if err := mgr.Init(); err != nil {
		t.Fatalf("state init: %v", err)
	}
	rd, err := mgr.CreateRun("test-run-001")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, name := range []string{"task.md", "plan.md", "state.md"} {
		if err := os.WriteFile(filepath.Join(rd.Path, name), []byte("# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return rd
}

// writeScript writes a YAML script file and returns its path.
func writeScript(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "script.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestEndToEnd_TASK_COMPLETE verifies the loop exits after TASK_COMPLETE is emitted.
func TestEndToEnd_TASK_COMPLETE(t *testing.T) {
	skipIfNoExec(t)

	// fake-backend returns the same response for any prompt; the script matches
	// on empty pattern (wildcard). We need 3 responses — use 3 sequential entries.
	// fake-backend picks the first matching entry. For wildcard cycling we rely on
	// the 3rd entry containing TASK_COMPLETE. The fake-backend reads stdin once per
	// invocation so we need one response per wrapper invocation.
	// Each invocation matches on empty pattern = "match all".
	// We wire 3 iterations by scripting the wrapper to use a counter file.
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	// Use a counter file to cycle through responses.
	counterDir := t.TempDir()
	responses := []string{"working on it", "more progress", "TASK_COMPLETE done"}

	iteration := 0
	mockBE := &iterCountBackend{
		responses: responses,
		iteration: &iteration,
	}

	var out strings.Builder
	res, err := Run(context.Background(), Options{
		RunDir:        rd,
		Backend:       mockBE,
		GitHelper:     git,
		StateManager:  state.NewManager(workDir),
		MaxIterations: 10,
		Path:          "create",
		Output:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = counterDir

	if res.Iterations != 3 {
		t.Errorf("iterations = %d, want 3", res.Iterations)
	}
	if !res.Complete {
		t.Error("Complete = false, want true")
	}
	if res.CapReached {
		t.Error("CapReached = true, unexpected")
	}

	entries, err := readLedger(rd.Path)
	if err != nil {
		t.Fatalf("readLedger: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("ledger entries = %d, want 3", len(entries))
	}
	if !entries[2].Complete {
		t.Error("ledger entry[2].Complete = false, want true")
	}
	if !strings.Contains(out.String(), "DONE") {
		t.Errorf("output missing DONE:\n%s", out.String())
	}
}

// TestCapEnforcement verifies the loop halts at max_iterations.
func TestCapEnforcement(t *testing.T) {
	iteration := 0
	mockBE := &iterCountBackend{
		responses: []string{"working", "working", "working", "working", "working"},
		iteration: &iteration,
	}

	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	var out strings.Builder
	res, err := Run(context.Background(), Options{
		RunDir:        rd,
		Backend:       mockBE,
		GitHelper:     git,
		StateManager:  state.NewManager(workDir),
		MaxIterations: 2,
		Path:          "create",
		Output:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", res.Iterations)
	}
	if res.Complete {
		t.Error("Complete = true, want false")
	}
	if !res.CapReached {
		t.Error("CapReached = false, want true")
	}

	entries, err := readLedger(rd.Path)
	if err != nil {
		t.Fatalf("readLedger: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("ledger entries = %d, want 2", len(entries))
	}
	if !strings.Contains(out.String(), "ESCALATION") {
		t.Errorf("output missing ESCALATION:\n%s", out.String())
	}
}

// TestEndToEnd_Commits verifies commits are created when files are written.
func TestEndToEnd_Commits(t *testing.T) {
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	filePath := filepath.Join(workDir, "output.txt")
	mockBE := &iterCountBackend{
		responses: []string{"working", "working", "TASK_COMPLETE"},
		iteration: &iteration,
		onRun: func(i int) {
			// Write unique content so git sees a real diff each iteration.
			_ = os.WriteFile(filePath, []byte(fmt.Sprintf("iter %d content", i)), 0o644)
		},
	}

	var out strings.Builder
	res, err := Run(context.Background(), Options{
		RunDir:        rd,
		Backend:       mockBE,
		GitHelper:     git,
		StateManager:  state.NewManager(workDir),
		MaxIterations: 10,
		Path:          "create",
		Output:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 3 {
		t.Errorf("iterations = %d, want 3", res.Iterations)
	}
	if res.Commits != 3 {
		t.Errorf("commits = %d, want 3", res.Commits)
	}

	commits, err := git.Log(context.Background(), forgegit.LogOptions{Grep: "forge(create)"})
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("forge commits = %d, want 3", len(commits))
	}
}

// iterCountBackend is a test Backend that cycles through canned responses.
type iterCountBackend struct {
	responses []string
	iteration *int
	onRun     func(i int) // optional side-effect before returning
}

func (b *iterCountBackend) Name() string                        { return "mock" }
func (b *iterCountBackend) Capabilities() backend.Capabilities { return backend.Capabilities{} }
func (b *iterCountBackend) Probe(_ context.Context) error      { return nil }

func (b *iterCountBackend) RunIteration(_ context.Context, _ backend.Prompt, _ backend.IterationOpts) (backend.IterationResult, error) {
	i := *b.iteration
	if b.onRun != nil {
		b.onRun(i + 1)
	}
	text := ""
	if i < len(b.responses) {
		text = b.responses[i]
	}
	*b.iteration++
	complete := strings.Contains(text, "TASK_COMPLETE")
	return backend.IterationResult{
		FinalText:          text,
		CompletionSentinel: complete,
	}, nil
}
