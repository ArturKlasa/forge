package escalate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/escalate"
	"github.com/google/renameio/v2"
)

// buildEsc creates a test escalation with gate-scanner options.
func buildEsc(id string, iteration int) *escalate.Escalation {
	return &escalate.Escalation{
		ID:        id,
		RaisedAt:  time.Now(),
		Tier:      3,
		Path:      "create",
		Iteration: iteration,
		WhatTried: "tried to modify package.json",
		Decision:  "Gate Scanner blocked this change.",
		Options: []escalate.Option{
			{Key: "a", Label: "Apply", Description: "apply this change"},
			{Key: "s", Label: "Revert", Description: "revert and continue"},
			{Key: "p", Label: "Pivot", Description: "pivot approach"},
			{Key: "d", Label: "Defer", Description: "defer"},
		},
		Recommended: "s",
		Mandatory:   true,
	}
}

func writeAnswer(t *testing.T, dir, id, key string) {
	t.Helper()
	content := "id: " + id + "\nanswer: " + key + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "answer.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write answer.md: %v", err)
	}
}

func writeAnswerAtomic(t *testing.T, dir, id, key string) {
	t.Helper()
	content := "id: " + id + "\nanswer: " + key + "\n---\n"
	if err := renameio.WriteFile(filepath.Join(dir, "answer.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write answer.md (atomic): %v", err)
	}
}

func writeAnswerVimsStyle(t *testing.T, dir, id, key string) {
	t.Helper()
	// Vim backup-style: rename original to .bak, write new, remove .bak.
	content := "id: " + id + "\nanswer: " + key + "\n---\n"
	target := filepath.Join(dir, "answer.md")
	bak := target + ".bak"
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, bak); err != nil {
			t.Fatalf("vim backup rename: %v", err)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatalf("vim write: %v", err)
	}
	_ = os.Remove(bak)
}

func writeAnswerJetBrainsStyle(t *testing.T, dir, id, key string) {
	t.Helper()
	// JetBrains: write ___jb_tmp___, rename original → ___jb_old___, rename tmp → target, delete old.
	content := "id: " + id + "\nanswer: " + key + "\n---\n"
	target := filepath.Join(dir, "answer.md")
	tmp := target + "___jb_tmp___"
	old := target + "___jb_old___"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatalf("jb write tmp: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		_ = os.Rename(target, old)
	}
	if err := os.Rename(tmp, target); err != nil {
		t.Fatalf("jb rename tmp→target: %v", err)
	}
	_ = os.Remove(old)
}

func runEscalate(t *testing.T, dir string, esc *escalate.Escalation, netFS bool) (escalate.Answer, error) {
	t.Helper()
	m := escalate.NewManager(dir)
	m.SetNetworkFSOverride(netFS)
	m.Output = os.Stderr // suppress banner in test output
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.Escalate(ctx, esc)
}

// TestTruncateOverwrite simulates VSCode/nano save.
func TestTruncateOverwrite(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-001"
	esc := buildEsc(id, 2)

	go func() {
		time.Sleep(100 * time.Millisecond)
		writeAnswer(t, dir, id, "a")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "a" {
		t.Errorf("expected option 'a', got %q", ans.OptionKey)
	}
	// answer.md should be deleted.
	if _, err := os.Stat(filepath.Join(dir, "answer.md")); !os.IsNotExist(err) {
		t.Error("answer.md should be deleted after consumption")
	}
}

// TestAtomicRename simulates JetBrains / vim default atomic save.
func TestAtomicRename(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-002"
	esc := buildEsc(id, 3)

	go func() {
		time.Sleep(100 * time.Millisecond)
		writeAnswerAtomic(t, dir, id, "s")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "s" {
		t.Errorf("expected 's', got %q", ans.OptionKey)
	}
}

// TestVimBackupStyle simulates vim backup-style save.
func TestVimBackupStyle(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-003"
	esc := buildEsc(id, 4)

	go func() {
		time.Sleep(100 * time.Millisecond)
		writeAnswerVimsStyle(t, dir, id, "p")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "p" {
		t.Errorf("expected 'p', got %q", ans.OptionKey)
	}
}

// TestJetBrainsStyle simulates JetBrains safe-write with ___jb_tmp___.
func TestJetBrainsStyle(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-004"
	esc := buildEsc(id, 5)

	go func() {
		time.Sleep(100 * time.Millisecond)
		writeAnswerJetBrainsStyle(t, dir, id, "d")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "d" {
		t.Errorf("expected 'd', got %q", ans.OptionKey)
	}
}

// TestIDMismatch ensures stale answers are renamed and waiting continues.
func TestIDMismatch(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-005"
	wrongID := "esc-2026-04-16-000000-999"
	esc := buildEsc(id, 6)

	go func() {
		// Write stale answer first.
		time.Sleep(80 * time.Millisecond)
		writeAnswer(t, dir, wrongID, "a")
		// Then write correct answer.
		time.Sleep(300 * time.Millisecond)
		writeAnswer(t, dir, id, "s")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "s" {
		t.Errorf("expected 's', got %q", ans.OptionKey)
	}
	// A stale file should exist.
	entries, _ := os.ReadDir(dir)
	foundStale := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "answer.stale.md.") {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Error("expected a answer.stale.md.* file to be created")
	}
}

// TestPartialWriteRace ensures partial writes are rejected and the complete
// write succeeds.
func TestPartialWriteRace(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-006"
	esc := buildEsc(id, 7)

	go func() {
		time.Sleep(80 * time.Millisecond)
		// Write incomplete content (no --- terminator).
		_ = os.WriteFile(filepath.Join(dir, "answer.md"), []byte("id: "+id+"\nanswer: a\n"), 0o644)
		// Complete it after a short delay.
		time.Sleep(200 * time.Millisecond)
		writeAnswer(t, dir, id, "a")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "a" {
		t.Errorf("expected 'a', got %q", ans.OptionKey)
	}
}

// TestNetworkFSPolling exercises the polling fallback.
func TestNetworkFSPolling(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-007"
	esc := buildEsc(id, 8)

	go func() {
		time.Sleep(300 * time.Millisecond)
		writeAnswer(t, dir, id, "s")
	}()

	ans, err := runEscalate(t, dir, esc, true /* netFS */)
	if err != nil {
		t.Fatalf("escalate (polling): %v", err)
	}
	if ans.OptionKey != "s" {
		t.Errorf("expected 's', got %q", ans.OptionKey)
	}
}

// TestAutoResolveAcceptRecommended verifies the 5s auto-pick path.
func TestAutoResolveAcceptRecommended(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-008"
	esc := buildEsc(id, 9)
	esc.Mandatory = false // auto-resolve only applies to non-mandatory

	m := escalate.NewManager(dir)
	m.SetNetworkFSOverride(false)
	m.AutoResolve = escalate.AutoResolveAcceptRecommended
	m.Output = os.Stderr

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ans, err := m.Escalate(ctx, esc)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != esc.Recommended {
		t.Errorf("expected recommended %q, got %q", esc.Recommended, ans.OptionKey)
	}
	if elapsed < 4*time.Second {
		t.Errorf("auto-resolve should wait ~5s, only waited %v", elapsed)
	}
}

// TestAwaitingHumanWritten verifies awaiting-human.md is atomically written.
func TestAwaitingHumanWritten(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-009"
	esc := buildEsc(id, 10)

	go func() {
		time.Sleep(100 * time.Millisecond)
		writeAnswer(t, dir, id, "a")
	}()

	_, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "awaiting-human.md"))
	if err != nil {
		t.Fatalf("awaiting-human.md not written: %v", err)
	}
	if !strings.Contains(string(data), "id: "+id) {
		t.Error("awaiting-human.md missing escalation id")
	}
	if !strings.Contains(string(data), "answer: s") { // recommended option in template
		t.Error("awaiting-human.md should show recommended answer template")
	}
}

// TestSidecarIgnored ensures editor sidecar files don't trigger parsing.
func TestSidecarIgnored(t *testing.T) {
	dir := t.TempDir()
	id := "esc-2026-04-16-143022-010"
	esc := buildEsc(id, 11)

	go func() {
		time.Sleep(80 * time.Millisecond)
		// Write various sidecar files — should all be ignored.
		for _, name := range []string{".answer.md.swp", "answer.md~", "#answer.md#", "4913"} {
			_ = os.WriteFile(filepath.Join(dir, name), []byte("garbage"), 0o644)
		}
		// Then write the real answer.
		time.Sleep(200 * time.Millisecond)
		writeAnswer(t, dir, id, "a")
	}()

	ans, err := runEscalate(t, dir, esc, false)
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if ans.OptionKey != "a" {
		t.Errorf("expected 'a', got %q", ans.OptionKey)
	}
}

// TestParseAnswerValid tests the parser directly with valid input.
func TestParseAnswerValid(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantID  string
		wantKey string
		wantNote string
	}{
		{
			name:    "minimal",
			input:   "id: esc-001\nanswer: r\n---\n",
			wantID:  "esc-001",
			wantKey: "r",
		},
		{
			name:     "with note",
			input:    "id: esc-002\nanswer: r\n\nLet's reset and try again.\n---\n",
			wantID:   "esc-002",
			wantKey:  "r",
			wantNote: "Let's reset and try again.",
		},
		{
			name:    "crlf",
			input:   "id: esc-003\r\nanswer: a\r\n---\r\n",
			wantID:  "esc-003",
			wantKey: "a",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ans, err := escalate.ParseAnswer([]byte(tc.input))
			if err != nil {
				t.Fatalf("ParseAnswer: %v", err)
			}
			if ans.IDField != tc.wantID {
				t.Errorf("id: want %q, got %q", tc.wantID, ans.IDField)
			}
			if ans.OptionKey != tc.wantKey {
				t.Errorf("key: want %q, got %q", tc.wantKey, ans.OptionKey)
			}
			if tc.wantNote != "" && ans.Note != tc.wantNote {
				t.Errorf("note: want %q, got %q", tc.wantNote, ans.Note)
			}
		})
	}
}

// TestParseAnswerInvalid verifies rejection of malformed inputs.
func TestParseAnswerInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"no terminator", "id: x\nanswer: a\n"},
		{"missing id", "answer: a\n---\n"},
		{"missing answer", "id: x\n---\n"},
		{"multi-char answer", "id: x\nanswer: ab\n---\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := escalate.ParseAnswer([]byte(tc.input))
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
