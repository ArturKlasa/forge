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
	"github.com/arturklasa/forge/internal/brain"
	"github.com/arturklasa/forge/internal/compdet"
	"github.com/arturklasa/forge/internal/ctxmgr"
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

	// Brain provides LLM meta-call primitives (Diagnose, Draft, Judge, etc.).
	// If nil, stub behaviours are used (same as pre-step-17).
	Brain *brain.Brain

	// ContextBudgetTokens is the token budget for prompt assembly.
	// 0 means use the default (100k tokens).
	ContextBudgetTokens int
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

	// Build context manager (uses Brain if available, else fallback).
	ctxMgr := ctxmgr.New(runDir, opts.Brain)

	for i := 1; i <= opts.MaxIterations; i++ {
		if err := deadlineCtx.Err(); err != nil {
			break
		}

		iterStart := opts.Clock()

		// Assemble prompt via Context Manager (real distillation + token budget).
		promptBody, err := ctxMgr.AssemblePrompt(deadlineCtx, path, opts.ContextBudgetTokens)
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
					msg := brainCommitMessage(deadlineCtx, opts.Brain, path, i, string(diff))
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

		// Call Brain.Judge for completion assessment when available.
		judgeVerdict := judgeCompletion(deadlineCtx, opts.Brain, runDir, iterResult.FinalText)

		// Completion detector.
		compSignals := buildCompletionSignals(entry, false, judgeVerdict)
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
		// Tier 1: Brain.Diagnose — append diagnosis + suggestion to state.md.
		finding := fmt.Sprintf("\n\n## Stuck Detector — Iter %d (Tier 1: soft-stuck)\n\nSignals: %v\nSoft sum: %d\n",
			iteration, result.FiringSignals, result.SoftSum)
		if opts.Brain != nil {
			stateData, _ := os.ReadFile(filepath.Join(runDir, "state.md"))
			ledgerWindow := ledgerSummary(runDir, 5)
			diag, err := opts.Brain.Diagnose(ctx, ledgerWindow, string(stateData))
			if err == nil {
				finding += fmt.Sprintf("Diagnosis: %s\nSuggestion: %s\n", diag.Diagnosis, diag.Suggestion)
			}
		}
		stateMD := filepath.Join(runDir, "state.md")
		f, err := os.OpenFile(stateMD, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err == nil {
			_, _ = f.WriteString(finding)
			f.Close()
		}
		return true, false, nil

	case stuckdet.TierHardStuck:
		// Tier 2: Brain.Draft — regenerate plan.md or append notice.
		notice := fmt.Sprintf("\n\n## Stuck Detector — Iter %d (Tier 2: hard-stuck)\n\nSignals: %v\n",
			iteration, result.FiringSignals)
		if opts.Brain != nil {
			taskData, _ := os.ReadFile(filepath.Join(runDir, "task.md"))
			stateData, _ := os.ReadFile(filepath.Join(runDir, "state.md"))
			draftCtx := fmt.Sprintf("Task:\n%s\n\nCurrent state:\n%s\n\nPrevious signals: %v", taskData, stateData, result.FiringSignals)
			newPlan, err := opts.Brain.Draft(ctx, "a revised implementation plan as a numbered list", draftCtx)
			if err == nil && newPlan != "" {
				notice += "Revised plan:\n" + newPlan + "\n"
			}
		} else {
			notice += "Plan regeneration: no Brain configured.\n"
		}
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

// brainCommitMessage generates a commit message using Brain.Draft if available,
// falling back to the standard template.
func brainCommitMessage(ctx context.Context, br *brain.Brain, path string, iter int, diff string) string {
	fallback := fmt.Sprintf("forge(%s): iter %d", path, iter)
	if br == nil {
		return fallback
	}
	draftCtx := fmt.Sprintf("Mode: %s, Iteration: %d\nDiff summary (first 2000 chars):\n%s",
		path, iter, truncateStr(diff, 2000))
	msg, err := br.Draft(ctx, "a concise git commit message (one line, imperative mood, no period)", draftCtx)
	if err != nil || msg == "" {
		return fallback
	}
	// Ensure it has the forge prefix.
	if !strings.HasPrefix(msg, "forge(") {
		msg = fmt.Sprintf("forge(%s): %s", path, msg)
	}
	return msg
}

// judgeCompletion calls Brain.Judge to assess task completion.
// Returns JudgeUnknown when Brain is unavailable or on error.
func judgeCompletion(ctx context.Context, br *brain.Brain, runDir string, finalText string) compdet.JudgeVerdict {
	if br == nil {
		return compdet.JudgeUnknown
	}
	taskData, _ := os.ReadFile(filepath.Join(runDir, "task.md"))
	stateData, _ := os.ReadFile(filepath.Join(runDir, "state.md"))
	result, err := br.Judge(ctx, string(taskData), string(stateData), truncateStr(finalText, 3000))
	if err != nil {
		return compdet.JudgeUnknown
	}
	switch result.Verdict {
	case brain.VerdictComplete:
		if result.Confidence == "high" {
			return compdet.JudgeHigh
		}
		return compdet.JudgeMedium
	case brain.VerdictIncomplete:
		return compdet.JudgeIncomplete
	case brain.VerdictAudit:
		return compdet.JudgeMedium
	default:
		return compdet.JudgeUnknown
	}
}

// ledgerSummary returns a brief text summary of the last n ledger entries.
func ledgerSummary(runDir string, n int) string {
	entries, err := readLedger(runDir)
	if err != nil || len(entries) == 0 {
		return "(no ledger entries)"
	}
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("iter=%d files=%d build=%s stuck=%d completion=%d",
			e.Iteration, len(e.FilesChanged), e.BuildStatus, e.StuckTier, e.CompletionScore))
	}
	return strings.Join(lines, "\n")
}

// truncateStr truncates s to max chars.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
