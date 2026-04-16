package loopengine

import (
	"fmt"
	"strings"
	"time"

	"github.com/arturklasa/forge/internal/compdet"
	"github.com/arturklasa/forge/internal/escalate"
	"github.com/arturklasa/forge/internal/stuckdet"
)

// parseFinalTextSignals extracts stuck-detector signal overrides that the
// backend embedded in its final text using FORGE: sentinel comments.
//
// Supported sentinels:
//
//	<!--FORGE:build_status=pass-->
//	<!--FORGE:build_status=fail-->
//	<!--FORGE:self_report=stuck-->
//	<!--FORGE:self_report=uncertain-->
//	<!--FORGE:self_report=progressing-->
//	<!--FORGE:error_fp=<hash>-->
//	<!--FORGE:regression=<name>-->
func parseFinalTextSignals(text string) (buildStatus, errorFingerprint, agentSelfReport string, regressions []string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<!--FORGE:") {
			continue
		}
		inner := strings.TrimPrefix(line, "<!--FORGE:")
		inner = strings.TrimSuffix(inner, "-->")
		k, v, ok := strings.Cut(inner, "=")
		if !ok {
			continue
		}
		switch k {
		case "build_status":
			buildStatus = v
		case "self_report":
			agentSelfReport = v
		case "error_fp":
			errorFingerprint = v
		case "regression":
			regressions = append(regressions, v)
		}
	}
	return
}

// toStuckEntry converts a LedgerEntry to a stuckdet.Entry.
func toStuckEntry(e LedgerEntry) stuckdet.Entry {
	return stuckdet.Entry{
		FilesChanged:                  e.FilesChanged,
		PlanItemsCompleted:            e.PlanItemsCompleted,
		StateSemanticDelta:            e.StateSemanticDelta.Changed,
		BuildStatus:                   e.BuildStatus,
		ErrorFingerprint:              e.ErrorFingerprint,
		AgentSelfReport:               e.AgentSelfReport,
		Regressions:                   e.Regressions,
		OffTopicDrift:                 e.OffTopicDrift,
		NewHighConfidencePlaceholders: e.NewHighConfidencePlaceholders,
		ExternalSignalDeath:           e.ExternalSignalDeath,
	}
}

// buildCompletionSignals derives compdet.Signals from the current ledger entry.
func buildCompletionSignals(entry LedgerEntry, allPlanItemsClosed bool) compdet.Signals {
	return compdet.Signals{
		TaskCompleteSentinel:     entry.Complete,
		BuildPasses:              entry.BuildStatus == "pass",
		TestsPasses:              len(entry.Regressions) == 0 && entry.BuildStatus == "pass",
		PathSpecificProgrammatic: false, // wired in step 19+
		AllPlanItemsClosed:       allPlanItemsClosed,
		JudgeVerdict:             compdet.JudgeUnknown, // Brain.Judge wired in step 17
		PlaceholderHits:          entry.NewHighConfidencePlaceholders,
	}
}

// stuckTierLabel returns a human-readable label for a stuck tier.
func stuckTierLabel(tier stuckdet.Tier) string {
	switch tier {
	case stuckdet.TierSoftStuck:
		return "soft-stuck"
	case stuckdet.TierHardStuck:
		return "hard-stuck"
	case stuckdet.TierDeadStuck:
		return "dead-stuck"
	default:
		return "progressing"
	}
}

// stuckEscalationReason builds the human-readable reason for a Tier-3 escalation.
func stuckEscalationReason(result stuckdet.Result) string {
	if len(result.HardTriggers) > 0 {
		return fmt.Sprintf("dead-stuck: %s", strings.Join(result.HardTriggers, ", "))
	}
	return fmt.Sprintf("dead-stuck: soft-sum=%d", result.SoftSum)
}

// buildCompletion runs the completion detector from signals.
func buildCompletion(s compdet.Signals) compdet.Result {
	return compdet.Evaluate(s)
}

// buildStuckEscalation creates an escalation for a dead-stuck Tier-3 scenario.
func buildStuckEscalation(
	_ string, // runDir (unused here; Manager has it)
	iteration int,
	path string,
	reason string,
	result stuckdet.Result,
	clock func() time.Time,
) *escalate.Escalation {
	triggersStr := strings.Join(result.FiringSignals, ", ")
	t := clock()
	return &escalate.Escalation{
		ID:        escalate.GenerateID(t, iteration),
		RaisedAt:  t,
		Tier:      3,
		Path:      path,
		Iteration: iteration,
		WhatTried: reason,
		Decision:  fmt.Sprintf("Stuck signals: %s · soft-sum=%d. Choose how to proceed.", triggersStr, result.SoftSum),
		Options: []escalate.Option{
			{Key: "p", Label: "Pivot", Description: "Change approach and continue"},
			{Key: "s", Label: "Split", Description: "Split task into smaller subtasks"},
			{Key: "r", Label: "Reset", Description: "Reset to last green commit"},
			{Key: "d", Label: "Defer", Description: "Abort this run"},
		},
		Recommended: "p",
		Mandatory:   false,
	}
}
