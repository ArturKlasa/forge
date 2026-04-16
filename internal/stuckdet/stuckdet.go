// Package stuckdet implements the hybrid stuck-detection algorithm.
//
// Hard signals gate directly to a tier; soft signals accumulate a weighted
// sum over a 3-iteration rolling window; final tier = max(hard, soft, ceiling).
package stuckdet

// Tier represents the severity of the stuck state.
type Tier int

const (
	TierProgressing Tier = 0
	TierSoftStuck   Tier = 1
	TierHardStuck   Tier = 2
	TierDeadStuck   Tier = 3
)

// Entry is the stuckdet view of a single ledger iteration. The loop engine
// projects its internal LedgerEntry into this type before calling Evaluate.
type Entry struct {
	// FilesChanged lists paths modified in this iteration.
	FilesChanged []string
	// PlanItemsCompleted lists plan items closed this iteration.
	PlanItemsCompleted []string
	// StateSemanticDelta is true when state.md had a meaningful change.
	StateSemanticDelta bool
	// BuildStatus is "pass", "fail", or "" (unknown).
	BuildStatus string
	// ErrorFingerprint is a content-hash of the primary error message, or "".
	ErrorFingerprint string
	// AgentSelfReport is "stuck", "uncertain", or "progressing".
	AgentSelfReport string
	// Regressions lists test names that newly failed this iteration.
	Regressions []string
	// OffTopicDrift is true when an LLM judge detects the agent has drifted
	// outside the task scope. Populated by Brain.Judge in step 17; stub = false.
	OffTopicDrift bool
	// NewHighConfidencePlaceholders is the count of high-confidence placeholder
	// hits found in the iteration diff.
	NewHighConfidencePlaceholders int
	// ExternalSignalDeath is true when the backend process was killed by an
	// OS signal that Forge did not initiate. These are excluded from scoring.
	ExternalSignalDeath bool
}

// Result holds the outcome of Evaluate.
type Result struct {
	// Tier is the final computed tier (max of hard, soft, ceiling).
	Tier Tier
	// HardTriggers lists the names of hard signals that fired.
	HardTriggers []string
	// SoftSum is the total weight of soft signals that fired.
	SoftSum int
	// SoftTier is the tier derived solely from SoftSum.
	SoftTier Tier
	// FiringSignals lists all signal names that contributed.
	FiringSignals []string
}

// hardSignal fires directly at a tier when its predicate is satisfied.
type hardSignal struct {
	name      string
	tier      Tier
	predicate func([]Entry) bool
}

// softSignal contributes its Weight to the soft sum when its predicate fires.
type softSignal struct {
	name      string
	weight    int
	predicate func([]Entry) bool
}

var hardSignals = []hardSignal{
	{
		name: "off_topic_drift_detected",
		tier: TierHardStuck,
		predicate: func(w []Entry) bool {
			return len(w) > 0 && w[len(w)-1].OffTopicDrift
		},
	},
	{
		name: "placeholder_accumulation_detected",
		tier: TierHardStuck,
		predicate: func(w []Entry) bool {
			return len(w) > 0 && w[len(w)-1].NewHighConfidencePlaceholders > 0
		},
	},
	{
		name: "same_error_fingerprint_4plus",
		tier: TierDeadStuck,
		predicate: func(w []Entry) bool {
			if len(w) < 4 {
				return false
			}
			last := w[len(w)-1].ErrorFingerprint
			if last == "" {
				return false
			}
			for _, e := range w[len(w)-4:] {
				if e.ErrorFingerprint != last {
					return false
				}
			}
			return true
		},
	},
	{
		name: "build_broken_5plus",
		tier: TierDeadStuck,
		predicate: func(w []Entry) bool {
			if len(w) < 5 {
				return false
			}
			for _, e := range w[len(w)-5:] {
				if e.BuildStatus != "fail" {
					return false
				}
			}
			return true
		},
	},
}

var softSignals = []softSignal{
	{
		name:   "no_files_changed_in_window",
		weight: 2,
		predicate: func(w []Entry) bool {
			if len(w) == 0 {
				return false
			}
			for _, e := range w {
				if len(e.FilesChanged) > 0 {
					return false
				}
			}
			return true
		},
	},
	{
		name:   "no_plan_items_closed_in_window",
		weight: 2,
		predicate: func(w []Entry) bool {
			if len(w) == 0 {
				return false
			}
			for _, e := range w {
				if len(e.PlanItemsCompleted) > 0 {
					return false
				}
			}
			return true
		},
	},
	{
		name:   "no_state_semantic_delta_in_window",
		weight: 2,
		predicate: func(w []Entry) bool {
			if len(w) == 0 {
				return false
			}
			for _, e := range w {
				if e.StateSemanticDelta {
					return false
				}
			}
			return true
		},
	},
	{
		name:   "test_regression_introduced",
		weight: 3,
		predicate: func(w []Entry) bool {
			return len(w) > 0 && len(w[len(w)-1].Regressions) > 0
		},
	},
	{
		name:   "agent_self_reports_stuck",
		weight: 2,
		predicate: func(w []Entry) bool {
			return len(w) > 0 && w[len(w)-1].AgentSelfReport == "stuck"
		},
	},
}

// softSumToTier maps a soft-signal sum to its tier per design §4.13.
func softSumToTier(sum int) Tier {
	switch {
	case sum >= 6:
		return TierHardStuck
	case sum >= 3:
		return TierSoftStuck
	default:
		return TierProgressing
	}
}

const softWindow = 3

// Evaluate computes the stuck tier from all available ledger entries.
//
// Hard signals inspect the full entry slice (up to last 5).
// Soft signals use the rolling 3-iteration window.
// External-signal deaths are excluded from scoring.
func Evaluate(entries []Entry) Result {
	if len(entries) == 0 {
		return Result{}
	}

	// Filter out external-signal deaths for scoring purposes, but preserve
	// the slice length for window-size-based predicates.
	filtered := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !e.ExternalSignalDeath {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		return Result{}
	}

	// Soft window: last 3 non-external-death entries.
	window := filtered
	if len(window) > softWindow {
		window = window[len(window)-softWindow:]
	}

	var hardTriggers []string
	var firingSignals []string
	maxHardTier := TierProgressing

	for _, s := range hardSignals {
		if s.predicate(filtered) {
			hardTriggers = append(hardTriggers, s.name)
			firingSignals = append(firingSignals, s.name)
			if s.tier > maxHardTier {
				maxHardTier = s.tier
			}
		}
	}

	softSum := 0
	for _, s := range softSignals {
		if s.predicate(window) {
			softSum += s.weight
			firingSignals = append(firingSignals, s.name)
		}
	}

	stTier := softSumToTier(softSum)

	finalTier := maxHardTier
	if stTier > finalTier {
		finalTier = stTier
	}

	return Result{
		Tier:          finalTier,
		HardTriggers:  hardTriggers,
		SoftSum:       softSum,
		SoftTier:      stTier,
		FiringSignals: firingSignals,
	}
}
