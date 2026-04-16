// Package loopengine implements the core iteration loop for Forge.
package loopengine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// SemanticDelta records whether state.md changed meaningfully this iteration.
type SemanticDelta struct {
	Changed bool `json:"changed"`
}

// LedgerEntry records the result of a single iteration.
type LedgerEntry struct {
	RunID        string    `json:"run_id"`
	Iteration    int       `json:"iteration"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationSec  float64   `json:"duration_sec"`
	Exit         exitInfo  `json:"exit"`
	FilesChanged []string  `json:"files_changed"`
	CommitSHA    string    `json:"commit_sha,omitempty"`
	PromptTokens int       `json:"prompt_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Complete     bool      `json:"complete"`

	// Stuck detector fields (§4.13).
	ErrorFingerprint              string        `json:"error_fingerprint,omitempty"`
	BuildStatus                   string        `json:"build_status,omitempty"`
	PlanItemsCompleted            []string      `json:"plan_items_completed,omitempty"`
	StateSemanticDelta            SemanticDelta `json:"state_semantic_delta"`
	AgentSelfReport               string        `json:"agent_self_report,omitempty"`
	Regressions                   []string      `json:"regressions,omitempty"`
	OffTopicDrift                 bool          `json:"off_topic_drift,omitempty"`
	NewHighConfidencePlaceholders int           `json:"new_high_confidence_placeholders,omitempty"`
	ExternalSignalDeath           bool          `json:"external_signal_death,omitempty"`
	StuckTier                     int           `json:"stuck_tier"`
	StuckHardTriggers             []string      `json:"stuck_hard_triggers,omitempty"`
	StuckSoftSum                  int           `json:"stuck_soft_sum"`
	CompletionScore               int           `json:"completion_score"`
}

type exitInfo struct {
	Code    int    `json:"code"`
	Subtype string `json:"subtype,omitempty"`
}

// appendLedger appends a single entry to ledger.jsonl inside runDir.
func appendLedger(runDir string, entry LedgerEntry) error {
	path := filepath.Join(runDir, "ledger.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(entry)
}

// readLedger reads all entries from ledger.jsonl.
func readLedger(runDir string) ([]LedgerEntry, error) {
	path := filepath.Join(runDir, "ledger.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []LedgerEntry
	dec := json.NewDecoder(f)
	for dec.More() {
		var e LedgerEntry
		if err := dec.Decode(&e); err != nil {
			return entries, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}
