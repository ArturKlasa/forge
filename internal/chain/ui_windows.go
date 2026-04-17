//go:build windows

package chain

import (
	"fmt"
	"os"
)

// stdinReadKey reads a single byte from stdin on Windows (no raw mode needed for single char).
func stdinReadKey() (byte, error) {
	buf := make([]byte, 1)
	_, err := os.Stdin.Read(buf)
	if err != nil {
		return 0, fmt.Errorf("read stdin: %w", err)
	}
	return buf[0], nil
}
