package state_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/state"
)

func newManager(t *testing.T) (*state.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := state.NewManager(dir)
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, dir
}

func TestRunDirCreation(t *testing.T) {
	m, _ := newManager(t)

	rd, err := m.CreateRun("test-2026-04-16-001")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if rd.ID != "test-2026-04-16-001" {
		t.Errorf("unexpected ID %q", rd.ID)
	}
	if _, err := os.Stat(rd.Path); err != nil {
		t.Errorf("run dir not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rd.Path, "iterations")); err != nil {
		t.Errorf("iterations dir not found: %v", err)
	}
}

func TestRunDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not portable on Windows")
	}
	m, _ := newManager(t)
	rd, _ := m.CreateRun("perm-test-001")

	info, err := os.Stat(rd.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected 0755, got %o", info.Mode().Perm())
	}
}

func TestMarkerWritten(t *testing.T) {
	m, _ := newManager(t)
	rd, _ := m.CreateRun("marker-test-001")

	mk, err := m.ReadMarker(rd)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if mk != state.MarkerRunning {
		t.Errorf("expected RUNNING, got %q", mk)
	}
}

func TestMarkerAtomicTransition(t *testing.T) {
	m, _ := newManager(t)
	rd, _ := m.CreateRun("transition-001")

	if err := m.Transition(rd, state.MarkerPaused); err != nil {
		t.Fatalf("Transition to PAUSED: %v", err)
	}

	mk, err := m.ReadMarker(rd)
	if err != nil {
		t.Fatalf("ReadMarker after transition: %v", err)
	}
	if mk != state.MarkerPaused {
		t.Errorf("expected PAUSED, got %q", mk)
	}

	// Old marker must be gone.
	if _, err := os.Stat(filepath.Join(rd.Path, string(state.MarkerRunning))); err == nil {
		t.Error("old RUNNING marker still present after transition")
	}
}

func TestNoTwoMarkersPresent(t *testing.T) {
	m, _ := newManager(t)
	rd, _ := m.CreateRun("two-markers-001")

	markers := []state.Marker{
		state.MarkerRunning, state.MarkerPaused, state.MarkerAwaitingHuman,
		state.MarkerDone, state.MarkerFailed, state.MarkerAborted,
	}
	for _, next := range markers {
		if err := m.Transition(rd, next); err != nil {
			t.Fatalf("Transition to %s: %v", next, err)
		}
		count := 0
		for _, mk := range markers {
			if _, err := os.Stat(filepath.Join(rd.Path, string(mk))); err == nil {
				count++
			}
		}
		if count != 1 {
			t.Errorf("after transitioning to %s, found %d marker files (want 1)", next, count)
		}
	}
}

func TestCurrentSymlink(t *testing.T) {
	m, _ := newManager(t)
	rd, err := m.CreateRun("symlink-test-001")
	if err != nil {
		t.Fatal(err)
	}

	current, err := m.CurrentRun()
	if err != nil {
		t.Fatalf("CurrentRun: %v", err)
	}
	if current == nil {
		t.Fatal("expected current run, got nil")
	}
	if current.ID != rd.ID {
		t.Errorf("current ID %q != created ID %q", current.ID, rd.ID)
	}
}

func TestNoCurrentRun(t *testing.T) {
	m, _ := newManager(t)
	current, err := m.CurrentRun()
	if err != nil {
		t.Fatalf("CurrentRun on empty state: %v", err)
	}
	if current != nil {
		t.Errorf("expected nil, got %+v", current)
	}
}

func TestClearCurrent(t *testing.T) {
	m, _ := newManager(t)
	m.CreateRun("clear-test-001")

	if err := m.ClearCurrent(); err != nil {
		t.Fatalf("ClearCurrent: %v", err)
	}
	current, err := m.CurrentRun()
	if err != nil {
		t.Fatalf("CurrentRun after clear: %v", err)
	}
	if current != nil {
		t.Error("expected nil after clear")
	}
}

func TestCurrentTextFileFallback(t *testing.T) {
	// Simulate Windows text-file current pointer on any OS by writing the file
	// directly and resolving it via the fallback path.
	m, dir := newManager(t)
	rd, _ := m.CreateRun("textfile-001")

	// Overwrite the current pointer with a plain text file (Windows style).
	if runtime.GOOS != "windows" {
		_ = os.Remove(filepath.Join(dir, ".forge", "current"))
		if err := os.WriteFile(
			filepath.Join(dir, ".forge", "current"),
			[]byte(rd.Path+"\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	current, err := m.CurrentRun()
	if err != nil {
		t.Fatalf("CurrentRun with text file: %v", err)
	}
	if current == nil || current.ID != rd.ID {
		t.Errorf("expected ID %q, got %v", rd.ID, current)
	}
}

func TestGitignoreCreated(t *testing.T) {
	m, dir := newManager(t)
	_ = m // Init already ran

	gitignore := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), ".forge/") {
		t.Errorf(".gitignore does not contain .forge/: %q", string(data))
	}
}

func TestGitignoreIdempotent(t *testing.T) {
	m, dir := newManager(t)

	// Run Init again — should not duplicate the entry.
	if err := m.Init(); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	gitignore := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(data), ".forge/")
	if count != 1 {
		t.Errorf("expected exactly 1 .forge/ entry, got %d:\n%s", count, string(data))
	}
}

func TestGitignoreAppend(t *testing.T) {
	_, dir := newManager(t)

	gitignore := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	// The entry should end with newline.
	if !strings.HasSuffix(string(data), "\n") {
		t.Error(".gitignore does not end with newline")
	}
}

func TestRunStartedAt(t *testing.T) {
	m, _ := newManager(t)
	before := time.Now().Add(-time.Second)
	rd, _ := m.CreateRun("time-test-001")
	after := time.Now().Add(time.Second)

	if rd.StartedAt.Before(before) || rd.StartedAt.After(after) {
		t.Errorf("StartedAt %v out of expected range [%v, %v]", rd.StartedAt, before, after)
	}
}
