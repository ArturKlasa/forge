//go:build darwin

package lock

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// processStartTimeNS returns the process start time as nanoseconds since the
// Unix epoch, using kern.proc.pid sysctl to read the KinfoProc struct.
func processStartTimeNS(pid int) (int64, error) {
	mib := []int32{unix.CTL_KERN, unix.KERN_PROC, unix.KERN_PROC_PID, int32(pid)}
	buf, err := unix.SysctlRaw("kern.proc.pid", pid)
	if err != nil {
		// Fallback: query via raw mib.
		_ = mib
		return 0, fmt.Errorf("sysctl kern.proc.pid.%d: %w", pid, err)
	}
	if len(buf) < int(unsafe.Sizeof(unix.KinfoProc{})) {
		return 0, fmt.Errorf("sysctl kern.proc.pid.%d: short response", pid)
	}
	kp := (*unix.KinfoProc)(unsafe.Pointer(&buf[0]))
	tv := kp.Proc.P_starttime
	return int64(tv.Sec)*1e9 + int64(tv.Usec)*1e3, nil
}
