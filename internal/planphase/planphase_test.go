package planphase

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// mockTermReader returns keys from a pre-defined sequence.
type mockTermReader struct {
	keys []byte
	pos  int
}

func (m *mockTermReader) ReadKey() (byte, error) {
	if m.pos >= len(m.keys) {
		return 'n', nil // default to abort when exhausted
	}
	k := m.keys[m.pos]
	m.pos++
	return k, nil
}

// mockBackend is a simple backend that returns canned responses.
type mockBackend struct {
	response string
}

func (m *mockBackend) Name() string { return "mock" }
func (m *mockBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{}
}
func (m *mockBackend) RunIteration(_ context.Context, _ backend.Prompt, _ backend.IterationOpts) (backend.IterationResult, error) {
	return backend.IterationResult{
		FinalText: m.response,
		ExitCode:  0,
	}, nil
}
func (m *mockBackend) Probe(_ context.Context) error { return nil }

// fixedClock returns a deterministic time.
func fixedClock() func() time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05Z", "2026-04-16T15:00:32Z")
	return func() time.Time { return t }
}

// initTestRepo creates a temporary git repository with a clean working tree.
// Returns the repo directory.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	// Initial commit so HEAD exists.
	readmeFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "init")

	return dir
}

// newTestOpts returns a minimal Options wired to a temp repo.
func newTestOpts(t *testing.T, dir string, task string, keys ...byte) Options {
	t.Helper()
	var buf bytes.Buffer
	return Options{
		Task:         task,
		WorkDir:      dir,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		TermReader:   &mockTermReader{keys: keys},
		Output:       &buf,
		Clock:        fixedClock(),
	}
}

// TestHappyPath_YKey verifies the full happy-path: no backend, user presses 'y'.
func TestHappyPath_YKey(t *testing.T) {
	dir := initTestRepo(t)

	// Create a non-protected branch (not main) so we stay on current branch.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feature/test")

	opts := newTestOpts(t, dir, "Create a hello-world Go CLI", 'y')

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
	if res.Path != router.PathCreate {
		t.Errorf("path = %q, want create", res.Path)
	}
	if res.RunDir == nil {
		t.Fatal("RunDir is nil")
	}
	// Verify RUNNING marker set.
	sm := state.NewManager(dir)
	marker, err := sm.ReadMarker(res.RunDir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if marker != state.MarkerRunning {
		t.Errorf("marker = %q, want RUNNING", marker)
	}
	// Verify artifacts exist.
	for _, name := range []string{"task.md", "plan.md", "target-shape.md", "state.md", "notes.md"} {
		if _, err := os.Stat(filepath.Join(res.RunDir.Path, name)); err != nil {
			t.Errorf("artifact %s missing: %v", name, err)
		}
	}
}

// TestAbortKey verifies that pressing 'n' aborts the run.
func TestAbortKey(t *testing.T) {
	dir := initTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feature/abort-test")

	opts := newTestOpts(t, dir, "Create a demo app", 'n')

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionAbort {
		t.Errorf("action = %q, want abort", res.Action)
	}
	sm2 := state.NewManager(dir)
	marker, err := sm2.ReadMarker(res.RunDir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if marker != state.MarkerAborted {
		t.Errorf("marker = %q, want ABORTED", marker)
	}
}

// TestForceYes verifies that --yes bypasses the prompt.
func TestForceYes(t *testing.T) {
	dir := initTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feature/yes-test")

	var buf bytes.Buffer
	opts := Options{
		Task:         "Create a hello-world app",
		ForceYes:     true,
		WorkDir:      dir,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		Output:       &buf,
		Clock:        fixedClock(),
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
}

// TestDirtyTreeGate verifies that uncommitted changes block the plan phase.
func TestDirtyTreeGate(t *testing.T) {
	dir := initTestRepo(t)

	// Dirty the tree.
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirt"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := newTestOpts(t, dir, "Create a hello-world Go CLI")

	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for dirty tree, got nil")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error %q does not mention uncommitted changes", err.Error())
	}
}

// TestProtectedBranchAutoSwitch verifies that a protected branch triggers auto-switch.
func TestProtectedBranchAutoSwitch(t *testing.T) {
	dir := initTestRepo(t)
	// main is always protected (offline convention tier).

	var buf bytes.Buffer
	opts := Options{
		Task:         "Create a trivial hello-world Go CLI",
		WorkDir:      dir,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		TermReader:   &mockTermReader{keys: []byte{'n'}},
		Output:       &buf,
		Clock:        fixedClock(),
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "forge/") {
		t.Errorf("expected forge branch mention in output, got: %s", output)
	}

	// The result branch should be a forge/ branch.
	if !strings.HasPrefix(res.Branch, "forge/") {
		t.Errorf("branch = %q, want forge/ prefix", res.Branch)
	}
}

// TestEditKey verifies that 'e' key triggers editor and re-renders.
func TestEditKey(t *testing.T) {
	dir := initTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feature/edit-test")

	var buf bytes.Buffer
	opts := Options{
		Task:         "Create a demo app",
		WorkDir:      dir,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		// 'e' then 'n': edit → abort.
		TermReader: &mockTermReader{keys: []byte{'e', 'n'}},
		Output:     &buf,
		// Use 'cat' as editor (no-op: reads the file and exits).
		EditorCmd: "cat",
		Clock:     fixedClock(),
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionAbort {
		t.Errorf("action = %q, want abort after edit+abort", res.Action)
	}
}

// TestBackendResearch verifies that backend-driven research populates plan.md.
func TestBackendResearch(t *testing.T) {
	dir := initTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feature/backend-test")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "1. Initialize Go module\n2. Write main.go\n3. Add README\n",
	}
	opts := Options{
		Task:         "Create a hello-world Go CLI",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		TermReader:   &mockTermReader{keys: []byte{'y'}},
		Output:       &buf,
		Clock:        fixedClock(),
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}

	// Verify plan.md contains the backend-generated items.
	planData, err := os.ReadFile(filepath.Join(res.RunDir.Path, "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	planStr := string(planData)
	if !strings.Contains(planStr, "Initialize Go module") {
		t.Errorf("plan.md missing backend content: %s", planStr)
	}
}

// TestRedoResearch verifies 'r' triggers re-research and re-render.
func TestRedoResearch(t *testing.T) {
	dir := initTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "feature/redo-test")

	var buf bytes.Buffer
	opts := Options{
		Task:         "Create a demo app",
		WorkDir:      dir,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		// 'r' redo, then 'n' abort.
		TermReader: &mockTermReader{keys: []byte{'r', 'n'}},
		Output:     &buf,
		Clock:      fixedClock(),
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionAbort {
		t.Errorf("action = %q, want abort", res.Action)
	}

	output := buf.String()
	if !strings.Contains(output, "Rerunning research") {
		t.Errorf("expected 'Rerunning research' in output: %s", output)
	}
}

// TestGenerateRunID verifies run ID format.
func TestGenerateRunID(t *testing.T) {
	t.Parallel()
	clock := fixedClock()
	id := generateRunID(clock(), router.PathCreate, "Create a trivial hello-world Go CLI")
	// Format: YYYY-MM-DD-HHMMSS-path-slug
	if !strings.HasPrefix(id, "2026-04-16-150032-create-") {
		t.Errorf("id = %q, want prefix 2026-04-16-150032-create-", id)
	}
}

// TestTaskSlug verifies slug generation.
func TestTaskSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		task string
		want string
	}{
		{"Create a hello-world Go CLI", "create-a-helloworld-go"},
		{"Fix the login redirect bug", "fix-the-login-redirect"},
		{"a", "a"},
	}
	for _, tt := range tests {
		got := taskSlug(tt.task)
		if got != tt.want {
			t.Errorf("taskSlug(%q) = %q, want %q", tt.task, got, tt.want)
		}
	}
}
