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
// Common artifacts: task.md, plan.md, state.md, notes.md.
// Path-specific artifacts vary per path (see below).
func writeArtifacts(task string, path router.Path, branch string, res *researchOutput, rd *state.RunDir) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// task.md — immutable scope statement.
	taskContent := fmt.Sprintf("# Task\n\n%s\n\n---\n_created: %s_\n", task, now)
	if err := writeFile(rd.Path, "task.md", taskContent); err != nil {
		return err
	}

	// Path-specific artifact(s).
	if err := writePathArtifact(task, path, branch, res, rd.Path, now); err != nil {
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

// writePathArtifact writes the path-specific artifact(s) for each loop path.
func writePathArtifact(task string, path router.Path, branch string, res *researchOutput, dir, now string) error {
	switch path {
	case router.PathCreate:
		content := fmt.Sprintf("# Target Shape\n\n## Task\n%s\n\n## Domain Analysis\n%s\n\n## Branch\n%s\n\n---\n_created: %s_\n",
			task, res.DomainSummary, branch, now)
		return writeFile(dir, "target-shape.md", content)

	case router.PathAdd:
		// codebase-map.md — where to integrate.
		codebaseMap := fmt.Sprintf("# Codebase Map\n\n## Task\n%s\n\n## Integration Analysis\n%s\n\n---\n_created: %s_\n",
			task, res.DomainSummary, now)
		if err := writeFile(dir, "codebase-map.md", codebaseMap); err != nil {
			return err
		}
		// specs.md — feature specification.
		var sb strings.Builder
		sb.WriteString("# Feature Specs\n\n## Task\n")
		sb.WriteString(task)
		sb.WriteString("\n\n## Implementation Steps\n\n")
		for i, item := range res.PlanItems {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item))
		}
		sb.WriteString(fmt.Sprintf("\n---\n_created: %s_\n", now))
		return writeFile(dir, "specs.md", sb.String())

	case router.PathFix:
		// bug.md — repro + root cause.
		content := fmt.Sprintf("# Bug Report\n\n## Task\n%s\n\n## Reproduction\n%s\n\n## Repro Script\n\n```bash\n# Steps to reproduce:\n# 1. Run the affected code path\n# 2. Observe incorrect behavior\n```\n\n---\n_created: %s_\n",
			task, res.DomainSummary, now)
		return writeFile(dir, "bug.md", content)

	case router.PathRefactor:
		// target-shape.md — desired end state.
		targetContent := fmt.Sprintf("# Target Shape\n\n## Task\n%s\n\n## Refactoring Analysis\n%s\n\n---\n_created: %s_\n",
			task, res.DomainSummary, now)
		if err := writeFile(dir, "target-shape.md", targetContent); err != nil {
			return err
		}
		// invariants.md — behaviors that must be preserved.
		var sb strings.Builder
		sb.WriteString("# Behavioral Invariants\n\n")
		sb.WriteString("The following behaviors must be preserved after this refactor:\n\n")
		for _, inv := range res.Invariants {
			sb.WriteString(fmt.Sprintf("- %s\n", inv))
		}
		sb.WriteString(fmt.Sprintf("\n---\n_created: %s_\n", now))
		return writeFile(dir, "invariants.md", sb.String())

	case router.PathUpgrade:
		// upgrade-scope.md — target version, breaking changes, affected files.
		var sb strings.Builder
		sb.WriteString("# Upgrade Scope\n\n")
		sb.WriteString(fmt.Sprintf("## Task\n%s\n\n", task))
		sb.WriteString(fmt.Sprintf("## Source Version\n%s\n\n", res.UpgradeSourceVersion))
		sb.WriteString(fmt.Sprintf("## Target Version\n%s\n\n", res.UpgradeTargetVersion))
		sb.WriteString(fmt.Sprintf("## Breaking Changes\n%d documented\n\n", res.UpgradeBreakingCount))
		sb.WriteString("## Expected Manifest Changes\n")
		for _, m := range res.UpgradeManifests {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
		sb.WriteString(fmt.Sprintf("\n## Research Summary\n%s\n\n", res.DomainSummary))
		sb.WriteString(fmt.Sprintf("---\n_created: %s_\n", now))
		if err := writeFile(dir, "upgrade-scope.md", sb.String()); err != nil {
			return err
		}
		// upgrade-target.md — locked source/target versions.
		targetContent := fmt.Sprintf("# Upgrade Target\n\nsource: %s\ntarget: %s\n\n---\n_created: %s_\n",
			res.UpgradeSourceVersion, res.UpgradeTargetVersion, now)
		return writeFile(dir, "upgrade-target.md", targetContent)

	default:
		// Other paths (Test, one-shot) handled in later steps.
		return nil
	}
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
