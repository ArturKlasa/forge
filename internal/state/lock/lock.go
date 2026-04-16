// Package lock implements the single-run-per-repo enforcement for Forge.
// It combines gofrs/flock advisory locking with a PID+start-time sidecar to
// survive crashes and defeat PID reuse.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/google/renameio/v2"
)

const (
	lockFile    = "lock"
	sidecarFile = "lock.json"
)

// Sidecar is atomically written alongside the lock file. It carries enough
// information to detect stale locks after crashes and PID reuse.
type Sidecar struct {
	PID         int    `json:"pid"`
	RunID       string `json:"run_id"`
	StartTimeNS int64  `json:"start_time_ns"`
	Hostname    string `json:"hostname"`
}

// ErrLocked is returned when another live Forge run owns the lock.
type ErrLocked struct {
	Sidecar Sidecar
}

func (e *ErrLocked) Error() string {
	return fmt.Sprintf(
		"another forge run is active\n  Run: %s (PID %d)\n  Run 'forge status' to inspect.",
		e.Sidecar.RunID, e.Sidecar.PID,
	)
}

// Lock represents a successfully acquired single-run lock.
// Call Release when the run ends (defer in main).
type Lock struct {
	fl          *flock.Flock // nil in network-FS mode
	sidecarPath string
	networkFS   bool
}

// IsNetworkFS reports whether this lock was acquired in PID-file-only mode
// because the filesystem was detected as a network or FUSE mount.
func (l *Lock) IsNetworkFS() bool { return l.networkFS }

// isNetworkFSOverride is a testing seam; nil means use the real detection.
var isNetworkFSOverride *bool

// SetNetworkFSOverride overrides the network-FS detection for the current
// package. Returns a restore function that resets the override. For tests only.
func SetNetworkFSOverride(v bool) func() {
	old := isNetworkFSOverride
	isNetworkFSOverride = &v
	return func() { isNetworkFSOverride = old }
}

// isNetFS calls the platform detector unless overridden by tests.
func isNetFS(path string) bool {
	if isNetworkFSOverride != nil {
		return *isNetworkFSOverride
	}
	return isNetworkFS(path)
}

// Acquire tries to take the single-run lock inside forgeDir.
// runID is the identifier for the new run being started.
// Returns *ErrLocked when a live run owns the lock.
func Acquire(forgeDir, runID string) (*Lock, error) {
	lockPath := filepath.Join(forgeDir, lockFile)
	sidecarPath := filepath.Join(forgeDir, sidecarFile)

	if isNetFS(forgeDir) {
		return acquirePIDFileOnly(sidecarPath, runID)
	}

	fl := flock.New(lockPath)
	ok, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("try lock: %w", err)
	}

	if ok {
		if err := writeSidecar(sidecarPath, runID); err != nil {
			_ = fl.Unlock()
			return nil, err
		}
		return &Lock{fl: fl, sidecarPath: sidecarPath}, nil
	}

	// Locked by someone else — check if the lock is stale.
	if err := handleConflict(sidecarPath); err != nil {
		return nil, err
	}

	// Stale lock removed. Retry once.
	ok, err = fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock retry: %w", err)
	}
	if !ok {
		// A different process snuck in. Give a proper message.
		sc, _ := readSidecar(sidecarPath)
		return nil, &ErrLocked{Sidecar: sc}
	}

	if err := writeSidecar(sidecarPath, runID); err != nil {
		_ = fl.Unlock()
		return nil, err
	}
	return &Lock{fl: fl, sidecarPath: sidecarPath}, nil
}

// Release releases the lock and removes the sidecar file.
func (l *Lock) Release() error {
	_ = os.Remove(l.sidecarPath)
	if l.fl != nil {
		return l.fl.Unlock()
	}
	return nil
}

// handleConflict reads the sidecar of an already-locked file and decides
// whether the lock is stale. If stale, removes the sidecar so the caller
// can retry TryLock. Returns ErrLocked when the lock is legitimately active.
func handleConflict(sidecarPath string) error {
	sc, err := readSidecar(sidecarPath)
	if err != nil {
		// Can't read sidecar — treat as irresolvable.
		return fmt.Errorf("active lock but sidecar unreadable: %w", err)
	}

	hostname, _ := os.Hostname()
	if sc.Hostname != hostname {
		// Another host owns the lock on a shared FS — honour it.
		return &ErrLocked{Sidecar: sc}
	}

	if !isProcessAlive(sc.PID) {
		// PID gone → stale.
		_ = os.Remove(sidecarPath)
		return nil
	}

	// PID alive — compare start time to detect PID reuse.
	actualStart, err := processStartTimeNS(sc.PID)
	if err != nil || actualStart != sc.StartTimeNS {
		// Start time mismatch → PID was recycled; stale.
		_ = os.Remove(sidecarPath)
		return nil
	}

	return &ErrLocked{Sidecar: sc}
}

// acquirePIDFileOnly is the network-FS fallback: no flock, just sidecar.
func acquirePIDFileOnly(sidecarPath, runID string) (*Lock, error) {
	sc, err := readSidecar(sidecarPath)
	if err == nil {
		hostname, _ := os.Hostname()
		if sc.Hostname != hostname {
			return nil, &ErrLocked{Sidecar: sc}
		}
		// Check liveness + PID reuse.
		alive := isProcessAlive(sc.PID)
		if alive {
			actualStart, err := processStartTimeNS(sc.PID)
			if err == nil && actualStart == sc.StartTimeNS {
				return nil, &ErrLocked{Sidecar: sc}
			}
		}
		// Stale — remove and proceed.
		_ = os.Remove(sidecarPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read sidecar: %w", err)
	}

	if err := writeSidecar(sidecarPath, runID); err != nil {
		return nil, err
	}
	return &Lock{sidecarPath: sidecarPath, networkFS: true}, nil
}

// writeSidecar atomically writes the lock sidecar.
func writeSidecar(sidecarPath, runID string) error {
	hostname, _ := os.Hostname()
	startNS, _ := processStartTimeNS(os.Getpid())

	sc := Sidecar{
		PID:         os.Getpid(),
		RunID:       runID,
		StartTimeNS: startNS,
		Hostname:    hostname,
	}
	data, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("marshal sidecar: %w", err)
	}
	return renameio.WriteFile(sidecarPath, data, 0o644)
}

// readSidecar reads and parses the sidecar JSON.
func readSidecar(sidecarPath string) (Sidecar, error) {
	var sc Sidecar
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return sc, err
	}
	if err := json.Unmarshal(data, &sc); err != nil {
		return sc, fmt.Errorf("parse sidecar: %w", err)
	}
	return sc, nil
}
