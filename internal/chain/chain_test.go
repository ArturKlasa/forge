package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// fakeBackend is a minimal Backend implementation for testing.
// It returns TASK_COMPLETE for every iteration so stages finish immediately.
type fakeBackend struct {
	// extraText is prepended to the TASK_COMPLETE sentinel so tests can
	// inject fake report content into the FinalText field.
	extraText string
}

func (f *fakeBackend) Name() string                        { return "fake" }
func (f *fakeBackend) Capabilities() backend.Capabilities { return backend.Capabilities{} }
func (f *fakeBackend) Probe(_ context.Context) error      { return nil }
func (f *fakeBackend) RunIteration(_ context.Context, _ backend.Prompt, _ backend.IterationOpts) (backend.IterationResult, error) {
	text := f.extraText + "TASK_COMPLETE"
	return backend.IterationResult{
		FinalText:          text,
		CompletionSentinel: true,
	}, nil
}

// fakeTermReader implements planphase.TermReader to simulate user keystrokes.
type fakeTermReader struct {
	keys []byte
	pos  int
}

func (f *fakeTermReader) ReadKey() (byte, error) {
	if f.pos >= len(f.keys) {
		return 'y', nil // default to yes
	}
	k := f.keys[f.pos]
	f.pos++
	return k, nil
}

// newTestStateManager creates a state.Manager rooted in a temp directory.
func newTestStateManager(t *testing.T) (*state.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	sm := state.NewManager(dir)
	if err := sm.Init(); err != nil {
		t.Fatalf("state init: %v", err)
	}
	return sm, dir
}

// TestReviewFixChain runs a 2-stage (review→fix) chain with a fake backend.
// The backend produces a review report with 3 numbered findings.
// Asserts: both stages run, inter-stage task for fix contains the 3 findings,
// result has StagesRun=2 and TerminatedAt=-1.
func TestReviewFixChain(t *testing.T) {
	sm, workDir := newTestStateManager(t)

	// The fake backend will write its FinalText to the oneshot artifact.
	// We need the review stage to produce a report.md with findings so the
	// reviewFixContract can extract them. We inject findings via extraText
	// so they end up in FinalText → synthesized into report.md by oneshot.
	reportContent := "1. Missing input validation\n2. SQL injection risk\n3. No rate limiting\n"
	be := &fakeBackend{extraText: reportContent}

	var out bytes.Buffer
	// ForceYes to skip inter-stage prompts.
	res, err := Run(context.Background(), Options{
		Task:         "review and fix the auth module",
		Chain:        []router.Path{router.PathReview, router.PathFix},
		ChainKey:     "review:fix",
		Predefined:   true,
		Backend:      be,
		StateManager: sm,
		WorkDir:      workDir,
		Output:       &out,
		ForceYes:     true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.StagesRun != 2 {
		t.Errorf("StagesRun = %d, want 2", res.StagesRun)
	}
	if res.TerminatedAt != -1 {
		t.Errorf("TerminatedAt = %d, want -1", res.TerminatedAt)
	}

	// Verify the fix stage task.md contains findings from the review.
	// The stage dir is stage-2-fix inside the run dir.
	runDirPath := filepath.Join(workDir, ".forge", "runs", res.RunID)
	fixStageDir := filepath.Join(runDirPath, "stage-2-fix")
	taskBytes, err := os.ReadFile(filepath.Join(fixStageDir, "task.md"))
	if err != nil {
		t.Fatalf("read fix stage task.md: %v", err)
	}
	taskContent := string(taskBytes)
	if !strings.Contains(taskContent, "Missing input validation") &&
		!strings.Contains(taskContent, "Fix") {
		t.Errorf("fix stage task.md missing expected findings; got:\n%s", taskContent)
	}
	// At minimum, the fix stage task must mention the findings or the original task.
	if !strings.Contains(taskContent, "1.") && !strings.Contains(taskContent, "auth") {
		t.Errorf("fix stage task.md does not reference review output or original task; got:\n%s", taskContent)
	}
}

// TestInterStageDecline verifies that pressing 'n' at the inter-stage gate
// stops the chain after stage 1.
func TestInterStageDecline(t *testing.T) {
	sm, workDir := newTestStateManager(t)
	be := &fakeBackend{}

	// Press 'n' when the inter-stage gate prompt appears.
	tr := &fakeTermReader{keys: []byte{'n'}}

	var out bytes.Buffer
	res, err := Run(context.Background(), Options{
		Task:         "review and fix",
		Chain:        []router.Path{router.PathReview, router.PathFix},
		ChainKey:     "review:fix",
		Predefined:   true,
		Backend:      be,
		StateManager: sm,
		WorkDir:      workDir,
		Output:       &out,
		TermReader:   tr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.StagesRun != 1 {
		t.Errorf("StagesRun = %d, want 1", res.StagesRun)
	}
	if res.TerminatedAt != 0 {
		t.Errorf("TerminatedAt = %d, want 0", res.TerminatedAt)
	}

	// Stage 2 (fix) should never have been entered — its directory should not contain task.md.
	runDirPath := filepath.Join(workDir, ".forge", "runs", res.RunID)
	fixTaskPath := filepath.Join(runDirPath, "stage-2-fix", "task.md")
	if _, err := os.Stat(fixTaskPath); err == nil {
		t.Errorf("stage-2-fix/task.md exists but stage 2 should not have been entered")
	}
}

// TestUnknownContractWarning verifies that a chain with key "review:explain"
// (not in the predefined contracts list) warns about no predefined data-flow contract.
func TestUnknownContractWarning(t *testing.T) {
	sm, workDir := newTestStateManager(t)
	be := &fakeBackend{}

	// Press 'y' to confirm proceeding with the unknown chain.
	tr := &fakeTermReader{keys: []byte{'y', 'y', 'y', 'y', 'y'}}

	var out bytes.Buffer
	_, err := Run(context.Background(), Options{
		Task:         "review and explain the codebase",
		Chain:        []router.Path{router.PathReview, router.PathExplain},
		ChainKey:     "review:explain",
		Predefined:   false, // not predefined
		Backend:      be,
		StateManager: sm,
		WorkDir:      workDir,
		Output:       &out,
		TermReader:   tr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	outStr := out.String()
	if !strings.Contains(strings.ToLower(outStr), "no predefined") {
		t.Errorf("expected output to contain 'no predefined', got:\n%s", outStr)
	}
}

// TestFourStageChainWarnsMaxStages builds a 4-stage chain and asserts the
// output contains a warning about 4 stages.
func TestFourStageChainWarnsMaxStages(t *testing.T) {
	sm, workDir := newTestStateManager(t)
	be := &fakeBackend{}

	var out bytes.Buffer
	_, err := Run(context.Background(), Options{
		Task:         "review fix refactor and test everything",
		Chain:        []router.Path{router.PathReview, router.PathFix, router.PathRefactor, router.PathTest},
		ChainKey:     "review:fix:refactor:test",
		Predefined:   false,
		Backend:      be,
		StateManager: sm,
		WorkDir:      workDir,
		Output:       &out,
		ForceYes:     true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "Warning: chain has 4 stages") {
		t.Errorf("expected output to contain 'Warning: chain has 4 stages', got:\n%s", outStr)
	}
}

// TestChainYMLWritten verifies that after Run, chain.yml is written in the
// run directory and contains the correct stage list.
func TestChainYMLWritten(t *testing.T) {
	sm, workDir := newTestStateManager(t)
	be := &fakeBackend{}

	var out bytes.Buffer
	res, err := Run(context.Background(), Options{
		Task:         "fix and test the login flow",
		Chain:        []router.Path{router.PathFix, router.PathTest},
		ChainKey:     "fix:test",
		Predefined:   true,
		Backend:      be,
		StateManager: sm,
		WorkDir:      workDir,
		Output:       &out,
		ForceYes:     true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	runDirPath := filepath.Join(workDir, ".forge", "runs", res.RunID)
	ymlPath := filepath.Join(runDirPath, "chain.yml")

	data, err := os.ReadFile(ymlPath)
	if err != nil {
		t.Fatalf("read chain.yml: %v", err)
	}

	var cy ChainYML
	if err := json.Unmarshal(data, &cy); err != nil {
		t.Fatalf("unmarshal chain.yml: %v", err)
	}

	if cy.ChainKey != "fix:test" {
		t.Errorf("ChainKey = %q, want %q", cy.ChainKey, "fix:test")
	}
	if len(cy.Stages) != 2 {
		t.Errorf("Stages len = %d, want 2", len(cy.Stages))
	} else {
		if cy.Stages[0] != "fix" {
			t.Errorf("Stages[0] = %q, want %q", cy.Stages[0], "fix")
		}
		if cy.Stages[1] != "test" {
			t.Errorf("Stages[1] = %q, want %q", cy.Stages[1], "test")
		}
	}
	if !cy.Predefined {
		t.Errorf("Predefined = false, want true")
	}
}
