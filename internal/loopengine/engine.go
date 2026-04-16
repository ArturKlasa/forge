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
	"github.com/arturklasa/forge/internal/escalate"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/policy"
	"github.com/arturklasa/forge/internal/state"
	"github.com/arturklasa/forge/internal/stuckdet"
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

	// PolicyScanner runs after each backend iteration before commit.
	// If nil, policy scanning is skipped.
	PolicyScanner *policy.Scanner

	// Clock overrides time.Now() for testing.
	Clock func() time.Time

	// EscalationManager handles human escalations (policy gates, stuck-dead, etc.).
	// If nil, hard stops break the loop without waiting for a human response.
	EscalationManager *escalate.Manager
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

		// Snapshot state.md before iteration for delta detection.
		stateMDPath := filepath.Join(runDir, "state.md")
		stateBefore, _ := os.ReadFile(stateMDPath)

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

		// Parse stuck-detector signal overrides from FinalText.
		buildStatus, errorFP, selfReport, regressions := parseFinalTextSignals(iterResult.FinalText)

		// Detect state.md semantic delta.
		stateAfter, _ := os.ReadFile(stateMDPath)
		stateDeltaChanged := string(stateBefore) != string(stateAfter)

		// Stage all changes, scan, then commit (or unstage on hard stop).
		policyHardStop := false
		commitSHA := ""
		changedFiles := []string{}
		newHighConfPlaceholders := 0

		if opts.GitHelper != nil {
			dirty, dirtyErr := opts.GitHelper.IsDirty(deadlineCtx)
			if dirtyErr == nil && dirty {
				// Stage everything so we can get a complete diff (incl. new files).
				_ = opts.GitHelper.StageAll(deadlineCtx)
				diff, _ := opts.GitHelper.DiffCached(deadlineCtx)

				if opts.PolicyScanner != nil && len(diff) > 0 {
					scan := opts.PolicyScanner.ScanIteration(diff, false)
					newHighConfPlaceholders = scan.HighConfidencePlaceholderCount()
					if err := policy.AppendPlaceholderLedger(runDir, scan.PlaceholderHits, i, "active"); err != nil {
						return nil, fmt.Errorf("iter %d append placeholder ledger: %w", i, err)
					}
					if scan.HasHardStop() {
						reason := scan.HardStopReason()
						fmt.Fprintf(opts.Output, "[%s] iter %d · ESCALATION — %s\n",
							opts.Clock().Format("15:04:05"), i, reason)

						if opts.EscalationManager != nil {
							esc := escalate.GateScannerEscalation(
								opts.EscalationManager.RunDir,
								i, path, reason, opts.Clock,
							)
							ans, escErr := opts.EscalationManager.Escalate(deadlineCtx, esc)
							if escErr != nil || ans.OptionKey == "d" || ans.OptionKey == "p" || ans.OptionKey == "abort-auto" {
								policyHardStop = true
								_ = opts.GitHelper.UnstageAll(deadlineCtx)
							} else if ans.OptionKey == "s" {
								// Revert: unstage and continue loop.
								_ = opts.GitHelper.UnstageAll(deadlineCtx)
								// policyHardStop stays false → loop continues
							} else if ans.OptionKey == "a" {
								// Apply: commit staged changes.
								msg := fmt.Sprintf("forge(%s): iter %d (gate-approved)", path, i)
								if commitErr := opts.GitHelper.CommitStaged(deadlineCtx, msg); commitErr == nil {
									result.Commits++
									sha, _, shaErr := opts.GitHelper.HEAD(deadlineCtx)
									if shaErr == nil {
										commitSHA = sha
									}
									changedFiles = diffPaths(string(diff))
								}
							}
						} else {
							policyHardStop = true
							_ = opts.GitHelper.UnstageAll(deadlineCtx)
						}
					}
				}

				if !policyHardStop {
					msg := fmt.Sprintf("forge(%s): iter %d", path, i)
					if commitErr := opts.GitHelper.CommitStaged(deadlineCtx, msg); commitErr == nil {
						result.Commits++
						sha, _, shaErr := opts.GitHelper.HEAD(deadlineCtx)
						if shaErr == nil {
							commitSHA = sha
						}
						changedFiles = diffPaths(string(diff))
					}
				}
			}
		}

		// Build and append ledger entry (with stuck+completion fields).
		entry := LedgerEntry{
			RunID:                         runID,
			Iteration:                     i,
			StartedAt:                     iterStart,
			FinishedAt:                    iterFinish,
			DurationSec:                   dur,
			Exit:                          exitInfo{Code: exitCode, Subtype: exitSubtype},
			FilesChanged:                  changedFiles,
			CommitSHA:                     commitSHA,
			PromptTokens:                  iterResult.TokensUsage.Input,
			OutputTokens:                  iterResult.TokensUsage.Output,
			Complete:                      complete,
			BuildStatus:                   buildStatus,
			ErrorFingerprint:              errorFP,
			AgentSelfReport:               selfReport,
			Regressions:                   regressions,
			NewHighConfidencePlaceholders: newHighConfPlaceholders,
			StateSemanticDelta:            SemanticDelta{Changed: stateDeltaChanged},
		}

		// Run stuck detector on the full ledger window (including current entry).
		allEntries, _ := readLedger(runDir)
		allEntries = append(allEntries, entry) // include current (not yet persisted)
		stuckWindow := make([]stuckdet.Entry, len(allEntries))
		for idx, e := range allEntries {
			stuckWindow[idx] = toStuckEntry(e)
		}
		stuckResult := stuckdet.Evaluate(stuckWindow)
		entry.StuckTier = int(stuckResult.Tier)
		entry.StuckHardTriggers = stuckResult.HardTriggers
		entry.StuckSoftSum = stuckResult.SoftSum

		// Completion detector.
		compSignals := buildCompletionSignals(entry, false)
		compResult := buildCompletion(compSignals)
		entry.CompletionScore = compResult.Score
		// Override TASK_COMPLETE with completion detector threshold.
		if !complete && compResult.ShouldComplete {
			complete = true
			entry.Complete = true
		}

		if appendErr := appendLedger(runDir, entry); appendErr != nil {
			return nil, fmt.Errorf("iter %d append ledger: %w", i, appendErr)
		}

		result.Iterations++

		if policyHardStop {
			result.CapReached = true
			break
		}

		// Handle stuck tier actions post-commit.
		if !complete && stuckResult.Tier > stuckdet.TierProgressing {
			stuckHandled, shouldBreak, err := handleStuckTier(deadlineCtx, opts, runDir, i, path, stuckResult)
			if err != nil {
				return nil, fmt.Errorf("iter %d stuck handler: %w", i, err)
			}
			if stuckHandled {
				fmt.Fprintf(opts.Output, "[%s] iter %d · stuck=%s · signals=%v\n",
					iterFinish.Format("15:04:05"), i, stuckTierLabel(stuckResult.Tier), stuckResult.FiringSignals)
			}
			if shouldBreak {
				result.CapReached = true
				break
			}
		}

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

// handleStuckTier performs the tier-specific action. Returns (acted, shouldBreak, err).
func handleStuckTier(
	ctx context.Context,
	opts Options,
	runDir string,
	iteration int,
	path string,
	result stuckdet.Result,
) (bool, bool, error) {
	switch result.Tier {
	case stuckdet.TierSoftStuck:
		// Tier 1: Brain.Diagnose (stub until step 17) — append finding to state.md.
		finding := fmt.Sprintf("\n\n## Stuck Detector — Iter %d (Tier 1: soft-stuck)\n\nSignals: %v\nSoft sum: %d\n",
			iteration, result.FiringSignals, result.SoftSum)
		stateMD := filepath.Join(runDir, "state.md")
		f, err := os.OpenFile(stateMD, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err == nil {
			_, _ = f.WriteString(finding)
			f.Close()
		}
		return true, false, nil

	case stuckdet.TierHardStuck:
		// Tier 2: Brain.Draft (stub until step 17) — append regeneration notice to plan.md.
		notice := fmt.Sprintf("\n\n## Stuck Detector — Iter %d (Tier 2: hard-stuck)\n\nSignals: %v\nPlan regeneration triggered (Brain.Draft stub — real regeneration in step 17).\n",
			iteration, result.FiringSignals)
		planMD := filepath.Join(runDir, "plan.md")
		f, err := os.OpenFile(planMD, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err == nil {
			_, _ = f.WriteString(notice)
			f.Close()
		}
		return true, false, nil

	case stuckdet.TierDeadStuck:
		// Tier 3: escalate to human.
		reason := stuckEscalationReason(result)
		fmt.Fprintf(opts.Output, "[STUCK] iter %d · ESCALATION — %s\n", iteration, reason)

		if opts.EscalationManager != nil {
			esc := buildStuckEscalation(opts.EscalationManager.RunDir, iteration, path, reason, result, opts.Clock)
			ans, escErr := opts.EscalationManager.Escalate(ctx, esc)
			if escErr != nil || ans.OptionKey == "abort-auto" || ans.OptionKey == "d" {
				return true, true, nil
			}
			// Any other answer (p=pivot, s=split, r=reset, a=apply) → break loop.
			return true, true, nil
		}
		return true, true, nil

	default:
		return false, false, nil
	}
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
