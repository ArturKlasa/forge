//go:build linux

package lock

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// processStartTimeNS returns the process start time as nanoseconds since the
// Unix epoch, derived from /proc/<pid>/stat and /proc/stat btime.
func processStartTimeNS(pid int) (int64, error) {
	bootNS, err := bootTimeNS()
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	// Format: "pid (comm) state fields..."
	// comm may contain spaces and parens; find the last ')'.
	s := string(data)
	idx := strings.LastIndex(s, ")")
	if idx < 0 {
		return 0, fmt.Errorf("unexpected /proc/%d/stat format", pid)
	}

	// Fields after ") ": state(0) ppid(1) pgrp(2) session(3) tty_nr(4)
	// tpgid(5) flags(6) minflt(7) cminflt(8) majflt(9) cmajflt(10)
	// utime(11) stime(12) cutime(13) cstime(14) priority(15) nice(16)
	// num_threads(17) itrealvalue(18) starttime(19) ...
	fields := strings.Fields(s[idx+1:])
	if len(fields) < 20 {
		return 0, fmt.Errorf("/proc/%d/stat: too few fields (%d)", pid, len(fields))
	}

	startTicks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime from /proc/%d/stat: %w", pid, err)
	}

	// CLK_TCK is 100 on all modern Linux kernels (CONFIG_HZ_100 default).
	const clkTck = int64(100)

	return bootNS + (startTicks*1e9)/clkTck, nil
}

func bootTimeNS() (int64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "btime ") {
			parts := strings.Fields(line)
			if len(parts) != 2 {
				return 0, fmt.Errorf("unexpected btime format: %q", line)
			}
			btime, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return btime * 1e9, nil
		}
	}
	return 0, fmt.Errorf("btime not found in /proc/stat")
}
