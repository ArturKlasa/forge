package oneshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// ── test helpers ──────────────────────────────────────────────────────────────

type mockBackend struct {
	// responses maps area keywords → response text; fallback to default.
	responses map[string]string
	// errAreas is the set of areas that should return an error.
	errAreas map[string]int // area → number of times to fail (0 after consumed)
}

func (m *mockBackend) Name() string { return "mock" }
func (m *mockBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{}
}
func (m *mockBackend) RunIteration(_ context.Context, p backend.Prompt, _ backend.IterationOpts) (backend.IterationResult, error) {
	// Extract area from prompt body.
	area := extractArea(p.Body)

	if m.errAreas != nil {
		if remaining, ok := m.errAreas[area]; ok && remaining > 0 {
			m.errAreas[area]--
			return backend.IterationResult{}, errors.New("subagent error")
		}
	}

	resp := fmt.Sprintf("# %s\n\n- finding 1\n- finding 2\n", strings.Title(area))
	if m.responses != nil {
		if r, ok := m.responses[area]; ok {
			resp = r
		} else if r, ok := m.responses["default"]; ok {
			resp = r
		}
	}
	return backend.IterationResult{FinalText: resp}, nil
}
func (m *mockBackend) Probe(_ context.Context) error { return nil }

func extractArea(promptBody string) string {
	for _, line := range strings.Split(promptBody, "\n") {
		if strings.HasPrefix(line, "Focus area: ") {
			return strings.TrimPrefix(line, "Focus area: ")
		}
	}
	return "unknown"
}

func makeRunDir(t *testing.T) *state.RunDir {
	t.Helper()
	dir := t.TempDir()
	runPath := filepath.Join(dir, ".forge", "runs", "test-run")
	if err := os.MkdirAll(runPath, 0o755); err != nil {
		t.Fatal(err)
	}
	return &state.RunDir{ID: "test-run", Path: runPath}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestIsOneShotPath(t *testing.T) {
	oneShotPaths := []router.Path{
		router.PathReview, router.PathDocument,
		router.PathExplain, router.PathResearch,
	}
	for _, p := range oneShotPaths {
		if !IsOneShotPath(p) {
			t.Errorf("IsOneShotPath(%s) = false, want true", p)
		}
	}

	loopPaths := []router.Path{
		router.PathCreate, router.PathAdd, router.PathFix, router.PathRefactor,
	}
	for _, p := range loopPaths {
		if IsOneShotPath(p) {
			t.Errorf("IsOneShotPath(%s) = true, want false", p)
		}
	}
}

func TestReviewHappyPath(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer
	mb := &mockBackend{}

	res, err := Run(context.Background(), Options{
		Task:    "Review the auth module",
		Path:    router.PathReview,
		RunDir:  rd,
		Backend: mb,
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Artifact must exist.
	if _, statErr := os.Stat(res.ArtifactPath); os.IsNotExist(statErr) {
		t.Errorf("artifact not created: %s", res.ArtifactPath)
	}
	artifact := mustReadFile(t, res.ArtifactPath)

	// Must have at least one of the expected area sections.
	for _, area := range []string{"Security", "Architecture", "Correctness"} {
		if !strings.Contains(artifact, area) {
			t.Errorf("artifact missing section %q", area)
		}
	}

	// DONE marker must exist.
	donePath := filepath.Join(rd.Path, "DONE")
	if _, err := os.Stat(donePath); os.IsNotExist(err) {
		t.Error("DONE marker not written")
	}

	// Chain suggestion for Review → review:fix
	if res.ChainSuggestion == "" {
		t.Error("expected chain suggestion for review mode")
	}
	if !strings.Contains(res.ChainSuggestion, "review:fix") {
		t.Errorf("unexpected chain suggestion: %s", res.ChainSuggestion)
	}

	// Output mentions subagents
	if !strings.Contains(out.String(), "Spawning") {
		t.Error("expected 'Spawning' in output")
	}
}

func TestExplainHappyPath(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	res, err := Run(context.Background(), Options{
		Task:    "Explain the auth middleware",
		Path:    router.PathExplain,
		RunDir:  rd,
		Backend: &mockBackend{},
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Artifact file must be explanation.md
	if !strings.HasSuffix(res.ArtifactPath, "explanation.md") {
		t.Errorf("expected explanation.md artifact, got %s", res.ArtifactPath)
	}
	if _, err := os.Stat(res.ArtifactPath); os.IsNotExist(err) {
		t.Error("explanation.md not created")
	}

	// No commits made (no Document path)
	outStr := out.String()
	if strings.Contains(outStr, "Writing documentation") {
		t.Error("explain mode should not write documentation files")
	}

	// No chain suggestion for explain.
	if res.ChainSuggestion != "" {
		t.Errorf("explain should have no chain suggestion, got %q", res.ChainSuggestion)
	}
}

func TestResearchHappyPath(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	res, err := Run(context.Background(), Options{
		Task:    "Research graph database alternatives",
		Path:    router.PathResearch,
		RunDir:  rd,
		Backend: &mockBackend{},
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasSuffix(res.ArtifactPath, "research-report.md") {
		t.Errorf("expected research-report.md artifact, got %s", res.ArtifactPath)
	}
	if _, err := os.Stat(res.ArtifactPath); os.IsNotExist(err) {
		t.Error("research-report.md not created")
	}

	artifact := mustReadFile(t, res.ArtifactPath)
	for _, area := range []string{"Alternatives", "Prior-Art"} {
		// areas are title-cased in synthesis
		if !strings.Contains(artifact, area) {
			t.Errorf("artifact missing area %q", area)
		}
	}
}

func TestDocumentHappyPath(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	res, err := Run(context.Background(), Options{
		Task:    "Document the auth package",
		Path:    router.PathDocument,
		RunDir:  rd,
		Backend: &mockBackend{},
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(res.ArtifactPath); os.IsNotExist(err) {
		t.Error("document artifact not created")
	}

	// Document mode should mention "Writing documentation"
	if !strings.Contains(out.String(), "Writing documentation") {
		t.Error("expected documentation writing notice")
	}
}

func TestSubagentFailureRetryAndStub(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	// security fails twice (1 initial + 1 retry = stub), architecture fails once then succeeds.
	mb := &mockBackend{
		errAreas: map[string]int{
			"security":     2, // both initial + retry fail → stub
			"architecture": 1, // initial fails, retry succeeds
		},
	}

	res, err := Run(context.Background(), Options{
		Task:    "Review the auth module",
		Path:    router.PathReview,
		RunDir:  rd,
		Backend: mb,
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	artifact := mustReadFile(t, res.ArtifactPath)

	// security subagent should produce a stub.
	if !strings.Contains(artifact, "[Subagent failed: security]") {
		t.Errorf("expected stub for security in artifact:\n%s", artifact)
	}

	// architecture should succeed on retry (no stub).
	if strings.Contains(artifact, "[Subagent failed: architecture]") {
		t.Errorf("architecture should have succeeded on retry")
	}

	// Output should mention FAILED for security.
	if !strings.Contains(out.String(), "FAILED") {
		t.Error("expected FAILED mention in output for security")
	}
}

func TestSubagentStubInArtifact(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	// All subagents fail both times.
	mb := &mockBackend{
		errAreas: map[string]int{
			"alternatives": 2,
			"pros-cons":    2,
			"prior-art":    2,
			"cost":         2,
		},
	}

	res, err := Run(context.Background(), Options{
		Task:    "Research graph databases",
		Path:    router.PathResearch,
		RunDir:  rd,
		Backend: mb,
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	artifact := mustReadFile(t, res.ArtifactPath)
	for _, area := range []string{"alternatives", "pros-cons", "prior-art", "cost"} {
		stub := fmt.Sprintf("[Subagent failed: %s]", area)
		if !strings.Contains(artifact, stub) {
			t.Errorf("expected stub %q in artifact", stub)
		}
	}
}

func TestExplainNoCommits(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	_, err := Run(context.Background(), Options{
		Task:    "Explain how logging works",
		Path:    router.PathExplain,
		RunDir:  rd,
		Backend: &mockBackend{},
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No "Writing documentation" output for non-Document modes.
	if strings.Contains(out.String(), "Writing documentation") {
		t.Error("explain mode must not write documentation files")
	}
}

func TestResearchNoCommits(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	_, err := Run(context.Background(), Options{
		Task:    "Research options",
		Path:    router.PathResearch,
		RunDir:  rd,
		Backend: &mockBackend{},
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(out.String(), "Writing documentation") {
		t.Error("research mode must not write documentation files")
	}
}

func TestNilBackendFallback(t *testing.T) {
	rd := makeRunDir(t)
	var out bytes.Buffer

	res, err := Run(context.Background(), Options{
		Task:    "Review the code",
		Path:    router.PathReview,
		RunDir:  rd,
		Backend: nil,
		Output:  &out,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(res.ArtifactPath); os.IsNotExist(err) {
		t.Error("artifact not written with nil backend")
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return string(data)
}
