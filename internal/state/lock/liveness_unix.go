//go:build !windows

package lock

import (
	"errors"
	"syscall"
)

// isProcessAlive reports whether the process with the given PID is running.
// ESRCH means not found (dead); EPERM means it exists but we lack permission.
func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
