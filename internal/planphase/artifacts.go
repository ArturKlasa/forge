package planphase

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// writeArtifacts creates the plan-phase artifact files inside the run directory.
// For Create path: task.md, target-shape.md, plan.md, state.md, notes.md
func writeArtifacts(task string, path router.Path, branch string, res *researchOutput, rd *state.RunDir) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// task.md — immutable scope statement.
	taskContent := fmt.Sprintf("# Task\n\n%s\n\n---\n_created: %s_\n", task, now)
	if err := writeFile(rd.Path, "task.md", taskContent); err != nil {
		return err
	}

	// target-shape.md — Create-path artifact.
	targetContent := fmt.Sprintf("# Target Shape\n\n## Task\n%s\n\n## Domain Analysis\n%s\n\n## Branch\n%s\n\n---\n_created: %s_\n",
		task, res.DomainSummary, branch, now)
	if err := writeFile(rd.Path, "target-shape.md", targetContent); err != nil {
		return err
	}

	// plan.md — numbered list of steps.
	var sb strings.Builder
	sb.WriteString("# Plan\n\n")
	for i, item := range res.PlanItems {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item))
	}
	sb.WriteString(fmt.Sprintf("\n---\n_path: %s | created: %s_\n", path, now))
	if err := writeFile(rd.Path, "plan.md", sb.String()); err != nil {
		return err
	}

	// state.md — initial progress tracker.
	stateContent := fmt.Sprintf("# State\n\n- Status: PENDING_CONFIRMATION\n- Path: %s\n- Branch: %s\n- Created: %s\n\n## Progress\n\n_(none yet)_\n", path, branch, now)
	if err := writeFile(rd.Path, "state.md", stateContent); err != nil {
		return err
	}

	// notes.md — initial learnings accumulator.
	notesContent := fmt.Sprintf("# Notes\n\n## Research Findings\n\n%s\n\n---\n_created: %s_\n", res.DomainSummary, now)
	if err := writeFile(rd.Path, "notes.md", notesContent); err != nil {
		return err
	}

	return nil
}

// writeFile writes content to path/name, creating any parent dirs as needed.
func writeFile(dir, name, content string) error {
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", name, err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// parsePlanLines extracts numbered list items from multi-line text.
func parsePlanLines(text string) []string {
	re := regexp.MustCompile(`^\d+\.\s+(.+)$`)
	var items []string
	for _, line := range strings.Split(text, "\n") {
		m := re.FindStringSubmatch(strings.TrimSpace(line))
		if m != nil {
			items = append(items, strings.TrimSpace(m[1]))
		}
	}
	return items
}
