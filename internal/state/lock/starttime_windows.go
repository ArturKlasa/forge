//go:build windows

package lock

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// processStartTimeNS returns the process start time as nanoseconds since the
// Unix epoch, using GetProcessTimes.
func processStartTimeNS(pid int) (int64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, fmt.Errorf("OpenProcess %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, fmt.Errorf("GetProcessTimes %d: %w", pid, err)
	}
	// FILETIME is 100-nanosecond intervals since 1601-01-01 UTC.
	// Convert to Unix nanoseconds by subtracting the Windows epoch offset.
	const windowsToUnixEpochNS = 116444736000000000 * 100 // nanoseconds
	ft := int64(creation.HighDateTime)<<32 | int64(creation.LowDateTime)
	return ft*100 - windowsToUnixEpochNS, nil
}
