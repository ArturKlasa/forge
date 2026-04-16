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

	// Upgrade path fields.
	UpgradeSourceVersion string
	UpgradeTargetVersion string
	UpgradeBreakingCount int
	UpgradeManifests     []string

	// Test path fields.
	TestFramework       string
	TestCoverageTarget  int    // target coverage percentage
	TestCurrentCoverage int    // current coverage percentage
	TestScope           string // e.g. "src/checkout/**/*.ts"
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
	case router.PathUpgrade:
		return researchUpgrade(ctx, opts, b)
	case router.PathTest:
		return researchTest(ctx, opts, b)
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

// researchUpgrade runs 2–4 Upgrade-path researchers: release notes + migration guides + affected imports.
func researchUpgrade(ctx context.Context, opts Options, b backend.Backend) (*researchOutput, error) {
	task := opts.Task

	// Researcher 1: identify source/target versions and dep manifests.
	scopePrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are an upgrade scope researcher.
Upgrade task: %s

Respond with exactly these fields (one per line):
source_version=<detected or unknown>
target_version=<detected or unknown>
breaking_changes=<estimated count or 0>
manifests=<comma-separated list of dep manifest files expected to change, e.g. package.json,package-lock.json>

Only output the four key=value lines.`, task),
	}
	scopeRes, err := b.RunIteration(ctx, scopePrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("upgrade scope research: %w", err)
	}

	// Researcher 2: migration guide + implementation plan.
	planPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are an upgrade migration planner.
Upgrade task: %s

Write a numbered migration plan (3–6 steps) covering:
1. Update dependency version
2. Apply breaking changes
3. Run tests and fix regressions

Format: numbered list only.
1. First step
2. Second step`, task),
	}
	planRes, err := b.RunIteration(ctx, planPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("upgrade migration plan: %w", err)
	}

	fmt.Fprintln(opts.Output, "done")
	fmt.Fprint(opts.Output, "Drafting plan... done\n")

	// Parse scope fields.
	sourceVer, targetVer, breakingCount, manifests := parseUpgradeScopeFields(scopeRes.FinalText)

	out := &researchOutput{
		DomainSummary:        scopeRes.FinalText,
		SimilarPatterns:      planRes.FinalText,
		PlanItems:            extractPlanItems(planRes.FinalText),
		UpgradeSourceVersion: sourceVer,
		UpgradeTargetVersion: targetVer,
		UpgradeBreakingCount: breakingCount,
		UpgradeManifests:     manifests,
	}
	if len(out.PlanItems) == 0 {
		out.PlanItems = stubResearch(task).PlanItems
	}
	if len(out.UpgradeManifests) == 0 {
		out.UpgradeManifests = []string{"package.json"}
	}
	return out, nil
}

// parseUpgradeScopeFields extracts key=value fields from the scope researcher response.
func parseUpgradeScopeFields(text string) (sourceVer, targetVer string, breakingCount int, manifests []string) {
	sourceVer = "unknown"
	targetVer = "unknown"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "source_version="):
			sourceVer = strings.TrimPrefix(line, "source_version=")
		case strings.HasPrefix(line, "target_version="):
			targetVer = strings.TrimPrefix(line, "target_version=")
		case strings.HasPrefix(line, "breaking_changes="):
			val := strings.TrimPrefix(line, "breaking_changes=")
			fmt.Sscanf(val, "%d", &breakingCount)
		case strings.HasPrefix(line, "manifests="):
			val := strings.TrimPrefix(line, "manifests=")
			for _, m := range strings.Split(val, ",") {
				m = strings.TrimSpace(m)
				if m != "" {
					manifests = append(manifests, m)
				}
			}
		}
	}
	return
}

// researchTest runs 2 Test-path researchers: framework detection + coverage gap analysis.
func researchTest(ctx context.Context, opts Options, b backend.Backend) (*researchOutput, error) {
	task := opts.Task

	// Researcher 1: detect test framework + current coverage + scope.
	frameworkPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a test framework detection researcher.
Task: %s

Respond with exactly these fields (one per line):
framework=<go|jest|vitest|pytest|unittest|cargo|rspec|minitest|unknown>
current_coverage=<integer 0-100 or 0 if unknown>
coverage_target=<integer 0-100; suggest 15 above current, max 95>
test_scope=<glob pattern for files under test, e.g. src/checkout/**/*.ts or ./internal/...>

Only output the four key=value lines.`, task),
	}
	frameworkRes, err := b.RunIteration(ctx, frameworkPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("framework detection research: %w", err)
	}

	// Researcher 2: implementation plan for the test task.
	planPrompt := backend.Prompt{
		Body: fmt.Sprintf(`You are a test coverage planner.
Task: %s

Write a numbered plan (3–6 steps) to add the requested test coverage.
Focus only on writing tests — do not modify production code unless absolutely unavoidable.
Format: numbered list only.
1. First step
2. Second step`, task),
	}
	planRes, err := b.RunIteration(ctx, planPrompt, backend.IterationOpts{MaxTurns: 1})
	if err != nil {
		return nil, fmt.Errorf("test plan research: %w", err)
	}

	fmt.Fprintln(opts.Output, "done")
	fmt.Fprint(opts.Output, "Drafting plan... done\n")

	framework, currentCov, targetCov, testScope := parseTestScopeFields(frameworkRes.FinalText)

	out := &researchOutput{
		DomainSummary:       frameworkRes.FinalText,
		SimilarPatterns:     planRes.FinalText,
		PlanItems:           extractPlanItems(planRes.FinalText),
		TestFramework:       framework,
		TestCurrentCoverage: currentCov,
		TestCoverageTarget:  targetCov,
		TestScope:           testScope,
	}
	if len(out.PlanItems) == 0 {
		out.PlanItems = stubResearch(task).PlanItems
	}
	if out.TestFramework == "" {
		out.TestFramework = "unknown"
	}
	if out.TestScope == "" {
		out.TestScope = "."
	}
	return out, nil
}

// parseTestScopeFields extracts key=value fields from the framework researcher response.
func parseTestScopeFields(text string) (framework string, currentCov, targetCov int, testScope string) {
	framework = "unknown"
	testScope = "."
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "framework="):
			framework = strings.TrimPrefix(line, "framework=")
		case strings.HasPrefix(line, "current_coverage="):
			fmt.Sscanf(strings.TrimPrefix(line, "current_coverage="), "%d", &currentCov)
		case strings.HasPrefix(line, "coverage_target="):
			fmt.Sscanf(strings.TrimPrefix(line, "coverage_target="), "%d", &targetCov)
		case strings.HasPrefix(line, "test_scope="):
			testScope = strings.TrimPrefix(line, "test_scope=")
		}
	}
	return
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
