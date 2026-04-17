// Package chain implements composite chaining: stage lifecycle + inter-stage contracts.
package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/loopengine"
	"github.com/arturklasa/forge/internal/oneshot"
	"github.com/arturklasa/forge/internal/planphase"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// ChainYML is persisted at the chain run root as chain.yml (JSON).
type ChainYML struct {
	ChainKey     string   `json:"chain_key"`
	Stages       []string `json:"stages"`        // mode names, e.g. ["review","fix"]
	CurrentStage int      `json:"current_stage"` // 0-based
	RunID        string   `json:"run_id"`
	Task         string   `json:"task"`
	Predefined   bool     `json:"predefined"`
}

// Options for running a composite chain.
type Options struct {
	Task         string
	Chain        []router.Path
	ChainKey     string
	Predefined   bool
	Backend      backend.Backend
	GitHelper    *forgegit.Git
	StateManager *state.Manager
	WorkDir      string
	Output       io.Writer
	ForceYes     bool
	TermReader   planphase.TermReader // for testing
	Clock        func() time.Time    // for testing
}

// Result is the outcome of a chain run.
type Result struct {
	RunID        string
	StagesRun    int
	TerminatedAt int // -1 = all complete, else index of last stage run (0-based)
}

const maxStagesWarn = 3

// Run executes the composite chain.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}

	// 1. Warn if chain has more than maxStagesWarn stages.
	if len(opts.Chain) > maxStagesWarn {
		fmt.Fprintf(opts.Output, "Warning: chain has %d stages (more than %d). This may take a long time.\n",
			len(opts.Chain), maxStagesWarn)
	}

	// 2. If not predefined, ask for confirmation.
	if !opts.Predefined && !opts.ForceYes {
		stageNames := make([]string, len(opts.Chain))
		for i, p := range opts.Chain {
			stageNames[i] = string(p)
		}
		fmt.Fprintf(opts.Output,
			"Chain stages %s have no predefined data-flow contract. Forge will pass stage %s's deliverables as free-form context to stage %s. Proceed? [y/n]\n",
			strings.Join(stageNames, "→"),
			stageNames[0],
			stageNames[len(stageNames)-1],
		)
		key, err := readKey(opts)
		if err != nil {
			return nil, fmt.Errorf("read key: %w", err)
		}
		if key == 'n' || key == 'N' {
			return &Result{TerminatedAt: -1}, nil
		}
	}

	// 3. Create run dir.
	sm := opts.StateManager
	if sm == nil {
		sm = state.NewManager(opts.WorkDir)
	}
	if err := sm.Init(); err != nil {
		return nil, fmt.Errorf("state init: %w", err)
	}

	runID := generateRunID(opts.Clock(), opts.ChainKey, opts.Task)
	runDir, err := sm.CreateRun(runID)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// 4. Write chain.yml.
	stageNames := make([]string, len(opts.Chain))
	for i, p := range opts.Chain {
		stageNames[i] = string(p)
	}
	chainYML := ChainYML{
		ChainKey:     opts.ChainKey,
		Stages:       stageNames,
		CurrentStage: 0,
		RunID:        runID,
		Task:         opts.Task,
		Predefined:   opts.Predefined,
	}
	if err := writeChainYML(runDir.Path, chainYML); err != nil {
		return nil, fmt.Errorf("write chain.yml: %w", err)
	}

	// 5. For each stage...
	for i, mode := range opts.Chain {
		// a. Create stage directory.
		stageDir := filepath.Join(runDir.Path, fmt.Sprintf("stage-%d-%s", i+1, string(mode)))
		if err := os.MkdirAll(filepath.Join(stageDir, "iterations"), 0o755); err != nil {
			return nil, fmt.Errorf("create stage dir: %w", err)
		}

		// c. Update "current" symlink inside runDir.Path.
		currentLink := filepath.Join(runDir.Path, "current")
		_ = os.Remove(currentLink)
		_ = os.Symlink(stageDir, currentLink)

		// d. Print header.
		fmt.Fprintf(opts.Output, "\n── Stage %d/%d: %s ──────────────────────────────────────\n",
			i+1, len(opts.Chain), strings.Title(string(mode)))

		// e. Compute stage task.
		var stageTask string
		if i == 0 {
			stageTask = opts.Task
		} else {
			prevStageDir := filepath.Join(runDir.Path, fmt.Sprintf("stage-%d-%s", i, string(opts.Chain[i-1])))
			contractFn := loadContract(opts.ChainKey, i)
			stageTask = contractFn(prevStageDir, opts.Task)
		}

		// f. Write stageDir/task.md.
		if err := os.WriteFile(filepath.Join(stageDir, "task.md"), []byte(stageTask+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("write task.md: %w", err)
		}
		// g. Write stageDir/plan.md.
		planContent := fmt.Sprintf("# Stage %d Plan\n\nExecuting %s stage...\n", i+1, string(mode))
		if err := os.WriteFile(filepath.Join(stageDir, "plan.md"), []byte(planContent), 0o644); err != nil {
			return nil, fmt.Errorf("write plan.md: %w", err)
		}
		// h. Write stageDir/state.md.
		stateContent := fmt.Sprintf("# State\n\nStarting stage %d of %d.\n", i+1, len(opts.Chain))
		if err := os.WriteFile(filepath.Join(stageDir, "state.md"), []byte(stateContent), 0o644); err != nil {
			return nil, fmt.Errorf("write state.md: %w", err)
		}
		// i. Write stageDir/notes.md.
		if err := os.WriteFile(filepath.Join(stageDir, "notes.md"), []byte(""), 0o644); err != nil {
			return nil, fmt.Errorf("write notes.md: %w", err)
		}

		// j. Update chain.yml CurrentStage = i.
		chainYML.CurrentStage = i
		if err := writeChainYML(runDir.Path, chainYML); err != nil {
			return nil, fmt.Errorf("update chain.yml: %w", err)
		}

		// k. Create stageRunDir.
		stageRunDir := &state.RunDir{
			ID:        runDir.ID,
			Path:      stageDir,
			StartedAt: opts.Clock(),
		}

		// l. Write RUNNING marker.
		if err := os.WriteFile(filepath.Join(stageDir, "RUNNING"), []byte(opts.Clock().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("write RUNNING marker: %w", err)
		}

		// m. Run stage.
		if oneshot.IsOneShotPath(mode) {
			_, err = oneshot.Run(ctx, oneshot.Options{
				Task:    stageTask,
				Path:    mode,
				RunDir:  stageRunDir,
				Backend: opts.Backend,
				Output:  opts.Output,
			})
		} else {
			_, err = loopengine.Run(ctx, loopengine.Options{
				RunDir:        stageRunDir,
				Backend:       opts.Backend,
				GitHelper:     opts.GitHelper,
				StateManager:  sm,
				MaxIterations: 100,
				Path:          string(mode),
				Output:        opts.Output,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("stage %d (%s): %w", i+1, string(mode), err)
		}

		// n. Write DONE marker.
		if err := os.WriteFile(filepath.Join(stageDir, "DONE"), []byte(opts.Clock().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("write DONE marker: %w", err)
		}

		// o. Print summary.
		fmt.Fprintf(opts.Output, "Stage %d/%d complete: %s\n", i+1, len(opts.Chain), strings.Title(string(mode)))

		// p. Inter-stage confirmation gate (not after the last stage).
		if i < len(opts.Chain)-1 {
			deliverableSummary := buildDeliverableSummary(stageDir, mode)
			fmt.Fprintf(opts.Output, "\n%s\n", deliverableSummary)
			fmt.Fprint(opts.Output, "[y] start next stage  [e] edit (continues)  [n] stop here\n> ")

			if opts.ForceYes {
				fmt.Fprintln(opts.Output, "Auto-confirming (--yes).")
			} else {
				key, err := readKey(opts)
				if err != nil {
					return nil, fmt.Errorf("read gate key: %w", err)
				}
				if key == 'n' || key == 'N' {
					return &Result{RunID: runDir.ID, StagesRun: i + 1, TerminatedAt: i}, nil
				}
				// 'e' is treated as 'y' for now.
			}
		}
	}

	// 6. Return complete result.
	return &Result{RunID: runDir.ID, StagesRun: len(opts.Chain), TerminatedAt: -1}, nil
}

// writeChainYML persists ChainYML as JSON at runDirPath/chain.yml.
func writeChainYML(runDirPath string, c ChainYML) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDirPath, "chain.yml"), data, 0o644)
}

// generateRunID creates a unique run ID from the current time, chainKey and task.
func generateRunID(t time.Time, chainKey, task string) string {
	slug := taskSlug(task)
	safeKey := strings.ReplaceAll(chainKey, ":", "-")
	return fmt.Sprintf("%s-chain-%s-%s", t.UTC().Format("2006-01-02-150405"), safeKey, slug)
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

// buildDeliverableSummary builds a brief summary of deliverables from a completed stage.
func buildDeliverableSummary(stageDir string, mode router.Path) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Stage deliverables (%s):\n", string(mode)))

	// Try to read the artifact for a brief summary.
	artifactFiles := []string{"report.md", "docs.md", "research-report.md", "explanation.md"}
	for _, af := range artifactFiles {
		data, err := os.ReadFile(filepath.Join(stageDir, af))
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			count := 5
			if len(lines) < count {
				count = len(lines)
			}
			sb.WriteString("  " + strings.Join(lines[:count], "\n  ") + "\n")
			if len(lines) > 5 {
				sb.WriteString(fmt.Sprintf("  ... (%d more lines)\n", len(lines)-5))
			}
			return sb.String()
		}
	}

	sb.WriteString("  (stage complete — see stage directory for full output)\n")
	return sb.String()
}

// readKey reads a single keystroke from the term reader or stdin.
func readKey(opts Options) (byte, error) {
	if opts.TermReader != nil {
		return opts.TermReader.ReadKey()
	}
	return stdinReadKey()
}
