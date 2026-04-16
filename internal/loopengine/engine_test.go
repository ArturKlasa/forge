package loopengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	claudebackend "github.com/arturklasa/forge/internal/backend/claude"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/policy"
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

// TestPolicyScannerGateHalt verifies the loop halts when a policy gate is hit.
func TestPolicyScannerGateHalt(t *testing.T) {
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	// Iteration 1: write a clean file. Iteration 2: write package.json (gate hit).
	mockBE := &iterCountBackend{
		responses: []string{"iter 1 ok", "iter 2 ok"},
		iteration: &iteration,
		onRun: func(i int) {
			switch i {
			case 1:
				_ = os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main"), 0o644)
			case 2:
				_ = os.WriteFile(filepath.Join(workDir, "package.json"), []byte(`{"name":"test"}`), 0o644)
			}
		},
	}

	pscanner, err := policy.NewScanner("", nil, nil, nil)
	if err != nil {
		t.Fatalf("policy.NewScanner: %v", err)
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
		PolicyScanner: pscanner,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Loop should have stopped at iteration 2 due to gate hit.
	if res.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", res.Iterations)
	}
	if res.Complete {
		t.Error("Complete should be false after gate halt")
	}
	if !strings.Contains(out.String(), "ESCALATION") {
		t.Errorf("output missing ESCALATION:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "dependency") {
		t.Errorf("output missing 'dependency':\n%s", out.String())
	}
}

// TestPolicyScannerSecretHalt verifies the loop halts on a secret hit.
func TestPolicyScannerSecretHalt(t *testing.T) {
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	mockBE := &iterCountBackend{
		responses: []string{"iter 1 ok", "iter 2 ok"},
		iteration: &iteration,
		onRun: func(i int) {
			if i == 1 {
				// Write a file with a fake AWS key (high entropy, not in allowlist).
				content := `package main
const key = "AKIAY3T6Z7WQXV5MNPKR"
`
				_ = os.WriteFile(filepath.Join(workDir, "config.go"), []byte(content), 0o644)
			}
		},
	}

	pscanner, err := policy.NewScanner("", nil, nil, nil)
	if err != nil {
		t.Fatalf("policy.NewScanner: %v", err)
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
		PolicyScanner: pscanner,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Complete {
		t.Error("Complete should be false after secret halt")
	}
	if !strings.Contains(out.String(), "ESCALATION") {
		t.Errorf("output missing ESCALATION:\n%s", out.String())
	}
}

// TestStuckDetectorExampleA verifies a normal-progress loop stays at Tier 0 (no interruption).
func TestStuckDetectorExampleA(t *testing.T) {
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	// 3 iterations with file changes + state.md updates → Tier 0.
	mockBE := &iterCountBackend{
		responses: []string{
			"working on feature\n<!--FORGE:build_status=pass-->",
			"more changes\n<!--FORGE:build_status=pass-->",
			"TASK_COMPLETE\n<!--FORGE:build_status=pass-->",
		},
		iteration: &iteration,
		onRun: func(i int) {
			_ = os.WriteFile(filepath.Join(workDir, fmt.Sprintf("file%d.go", i)), []byte(fmt.Sprintf("// iter %d", i)), 0o644)
			// Also update state.md to simulate real agent progress (prevents no_state_semantic_delta signal).
			_ = os.WriteFile(filepath.Join(rd.Path, "state.md"), []byte(fmt.Sprintf("# State\nProgress: iter %d", i)), 0o644)
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
	if !res.Complete {
		t.Error("Complete = false; want true (Example A: no stuck → TASK_COMPLETE reached)")
	}

	entries, _ := readLedger(rd.Path)
	for _, e := range entries {
		if e.StuckTier != 0 {
			t.Errorf("iter %d: StuckTier = %d, want 0 (Tier 0 progressing)", e.Iteration, e.StuckTier)
		}
	}
}

// TestStuckDetectorExampleB verifies soft-stuck window triggers Tier 2 (plan regenerated).
func TestStuckDetectorExampleB(t *testing.T) {
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	// 3 iterations with NO file changes + self-report stuck → soft sum ≥ 6 → Tier 2.
	// After Tier 2 action (plan.md appended), continue; then TASK_COMPLETE.
	mockBE := &iterCountBackend{
		responses: []string{
			"working...\n<!--FORGE:self_report=stuck-->",
			"still here\n<!--FORGE:self_report=stuck-->",
			"uncertain\n<!--FORGE:self_report=uncertain-->",
			"TASK_COMPLETE",
		},
		iteration: &iteration,
		// No onRun: no files changed → no_files_changed_in_window fires.
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
	if !res.Complete {
		t.Errorf("Complete = false; want true")
	}

	entries, _ := readLedger(rd.Path)
	// Find an entry with StuckTier = 2 (hard-stuck action taken).
	found := false
	for _, e := range entries {
		if e.StuckTier == 2 {
			found = true
		}
	}
	if !found {
		t.Error("no ledger entry with StuckTier=2; expected Tier 2 from soft-stuck signals")
	}

	// Verify plan.md was annotated (Tier 2 action).
	planContent, err := os.ReadFile(filepath.Join(rd.Path, "plan.md"))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	if !strings.Contains(string(planContent), "Tier 2: hard-stuck") {
		t.Error("plan.md missing Tier 2 annotation; expected Brain.Draft stub notice")
	}
}

// TestStuckDetectorExampleC verifies same_error_fingerprint_4plus triggers Tier 3 (escalation).
func TestStuckDetectorExampleC(t *testing.T) {
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	fp := "f3a2b81c"
	// 4 iterations with same error fingerprint → Tier 3 (dead stuck).
	mockBE := &iterCountBackend{
		responses: []string{
			fmt.Sprintf("build failed\n<!--FORGE:build_status=fail-->\n<!--FORGE:error_fp=%s-->", fp),
			fmt.Sprintf("still failing\n<!--FORGE:build_status=fail-->\n<!--FORGE:error_fp=%s-->", fp),
			fmt.Sprintf("same error\n<!--FORGE:build_status=fail-->\n<!--FORGE:error_fp=%s-->", fp),
			fmt.Sprintf("same error\n<!--FORGE:build_status=fail-->\n<!--FORGE:error_fp=%s-->", fp),
		},
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
	// Loop should have been interrupted by Tier 3 (no EscalationManager → break).
	if res.Complete {
		t.Error("Complete = true; want false (loop should have been broken by dead-stuck)")
	}
	if !strings.Contains(out.String(), "ESCALATION") && !strings.Contains(out.String(), "dead-stuck") {
		t.Errorf("output missing escalation/dead-stuck indicator:\n%s", out.String())
	}

	entries, _ := readLedger(rd.Path)
	found := false
	for _, e := range entries {
		if e.StuckTier == 3 {
			found = true
		}
	}
	if !found {
		tiers := make([]int, len(entries))
		for i, e := range entries {
			tiers[i] = e.StuckTier
		}
		t.Errorf("no ledger entry with StuckTier=3; tiers per entry: %v", tiers)
	}
}

// TestDepGateInverted verifies that dep-manifest changes do NOT halt the loop when
// DepGateInverted=true (Upgrade mode), and that non-dep gates still halt.
func TestDepGateInverted(t *testing.T) {
	skipIfNoExec(t)
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	// Iter 1: modify package.json (dep manifest — should NOT escalate in Upgrade mode).
	// Iter 2: complete.
	mockBE := &iterCountBackend{
		responses: []string{"iter 1 ok", "TASK_COMPLETE"},
		iteration: &iteration,
		onRun: func(i int) {
			if i == 1 {
				_ = os.WriteFile(filepath.Join(workDir, "package.json"), []byte(`{"name":"app","version":"14.0.0"}`), 0o644)
			}
		},
	}

	pscanner, err := policy.NewScanner("", nil, nil, nil)
	if err != nil {
		t.Fatalf("policy.NewScanner: %v", err)
	}

	var out strings.Builder
	res, err := Run(context.Background(), Options{
		RunDir:          rd,
		Backend:         mockBE,
		GitHelper:       git,
		StateManager:    state.NewManager(workDir),
		MaxIterations:   10,
		Path:            "upgrade",
		Output:          &out,
		PolicyScanner:   pscanner,
		DepGateInverted: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Loop should complete (not halt on dep manifest).
	if !res.Complete {
		t.Errorf("Complete = false, want true — dep gate should be suppressed in Upgrade mode")
	}
	if strings.Contains(out.String(), "ESCALATION") {
		t.Errorf("unexpected ESCALATION in Upgrade mode output:\n%s", out.String())
	}
}

// TestDepGateInverted_NonDepGatesStillFire verifies that non-dep mandatory gates
// (e.g. CI pipeline files) still halt even in Upgrade mode (DepGateInverted=true).
func TestDepGateInverted_NonDepGatesStillFire(t *testing.T) {
	skipIfNoExec(t)
	workDir, git := initGitRepo(t)
	rd := makeRunDir(t, workDir)

	iteration := 0
	mockBE := &iterCountBackend{
		responses: []string{"iter 1 ok", "iter 2 ok"},
		iteration: &iteration,
		onRun: func(i int) {
			if i == 1 {
				// Modify package.json (dep — OK in Upgrade mode) AND .github/workflows/ci.yml (CI — still gates).
				_ = os.MkdirAll(filepath.Join(workDir, ".github", "workflows"), 0o755)
				_ = os.WriteFile(filepath.Join(workDir, "package.json"), []byte(`{"version":"14"}`), 0o644)
				_ = os.WriteFile(filepath.Join(workDir, ".github", "workflows", "ci.yml"), []byte("name: CI"), 0o644)
			}
		},
	}

	pscanner, err := policy.NewScanner("", nil, nil, nil)
	if err != nil {
		t.Fatalf("policy.NewScanner: %v", err)
	}

	var out strings.Builder
	res, err := Run(context.Background(), Options{
		RunDir:          rd,
		Backend:         mockBE,
		GitHelper:       git,
		StateManager:    state.NewManager(workDir),
		MaxIterations:   10,
		Path:            "upgrade",
		Output:          &out,
		PolicyScanner:   pscanner,
		DepGateInverted: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Loop should halt due to CI gate (not dep gate).
	if res.Complete {
		t.Error("Complete = true, want false — CI gate should halt even in Upgrade mode")
	}
	if !strings.Contains(out.String(), "ESCALATION") {
		t.Errorf("expected ESCALATION for CI gate hit:\n%s", out.String())
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
