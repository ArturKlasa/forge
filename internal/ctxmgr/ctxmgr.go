// Package ctxmgr implements the Context Manager: prompt assembly, token-budget
// enforcement, and automatic distillation of run-dir artifacts.
package ctxmgr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/brain"
)

const (
	charsPerToken = 4 // heuristic approximation

	thresholdStateMD = 8_000  // tokens
	thresholdNotesMD = 10_000 // tokens
	thresholdPlanMD  = 6_000  // tokens

	// Target sizes after distillation (tokens).
	distillTargetState = 4_000
	distillTargetNotes = 5_000
	distillTargetPlan  = 3_000
)

// Manager assembles prompts and manages distillation.
type Manager struct {
	Brain   *brain.Brain
	RunDir  string
	Verbose bool
}

// New creates a Manager for the given run directory.
func New(runDir string, b *brain.Brain) *Manager {
	return &Manager{
		Brain:  b,
		RunDir: runDir,
	}
}

// AssemblePrompt builds the full prompt.md body for one iteration.
// It enforces the budget via a char-based heuristic.
// Distillation is triggered automatically when artifact files exceed thresholds.
func (m *Manager) AssemblePrompt(ctx context.Context, path string, budgetTokens int) (string, error) {
	// Run distillation if any artifact exceeds its threshold.
	if err := m.distillIfNeeded(ctx); err != nil {
		return "", fmt.Errorf("distill check: %w", err)
	}

	if budgetTokens <= 0 {
		budgetTokens = 100_000 // sensible default
	}
	budgetChars := budgetTokens * charsPerToken

	var parts []string
	remaining := budgetChars

	// 1. System prompt (path-specific) — always included.
	sys := systemPrompt(path)
	if len(sys) <= remaining {
		parts = append(parts, sys)
		remaining -= len(sys)
	}

	// 2. task.md — always included.
	if data := m.readFile("task.md"); data != "" {
		if len(data) <= remaining {
			parts = append(parts, data)
			remaining -= len(data)
		} else {
			// Truncate to budget.
			parts = append(parts, data[:remaining])
			remaining = 0
		}
	}

	if remaining <= 0 {
		return join(parts), nil
	}

	// 3. Path-specific artifact.
	for _, name := range pathArtifacts(path) {
		if data := m.readFile(name); data != "" {
			if len(data) <= remaining {
				parts = append(parts, data)
				remaining -= len(data)
			} else {
				parts = append(parts, data[:remaining])
				remaining = 0
			}
			break
		}
	}

	if remaining <= 0 {
		return join(parts), nil
	}

	// 4. plan.md (top items).
	if data := m.readFile("plan.md"); data != "" {
		chunk := topPlanItems(data, remaining)
		if chunk != "" {
			parts = append(parts, chunk)
			remaining -= len(chunk)
		}
	}

	if remaining <= 0 {
		return join(parts), nil
	}

	// 5. state.md.
	if data := m.readFile("state.md"); data != "" {
		if len(data) <= remaining {
			parts = append(parts, data)
			remaining -= len(data)
		} else {
			parts = append(parts, data[:remaining])
			remaining = 0
		}
	}

	if remaining <= 0 {
		return join(parts), nil
	}

	// 6. notes.md (semantically selected — for now include as much as fits).
	if data := m.readFile("notes.md"); data != "" {
		if len(data) <= remaining {
			parts = append(parts, data)
			remaining -= len(data)
		} else {
			parts = append(parts, data[:remaining])
			remaining = 0
		}
	}

	if remaining <= 0 {
		return join(parts), nil
	}

	// 7. Per-iteration instructions.
	instr := perIterationInstructions(path)
	if len(instr) <= remaining {
		parts = append(parts, instr)
	}

	return join(parts), nil
}

// distillIfNeeded checks each artifact against its threshold and distills if needed.
func (m *Manager) distillIfNeeded(ctx context.Context) error {
	type distillTarget struct {
		name        string
		threshold   int
		targetSz    int
		archiveBase string
	}
	targets := []distillTarget{
		{"state.md", thresholdStateMD, distillTargetState, "state"},
		{"notes.md", thresholdNotesMD, distillTargetNotes, "notes"},
		{"plan.md", thresholdPlanMD, distillTargetPlan, "plan"},
	}

	for _, t := range targets {
		path := filepath.Join(m.RunDir, t.name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file doesn't exist yet
		}
		tokens := len(data) / charsPerToken
		if tokens <= t.threshold {
			continue
		}

		if m.Brain == nil {
			// No brain — archive original then truncate to target.
			archiveName := fmt.Sprintf("%s-%s.md", t.archiveBase, time.Now().Format("20060102-150405"))
			_ = os.WriteFile(filepath.Join(m.RunDir, archiveName), data, 0o644)
			truncated := data
			maxChars := t.targetSz * charsPerToken
			if len(truncated) > maxChars {
				truncated = truncated[len(truncated)-maxChars:]
			}
			if err := os.WriteFile(path, truncated, 0o644); err != nil {
				return fmt.Errorf("truncate %s: %w", t.name, err)
			}
			continue
		}

		compressed, err := m.Brain.Distill(ctx, string(data), t.targetSz)
		if err != nil {
			// Fallback: truncate.
			maxChars := t.targetSz * charsPerToken
			if len(data) > maxChars {
				data = data[len(data)-maxChars:]
			}
			_ = os.WriteFile(path, data, 0o644)
			continue
		}

		// Archive the original.
		archiveName := fmt.Sprintf("%s-%s.md", t.archiveBase, time.Now().Format("20060102-150405"))
		archivePath := filepath.Join(m.RunDir, archiveName)
		_ = os.WriteFile(archivePath, data, 0o644)

		if m.Verbose {
			origTokens := len(data) / charsPerToken
			newTokens := len(compressed) / charsPerToken
			fmt.Printf("%s distilled: %dk → %dk tokens\n", t.name, origTokens/1000, newTokens/1000)
		}

		if err := os.WriteFile(path, []byte(compressed), 0o644); err != nil {
			return fmt.Errorf("write distilled %s: %w", t.name, err)
		}
	}

	return nil
}

// ApproxTokens returns the approximate token count of s using the 4 chars/token heuristic.
func ApproxTokens(s string) int {
	return len(s) / charsPerToken
}

// NeedsDistillation returns true if any artifact exceeds its threshold.
func (m *Manager) NeedsDistillation() bool {
	checks := []struct {
		name      string
		threshold int
	}{
		{"state.md", thresholdStateMD},
		{"notes.md", thresholdNotesMD},
		{"plan.md", thresholdPlanMD},
	}
	for _, c := range checks {
		data, err := os.ReadFile(filepath.Join(m.RunDir, c.name))
		if err != nil {
			continue
		}
		if len(data)/charsPerToken > c.threshold {
			return true
		}
	}
	return false
}

func (m *Manager) readFile(name string) string {
	data, err := os.ReadFile(filepath.Join(m.RunDir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func join(parts []string) string {
	return strings.Join(parts, "\n\n---\n\n")
}

// topPlanItems returns as much of plan.md as fits in maxChars,
// preferring the start of the file (objectives + early steps).
func topPlanItems(plan string, maxChars int) string {
	if len(plan) <= maxChars {
		return plan
	}
	return plan[:maxChars]
}

// pathArtifacts returns the path-specific artifact filenames in preference order.
func pathArtifacts(path string) []string {
	switch strings.ToLower(path) {
	case "fix":
		return []string{"bug.md"}
	case "refactor":
		return []string{"target-shape.md", "invariants.md"}
	case "add":
		return []string{"specs.md", "codebase-map.md"}
	default:
		return []string{"specs.md", "target-shape.md"}
	}
}

// systemPrompt returns the path-specific system prompt.
func systemPrompt(path string) string {
	base := "You are an expert software engineer working in a Forge orchestration loop.\n"
	switch strings.ToLower(path) {
	case "create":
		return base + "Mode: Create — build a new feature or project from scratch.\n"
	case "fix":
		return base + "Mode: Fix — diagnose and fix the described bug. Add a regression test.\n"
	case "refactor":
		return base + "Mode: Refactor — improve code structure without changing observable behaviour.\n"
	case "add":
		return base + "Mode: Add — extend existing functionality with the described feature.\n"
	default:
		return base + fmt.Sprintf("Mode: %s\n", path)
	}
}

// perIterationInstructions returns the tail instructions added to every prompt.
func perIterationInstructions(path string) string {
	lines := []string{
		"## Instructions for this iteration",
		"- Complete exactly one testable unit of work.",
		"- Run all tests before finishing.",
		"- When the overall task is complete, output TASK_COMPLETE on its own line.",
	}
	if path == "fix" {
		lines = append(lines, "- Ensure your fix includes a regression test.")
	}
	return strings.Join(lines, "\n")
}
