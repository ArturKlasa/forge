package lock_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/arturklasa/forge/internal/state/lock"
)

// TestMain enables subprocess helper mode for concurrent-acquisition tests.
func TestMain(m *testing.M) {
	if os.Getenv("_FORGE_LOCK_HELPER") == "1" {
		runLockHelper()
		return
	}
	os.Exit(m.Run())
}

// runLockHelper acquires the lock, prints "READY\n", then waits for stdin EOF.
func runLockHelper() {
	dir := os.Getenv("_FORGE_LOCK_DIR")
	l, err := lock.Acquire(dir, "subprocess-run")
	if err != nil {
		fmt.Fprintln(os.Stderr, "lock helper: acquire failed:", err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "READY")
	_ = os.Stdout.Sync()

	// Block until parent closes stdin to signal release.
	_, _ = io.ReadAll(os.Stdin)
	_ = l.Release()
	os.Exit(0)
}

// TestAcquireRelease verifies basic lock acquisition and release.
func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()

	l, err := lock.Acquire(dir, "run-001")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	sidecarPath := filepath.Join(dir, "lock.json")
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	var sc lock.Sidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		t.Fatalf("sidecar JSON: %v", err)
	}
	if sc.PID != os.Getpid() {
		t.Errorf("sidecar PID: got %d, want %d", sc.PID, os.Getpid())
	}
	if sc.RunID != "run-001" {
		t.Errorf("sidecar RunID: got %q, want %q", sc.RunID, "run-001")
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(sidecarPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("sidecar not removed after release")
	}
}

// TestConcurrentAcquisition spawns a subprocess holding the lock and verifies
// that a second Acquire returns ErrLocked with the correct run ID.
func TestConcurrentAcquisition(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		"_FORGE_LOCK_HELPER=1",
		"_FORGE_LOCK_DIR="+dir,
	)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = os.Stderr
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait for subprocess to signal it holds the lock.
	readyCh := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		if sc.Scan() && sc.Text() == "READY" {
			pw.Close()
			readyCh <- nil
		} else {
			pw.CloseWithError(fmt.Errorf("subprocess did not print READY"))
			readyCh <- fmt.Errorf("subprocess not ready")
		}
	}()
	if err := <-readyCh; err != nil {
		t.Fatal(err)
	}

	// Acquire must fail with ErrLocked.
	_, acquireErr := lock.Acquire(dir, "new-run")
	var locked *lock.ErrLocked
	if !errors.As(acquireErr, &locked) {
		t.Fatalf("expected ErrLocked, got: %v", acquireErr)
	}
	if locked.Sidecar.RunID != "subprocess-run" {
		t.Errorf("ErrLocked.RunID: got %q, want %q", locked.Sidecar.RunID, "subprocess-run")
	}

	// Release subprocess.
	stdinPipe.Close()
	cmd.Wait() //nolint:errcheck

	// Now acquire must succeed.
	l2, err := lock.Acquire(dir, "recovered-run")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	defer l2.Release()
}

// TestStaleLockRecovery writes a sidecar for a dead PID and verifies that
// Acquire cleans it up and succeeds (using network-FS path for simplicity).
func TestStaleLockRecovery(t *testing.T) {
	dir := t.TempDir()
	restore := lock.SetNetworkFSOverride(true)
	defer restore()

	deadPID := findDeadPID(t)
	writeSidecarForTest(t, dir, lock.Sidecar{
		PID:         deadPID,
		RunID:       "stale-run",
		StartTimeNS: 1234567890,
		Hostname:    mustHostname(t),
	})

	l, err := lock.Acquire(dir, "fresh-run")
	if err != nil {
		t.Fatalf("expected stale lock recovery, got: %v", err)
	}
	defer l.Release()

	// Verify the new sidecar was written for the current process.
	data, _ := os.ReadFile(filepath.Join(dir, "lock.json"))
	var sc lock.Sidecar
	json.Unmarshal(data, &sc) //nolint:errcheck
	if sc.RunID != "fresh-run" {
		t.Errorf("new sidecar RunID: got %q, want %q", sc.RunID, "fresh-run")
	}
}

// TestPIDReuseDetection writes a sidecar with a live PID but wrong start time,
// simulating PID reuse. Acquire must treat it as stale (network-FS path).
func TestPIDReuseDetection(t *testing.T) {
	dir := t.TempDir()
	restore := lock.SetNetworkFSOverride(true)
	defer restore()

	writeSidecarForTest(t, dir, lock.Sidecar{
		PID:         os.Getpid(),
		RunID:       "recycled-pid-run",
		StartTimeNS: 42, // deliberately wrong
		Hostname:    mustHostname(t),
	})

	l, err := lock.Acquire(dir, "new-run")
	if err != nil {
		t.Fatalf("expected PID-reuse stale recovery, got: %v", err)
	}
	defer l.Release()
}

// TestHostnameMismatch writes a sidecar from a different host and verifies
// immediate ErrLocked refusal (network-FS path).
func TestHostnameMismatch(t *testing.T) {
	dir := t.TempDir()
	restore := lock.SetNetworkFSOverride(true)
	defer restore()

	writeSidecarForTest(t, dir, lock.Sidecar{
		PID:         os.Getpid(),
		RunID:       "remote-run",
		StartTimeNS: 999,
		Hostname:    "some-other-host-does-not-exist-xyzzy",
	})

	_, err := lock.Acquire(dir, "local-run")
	var locked *lock.ErrLocked
	if !errors.As(err, &locked) {
		t.Fatalf("expected ErrLocked for hostname mismatch, got: %v", err)
	}
	if locked.Sidecar.RunID != "remote-run" {
		t.Errorf("locked RunID: got %q, want %q", locked.Sidecar.RunID, "remote-run")
	}
}

// TestNetworkFSFallback enables the network-FS override and verifies that
// Acquire uses PID-file-only mode and correctly blocks a second acquisition.
func TestNetworkFSFallback(t *testing.T) {
	dir := t.TempDir()
	restore := lock.SetNetworkFSOverride(true)
	defer restore()

	l, err := lock.Acquire(dir, "net-run")
	if err != nil {
		t.Fatalf("Acquire on net FS: %v", err)
	}
	if !l.IsNetworkFS() {
		t.Error("expected IsNetworkFS() == true")
	}
	defer l.Release()

	// Second acquire must return ErrLocked.
	_, err2 := lock.Acquire(dir, "net-run-2")
	var locked *lock.ErrLocked
	if !errors.As(err2, &locked) {
		t.Fatalf("expected ErrLocked on second net-FS acquire, got: %v", err2)
	}
}

// TestErrLockedMessage verifies the error message format matches the design.
func TestErrLockedMessage(t *testing.T) {
	e := &lock.ErrLocked{Sidecar: lock.Sidecar{RunID: "my-run", PID: 12345}}
	msg := e.Error()
	for _, want := range []string{"another forge run is active", "my-run", "12345", "forge status"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
}

// --- helpers ---

func mustHostname(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func writeSidecarForTest(t *testing.T, dir string, sc lock.Sidecar) {
	t.Helper()
	data, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func findDeadPID(t *testing.T) int {
	t.Helper()
	for pid := 65535; pid > 65400; pid-- {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return pid
		}
	}
	t.Skip("could not find a dead PID for stale-lock test")
	return 0
}
