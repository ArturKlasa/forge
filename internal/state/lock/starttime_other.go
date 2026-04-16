//go:build !linux && !darwin && !windows

package lock

import "fmt"

// processStartTimeNS is not implemented for this platform.
// Returns 0, which causes stale-lock comparison to always treat a running
// process as "unknown start time" → safe: the lock is not treated as stale.
func processStartTimeNS(pid int) (int64, error) {
	return 0, fmt.Errorf("processStartTimeNS not implemented on this platform")
}
