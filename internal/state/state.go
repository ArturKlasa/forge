// Package state manages run directories and lifecycle markers for Forge.
package state

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/renameio/v2"
)

// Marker represents a run lifecycle state.
type Marker string

const (
	MarkerRunning        Marker = "RUNNING"
	MarkerAwaitingHuman  Marker = "AWAITING_HUMAN"
	MarkerPaused         Marker = "PAUSED"
	MarkerDone           Marker = "DONE"
	MarkerAborted        Marker = "ABORTED"
	MarkerFailed         Marker = "FAILED"
)

// allMarkers lists all valid lifecycle markers (in precedence order for cleanup).
var allMarkers = []Marker{
	MarkerRunning, MarkerAwaitingHuman, MarkerPaused,
	MarkerDone, MarkerAborted, MarkerFailed,
}

// RunDir encapsulates a run's directory path and metadata.
type RunDir struct {
	// ID is the run identifier (directory name under .forge/runs/).
	ID string
	// Path is the absolute path to the run directory.
	Path string
	// StartedAt is when this run directory was created.
	StartedAt time.Time
}

// Manager owns the .forge/ directory and manages run lifecycle.
type Manager struct {
	// forgeDir is the absolute path to .forge/.
	forgeDir string
}

// NewManager creates a Manager rooted at workDir/.forge/.
func NewManager(workDir string) *Manager {
	return &Manager{forgeDir: filepath.Join(workDir, ".forge")}
}

// Init ensures .forge/runs/ exists and updates .gitignore.
func (m *Manager) Init() error {
	if err := os.MkdirAll(filepath.Join(m.forgeDir, "runs"), 0o755); err != nil {
		return fmt.Errorf("create .forge/runs: %w", err)
	}
	return m.ensureGitignore()
}

// CreateRun creates a new run directory with the given ID and writes a RUNNING marker.
// It also updates the current symlink/pointer and sets up forge.log.
// The caller is responsible for writing the PID file if RUNNING.
func (m *Manager) CreateRun(id string) (*RunDir, error) {
	runPath := filepath.Join(m.forgeDir, "runs", id)
	if err := os.MkdirAll(filepath.Join(runPath, "iterations"), 0o755); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	rd := &RunDir{ID: id, Path: runPath, StartedAt: time.Now()}

	if err := m.writeMarker(rd, MarkerRunning); err != nil {
		return nil, err
	}

	if err := m.setCurrent(runPath); err != nil {
		return nil, err
	}

	return rd, nil
}

// CurrentRun returns the active run by reading the current pointer.
// Returns nil, nil when no run is current.
func (m *Manager) CurrentRun() (*RunDir, error) {
	runPath, err := m.getCurrent()
	if err != nil {
		return nil, err
	}
	if runPath == "" {
		return nil, nil
	}

	info, err := os.Stat(runPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	rd := &RunDir{
		ID:        filepath.Base(runPath),
		Path:      runPath,
		StartedAt: info.ModTime(),
	}
	return rd, nil
}

// Marker returns the current lifecycle marker for a run.
func (m *Manager) ReadMarker(rd *RunDir) (Marker, error) {
	for _, mk := range allMarkers {
		if _, err := os.Stat(filepath.Join(rd.Path, string(mk))); err == nil {
			return mk, nil
		}
	}
	return "", fmt.Errorf("no lifecycle marker found in %s", rd.Path)
}

// Transition atomically moves a run from its current marker to next.
// It writes the new marker file first, then removes the old one.
func (m *Manager) Transition(rd *RunDir, next Marker) error {
	return m.writeMarker(rd, next)
}

// writeMarker writes the new marker file atomically (via temp+rename), then
// removes all other marker files.
func (m *Manager) writeMarker(rd *RunDir, mk Marker) error {
	dst := filepath.Join(rd.Path, string(mk))

	// Write new marker atomically. Use renameio on Unix; on Windows renameio
	// falls back to MoveFileEx internally.
	if err := renameio.WriteFile(dst, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write marker %s: %w", mk, err)
	}

	// Remove all other markers — new one is already visible.
	for _, other := range allMarkers {
		if other == mk {
			continue
		}
		path := filepath.Join(rd.Path, string(other))
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove old marker %s: %w", other, err)
		}
	}
	return nil
}

// setCurrent updates the .forge/current pointer to point at runPath.
func (m *Manager) setCurrent(runPath string) error {
	current := filepath.Join(m.forgeDir, "current")
	if runtime.GOOS == "windows" {
		return renameio.WriteFile(current, []byte(runPath+"\n"), 0o644)
	}
	// Unix: use a symlink. Remove any existing entry first (could be old symlink
	// or text file from a previous Windows migration), then create new symlink.
	_ = os.Remove(current)
	return os.Symlink(runPath, current)
}

// getCurrent reads the .forge/current pointer.
func (m *Manager) getCurrent() (string, error) {
	current := filepath.Join(m.forgeDir, "current")
	if runtime.GOOS == "windows" {
		data, err := os.ReadFile(current)
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	// Unix: resolve symlink.
	target, err := os.Readlink(current)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		// If current exists but is not a symlink (e.g. text file from Windows run),
		// fall back to reading it as a plain file.
		data, rerr := os.ReadFile(current)
		if rerr != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	return target, nil
}

// ClearCurrent removes the .forge/current pointer (called on terminal state).
func (m *Manager) ClearCurrent() error {
	current := filepath.Join(m.forgeDir, "current")
	if err := os.Remove(current); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ensureGitignore idempotently adds ".forge/" to the repo's .gitignore.
func (m *Manager) ensureGitignore() error {
	// .gitignore lives one directory above .forge/ (at the repo root).
	repoRoot := filepath.Dir(m.forgeDir)
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	const entry = ".forge/"

	// Read existing content.
	f, err := os.Open(gitignorePath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == entry {
				return nil // already present
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		f.Close()
	}

	// Append (or create) the entry.
	gf, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer gf.Close()

	// Ensure we start on a new line.
	fi, err := gf.Stat()
	if err != nil {
		return err
	}
	prefix := ""
	if fi.Size() > 0 {
		// Peek last byte.
		buf := make([]byte, 1)
		rf, err := os.Open(gitignorePath)
		if err != nil {
			return err
		}
		if _, err := rf.ReadAt(buf, fi.Size()-1); err == nil && buf[0] != '\n' {
			prefix = "\n"
		}
		rf.Close()
	}

	_, err = fmt.Fprintf(gf, "%s%s\n", prefix, entry)
	return err
}

// ForgeDir returns the absolute path to .forge/.
func (m *Manager) ForgeDir() string {
	return m.forgeDir
}

// RunEntry describes a run found in .forge/runs/.
type RunEntry struct {
	ID        string
	Path      string
	StartedAt time.Time
	Marker    Marker
}

// ListRuns returns all run entries sorted by ID (lexicographic = chronological for UUID-v7).
func (m *Manager) ListRuns() ([]RunEntry, error) {
	runsDir := filepath.Join(m.forgeDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list runs: %w", err)
	}

	var runs []RunEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runPath := filepath.Join(runsDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		rd := &RunDir{ID: e.Name(), Path: runPath, StartedAt: info.ModTime()}
		mk, _ := m.ReadMarker(rd)
		runs = append(runs, RunEntry{
			ID:        e.Name(),
			Path:      runPath,
			StartedAt: info.ModTime(),
			Marker:    mk,
		})
	}
	return runs, nil
}
