package ctxmgr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arturklasa/forge/internal/ctxmgr"
)

func TestAssemblePrompt_BasicFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task.md", "Build a hello-world CLI")
	writeFile(t, dir, "plan.md", "1. Create main.go\n2. Add README")
	writeFile(t, dir, "state.md", "Iteration 1: started")

	mgr := ctxmgr.New(dir, nil)
	prompt, err := mgr.AssemblePrompt(context.Background(), "create", 50_000)
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}

	if !strings.Contains(prompt, "Build a hello-world CLI") {
		t.Error("prompt should contain task.md content")
	}
	if !strings.Contains(prompt, "Create main.go") {
		t.Error("prompt should contain plan.md content")
	}
	if !strings.Contains(prompt, "Iteration 1: started") {
		t.Error("prompt should contain state.md content")
	}
}

func TestAssemblePrompt_PathSpecificArtifact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task.md", "Fix the bug")
	writeFile(t, dir, "bug.md", "Repro: run tests → off-by-one at line 42")

	mgr := ctxmgr.New(dir, nil)
	prompt, err := mgr.AssemblePrompt(context.Background(), "fix", 50_000)
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}

	if !strings.Contains(prompt, "off-by-one at line 42") {
		t.Error("fix path should include bug.md content")
	}
}

func TestAssemblePrompt_SystemPromptIncluded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task.md", "task content")

	for _, path := range []string{"create", "fix", "refactor", "add"} {
		mgr := ctxmgr.New(dir, nil)
		prompt, err := mgr.AssemblePrompt(context.Background(), path, 50_000)
		if err != nil {
			t.Fatalf("AssemblePrompt(%s): %v", path, err)
		}
		if !strings.Contains(strings.ToLower(prompt), "mode:") {
			t.Errorf("path %s: prompt should contain Mode: line", path)
		}
	}
}

func TestAssemblePrompt_TokenBudgetRespected(t *testing.T) {
	dir := t.TempDir()
	// Write a large task.md that exceeds a tiny budget.
	bigContent := strings.Repeat("x", 10_000)
	writeFile(t, dir, "task.md", bigContent)

	mgr := ctxmgr.New(dir, nil)
	// Budget of 100 tokens ≈ 400 chars.
	prompt, err := mgr.AssemblePrompt(context.Background(), "create", 100)
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}

	// The prompt should be less than 200 tokens (= 800 chars) — system prompt + truncated task.
	approxTokens := ctxmgr.ApproxTokens(prompt)
	if approxTokens > 200 {
		t.Errorf("prompt token count %d exceeds budget+overhead expectation", approxTokens)
	}
}

func TestDistillation_TriggerOnStateMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task.md", "task")
	// Write state.md > 8000 tokens (32000 chars).
	bigState := strings.Repeat("state content line\n", 2000) // ~36000 chars
	writeFile(t, dir, "state.md", bigState)

	// Without a brain, it falls back to truncation.
	mgr := ctxmgr.New(dir, nil)
	_, err := mgr.AssemblePrompt(context.Background(), "create", 200_000)
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}

	// state.md should be truncated now.
	data, _ := os.ReadFile(filepath.Join(dir, "state.md"))
	tokensAfter := ctxmgr.ApproxTokens(string(data))
	if tokensAfter > thresholdStateMDTest+500 {
		t.Errorf("state.md not truncated: %d tokens still exceeds threshold", tokensAfter)
	}
}

const thresholdStateMDTest = 4000 // distill target size in tokens

func TestDistillation_TriggerOnNotesMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task.md", "task")
	bigNotes := strings.Repeat("notes content line\n", 2500) // ~47500 chars > 10000 tokens
	writeFile(t, dir, "notes.md", bigNotes)

	mgr := ctxmgr.New(dir, nil)
	_, err := mgr.AssemblePrompt(context.Background(), "create", 200_000)
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "notes.md"))
	if ctxmgr.ApproxTokens(string(data)) > 10_500 {
		t.Errorf("notes.md not truncated after distillation trigger")
	}
}

func TestDistillation_ArchivesOriginal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "task.md", "task")
	bigState := strings.Repeat("line\n", 10000) // ~50000 chars
	writeFile(t, dir, "state.md", bigState)

	mgr := ctxmgr.New(dir, nil)
	_, err := mgr.AssemblePrompt(context.Background(), "create", 200_000)
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}

	// An archive file should have been created.
	entries, _ := filepath.Glob(filepath.Join(dir, "state-*.md"))
	if len(entries) == 0 {
		t.Error("expected archive file state-<timestamp>.md to be created")
	}
}

func TestNeedsDistillation(t *testing.T) {
	dir := t.TempDir()

	mgr := ctxmgr.New(dir, nil)
	if mgr.NeedsDistillation() {
		t.Error("empty dir should not need distillation")
	}

	// Write state.md exceeding threshold.
	bigState := strings.Repeat("x", 33_000) // > 8000 tokens
	writeFile(t, dir, "state.md", bigState)

	if !mgr.NeedsDistillation() {
		t.Error("dir with big state.md should need distillation")
	}
}

func TestApproxTokens(t *testing.T) {
	s := strings.Repeat("a", 400)
	tokens := ctxmgr.ApproxTokens(s)
	if tokens != 100 {
		t.Errorf("ApproxTokens(400 chars) = %d, want 100", tokens)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
