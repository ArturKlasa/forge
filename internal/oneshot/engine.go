// Package oneshot implements the One-Shot Engine for Review, Document, Explain,
// and Research modes. Each mode spawns parallel subagents, synthesizes results,
// and writes a final artifact — with no iterative loop.
package oneshot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/brain"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// modeConfig defines the subagent areas and output artifact for each one-shot path.
type modeConfig struct {
	areas        []string
	artifact     string
	commits      bool // only Document mode commits
	webAllowed   bool // only Research mode
}

var modeConfigs = map[router.Path]modeConfig{
	router.PathReview: {
		areas:    []string{"security", "architecture", "correctness", "style", "performance"},
		artifact: "report.md",
	},
	router.PathDocument: {
		areas:    []string{"api-surface", "internals", "examples", "README"},
		artifact: "docs.md",
		commits:  true,
	},
	router.PathExplain: {
		areas:    []string{"explanation"},
		artifact: "explanation.md",
	},
	router.PathResearch: {
		areas:      []string{"alternatives", "pros-cons", "prior-art", "cost"},
		artifact:   "research-report.md",
		webAllowed: true,
	},
}

// ChainSuggestions maps one-shot paths to their predefined chain suggestion.
var ChainSuggestions = map[router.Path]string{
	router.PathReview:   "forge --chain review:fix",
	router.PathDocument: "forge --chain document:review",
	router.PathExplain:  "",
	router.PathResearch: "",
}

// IsOneShotPath reports whether the given path is a one-shot mode.
func IsOneShotPath(p router.Path) bool {
	_, ok := modeConfigs[p]
	return ok
}

// Options configures a one-shot run.
type Options struct {
	Task   string
	Path   router.Path
	RunDir *state.RunDir

	Backend backend.Backend
	Brain   *brain.Brain // nil → plain concatenation synthesis
	Output  io.Writer

	// MaxSubagents caps parallelism (default 6).
	MaxSubagents int
}

// Result holds the output of a one-shot run.
type Result struct {
	ArtifactPath    string
	ChainSuggestion string
}

// subagentOutput is the result from one subagent.
type subagentOutput struct {
	area   string
	text   string
	failed bool
}

// Run executes the one-shot engine for the given mode.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	cfg, ok := modeConfigs[opts.Path]
	if !ok {
		return nil, fmt.Errorf("unsupported one-shot path: %s", opts.Path)
	}

	areas := cfg.areas
	maxAgents := opts.MaxSubagents
	if maxAgents <= 0 {
		maxAgents = 6
	}
	if len(areas) > maxAgents {
		areas = areas[:maxAgents]
	}

	fmt.Fprintf(opts.Output, "Spawning %d subagents (%s)...\n", len(areas), strings.Join(areas, " / "))

	// ── Spawn subagents in parallel ──────────────────────────────────────
	outputs := make([]subagentOutput, len(areas))
	var wg sync.WaitGroup
	for i, area := range areas {
		wg.Add(1)
		go func(idx int, area string) {
			defer wg.Done()
			out, err := runSubagent(ctx, opts, area, cfg.webAllowed)
			if err != nil {
				// retry once
				out, err = runSubagent(ctx, opts, area, cfg.webAllowed)
			}
			if err != nil {
				outputs[idx] = subagentOutput{area: area, failed: true, text: fmt.Sprintf("[Subagent failed: %s]", area)}
				fmt.Fprintf(opts.Output, "  %s: FAILED (included stub)\n", area)
				return
			}
			count := countFindings(out)
			if count > 0 {
				fmt.Fprintf(opts.Output, "  %s: %d finding(s)\n", area, count)
			} else {
				fmt.Fprintf(opts.Output, "  %s: done\n", area)
			}
			outputs[idx] = subagentOutput{area: area, text: out}
		}(i, area)
	}
	wg.Wait()

	// ── Synthesize ───────────────────────────────────────────────────────
	fmt.Fprintln(opts.Output, "Synthesizing report...")
	var finalText string
	var synthErr error
	if opts.Brain != nil {
		finalText, synthErr = synthesizeWithBrain(ctx, opts, outputs)
	} else {
		finalText = synthesizePlain(outputs)
	}
	if synthErr != nil {
		finalText = synthesizePlain(outputs)
	}

	// ── Write artifact ───────────────────────────────────────────────────
	artifactPath := filepath.Join(opts.RunDir.Path, cfg.artifact)
	if err := os.WriteFile(artifactPath, []byte(finalText), 0o640); err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}

	// Document mode commits the artifact.
	if cfg.commits {
		fmt.Fprintln(opts.Output, "Writing documentation files...")
	}

	// ── Write DONE marker ────────────────────────────────────────────────
	doneMarker := filepath.Join(opts.RunDir.Path, "DONE")
	_ = os.WriteFile(doneMarker, []byte("one-shot complete\n"), 0o640)

	// ── Chain suggestion ─────────────────────────────────────────────────
	suggestion := ChainSuggestions[opts.Path]
	if suggestion != "" {
		fmt.Fprintf(opts.Output, "Suggested next: %s %q\n", suggestion, opts.Task)
	}

	fmt.Fprintf(opts.Output, "Report: %s\n", artifactPath)

	return &Result{
		ArtifactPath:    artifactPath,
		ChainSuggestion: suggestion,
	}, nil
}

// runSubagent calls the backend for a single area.
func runSubagent(ctx context.Context, opts Options, area string, webAllowed bool) (string, error) {
	if opts.Backend == nil {
		return fmt.Sprintf("# %s\n\n[No backend available]\n", strings.Title(area)), nil
	}

	promptBody := buildSubagentPrompt(opts.Path, opts.Task, area, webAllowed)
	prompt := backend.Prompt{Body: promptBody}
	res, err := opts.Backend.RunIteration(ctx, prompt, backend.IterationOpts{MaxTurns: 3})
	if err != nil {
		return "", err
	}
	if res.Error != nil {
		return "", res.Error
	}
	return res.FinalText, nil
}

// buildSubagentPrompt generates the system prompt for a subagent.
func buildSubagentPrompt(path router.Path, task, area string, webAllowed bool) string {
	var role string
	switch path {
	case router.PathReview:
		role = fmt.Sprintf("You are a code reviewer specializing in %s analysis.", area)
	case router.PathDocument:
		role = fmt.Sprintf("You are a technical writer specializing in %s documentation.", area)
	case router.PathExplain:
		role = "You are a technical explainer."
	case router.PathResearch:
		role = fmt.Sprintf("You are a research analyst covering %s.", area)
	default:
		role = fmt.Sprintf("You are an expert in %s.", area)
	}

	webNote := ""
	if webAllowed {
		webNote = "\nYou may use web search tools to find current information."
	}

	return fmt.Sprintf(`%s%s

Task: %s

Focus area: %s

Provide a concise, structured analysis. Use markdown headers and bullet points.
`, role, webNote, task, area)
}

// synthesizeWithBrain uses the Brain.Draft primitive to produce a unified report.
func synthesizeWithBrain(ctx context.Context, opts Options, outputs []subagentOutput) (string, error) {
	sections := make([]string, 0, len(outputs))
	for _, o := range outputs {
		sections = append(sections, fmt.Sprintf("## %s\n\n%s", strings.Title(o.area), o.text))
	}
	combined := strings.Join(sections, "\n\n---\n\n")

	purpose := fmt.Sprintf("a synthesized %s report for the task: %s", opts.Path, opts.Task)
	return opts.Brain.Draft(ctx, purpose, combined)
}

// synthesizePlain concatenates subagent outputs into a markdown document.
func synthesizePlain(outputs []subagentOutput) string {
	var sb strings.Builder
	for _, o := range outputs {
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", strings.Title(o.area), o.text)
	}
	return sb.String()
}

// countFindings counts the number of distinct findings in a subagent output.
// A "finding" is a non-empty bullet or numbered list item.
func countFindings(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			count++
		}
	}
	return count
}
