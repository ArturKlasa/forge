package planphase

import (
	"context"
	"fmt"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// researchOutput holds the synthesized findings from research subagents.
type researchOutput struct {
	DomainSummary   string
	SimilarPatterns string
	PlanItems       []string
}

// runResearch executes 1–2 research subagents appropriate for the given path.
// For step 11 only the Create path is implemented.
func runResearch(ctx context.Context, opts Options, path router.Path, rd *state.RunDir) (*researchOutput, error) {
	fmt.Fprint(opts.Output, "Researching codebase... ")

	b := opts.Backend
	if b == nil {
		// No backend — use a minimal stub so tests without a backend still pass.
		fmt.Fprintln(opts.Output, "done (no backend)")
		return stubResearch(opts.Task), nil
	}

	switch path {
	case router.PathCreate:
		return researchCreate(ctx, opts, b)
	default:
		// Other paths will be implemented in later steps.
		fmt.Fprintln(opts.Output, "done (stub)")
		return stubResearch(opts.Task), nil
	}
}

// researchCreate runs the two Create-path researchers (domain + similar-solutions).
func researchCreate(ctx context.Context, opts Options, b backend.Backend) (*researchOutput, error) {
	task := opts.Task

	// Researcher 1: domain + scope.
	domainPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a domain researcher for a software task.
Task: %s

Briefly describe (3-5 sentences):
1. What domain/technology would this require?
2. What high-level approach would you take?

Respond in plain text.`, task),
	}
	domainRes, err := b.RunIteration(ctx, domainPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("domain research: %w", err)
	}

	// Researcher 2: similar patterns in codebase.
	similarPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a codebase pattern researcher.
Task: %s

Based on common Go project patterns, suggest a minimal step-by-step plan to implement this task.
Format: numbered list, one step per line, e.g.:
1. First step
2. Second step

Keep it to 3–6 steps. Respond only with the numbered list.`, task),
	}
	similarRes, err := b.RunIteration(ctx, similarPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("similar-solutions research: %w", err)
	}

	fmt.Fprintln(opts.Output, "done")
	fmt.Fprint(opts.Output, "Drafting plan... done\n")

	out := &researchOutput{
		DomainSummary:   domainRes.FinalText,
		SimilarPatterns: similarRes.FinalText,
		PlanItems:       extractPlanItems(similarRes.FinalText),
	}

	// Fallback: if no plan items extracted, use stub.
	if len(out.PlanItems) == 0 {
		out.PlanItems = stubResearch(task).PlanItems
	}

	return out, nil
}

// stubResearch returns a minimal canned research output when no backend is available.
func stubResearch(task string) *researchOutput {
	return &researchOutput{
		DomainSummary:   "Research not available (no backend configured).",
		SimilarPatterns: "",
		PlanItems: []string{
			"Analyze requirements for: " + task,
			"Design the solution",
			"Implement and test",
		},
	}
}

// extractPlanItems parses numbered list items from text.
func extractPlanItems(text string) []string {
	return parsePlanLines(text)
}
