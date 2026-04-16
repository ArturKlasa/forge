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

// TestAddPathHappyPath verifies the Add path produces codebase-map.md + specs.md.
func TestAddPathHappyPath(t *testing.T) {
	dir := initTestRepo(t)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("checkout", "-b", "feature/add-test")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "1. Map codebase\n2. Add feature\n3. Test it\n",
	}
	opts := Options{
		Task:         "Add a metrics endpoint",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		TermReader:   &mockTermReader{keys: []byte{'y'}},
		Output:       &buf,
		Clock:        fixedClock(),
		// Force Add path.
		PathOverride: router.PathAdd,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
	if res.Path != router.PathAdd {
		t.Errorf("path = %q, want add", res.Path)
	}
	// Verify Add-specific artifacts.
	for _, name := range []string{"codebase-map.md", "specs.md"} {
		if _, err := os.Stat(filepath.Join(res.RunDir.Path, name)); err != nil {
			t.Errorf("artifact %s missing: %v", name, err)
		}
	}
	// Verify specs.md contains plan items.
	specsData, _ := os.ReadFile(filepath.Join(res.RunDir.Path, "specs.md"))
	if !strings.Contains(string(specsData), "Map codebase") {
		t.Errorf("specs.md missing plan items: %s", specsData)
	}
}

// TestFixPathHappyPath verifies the Fix path produces bug.md.
func TestFixPathHappyPath(t *testing.T) {
	dir := initTestRepo(t)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("checkout", "-b", "feature/fix-test")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "1. Write regression test\n2. Fix the root cause\n3. Verify fix\n",
	}
	opts := Options{
		Task:         "Fix the off-by-one in parser.go at line 42",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		TermReader:   &mockTermReader{keys: []byte{'y'}},
		Output:       &buf,
		Clock:        fixedClock(),
		PathOverride: router.PathFix,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
	// Verify bug.md exists.
	bugPath := filepath.Join(res.RunDir.Path, "bug.md")
	if _, err := os.Stat(bugPath); err != nil {
		t.Errorf("bug.md missing: %v", err)
	}
	bugData, _ := os.ReadFile(bugPath)
	if !strings.Contains(string(bugData), "Repro Script") {
		t.Errorf("bug.md missing repro script section: %s", bugData)
	}
}

// TestRefactorPathHappyPath verifies the Refactor path produces target-shape.md + invariants.md.
func TestRefactorPathHappyPath(t *testing.T) {
	dir := initTestRepo(t)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("checkout", "-b", "feature/refactor-test")

	var buf bytes.Buffer
	mb := &mockBackend{
		// First call: invariants research; second call: plan research.
		response: "- All existing APIs remain stable\n- Tests still pass\n",
	}
	opts := Options{
		Task:         "Refactor the auth module to reduce coupling",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		// invariant gate: 'y' confirm; plan gate: 'y' go.
		TermReader:   &mockTermReader{keys: []byte{'y', 'y'}},
		Output:       &buf,
		Clock:        fixedClock(),
		PathOverride: router.PathRefactor,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
	// Verify Refactor-specific artifacts.
	for _, name := range []string{"target-shape.md", "invariants.md"} {
		if _, err := os.Stat(filepath.Join(res.RunDir.Path, name)); err != nil {
			t.Errorf("artifact %s missing: %v", name, err)
		}
	}
	// Verify invariants.md content.
	invData, _ := os.ReadFile(filepath.Join(res.RunDir.Path, "invariants.md"))
	if !strings.Contains(string(invData), "Behavioral Invariants") {
		t.Errorf("invariants.md missing header: %s", invData)
	}
	// Verify invariant gate fired (banner in output).
	output := buf.String()
	if !strings.Contains(output, "Invariant Gate") {
		t.Errorf("expected invariant gate in output, got: %s", output)
	}
}

// TestRefactorInvariantGateAbort verifies 'n' at invariant gate aborts the run.
func TestRefactorInvariantGateAbort(t *testing.T) {
	dir := initTestRepo(t)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("checkout", "-b", "feature/refactor-abort")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "- All APIs remain stable\n- Tests still pass\n",
	}
	opts := Options{
		Task:         "Refactor auth module",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		// 'n' at invariant gate → abort.
		TermReader:   &mockTermReader{keys: []byte{'n'}},
		Output:       &buf,
		Clock:        fixedClock(),
		PathOverride: router.PathRefactor,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionAbort {
		t.Errorf("action = %q, want abort", res.Action)
	}
	sm := state.NewManager(dir)
	marker, err := sm.ReadMarker(res.RunDir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if marker != state.MarkerAborted {
		t.Errorf("marker = %q, want ABORTED", marker)
	}
}

// TestUpgradeHappyPath verifies that Upgrade path produces upgrade-scope.md + upgrade-target.md
// and sets DepGateInverted=true on the result.
func TestUpgradeHappyPath(t *testing.T) {
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
	run("checkout", "-b", "feature/upgrade-test")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "source_version=next@13.5.6\ntarget_version=next@14.x\nbreaking_changes=12\nmanifests=package.json,package-lock.json\n",
	}
	opts := Options{
		Task:         "Upgrade Next.js from 13 to 14",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		// 'y' at upgrade confirmation gate, then 'y' at main plan confirm.
		TermReader:   &mockTermReader{keys: []byte{'y', 'y'}},
		Output:       &buf,
		Clock:        fixedClock(),
		PathOverride: router.PathUpgrade,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
	if res.Path != router.PathUpgrade {
		t.Errorf("path = %q, want upgrade", res.Path)
	}
	if !res.DepGateInverted {
		t.Error("DepGateInverted = false, want true for Upgrade mode")
	}
	// Verify upgrade artifacts.
	for _, name := range []string{"upgrade-scope.md", "upgrade-target.md", "task.md", "plan.md"} {
		if _, err := os.Stat(filepath.Join(res.RunDir.Path, name)); err != nil {
			t.Errorf("artifact %s missing: %v", name, err)
		}
	}
	// upgrade-scope.md should mention target version.
	scopeData, err := os.ReadFile(filepath.Join(res.RunDir.Path, "upgrade-scope.md"))
	if err != nil {
		t.Fatalf("read upgrade-scope.md: %v", err)
	}
	if !strings.Contains(string(scopeData), "next@14.x") {
		t.Errorf("upgrade-scope.md does not mention target version; content:\n%s", scopeData)
	}
}

// TestUpgradeGateDeclineAborts verifies that 'n' at the upgrade confirmation gate aborts.
func TestUpgradeGateDeclineAborts(t *testing.T) {
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
	run("checkout", "-b", "feature/upgrade-abort")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "source_version=django@3.2\ntarget_version=django@4.2\nbreaking_changes=5\nmanifests=requirements.txt\n",
	}
	opts := Options{
		Task:         "Upgrade Django from 3.2 to 4.2",
		WorkDir:      dir,
		Backend:      mb,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		// 'n' at upgrade confirmation gate → abort.
		TermReader:   &mockTermReader{keys: []byte{'n'}},
		Output:       &buf,
		Clock:        fixedClock(),
		PathOverride: router.PathUpgrade,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionAbort {
		t.Errorf("action = %q, want abort", res.Action)
	}
	sm := state.NewManager(dir)
	marker, err := sm.ReadMarker(res.RunDir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if marker != state.MarkerAborted {
		t.Errorf("marker = %q, want ABORTED", marker)
	}
}

// TestUpgradeForceYes verifies --yes bypasses upgrade gate and sets DepGateInverted.
func TestUpgradeForceYes(t *testing.T) {
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
	run("checkout", "-b", "feature/upgrade-yes")

	var buf bytes.Buffer
	mb := &mockBackend{
		response: "source_version=rails@7.0\ntarget_version=rails@7.1\nbreaking_changes=3\nmanifests=Gemfile,Gemfile.lock\n",
	}
	opts := Options{
		Task:         "Upgrade Rails from 7.0 to 7.1",
		WorkDir:      dir,
		Backend:      mb,
		ForceYes:     true,
		GitHelper:    forgegit.New(dir),
		StateManager: state.NewManager(dir),
		Output:       &buf,
		Clock:        fixedClock(),
		PathOverride: router.PathUpgrade,
	}

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Errorf("action = %q, want go", res.Action)
	}
	if !res.DepGateInverted {
		t.Error("DepGateInverted = false, want true for Upgrade mode")
	}
}

// TestTestPathArtifacts verifies the Test path produces test-scope.md.
func TestTestPathArtifacts(t *testing.T) {
	dir := initTestRepo(t)
	var buf bytes.Buffer
	mb := &mockBackend{
		response: "framework=go\ncurrent_coverage=45\ncoverage_target=60\ntest_scope=./internal/...\n",
	}
	opts := newTestOpts(t, dir, "Add tests for the checkout flow", 'y')
	opts.Backend = mb
	opts.Output = &buf
	opts.PathOverride = router.PathTest

	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Action != ActionGo {
		t.Fatalf("action = %q, want go", res.Action)
	}
	if !res.TestMode {
		t.Error("TestMode = false, want true for Test path")
	}
	// Verify test-scope.md was created.
	scopePath := filepath.Join(res.RunDir.Path, "test-scope.md")
	data, err := os.ReadFile(scopePath)
	if err != nil {
		t.Fatalf("test-scope.md not found: %v", err)
	}
	content := string(data)
	for _, want := range []string{"# Test Scope", "go", "60%", "45%", "./internal/..."} {
		if !strings.Contains(content, want) {
			t.Errorf("test-scope.md missing %q:\n%s", want, content)
		}
	}
}

// TestTestPathResearch verifies the Test path research output parses correctly.
func TestTestPathResearch(t *testing.T) {
	text := "framework=vitest\ncurrent_coverage=62\ncoverage_target=77\ntest_scope=src/checkout/**/*.ts\n"
	framework, current, target, scope := parseTestScopeFields(text)
	if framework != "vitest" {
		t.Errorf("framework = %q, want vitest", framework)
	}
	if current != 62 {
		t.Errorf("current_coverage = %d, want 62", current)
	}
	if target != 77 {
		t.Errorf("coverage_target = %d, want 77", target)
	}
	if scope != "src/checkout/**/*.ts" {
		t.Errorf("test_scope = %q, want src/checkout/**/*.ts", scope)
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
