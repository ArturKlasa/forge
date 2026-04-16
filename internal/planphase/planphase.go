// Package planphase implements the Plan Phase pipeline: pre-gates, research,
// artifact generation, and confirmation UI. Supports Create, Add, Fix, Refactor paths.
package planphase

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// oneShotPaths are the 4 read-only/no-loop modes that skip pre-gates and artifacts.
var oneShotPaths = map[router.Path]bool{
	router.PathReview:   true,
	router.PathDocument: true,
	router.PathExplain:  true,
	router.PathResearch: true,
}

// Action describes the user's decision at the confirmation prompt.
type Action string

const (
	ActionGo    Action = "go"
	ActionAbort Action = "abort"
	ActionChain Action = "chain" // chain detected — not yet fully handled (step 23)
)

// Result is returned from Run after the plan phase completes.
type Result struct {
	Action   Action
	RunDir   *state.RunDir
	Path     router.Path
	Branch   string
	ChainKey string       // set when Action == ActionChain
	Chain    []router.Path // set when Action == ActionChain
	// DepGateInverted is true for Upgrade mode — dep-manifest changes are expected,
	// not treated as hard-stop gate hits.
	DepGateInverted bool
}

// TermReader abstracts single-keystroke reading for testability.
type TermReader interface {
	ReadKey() (byte, error)
}

// Options configures the plan phase run.
type Options struct {
	Task         string
	ForceYes     bool
	PathOverride router.Path

	WorkDir      string
	Backend      backend.Backend // nil → keyword-only routing
	StateManager *state.Manager
	GitHelper    *forgegit.Git

	// ProtectedBranches is the optional list of explicitly-configured branch names.
	ProtectedBranches []string

	// TermReader overrides stdin key reader (for tests).
	TermReader TermReader

	// Output is where UI text is written; defaults to os.Stdout.
	Output io.Writer

	// EditorCmd overrides $EDITOR for the 'e' keystroke (for tests).
	EditorCmd string

	// Clock overrides time.Now() for deterministic branch naming in tests.
	Clock func() time.Time
}

// Run executes the full plan phase for the given task.
// For step 11 only the Create path is fully implemented.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}

	// ── 1. Detect intent ────────────────────────────────────────────────────
	var routerOpts []router.Option
	if opts.PathOverride != "" {
		routerOpts = append(routerOpts, router.WithPathOverride(opts.PathOverride))
	}
	if opts.Backend != nil {
		routerOpts = append(routerOpts, router.WithBackend(opts.Backend))
	}
	r := router.New(routerOpts...)
	routerRes, err := r.Route(ctx, opts.Task)
	if err != nil {
		return nil, fmt.Errorf("intent routing: %w", err)
	}
	if routerRes.NeedsHumanEscalation {
		return nil, fmt.Errorf("intent unclear — please specify a path with --path")
	}

	if routerRes.IsChain {
		// Chain handling is not yet fully implemented (step 23).
		return &Result{
			Action:   ActionChain,
			Path:     routerRes.Chain[0],
			ChainKey: routerRes.ChainKey,
			Chain:    routerRes.Chain,
		}, nil
	}

	detectedPath := routerRes.Path

	// ── 1b. One-shot fast path ───────────────────────────────────────────────
	// One-shot modes (review/document/explain/research) skip pre-gates, research,
	// and confirmation — they go directly to the one-shot engine.
	if oneShotPaths[detectedPath] {
		sm := opts.StateManager
		if sm == nil {
			sm = state.NewManager(opts.WorkDir)
		}
		if err := sm.Init(); err != nil {
			return nil, fmt.Errorf("init state: %w", err)
		}
		runID := generateRunID(opts.Clock(), detectedPath, opts.Task)
		rd, err := sm.CreateRun(runID)
		if err != nil {
			return nil, fmt.Errorf("create run: %w", err)
		}
		if err := sm.Transition(rd, state.MarkerRunning); err != nil {
			return nil, fmt.Errorf("set running marker: %w", err)
		}
		return &Result{Action: ActionGo, RunDir: rd, Path: detectedPath}, nil
	}

	// ── 2. Pre-gates ────────────────────────────────────────────────────────
	branch, err := runPreGates(ctx, opts, detectedPath)
	if err != nil {
		return nil, err
	}

	// ── 3. Init state manager + run dir ─────────────────────────────────────
	sm := opts.StateManager
	if sm == nil {
		sm = state.NewManager(opts.WorkDir)
	}
	if err := sm.Init(); err != nil {
		return nil, fmt.Errorf("init state: %w", err)
	}

	runID := generateRunID(opts.Clock(), detectedPath, opts.Task)
	rd, err := sm.CreateRun(runID)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// Write initial PAUSED marker (not yet confirmed).
	if err := sm.Transition(rd, state.MarkerPaused); err != nil {
		return nil, fmt.Errorf("set paused marker: %w", err)
	}

	// ── 4. Research ─────────────────────────────────────────────────────────
	researchResult, err := runResearch(ctx, opts, detectedPath, rd)
	if err != nil {
		return nil, fmt.Errorf("research phase: %w", err)
	}

	// ── 5. Write artifacts ──────────────────────────────────────────────────
	if err := writeArtifacts(opts.Task, detectedPath, branch, researchResult, rd); err != nil {
		return nil, fmt.Errorf("write artifacts: %w", err)
	}

	// ── 6. Refactor invariant gate (before main confirmation) ───────────────
	if detectedPath == router.PathRefactor {
		aborted, err := runInvariantGate(ctx, opts, rd, sm)
		if err != nil {
			return nil, err
		}
		if aborted {
			return &Result{Action: ActionAbort, RunDir: rd, Path: detectedPath, Branch: branch}, nil
		}
	}

	// ── 6b. Upgrade version confirmation gate ────────────────────────────────
	if detectedPath == router.PathUpgrade {
		aborted, err := runUpgradeGate(ctx, opts, rd, sm, researchResult)
		if err != nil {
			return nil, err
		}
		if aborted {
			return &Result{Action: ActionAbort, RunDir: rd, Path: detectedPath, Branch: branch}, nil
		}
	}

	// ── 7. Confirmation UI ──────────────────────────────────────────────────
	planItems, err := parsePlanItems(filepath.Join(rd.Path, "plan.md"))
	if err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	for {
		renderConfirmation(opts.Output, detectedPath, branch, planItems)

		if opts.ForceYes {
			fmt.Fprintln(opts.Output, "Auto-confirming (--yes).")
			if err := sm.Transition(rd, state.MarkerRunning); err != nil {
				return nil, err
			}
			return &Result{
				Action:          ActionGo,
				RunDir:          rd,
				Path:            detectedPath,
				Branch:          branch,
				DepGateInverted: detectedPath == router.PathUpgrade,
			}, nil
		}

		key, err := readKey(opts)
		if err != nil {
			return nil, fmt.Errorf("read key: %w", err)
		}

		switch key {
		case 'y', 'Y':
			fmt.Fprintln(opts.Output, "")
			if err := sm.Transition(rd, state.MarkerRunning); err != nil {
				return nil, err
			}
			return &Result{
				Action:          ActionGo,
				RunDir:          rd,
				Path:            detectedPath,
				Branch:          branch,
				DepGateInverted: detectedPath == router.PathUpgrade,
			}, nil

		case 'n', 'N':
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "Aborted.")
			if err := sm.Transition(rd, state.MarkerAborted); err != nil {
				return nil, err
			}
			return &Result{Action: ActionAbort, RunDir: rd, Path: detectedPath, Branch: branch}, nil

		case 'e', 'E':
			fmt.Fprintln(opts.Output, "")
			planPath := filepath.Join(rd.Path, "plan.md")
			if err := openEditor(opts, planPath); err != nil {
				fmt.Fprintf(opts.Output, "editor error: %v\n", err)
			}
			// Re-parse after edit.
			planItems, err = parsePlanItems(planPath)
			if err != nil {
				return nil, fmt.Errorf("parse plan after edit: %w", err)
			}
			// Loop → re-render.

		case 'r', 'R':
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "Rerunning research...")
			researchResult, err = runResearch(ctx, opts, detectedPath, rd)
			if err != nil {
				return nil, fmt.Errorf("redo research: %w", err)
			}
			if err := writeArtifacts(opts.Task, detectedPath, branch, researchResult, rd); err != nil {
				return nil, fmt.Errorf("write artifacts (redo): %w", err)
			}
			planItems, err = parsePlanItems(filepath.Join(rd.Path, "plan.md"))
			if err != nil {
				return nil, err
			}
			// Loop → re-render.

		default:
			// Ignore unknown keys.
		}
	}
}

// runInvariantGate renders the Refactor invariant confirmation gate.
// Returns (true, nil) when the user aborts; (false, nil) when confirmed.
func runInvariantGate(ctx context.Context, opts Options, rd *state.RunDir, sm *state.Manager) (aborted bool, err error) {
	_ = ctx // may be used for future async operations

	invariantsPath := filepath.Join(rd.Path, "invariants.md")
	data, readErr := os.ReadFile(invariantsPath)
	if readErr != nil {
		// No invariants file — skip the gate.
		return false, nil
	}

	fmt.Fprintln(opts.Output, "")
	fmt.Fprintln(opts.Output, "═══ Refactor Invariant Gate ═══════════════════════════════════")
	fmt.Fprintln(opts.Output, "The following behaviors must be preserved after the refactor:")
	fmt.Fprintln(opts.Output, "")
	fmt.Fprintln(opts.Output, string(data))
	fmt.Fprint(opts.Output, "[y] confirm invariants  [e] edit invariants  [n] abort\n\n> ")

	if opts.ForceYes {
		fmt.Fprintln(opts.Output, "Auto-confirming invariants (--yes).")
		return false, nil
	}

	for {
		key, err := readKey(opts)
		if err != nil {
			return false, fmt.Errorf("read invariant gate key: %w", err)
		}
		switch key {
		case 'y', 'Y':
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "Invariants confirmed.")
			return false, nil
		case 'n', 'N':
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "Aborted at invariant gate.")
			if err := sm.Transition(rd, state.MarkerAborted); err != nil {
				return true, err
			}
			return true, nil
		case 'e', 'E':
			fmt.Fprintln(opts.Output, "")
			if err := openEditor(opts, invariantsPath); err != nil {
				fmt.Fprintf(opts.Output, "editor error: %v\n", err)
			}
			// Re-read and re-render.
			data, readErr = os.ReadFile(invariantsPath)
			if readErr != nil {
				return false, fmt.Errorf("read invariants after edit: %w", readErr)
			}
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "═══ Refactor Invariant Gate ═══════════════════════════════════")
			fmt.Fprintln(opts.Output, "The following behaviors must be preserved after the refactor:")
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, string(data))
			fmt.Fprint(opts.Output, "[y] confirm invariants  [e] edit invariants  [n] abort\n\n> ")
		}
	}
}

// runUpgradeGate renders the Upgrade version confirmation gate.
// Returns (true, nil) when the user declines; (false, nil) when confirmed.
func runUpgradeGate(_ context.Context, opts Options, rd *state.RunDir, sm *state.Manager, res *researchOutput) (aborted bool, err error) {
	if res == nil {
		return false, nil
	}

	fmt.Fprintln(opts.Output, "")
	fmt.Fprintln(opts.Output, "═══ Upgrade Confirmation ═══════════════════════════════════════")
	fmt.Fprintf(opts.Output, "Upgrading:      %s → %s\n", res.UpgradeSourceVersion, res.UpgradeTargetVersion)
	if res.UpgradeBreakingCount > 0 {
		fmt.Fprintf(opts.Output, "Breaking changes: %d documented\n", res.UpgradeBreakingCount)
	}
	if len(res.UpgradeManifests) > 0 {
		fmt.Fprintf(opts.Output, "Expected manifest changes: %s\n", strings.Join(res.UpgradeManifests, ", "))
	}
	fmt.Fprintln(opts.Output, "(Dep-manifest gate is inverted for this run — manifest changes are expected.)")
	fmt.Fprintln(opts.Output, "")

	if opts.ForceYes {
		fmt.Fprintln(opts.Output, "Auto-confirming upgrade (--yes).")
		// Write the upgrade-target.md to record locked versions.
		return false, nil
	}

	fmt.Fprint(opts.Output, "[y] confirm  [n] abort\n\n> ")
	for {
		key, kErr := readKey(opts)
		if kErr != nil {
			return false, fmt.Errorf("read upgrade gate key: %w", kErr)
		}
		switch key {
		case 'y', 'Y':
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "Upgrade confirmed.")
			return false, nil
		case 'n', 'N':
			fmt.Fprintln(opts.Output, "")
			fmt.Fprintln(opts.Output, "Aborted at upgrade confirmation gate.")
			if tErr := sm.Transition(rd, state.MarkerAborted); tErr != nil {
				return true, tErr
			}
			return true, nil
		}
	}
}

// generateRunID creates a unique run ID from the current time, path, and task.
func generateRunID(t time.Time, p router.Path, task string) string {
	slug := taskSlug(task)
	return fmt.Sprintf("%s-%s-%s", t.UTC().Format("2006-01-02-150405"), string(p), slug)
}

// taskSlug converts a task string to a short URL-safe slug (max 4 words).
func taskSlug(task string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9\s]+`)
	clean := re.ReplaceAllString(strings.ToLower(task), "")
	words := strings.Fields(clean)
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, "-")
}

// branchName returns the forge branch name for a given path and task.
func branchName(t time.Time, p router.Path, task string) string {
	slug := taskSlug(task)
	return fmt.Sprintf("forge/%s-%s-%s", t.UTC().Format("2006-01-02-150405"), string(p), slug)
}

// renderConfirmation prints the plan-phase confirmation UI.
func renderConfirmation(w io.Writer, p router.Path, branch string, items []string) {
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Path:     %s\n", strings.Title(string(p)))
	fmt.Fprintf(w, "Estimate: ~3–6 iterations (hard cap: 100)\n")
	fmt.Fprintf(w, "Branch:   %s\n", branch)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Plan:")
	for i, item := range items {
		fmt.Fprintf(w, "  %d. %s\n", i+1, item)
	}
	fmt.Fprintln(w, "")
	fmt.Fprint(w, "[y] go  [n] abort  [e] edit  [r] redo plan\n\n> ")
}

// readKey reads a single keystroke from the terminal reader or stdin.
func readKey(opts Options) (byte, error) {
	if opts.TermReader != nil {
		return opts.TermReader.ReadKey()
	}
	return stdinReadKey()
}

// openEditor spawns $EDITOR (or opts.EditorCmd) on the given file.
func openEditor(opts Options, path string) error {
	editorCmd := opts.EditorCmd
	if editorCmd == "" {
		editorCmd = os.Getenv("EDITOR")
	}
	if editorCmd == "" {
		editorCmd = "vi"
	}
	cmd := exec.Command(editorCmd, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// parsePlanItems extracts numbered list items from plan.md.
func parsePlanItems(planPath string) ([]string, error) {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return nil, err
	}
	var items []string
	re := regexp.MustCompile(`^\d+\.\s+(.+)$`)
	for _, line := range strings.Split(string(data), "\n") {
		m := re.FindStringSubmatch(strings.TrimSpace(line))
		if m != nil {
			items = append(items, m[1])
		}
	}
	return items, nil
}
