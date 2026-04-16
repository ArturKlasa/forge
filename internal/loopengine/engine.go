package loopengine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/backend"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/state"
)

// Options configure a single Loop Engine run.
type Options struct {
	// RunDir is the active run directory (already created by plan phase).
	RunDir *state.RunDir

	// Backend runs each iteration.
	Backend backend.Backend

	// GitHelper commits per-iteration diffs.
	GitHelper *forgegit.Git

	// StateManager transitions lifecycle markers.
	StateManager *state.Manager

	// MaxIterations caps the loop (0 = no cap).
	MaxIterations int

	// MaxDuration caps wall-clock time (0 = no cap).
	MaxDuration time.Duration

	// Path is the detected mode (e.g. "create", "fix"). Used in commit messages.
	Path string

	// Output is where terminal summary lines are written.
	Output io.Writer

	// Clock overrides time.Now() for testing.
	Clock func() time.Time
}

// Result summarises the completed loop.
type Result struct {
	Iterations int
	Commits    int
	Complete   bool
	CapReached bool
}

// Run executes the minimal Ralph loop until TASK_COMPLETE is signalled, the
// iteration or duration cap is reached, or ctx is cancelled.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.MaxIterations == 0 {
		opts.MaxIterations = 100 // safety cap
	}

	runDir := opts.RunDir.Path
	runID := opts.RunDir.ID
	path := opts.Path
	if path == "" {
		path = "create"
	}

	var deadlineCtx context.Context
	var cancel context.CancelFunc
	if opts.MaxDuration > 0 {
		deadlineCtx, cancel = context.WithTimeout(ctx, opts.MaxDuration)
		defer cancel()
	} else {
		deadlineCtx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	result := &Result{}

	for i := 1; i <= opts.MaxIterations; i++ {
		if err := deadlineCtx.Err(); err != nil {
			break
		}

		iterStart := opts.Clock()

		// Assemble prompt.
		promptBody, err := assemblePrompt(runDir)
		if err != nil {
			return nil, fmt.Errorf("iter %d assemble prompt: %w", i, err)
		}

		// Write prompt.md for this iteration.
		promptPath := filepath.Join(runDir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte(promptBody), 0o644); err != nil {
			return nil, fmt.Errorf("iter %d write prompt: %w", i, err)
		}

		// Invoke backend.
		iterResult, err := opts.Backend.RunIteration(deadlineCtx, backend.Prompt{
			Path: promptPath,
		}, backend.IterationOpts{})
		iterFinish := opts.Clock()
		dur := iterFinish.Sub(iterStart).Seconds()

		exitCode := 0
		exitSubtype := ""
		if err != nil {
			exitCode = 1
			exitSubtype = err.Error()
		} else if iterResult.ExitCode != 0 {
			exitCode = iterResult.ExitCode
		}
		if iterResult.Truncated {
			exitSubtype = "truncated"
		}

		// Detect TASK_COMPLETE.
		complete := iterResult.CompletionSentinel ||
			strings.Contains(iterResult.FinalText, "TASK_COMPLETE")

		// Commit diff if non-empty.
		commitSHA := ""
		changedFiles := []string{}
		if opts.GitHelper != nil {
			dirty, dirtyErr := opts.GitHelper.IsDirty(deadlineCtx)
			if dirtyErr == nil && dirty {
				// Capture diff before staging for file-list extraction.
				diff, _ := opts.GitHelper.DiffSinceLastCommit(deadlineCtx)
				msg := fmt.Sprintf("forge(%s): iter %d", path, i)
				if commitErr := opts.GitHelper.CommitAll(deadlineCtx, msg); commitErr == nil {
					result.Commits++
					sha, _, shaErr := opts.GitHelper.HEAD(deadlineCtx)
					if shaErr == nil {
						commitSHA = sha
					}
					changedFiles = diffPaths(string(diff))
				}
			}
		}

		// Append ledger entry.
		entry := LedgerEntry{
			RunID:        runID,
			Iteration:    i,
			StartedAt:    iterStart,
			FinishedAt:   iterFinish,
			DurationSec:  dur,
			Exit:         exitInfo{Code: exitCode, Subtype: exitSubtype},
			FilesChanged: changedFiles,
			CommitSHA:    commitSHA,
			PromptTokens: iterResult.TokensUsage.Input,
			OutputTokens: iterResult.TokensUsage.Output,
			Complete:     complete,
		}
		if appendErr := appendLedger(runDir, entry); appendErr != nil {
			return nil, fmt.Errorf("iter %d append ledger: %w", i, appendErr)
		}

		result.Iterations++

		// Print terminal summary line.
		status := ""
		if complete {
			status = " · TASK_COMPLETE"
		}
		fmt.Fprintf(opts.Output, "[%s] iter %d · %s · files=%d · duration=%.0fs%s\n",
			iterFinish.Format("15:04:05"), i, path, len(changedFiles), dur, status)

		if complete {
			result.Complete = true
			break
		}
	}

	// Cap reached without TASK_COMPLETE.
	if !result.Complete && result.Iterations >= opts.MaxIterations {
		result.CapReached = true
		fmt.Fprintf(opts.Output, "ESCALATION — iteration cap reached (%d). Run 'forge resume' to continue.\n", opts.MaxIterations)
	}

	// Transition state marker.
	if opts.StateManager != nil {
		mk := state.MarkerDone
		if !result.Complete && result.CapReached {
			mk = state.MarkerAwaitingHuman
		}
		_ = opts.StateManager.Transition(opts.RunDir, mk)
	}

	if result.Complete {
		fmt.Fprintf(opts.Output, "DONE — %d iterations, %d commits\n", result.Iterations, result.Commits)
	}

	return result, nil
}

// diffPaths extracts unique file paths from a unified diff.
func diffPaths(diff string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			p := strings.TrimPrefix(line, "+++ b/")
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}
