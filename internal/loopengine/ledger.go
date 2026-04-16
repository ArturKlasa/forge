// Package loopengine implements the core iteration loop for Forge.
package loopengine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

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
