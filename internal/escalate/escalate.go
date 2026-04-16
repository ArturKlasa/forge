package escalate

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AutoResolveMode controls the --auto-resolve flag behaviour.
type AutoResolveMode int

const (
	AutoResolveNever             AutoResolveMode = iota
	AutoResolveAcceptRecommended                 // auto-pick recommendation after 5s for non-mandatory
	AutoResolveAbort                             // abort on any escalation
)

// Option is one choice in an escalation.
type Option struct {
	Key         string // single letter, e.g. "a"
	Label       string // short label, e.g. "Apply"
	Description string // one-line description
}

// Escalation describes a single human escalation event.
type Escalation struct {
	ID          string    // e.g. "esc-2026-04-16-143022-001"
	RaisedAt    time.Time
	Tier        int
	Path        string // forge mode: "create", "fix", etc.
	Iteration   int
	WhatTried   string
	Decision    string
	Options     []Option
	Recommended string // option key
	Mandatory   bool   // if true, --auto-resolve does not apply
}

// Answer holds the human's parsed response.
type Answer struct {
	OptionKey string
	Note      string
}

// Manager coordinates writing awaiting-human.md, watching for answer.md, and
// enforcing --auto-resolve / --yes policies.
type Manager struct {
	RunDir      string
	AutoResolve AutoResolveMode
	YesFlag     bool
	Output      io.Writer
	Clock       func() time.Time
	// overrideable for tests
	netFSOverride *bool
}

// NewManager returns a Manager for the given run directory.
func NewManager(runDir string) *Manager {
	return &Manager{
		RunDir: runDir,
		Output: os.Stdout,
		Clock:  time.Now,
	}
}

// SetNetworkFSOverride forces polling mode (true) or fsnotify mode (false) for
// tests, bypassing the real statfs detection.
func (m *Manager) SetNetworkFSOverride(v bool) {
	m.netFSOverride = &v
}

// isNetworkFS returns true when the run directory is on a remote filesystem.
func (m *Manager) isNetFS() bool {
	if m.netFSOverride != nil {
		return *m.netFSOverride
	}
	return IsNetworkFS(m.RunDir)
}

// Escalate writes awaiting-human.md, displays a banner, then blocks until the
// user provides a valid answer (via answer.md or --auto-resolve rules).
func (m *Manager) Escalate(ctx context.Context, esc *Escalation) (Answer, error) {
	// 1. Write awaiting-human.md atomically.
	content := renderAwaitingHuman(esc)
	awaitingPath := filepath.Join(m.RunDir, "awaiting-human.md")
	if err := AtomicWrite(awaitingPath, []byte(content)); err != nil {
		return Answer{}, fmt.Errorf("write awaiting-human.md: %w", err)
	}

	// 2. Write ESCALATION sentinel (1-line summary).
	sentinelPath := filepath.Join(m.RunDir, "ESCALATION")
	sentinel := fmt.Sprintf("id: %s  tier: %d  iter: %d  path: %s\n",
		esc.ID, esc.Tier, esc.Iteration, esc.Path)
	if err := os.WriteFile(sentinelPath, []byte(sentinel), 0o644); err != nil {
		return Answer{}, fmt.Errorf("write ESCALATION sentinel: %w", err)
	}

	// 3. Print banner.
	m.printBanner(esc)

	// 4. --auto-resolve abort applies to all escalations.
	if m.AutoResolve == AutoResolveAbort {
		fmt.Fprintf(m.Output, "[escalation] --auto-resolve abort: aborting run\n")
		return Answer{OptionKey: "abort-auto"}, nil
	}

	// --auto-resolve accept-recommended applies only to non-mandatory gates.
	if m.AutoResolve == AutoResolveAcceptRecommended && !esc.Mandatory && esc.Recommended != "" {
		fmt.Fprintf(m.Output, "[escalation] --auto-resolve: waiting 5s then accepting [%s]...\n", esc.Recommended)
		select {
		case <-ctx.Done():
			return Answer{}, ctx.Err()
		case <-time.After(5 * time.Second):
			fmt.Fprintf(m.Output, "[escalation] auto-resolved: accepted [%s]\n", esc.Recommended)
			return Answer{OptionKey: esc.Recommended}, nil
		}
	}

	// 5. Watch for answer.md.
	return m.waitForAnswer(ctx, esc)
}

// waitForAnswer blocks until a valid answer is written to answer.md.
func (m *Manager) waitForAnswer(ctx context.Context, esc *Escalation) (Answer, error) {
	validKeys := make(map[string]bool, len(esc.Options))
	for _, o := range esc.Options {
		validKeys[o.Key] = true
	}

	var result Answer
	var parseErr error
	done := make(chan struct{})

	onContent := func(data []byte) bool {
		pa, err := ParseAnswer(data)
		if err != nil {
			// parse failure — log and wait for next event
			parseErr = err
			return false
		}
		if pa.OptionKey == "" {
			return false
		}
		if pa.IDField != esc.ID {
			// ID mismatch: rename to stale
			answerPath := filepath.Join(m.RunDir, "answer.md")
			ts := time.Now().UTC().Format("20060102T150405")
			stalePath := filepath.Join(m.RunDir, fmt.Sprintf("answer.stale.md.%s", ts))
			_ = os.Rename(answerPath, stalePath)
			fmt.Fprintf(m.Output, "[escalation] stale answer (id mismatch) → %s\n", stalePath)
			return false
		}
		if !validKeys[pa.OptionKey] {
			parseErr = fmt.Errorf("unknown option %q", pa.OptionKey)
			return false
		}
		result = Answer{OptionKey: pa.OptionKey, Note: pa.Note}
		// Consume: delete answer.md.
		answerPath := filepath.Join(m.RunDir, "answer.md")
		_ = os.Remove(answerPath)
		close(done)
		return true
	}

	_ = parseErr // consumed below if needed

	watchErr := make(chan error, 1)
	go func() {
		watchErr <- Watch(ctx, m.RunDir, onContent, m.isNetFS())
	}()

	select {
	case <-done:
		fmt.Fprintf(m.Output, "[%s] escalation resolved: %s (%s)\n",
			m.Clock().Format("15:04:05"), optionLabel(esc, result.OptionKey), result.OptionKey)
		return result, nil
	case err := <-watchErr:
		if err != nil && err != context.Canceled {
			return Answer{}, fmt.Errorf("watch answer.md: %w", err)
		}
		return Answer{}, context.Canceled
	case <-ctx.Done():
		return Answer{}, ctx.Err()
	}
}

// GenerateID builds an escalation ID from the current time and a counter.
func GenerateID(t time.Time, n int) string {
	return fmt.Sprintf("esc-%s-%03d", t.UTC().Format("2006-01-02-150405"), n)
}

// optionLabel returns the label for a given option key.
func optionLabel(esc *Escalation, key string) string {
	for _, o := range esc.Options {
		if o.Key == key {
			return strings.ToLower(o.Label)
		}
	}
	return key
}

// printBanner writes the escalation banner to m.Output.
func (m *Manager) printBanner(esc *Escalation) {
	line := strings.Repeat("=", 72)
	var opts []string
	for _, o := range esc.Options {
		opts = append(opts, fmt.Sprintf("[%s] %s", o.Key, strings.ToLower(o.Label)))
	}
	rec := ""
	if esc.Recommended != "" {
		rec = fmt.Sprintf("  ·  Recommended: [%s]", esc.Recommended)
	}
	fmt.Fprintf(m.Output, "%s\n", line)
	fmt.Fprintf(m.Output, "ESCALATION — Forge needs your decision\n")
	fmt.Fprintf(m.Output, "Run: iter %d · Tier %d · path: %s\n", esc.Iteration, esc.Tier, esc.Path)
	if esc.Decision != "" {
		fmt.Fprintf(m.Output, "%s\n", esc.Decision)
	}
	fmt.Fprintf(m.Output, "Options: %s%s\n", strings.Join(opts, "  "), rec)
	fmt.Fprintf(m.Output, "%s\n", line)
	fmt.Fprintf(m.Output, "  Respond: edit %s/answer.md\n\n", m.RunDir)
}

// GateScannerEscalation builds an Escalation for a Gate Scanner hard stop.
func GateScannerEscalation(runDir string, iteration int, path string, reason string, clock func() time.Time) *Escalation {
	t := clock()
	return &Escalation{
		ID:        GenerateID(t, iteration),
		RaisedAt:  t,
		Tier:      3,
		Path:      path,
		Iteration: iteration,
		WhatTried: reason,
		Decision:  "Gate Scanner blocked this iteration. Choose how to proceed.",
		Options: []Option{
			{Key: "a", Label: "Apply", Description: "apply this change and continue"},
			{Key: "s", Label: "Revert", Description: "revert and continue"},
			{Key: "p", Label: "Pivot", Description: "pivot approach"},
			{Key: "d", Label: "Defer", Description: "defer this change"},
		},
		Recommended: "s",
		Mandatory:   true,
	}
}
