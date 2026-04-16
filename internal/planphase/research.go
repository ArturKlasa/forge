package planphase

import (
	"context"
	"fmt"
	"strings"

	"github.com/arturklasa/forge/internal/backend"
	"github.com/arturklasa/forge/internal/router"
	"github.com/arturklasa/forge/internal/state"
)

// researchOutput holds the synthesized findings from research subagents.
type researchOutput struct {
	DomainSummary   string
	SimilarPatterns string
	PlanItems       []string
	Invariants      []string // Refactor path only
}

// runResearch executes 1–3 research subagents appropriate for the given path.
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
	case router.PathAdd:
		return researchAdd(ctx, opts, b)
	case router.PathFix:
		return researchFix(ctx, opts, b)
	case router.PathRefactor:
		return researchRefactor(ctx, opts, b)
	default:
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

// researchAdd runs 2–3 Add-path researchers: codebase-map + specs.
func researchAdd(ctx context.Context, opts Options, b backend.Backend) (*researchOutput, error) {
	task := opts.Task

	// Researcher 1: codebase integration points.
	codebasePrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a codebase integration researcher.
Task: %s

Describe in 3–5 sentences:
1. Where in the codebase should this feature be integrated?
2. What existing patterns or modules are relevant?

Respond in plain text.`, task),
	}
	codebaseRes, err := b.RunIteration(ctx, codebasePrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("codebase-map research: %w", err)
	}

	// Researcher 2: feature specs + implementation plan.
	specsPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a feature specification writer.
Task: %s

Write a minimal numbered implementation plan (3–6 steps) for adding this feature safely.
Format: numbered list only.
1. First step
2. Second step`, task),
	}
	specsRes, err := b.RunIteration(ctx, specsPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("specs research: %w", err)
	}

	fmt.Fprintln(opts.Output, "done")
	fmt.Fprint(opts.Output, "Drafting plan... done\n")

	out := &researchOutput{
		DomainSummary:   codebaseRes.FinalText,
		SimilarPatterns: specsRes.FinalText,
		PlanItems:       extractPlanItems(specsRes.FinalText),
	}
	if len(out.PlanItems) == 0 {
		out.PlanItems = stubResearch(task).PlanItems
	}
	return out, nil
}

// researchFix runs 2–3 Fix-path researchers: reproduce + root-cause + adjacent-risk.
func researchFix(ctx context.Context, opts Options, b backend.Backend) (*researchOutput, error) {
	task := opts.Task

	// Researcher 1: reproduce the bug.
	reproPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a bug reproduction researcher.
Bug report: %s

In 3–5 sentences describe:
1. How to reproduce this bug locally.
2. What the expected vs actual behavior is.

Respond in plain text.`, task),
	}
	reproRes, err := b.RunIteration(ctx, reproPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("reproduce research: %w", err)
	}

	// Researcher 2: root-cause candidates + fix plan.
	rootCausePrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a root-cause analysis researcher.
Bug: %s

Provide a numbered fix plan (3–6 steps) to:
1. Write a regression test that captures the bug
2. Fix the root cause
3. Verify the fix

Format: numbered list only.
1. First step
2. Second step`, task),
	}
	planRes, err := b.RunIteration(ctx, rootCausePrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("root-cause research: %w", err)
	}

	fmt.Fprintln(opts.Output, "done")
	fmt.Fprint(opts.Output, "Drafting plan... done\n")

	out := &researchOutput{
		DomainSummary:   reproRes.FinalText,
		SimilarPatterns: planRes.FinalText,
		PlanItems:       extractPlanItems(planRes.FinalText),
	}
	if len(out.PlanItems) == 0 {
		out.PlanItems = stubResearch(task).PlanItems
	}
	return out, nil
}

// researchRefactor runs 2–3 Refactor-path researchers: current-shape + invariants + affected-tests.
func researchRefactor(ctx context.Context, opts Options, b backend.Backend) (*researchOutput, error) {
	task := opts.Task

	// Researcher 1: current shape + behavioral invariants.
	invariantsPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a behavioral invariant researcher.
Refactor task: %s

List the key behavioral invariants that must be preserved after this refactor.
Format: bullet list using "- " prefix, one invariant per line.
- Invariant one
- Invariant two`, task),
	}
	invariantsRes, err := b.RunIteration(ctx, invariantsPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("invariants research: %w", err)
	}

	// Researcher 2: refactor plan.
	planPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a refactoring planner.
Refactor task: %s

Write a minimal numbered refactor plan (3–6 steps).
Format: numbered list only.
1. First step
2. Second step`, task),
	}
	planRes, err := b.RunIteration(ctx, planPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("refactor plan research: %w", err)
	}

	fmt.Fprintln(opts.Output, "done")
	fmt.Fprint(opts.Output, "Drafting plan... done\n")

	out := &researchOutput{
		DomainSummary:   invariantsRes.FinalText,
		SimilarPatterns: planRes.FinalText,
		PlanItems:       extractPlanItems(planRes.FinalText),
		Invariants:      extractBulletItems(invariantsRes.FinalText),
	}
	if len(out.PlanItems) == 0 {
		out.PlanItems = stubResearch(task).PlanItems
	}
	if len(out.Invariants) == 0 {
		out.Invariants = []string{"All existing public APIs remain unchanged", "All existing tests continue to pass"}
	}
	return out, nil
}

// extractBulletItems parses "- item" lines from text.
func extractBulletItems(text string) []string {
	var items []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			items = append(items, strings.TrimPrefix(line, "- "))
		}
	}
	return items
}
