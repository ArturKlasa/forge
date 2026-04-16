package compdet

import (
	"testing"
)

// TestTaskCompleteSentinelAlone verifies sentinel alone is below complete threshold.
func TestTaskCompleteSentinelAlone(t *testing.T) {
	got := Evaluate(Signals{TaskCompleteSentinel: true})
	// score = 3 < 5 → Continue
	if got.Score != 3 {
		t.Errorf("Score = %d, want 3", got.Score)
	}
	if got.Threshold != ThresholdContinue {
		t.Errorf("Threshold = %d, want Continue", got.Threshold)
	}
}

// TestCompleteThreshold verifies the ≥8 + judge-medium path.
func TestCompleteThreshold(t *testing.T) {
	got := Evaluate(Signals{
		TaskCompleteSentinel: true, // +3
		BuildPasses:          true, // +2
		TestsPasses:          true, // +2
		JudgeVerdict:         JudgeMedium, // +2
	})
	// score = 3+2+2+2 = 9, judge ≥ medium → Complete
	if got.Score != 9 {
		t.Errorf("Score = %d, want 9", got.Score)
	}
	if !got.ShouldComplete {
		t.Error("ShouldComplete = false, want true")
	}
}

// TestCompleteThresholdHighJudge verifies high-judge path.
func TestCompleteThresholdHighJudge(t *testing.T) {
	got := Evaluate(Signals{
		TaskCompleteSentinel: true, // +3
		BuildPasses:          true, // +2
		TestsPasses:          true, // +2
		JudgeVerdict:         JudgeHigh, // +3
	})
	// score = 10 ≥ 8, judge high → Complete
	if !got.ShouldComplete {
		t.Error("ShouldComplete = false, want true")
	}
}

// TestScoreAbove8WithoutJudgeIsAudit verifies score ≥ 8 but no judge → Audit.
func TestScoreAbove8WithoutJudgeIsAudit(t *testing.T) {
	got := Evaluate(Signals{
		TaskCompleteSentinel:     true, // +3
		BuildPasses:              true, // +2
		TestsPasses:              true, // +2
		PathSpecificProgrammatic: true, // +2
	})
	// score = 9 ≥ 8, but judge = Unknown (not ≥ medium) → Audit
	if got.ShouldComplete {
		t.Error("ShouldComplete = true, want false (judge not medium+)")
	}
	if !got.ShouldAudit {
		t.Error("ShouldAudit = false, want true")
	}
}

// TestAuditThreshold verifies score 5-7 → Audit.
func TestAuditThreshold(t *testing.T) {
	got := Evaluate(Signals{
		TaskCompleteSentinel: true, // +3
		BuildPasses:          true, // +2
	})
	// score = 5 → Audit
	if got.Score != 5 {
		t.Errorf("Score = %d, want 5", got.Score)
	}
	if !got.ShouldAudit {
		t.Error("ShouldAudit = false, want true")
	}
}

// TestJudgeIncompleteVeto verifies judge-says-incomplete reduces score by 4.
func TestJudgeIncompleteVeto(t *testing.T) {
	got := Evaluate(Signals{
		TaskCompleteSentinel: true, // +3
		BuildPasses:          true, // +2
		TestsPasses:          true, // +2
		JudgeVerdict:         JudgeIncomplete, // -4
	})
	// score = 3+2+2-4 = 3 → Continue
	if got.Score != 3 {
		t.Errorf("Score = %d, want 3", got.Score)
	}
	if got.ShouldComplete {
		t.Error("ShouldComplete = true, want false after veto")
	}
}

// TestPlaceholderPenalty verifies placeholder hits reduce score.
func TestPlaceholderPenalty(t *testing.T) {
	got := Evaluate(Signals{
		TaskCompleteSentinel: true, // +3
		BuildPasses:          true, // +2
		TestsPasses:          true, // +2
		JudgeVerdict:         JudgeHigh, // +3
		PlaceholderHits:      2, // -8
	})
	// score = 3+2+2+3-8 = 2 → Continue
	if got.Score != 2 {
		t.Errorf("Score = %d, want 2", got.Score)
	}
	if got.ShouldComplete {
		t.Error("ShouldComplete = true, want false after placeholder penalty")
	}
}

// TestAllPlanItemsClosed contributes +2.
func TestAllPlanItemsClosed(t *testing.T) {
	got := Evaluate(Signals{
		AllPlanItemsClosed: true,
	})
	if got.Score != 2 {
		t.Errorf("Score = %d, want 2", got.Score)
	}
}

// TestNoSignals verifies zero score → Continue.
func TestNoSignals(t *testing.T) {
	got := Evaluate(Signals{})
	if got.Score != 0 {
		t.Errorf("Score = %d, want 0", got.Score)
	}
	if got.Threshold != ThresholdContinue {
		t.Errorf("Threshold = %d, want Continue", got.Threshold)
	}
}
