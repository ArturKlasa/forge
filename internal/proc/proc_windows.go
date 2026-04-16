//go:build windows

package proc

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandles maps each Wrapper to its Windows Job Object handle so that
// Terminate() / Kill() can close it, triggering JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
var jobHandles = struct {
	mu      sync.Mutex
	handles map[*Wrapper]windows.Handle
}{handles: make(map[*Wrapper]windows.Handle)}

// platformStart configures SysProcAttr to start the child process suspended
// so we can assign it to a Job Object before it runs any code.
func platformStart(w *Wrapper) {
	w.cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_SUSPENDED,
	}
}

// platformPostStart is called immediately after cmd.Start() succeeds.
// It creates the Job Object, assigns the suspended process, then resumes it.
func platformPostStart(w *Wrapper) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("proc: CreateJobObject: %w", err)
	}

	// Configure the job to kill all members when the handle is closed.
	extInfo := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&extInfo)),
		uint32(unsafe.Sizeof(extInfo)),
	)
	if err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("proc: SetInformationJobObject: %w", err)
	}

	// Open a handle to the child process by PID so we can assign it to the job.
	ph, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(w.cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("proc: OpenProcess: %w", err)
	}
	defer windows.CloseHandle(ph) //nolint:errcheck
	if err := windows.AssignProcessToJobObject(job, ph); err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("proc: AssignProcessToJobObject: %w", err)
	}

	if err := resumeProcess(w.cmd.Process.Pid); err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("proc: resumeProcess: %w", err)
	}

	jobHandles.mu.Lock()
	jobHandles.handles[w] = job
	jobHandles.mu.Unlock()

	return nil
}

// resumeProcess resumes all threads belonging to pid.
func resumeProcess(pid int) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap) //nolint:errcheck

	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	if err := windows.Thread32First(snap, &te); err != nil {
		return err
	}
	for {
		if int(te.OwnerProcessID) == pid {
			th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
			if err == nil {
				windows.ResumeThread(th)  //nolint:errcheck
				windows.CloseHandle(th)   //nolint:errcheck
			}
		}
		if err := windows.Thread32Next(snap, &te); err != nil {
			break
		}
	}
	return nil
}

// Terminate closes the Job Object handle (killing the process tree) and then
// calls cmd.Process.Kill() as a backstop.
func (w *Wrapper) Terminate() error {
	w.forgeKilled.Store(true)
	closeJob(w)
	return w.cmd.Process.Kill()
}

// Kill immediately closes the Job Object and kills the root process.
func (w *Wrapper) Kill() error {
	w.forgeKilled.Store(true)
	closeJob(w)
	return w.cmd.Process.Kill()
}

func closeJob(w *Wrapper) {
	jobHandles.mu.Lock()
	job, ok := jobHandles.handles[w]
	if ok {
		delete(jobHandles.handles, w)
	}
	jobHandles.mu.Unlock()
	if ok {
		windows.CloseHandle(job) //nolint:errcheck
	}
}

// classifyExit for Windows: signal information is not available via WaitStatus
// the same way as on Unix, so we rely on forgeKilled and the exit code.
func classifyExit(w *Wrapper, waitErr error) Result {
	ps := w.cmd.ProcessState
	if ps == nil {
		return Result{ExitCode: -1, Classification: ExitIterationFail, Err: waitErr}
	}
	code := ps.ExitCode()
	if w.forgeKilled.Load() {
		return Result{ExitCode: code, Classification: ExitForgeTerminated}
	}
	if code == 0 {
		return Result{ExitCode: 0, Classification: ExitNormal}
	}
	return Result{ExitCode: code, Classification: ExitIterationFail, Err: waitErr}
}

// newCmdForTest is a helper used only in tests.
func newCmdForTest(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
