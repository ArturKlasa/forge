package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ScanResult holds all policy scan results for a single iteration.
type ScanResult struct {
	// SecretHits triggers a hard-stop mandatory gate.
	SecretHits []SecretFinding
	// PlaceholderHits — high-severity ones block completion; low-severity are advisory.
	PlaceholderHits []PlaceholderHit
	// GateHits — IsHardStop() entries trigger mandatory stop.
	GateHits []GateHit
}

// HasHardStop returns true when any result requires immediate loop halt.
func (r *ScanResult) HasHardStop() bool {
	if len(r.SecretHits) > 0 {
		return true
	}
	for _, g := range r.GateHits {
		if g.IsHardStop() {
			return true
		}
	}
	return false
}

// HardStopReason returns a short human-readable explanation of the first hard-stop hit.
func (r *ScanResult) HardStopReason() string {
	if len(r.SecretHits) > 0 {
		f := r.SecretHits[0]
		return fmt.Sprintf("Security Scanner hit — %s in %s (line %d)", f.RuleID, f.File, f.Line)
	}
	for _, g := range r.GateHits {
		if g.IsHardStop() {
			return fmt.Sprintf("Gate Scanner hit — %s: %s", g.Class, g.Reason)
		}
	}
	return ""
}

// Scanner runs all three policy sub-scanners.
type Scanner struct {
	sec  *SecurityScanner
	ph   *PlaceholderScanner
	gate *GateScanner
}

// NewScanner constructs a Scanner. gitleaksToml may be empty (uses default rules).
// Additional gate paths extend the built-in tables.
func NewScanner(gitleaksToml string, additionalManifests, additionalCI, additionalSecrets []string) (*Scanner, error) {
	sec, err := NewSecurityScanner(gitleaksToml)
	if err != nil {
		return nil, fmt.Errorf("policy.NewScanner: gitleaks init: %w", err)
	}
	return &Scanner{
		sec:  sec,
		ph:   NewPlaceholderScanner(),
		gate: &GateScanner{AdditionalManifests: additionalManifests, AdditionalCI: additionalCI, AdditionalSecrets: additionalSecrets},
	}, nil
}

// ScanIteration runs all scanners on the per-iteration diff. testsPassed is
// used by the gate scanner for lockfile-only classification.
func (s *Scanner) ScanIteration(diff []byte, testsPassed bool) *ScanResult {
	return &ScanResult{
		SecretHits:      s.sec.Scan(diff),
		PlaceholderHits: s.ph.ScanDiff(diff),
		GateHits:        s.gate.Scan(diff, testsPassed),
	}
}

// PlaceholderEntry is one row in placeholders.jsonl.
type PlaceholderEntry struct {
	File      string    `json:"file"`
	Line      int       `json:"line"`
	Pattern   string    `json:"pattern"`
	Text      string    `json:"text"`
	Severity  string    `json:"severity"`
	Iteration int       `json:"iteration"`
	Status    string    `json:"status"` // "active" | "pre-existing" | "resolved"
	RecordedAt time.Time `json:"recorded_at"`
}

// AppendPlaceholderLedger appends placeholder detections to placeholders.jsonl
// in the run directory.
func AppendPlaceholderLedger(runDir string, hits []PlaceholderHit, iteration int, status string) error {
	if len(hits) == 0 {
		return nil
	}
	path := filepath.Join(runDir, "placeholders.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, h := range hits {
		sev := "low"
		if h.Severity == SeverityHigh {
			sev = "high"
		}
		e := PlaceholderEntry{
			File:      h.File,
			Line:      h.Line,
			Pattern:   h.Pattern,
			Text:      h.Text,
			Severity:  sev,
			Iteration: iteration,
			Status:    status,
			RecordedAt: time.Now().UTC(),
		}
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}
