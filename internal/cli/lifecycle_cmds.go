package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	claudebackend "github.com/arturklasa/forge/internal/backend/claude"
	geminibackend "github.com/arturklasa/forge/internal/backend/gemini"
	kirobackend "github.com/arturklasa/forge/internal/backend/kiro"
	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/config"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/loopengine"
	"github.com/arturklasa/forge/internal/state"
	forgelock "github.com/arturklasa/forge/internal/state/lock"
)

// ─── selectBackend returns a Backend based on config ──────────────────────

func selectBackend(cfgMgr *config.Manager) backend.Backend {
	if cfgMgr == nil {
		return claudebackend.New()
	}
	switch cfgMgr.GetString("backend.default") {
	case "gemini":
		return geminibackend.New()
	case "kiro":
		return kirobackend.New()
	default:
		return claudebackend.New()
	}
}

// ─── forge status (full) ──────────────────────────────────────────────────

func printFullStatus(out io.Writer, mgr *state.Manager, runID string, verbose bool) error {
	var rd *state.RunDir
	if runID != "" {
		runs, err := mgr.ListRuns()
		if err != nil {
			return err
		}
		for _, r := range runs {
			if r.ID == runID {
				rd = &state.RunDir{ID: r.ID, Path: r.Path, StartedAt: r.StartedAt}
				break
			}
		}
		if rd == nil {
			return fmt.Errorf("run %q not found", runID)
		}
	} else {
		current, err := mgr.CurrentRun()
		if err != nil {
			return err
		}
		if current == nil {
			fmt.Fprintln(out, "No active run.")
			return nil
		}
		rd = current
	}

	mk, err := mgr.ReadMarker(rd)
	if err != nil {
		return err
	}

	elapsed := time.Since(rd.StartedAt).Round(time.Second)
	fmt.Fprintf(out, "Run:   %s\nState: %s (started %s ago)\nPath:  %s\n",
		rd.ID, mk, elapsed, rd.Path)

	if verbose {
		entries, err := loopengine.ReadLedger(rd.Path)
		if err == nil && len(entries) > 0 {
			last := entries[len(entries)-1]
			fmt.Fprintf(out, "Iters: %d (last: iter %d, score=%d, stuck=%d)\n",
				len(entries), last.Iteration, last.CompletionScore, last.StuckTier)
		}
		if mk == state.MarkerAwaitingHuman {
			awaitPath := filepath.Join(rd.Path, "awaiting-human.md")
			if data, err := os.ReadFile(awaitPath); err == nil {
				fmt.Fprintln(out, "\n--- awaiting-human.md ---")
				fmt.Fprint(out, string(data))
			}
		}
	}
	return nil
}

// ─── forge history ────────────────────────────────────────────────────────

func runHistory(ctx context.Context, out io.Writer, mgr *state.Manager, full bool) error {
	runs, err := mgr.ListRuns()
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintln(out, "No runs found.")
		return nil
	}

	// Sort newest first (reverse UUID-v7 lexicographic = reverse chronological).
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].ID > runs[j].ID
	})

	limit := 20
	if full {
		limit = len(runs)
	}
	if limit > len(runs) {
		limit = len(runs)
	}

	fmt.Fprintf(out, "%-45s  %-16s  %s\n", "RUN ID", "STATE", "STARTED")
	fmt.Fprintln(out, strings.Repeat("-", 80))
	for _, r := range runs[:limit] {
		started := r.StartedAt.Format("2006-01-02 15:04")
		fmt.Fprintf(out, "%-45s  %-16s  %s\n", r.ID, string(r.Marker), started)
	}
	if !full && len(runs) > limit {
		fmt.Fprintf(out, "\n(%d more — use 'forge history --full' to see all)\n", len(runs)-limit)
	}
	return nil
}

// ─── forge show ───────────────────────────────────────────────────────────

func runShow(out io.Writer, mgr *state.Manager, runID string, iter int) error {
	runs, err := mgr.ListRuns()
	if err != nil {
		return err
	}

	var target *state.RunEntry
	for i := range runs {
		if runs[i].ID == runID {
			target = &runs[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("run %q not found", runID)
	}

	if iter > 0 {
		entries, err := loopengine.ReadLedger(target.Path)
		if err != nil {
			return fmt.Errorf("read ledger: %w", err)
		}
		for _, e := range entries {
			if e.Iteration == iter {
				data, _ := json.MarshalIndent(e, "", "  ")
				fmt.Fprintln(out, string(data))
				return nil
			}
		}
		return fmt.Errorf("iteration %d not found in run %s", iter, runID)
	}

	fmt.Fprintf(out, "Run:   %s\nState: %s\nPath:  %s\n\n", target.ID, string(target.Marker), target.Path)

	artifacts := []string{
		"task.md", "plan.md", "state.md", "notes.md",
		"target-shape.md", "bug.md", "specs.md", "test-scope.md",
	}
	for _, a := range artifacts {
		p := filepath.Join(target.Path, a)
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			continue
		}
		fmt.Fprintf(out, "=== %s ===\n%s\n\n", a, string(data))
	}

	// Show ledger summary.
	entries, err := loopengine.ReadLedger(target.Path)
	if err == nil && len(entries) > 0 {
		fmt.Fprintf(out, "=== ledger (%d iterations) ===\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(out, "  iter %-3d exit=%-3d files=%-2d score=%-3d stuck=%d\n",
				e.Iteration, e.Exit.Code, len(e.FilesChanged), e.CompletionScore, e.StuckTier)
		}
	}
	return nil
}

// ─── forge clean ─────────────────────────────────────────────────────────

func runClean(out io.Writer, mgr *state.Manager, maxRuns int) error {
	runs, err := mgr.ListRuns()
	if err != nil {
		return err
	}

	var terminal, nonTerminal []state.RunEntry
	for _, r := range runs {
		switch r.Marker {
		case state.MarkerDone, state.MarkerAborted, state.MarkerFailed:
			terminal = append(terminal, r)
		default:
			nonTerminal = append(nonTerminal, r)
		}
	}

	// Sort terminal oldest-first so we remove oldest.
	sort.Slice(terminal, func(i, j int) bool {
		return terminal[i].ID < terminal[j].ID
	})

	total := len(nonTerminal) + len(terminal)
	toRemove := total - maxRuns
	if toRemove <= 0 {
		fmt.Fprintln(out, "Nothing to clean.")
		return nil
	}

	removed := 0
	for i := 0; i < toRemove && i < len(terminal); i++ {
		r := terminal[i]
		if err := os.RemoveAll(r.Path); err != nil {
			fmt.Fprintf(out, "WARN: failed to remove %s: %v\n", r.ID, err)
			continue
		}
		fmt.Fprintf(out, "Removed: %s (%s)\n", r.ID, string(r.Marker))
		removed++
	}

	cleaned := cleanForgeStashes(out)
	if cleaned > 0 {
		fmt.Fprintf(out, "Cleaned %d forge-emergency git stash(es).\n", cleaned)
	}

	if removed == 0 && cleaned == 0 {
		fmt.Fprintln(out, "Nothing to clean.")
	}
	return nil
}

func cleanForgeStashes(out io.Writer) int {
	result, err := exec.Command("git", "stash", "list").Output() //nolint:gosec
	if err != nil {
		return 0
	}
	cleaned := 0
	for _, line := range strings.Split(string(result), "\n") {
		if !strings.Contains(line, "forge-emergency-") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 1 {
			continue
		}
		ref := strings.TrimSpace(parts[0])
		if err := exec.Command("git", "stash", "drop", ref).Run(); err != nil { //nolint:gosec
			fmt.Fprintf(out, "WARN: failed to drop stash %s: %v\n", ref, err)
			continue
		}
		cleaned++
	}
	return cleaned
}

// ─── forge stop ───────────────────────────────────────────────────────────

func runStop(out io.Writer, forgeDir string) error {
	sc, err := forgelock.ReadSidecar(forgeDir)
	if err != nil {
		return fmt.Errorf("read lock sidecar: %w", err)
	}
	if sc.PID == 0 {
		fmt.Fprintln(out, "No active run to stop.")
		return nil
	}

	proc, err := os.FindProcess(sc.PID)
	if err != nil {
		return fmt.Errorf("find process %d: %w", sc.PID, err)
	}

	fmt.Fprintf(out, "Sending SIGTERM to PID %d (run %s)...\n", sc.PID, sc.RunID)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process: %w", err)
	}

	// Wait for PAUSED marker (up to 30s).
	workDir := filepath.Dir(forgeDir)
	mgr := state.NewManager(workDir)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := mgr.CurrentRun()
		if current != nil {
			mk, _ := mgr.ReadMarker(current)
			if mk == state.MarkerPaused {
				fmt.Fprintln(out, "Run paused successfully.")
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(out, "WARN: timed out waiting for PAUSED state.")
	return nil
}

// ─── forge resume ─────────────────────────────────────────────────────────

func runResume(ctx context.Context, out io.Writer, mgr *state.Manager, workDir string, runID string) error {
	var rd *state.RunDir
	if runID != "" {
		runs, err := mgr.ListRuns()
		if err != nil {
			return err
		}
		for _, r := range runs {
			if r.ID == runID {
				rd = &state.RunDir{ID: r.ID, Path: r.Path, StartedAt: r.StartedAt}
				break
			}
		}
		if rd == nil {
			return fmt.Errorf("run %q not found", runID)
		}
	} else {
		current, err := mgr.CurrentRun()
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("no active run to resume")
		}
		rd = current
	}

	mk, err := mgr.ReadMarker(rd)
	if err != nil {
		return err
	}

	switch mk {
	case state.MarkerPaused, state.MarkerAwaitingHuman, state.MarkerRunning:
		// Resumable states.
	default:
		return fmt.Errorf("run %s is in %s state and cannot be resumed", rd.ID, mk)
	}

	// Count already-completed iterations.
	entries, _ := loopengine.ReadLedger(rd.Path)
	doneIters := len(entries)

	// Determine mode from task.md.
	taskData, _ := os.ReadFile(filepath.Join(rd.Path, "task.md"))
	path := extractPathFromTask(string(taskData))

	// Transition back to RUNNING.
	if err := mgr.Transition(rd, state.MarkerRunning); err != nil {
		return fmt.Errorf("transition to RUNNING: %w", err)
	}

	l, err := forgelock.Acquire(mgr.ForgeDir(), rd.ID)
	if err != nil {
		return err
	}
	defer l.Release()

	fmt.Fprintf(out, "Resuming run %s from iteration %d...\n", rd.ID, doneIters+1)

	cfgMgr, _ := config.Load(workDir)
	be := selectBackend(cfgMgr)

	remaining := 100 - doneIters
	if remaining <= 0 {
		remaining = 1
	}

	_, loopErr := loopengine.Run(ctx, loopengine.Options{
		RunDir:         rd,
		Backend:        be,
		GitHelper:      forgegit.New(workDir),
		StateManager:   mgr,
		MaxIterations:  remaining,
		Path:           path,
		Output:         out,
		StartIteration: doneIters,
	})
	return loopErr
}

func extractPathFromTask(taskContent string) string {
	for _, line := range strings.Split(taskContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "path:") {
			return strings.ToLower(strings.TrimSpace(line[5:]))
		}
	}
	return "create"
}

// retentionMaxRuns reads the retention.max_runs value from config.
func retentionMaxRuns(workDir string) int {
	m, err := config.Load(workDir)
	if err != nil {
		return 50
	}
	v := m.GetString("retention.max_runs")
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return 50
}
