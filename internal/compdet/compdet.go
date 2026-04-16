// Package compdet implements the multi-signal completion detector per design §4.14.
//
// Signals are weighted and summed. Thresholds:
//
//	≥ 8 with judge ≥ medium → declare complete
//	5–7 → one additional audit iteration
//	< 5 → continue normally
package compdet

// JudgeVerdict is the LLM judge's assessment of task completion.
type JudgeVerdict int

const (
	JudgeUnknown    JudgeVerdict = 0
	JudgeLow        JudgeVerdict = 1
	JudgeMedium     JudgeVerdict = 2
	JudgeHigh       JudgeVerdict = 3
	JudgeIncomplete JudgeVerdict = -1 // veto: score -4
)

// Threshold represents the decision class from a completion score.
type Threshold int

const (
	ThresholdContinue Threshold = iota
	ThresholdAudit              // one more "audit for placeholders" iteration
	ThresholdComplete
)

// Signals is the full set of per-iteration signals evaluated by the detector.
type Signals struct {
	// TaskCompleteSentinel is true when the backend emitted TASK_COMPLETE.
	TaskCompleteSentinel bool
	// BuildPasses is true when the build status is "pass".
	BuildPasses bool
	// TestsPasses is true when the test suite passed.
	TestsPasses bool
	// PathSpecificProgrammatic is true when the path-specific programmatic check passes.
	// Populated by the loop engine based on mode; stub = false until step 19+.
	PathSpecificProgrammatic bool
	// AllPlanItemsClosed is true when plan.md has no unchecked boxes.
	AllPlanItemsClosed bool
	// JudgeVerdict is the LLM judge's assessment (stub = JudgeUnknown until step 17).
	JudgeVerdict JudgeVerdict
	// PlaceholderHits is the count of new placeholder hits found in the diff.
	PlaceholderHits int
}

// Result holds the completion-detector outcome.
type Result struct {
	// Score is the weighted sum of all signals.
	Score int
	// Threshold is the decision class.
	Threshold Threshold
	// ShouldComplete is true when the task should be declared complete.
	ShouldComplete bool
	// ShouldAudit is true when one more audit iteration should run.
	ShouldAudit bool
	// Contributing lists signal names that added to the score.
	Contributing []string
}

const (
	weightTaskCompleteSentinel  = 3
	weightBuildPasses           = 2
	weightTestsPasses           = 2
	weightPathProgrammatic      = 2
	weightAllPlanItemsClosed    = 2
	weightJudgeHigh             = 3
	weightJudgeMedium           = 2
	weightJudgeIncomplete       = -4
	weightPlaceholderHit        = -4 // per hit
)

// Evaluate computes the completion score from the given signals.
func Evaluate(s Signals) Result {
	score := 0
	var contributing []string

	if s.TaskCompleteSentinel {
		score += weightTaskCompleteSentinel
		contributing = append(contributing, "task_complete_sentinel")
	}
	if s.BuildPasses {
		score += weightBuildPasses
		contributing = append(contributing, "build_passes")
	}
	if s.TestsPasses {
		score += weightTestsPasses
		contributing = append(contributing, "tests_pass")
	}
	if s.PathSpecificProgrammatic {
		score += weightPathProgrammatic
		contributing = append(contributing, "path_programmatic")
	}
	if s.AllPlanItemsClosed {
		score += weightAllPlanItemsClosed
		contributing = append(contributing, "all_plan_items_closed")
	}

	judgeMinMedium := false
	switch s.JudgeVerdict {
	case JudgeHigh:
		score += weightJudgeHigh
		contributing = append(contributing, "judge_high")
		judgeMinMedium = true
	case JudgeMedium:
		score += weightJudgeMedium
		contributing = append(contributing, "judge_medium")
		judgeMinMedium = true
	case JudgeIncomplete:
		score += weightJudgeIncomplete
		contributing = append(contributing, "judge_incomplete_veto")
	}

	if s.PlaceholderHits > 0 {
		penalty := s.PlaceholderHits * weightPlaceholderHit
		score += penalty
		contributing = append(contributing, "placeholder_hits")
	}

	var threshold Threshold
	switch {
	case score >= 8 && judgeMinMedium:
		threshold = ThresholdComplete
	case score >= 5:
		threshold = ThresholdAudit
	default:
		threshold = ThresholdContinue
	}

	return Result{
		Score:          score,
		Threshold:      threshold,
		ShouldComplete: threshold == ThresholdComplete,
		ShouldAudit:    threshold == ThresholdAudit,
		Contributing:   contributing,
	}
}
