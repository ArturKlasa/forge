//go:build !windows

package proc

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// platformStart sets SysProcAttr so the child runs in its own process group
// and session. This ensures that signals sent to -pid reach the entire tree.
func platformStart(w *Wrapper) {
	w.cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Setsid:  true,
	}
}

// Terminate gracefully stops the process tree by sending SIGTERM to the process
// group. After the grace period it escalates to SIGKILL.
func (w *Wrapper) Terminate() error {
	w.forgeKilled.Store(true)

	pid := w.cmd.Process.Pid
	// Send SIGTERM to the entire process group (negative PID).
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	// After grace period, send SIGKILL to whatever remains.
	go func() {
		time.Sleep(w.gracePeriod)
		syscall.Kill(-pid, syscall.SIGKILL) //nolint:errcheck
	}()

	return nil
}

// Kill immediately sends SIGKILL to the process group.
func (w *Wrapper) Kill() error {
	w.forgeKilled.Store(true)
	pid := w.cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// classifyExit inspects the process state and the forgeKilled flag to produce
// an ExitClassification for the process that just exited.
func classifyExit(w *Wrapper, waitErr error) Result {
	ps := w.cmd.ProcessState
	if ps == nil {
		// cmd.Wait failed before the process even produced a state.
		return Result{ExitCode: -1, Classification: ExitIterationFail, Err: waitErr}
	}

	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok {
		// Fallback: use the plain exit code.
		code := ps.ExitCode()
		if code == 0 {
			return Result{ExitCode: 0, Classification: ExitNormal}
		}
		return Result{ExitCode: code, Classification: ExitIterationFail, Err: waitErr}
	}

	if ws.Signaled() {
		sig := ws.Signal()
		if w.forgeKilled.Load() {
			return Result{
				ExitCode:       -1,
				Classification: ExitForgeTerminated,
				Signal:         sig,
			}
		}
		return Result{
			ExitCode:       -1,
			Classification: ExitExternalSignal,
			Signal:         sig,
		}
	}

	code := ws.ExitStatus()
	if code == 0 {
		return Result{ExitCode: 0, Classification: ExitNormal}
	}
	return Result{ExitCode: code, Classification: ExitIterationFail, Err: waitErr}
}

// platformPostStart is a no-op on Unix; process-group setup is done entirely
// in platformStart via SysProcAttr before cmd.Start().
func platformPostStart(_ *Wrapper) error { return nil }

// newCmdForTest is a helper used only in tests to build a *exec.Cmd.
// It exists here (rather than in the test file) to avoid import cycles when
// tests need access to unexported helpers, but it is not part of the public API.
func newCmdForTest(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// signalFromOS returns the os.Signal corresponding to a syscall.Signal.
// Exposed for use in tests that inspect Result.Signal.
func signalFromOS(s syscall.Signal) os.Signal { return s }
