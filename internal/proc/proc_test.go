//go:build !windows

package proc_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arturklasa/forge/internal/proc"
)

// setpgidAllowed is set by TestMain to indicate whether the running environment
// permits SysProcAttr{Setpgid: true, Setsid: true}. Some sandbox environments
// (Docker no-new-privileges, certain seccomp profiles) block these syscalls.
var setpgidAllowed bool

func TestMain(m *testing.M) {
	// Probe: try to start a trivial process with the same SysProcAttr that the
	// Wrapper uses on Unix. If it fails, mark the capability as unavailable.
	probe := exec.Command("echo", "probe")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setsid: true}
	if err := probe.Run(); err == nil {
		setpgidAllowed = true
	}
	os.Exit(m.Run())
}

// skipIfNoSetpgid skips a test when the environment cannot run processes
// with Setpgid/Setsid (e.g. restricted sandbox environments).
func skipIfNoSetpgid(t *testing.T) {
	t.Helper()
	if !setpgidAllowed {
		t.Skip("setpgid/setsid not permitted in this environment — skipping subprocess test")
	}
}

// TestBasicStartWait spawns "echo hello" and checks it exits normally.
func TestBasicStartWait(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	cmd := exec.Command("echo", "hello")
	w := proc.New(cmd)

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result := w.Wait()

	if result.ExitCode != 0 {
		t.Errorf("want exit 0, got %d", result.ExitCode)
	}
	if result.Classification != proc.ExitNormal {
		t.Errorf("want ExitNormal, got %v", result.Classification)
	}
	if result.Err != nil {
		t.Errorf("unexpected error: %v", result.Err)
	}
}

// TestTerminateKillsTree spawns a bash that forks child processes and verifies
// that Terminate() kills the entire process group and classifies exit correctly.
func TestTerminateKillsTree(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	// This script forks two background sleeps and waits for them.
	cmd := exec.Command("bash", "-c", "sleep 100 & sleep 200 & wait")
	w := proc.New(cmd, proc.WithGracePeriod(2*time.Second))

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Let the shell and children start.
	time.Sleep(200 * time.Millisecond)

	if err := w.Terminate(); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	done := make(chan proc.Result, 1)
	go func() { done <- w.Wait() }()

	select {
	case result := <-done:
		if result.Classification != proc.ExitForgeTerminated {
			t.Errorf("want ExitForgeTerminated, got %v (signal=%v)", result.Classification, result.Signal)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait timed out — process was not killed")
	}
}

// TestSIGKILLEscalation spawns a bash that ignores SIGTERM. With a very short
// grace period the SIGKILL timer should fire and kill the process.
func TestSIGKILLEscalation(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	// trap '' TERM makes the shell ignore SIGTERM.
	cmd := exec.Command("bash", "-c", "trap '' TERM; sleep 100")
	w := proc.New(cmd, proc.WithGracePeriod(500*time.Millisecond))

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := w.Terminate(); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	done := make(chan proc.Result, 1)
	go func() { done <- w.Wait() }()

	select {
	case result := <-done:
		if result.Classification != proc.ExitForgeTerminated {
			t.Errorf("want ExitForgeTerminated, got %v (signal=%v)", result.Classification, result.Signal)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Wait timed out — SIGKILL escalation did not fire")
	}
}

// TestRingBufferRetainsLastN spawns a process that writes 1 MiB to stdout and
// verifies that the ring buffer holds at most 64 KiB.
func TestRingBufferRetainsLastN(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	// Write 1 MiB of 'y\n' lines via yes | head -c 1048576.
	cmd := exec.Command("bash", "-c", "yes | head -c 1048576")
	w := proc.New(cmd) // default 64 KiB ring buffer

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result := w.Wait()
	if result.Classification != proc.ExitNormal {
		t.Errorf("want ExitNormal, got %v (err=%v)", result.Classification, result.Err)
	}

	const wantMax = 64 * 1024
	got := w.RingBuffer().Len()
	if got > wantMax {
		t.Errorf("ring buffer len %d > max %d — buffer is not capping output", got, wantMax)
	}
	if got == 0 {
		t.Error("ring buffer is empty — stdout was not captured")
	}

	// The ring buffer should contain exactly wantMax bytes since 1 MiB >> 64 KiB.
	if got != wantMax {
		t.Errorf("want ring buffer full (%d bytes), got %d", wantMax, got)
	}
}

// TestExternalSignalClassification directly validates the classification logic
// for signals that Forge did not send, without needing a subprocess trick.
// We create a Wrapper, leave forgeKilled=false, and confirm that a simulated
// Signaled WaitStatus produces ExitExternalSignal.
//
// Because classifyExit is unexported, we drive it indirectly: spawn a tiny
// helper process, send it a signal from *outside* the Wrapper (so forgeKilled
// stays false), and assert on the result.
func TestExternalSignalClassification(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	cmd := exec.Command("sleep", "60")
	w := proc.New(cmd)

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Send SIGTERM directly to the process without using Wrapper.Terminate(),
	// so forgeKilled remains false.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	result := w.Wait()
	if result.Classification != proc.ExitExternalSignal {
		t.Errorf("want ExitExternalSignal, got %v (signal=%v, forgeKilled should be false)", result.Classification, result.Signal)
	}
	if result.Signal == nil {
		t.Error("want non-nil Signal, got nil")
	}
}

// TestNonZeroExit spawns 'bash -c "exit 42"' and checks ExitIterationFail + code 42.
func TestNonZeroExit(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	cmd := exec.Command("bash", "-c", "exit 42")
	w := proc.New(cmd)

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result := w.Wait()
	if result.Classification != proc.ExitIterationFail {
		t.Errorf("want ExitIterationFail, got %v", result.Classification)
	}
	if result.ExitCode != 42 {
		t.Errorf("want exit code 42, got %d", result.ExitCode)
	}
}

// TestRingBufferBasic exercises the ring buffer directly to verify wrap-around
// and Bytes() ordering — no subprocess needed, always runs.
func TestRingBufferBasic(t *testing.T) {
	t.Parallel()

	rb := proc.NewRingBuffer(8)

	// Write 5 bytes; no wrap yet.
	rb.Write([]byte("hello")) //nolint:errcheck
	if rb.Len() != 5 {
		t.Errorf("Len: want 5, got %d", rb.Len())
	}
	if got := string(rb.Bytes()); got != "hello" {
		t.Errorf("Bytes: want %q, got %q", "hello", got)
	}

	// Write 5 more bytes; buffer wraps (capacity 8).
	rb.Write([]byte("world!")) //nolint:errcheck  -- 11 total, only last 8 retained
	if rb.Len() != 8 {
		t.Errorf("Len after wrap: want 8, got %d", rb.Len())
	}
	// "helloworld!" is 11 bytes; last 8 = "loworld!"
	want := "loworld!"
	if got := string(rb.Bytes()); got != want {
		t.Errorf("Bytes after wrap: want %q, got %q", want, got)
	}
}

// TestRingBufferLargeWrite verifies that a write larger than the buffer capacity
// is handled correctly — only the last N bytes are retained.
func TestRingBufferLargeWrite(t *testing.T) {
	t.Parallel()

	rb := proc.NewRingBuffer(4)
	n, err := rb.Write([]byte("abcdefgh")) // 8 bytes into a 4-byte buffer
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 8 {
		t.Errorf("Write returned %d, want 8", n)
	}
	if rb.Len() != 4 {
		t.Errorf("Len: want 4, got %d", rb.Len())
	}
	// Last 4 bytes of "abcdefgh" are "efgh".
	if got := string(rb.Bytes()); got != "efgh" {
		t.Errorf("Bytes: want %q, got %q", "efgh", got)
	}
}

// TestWaitIsIdempotent verifies that calling Wait() multiple times returns
// the same cached Result without blocking.
func TestWaitIsIdempotent(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	cmd := exec.Command("echo", "idempotent")
	w := proc.New(cmd)

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	r1 := w.Wait()
	r2 := w.Wait()

	if r1.ExitCode != r2.ExitCode || r1.Classification != r2.Classification {
		t.Errorf("Wait is not idempotent: r1=%+v r2=%+v", r1, r2)
	}
}

// TestStdoutWriterOption verifies that WithStdoutWriter delivers a copy of
// stdout to the supplied writer in addition to the ring buffer.
func TestStdoutWriterOption(t *testing.T) {
	t.Parallel()
	skipIfNoSetpgid(t)

	var buf bytes.Buffer
	cmd := exec.Command("echo", "forwarded")
	w := proc.New(cmd, proc.WithStdoutWriter(&buf))

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	w.Wait()

	if !strings.Contains(buf.String(), "forwarded") {
		t.Errorf("extra writer did not receive stdout; got %q", buf.String())
	}
}
