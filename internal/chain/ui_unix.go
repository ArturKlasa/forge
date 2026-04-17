//go:build !windows

package chain

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// stdinReadKey reads a single raw keystroke from stdin using terminal raw mode.
func stdinReadKey() (byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Non-TTY fallback: read a byte normally (useful in pipe tests).
		buf := make([]byte, 1)
		_, err := os.Stdin.Read(buf)
		if err != nil {
			return 0, fmt.Errorf("read stdin: %w", err)
		}
		return buf[0], nil
	}

	old, err := term.MakeRaw(fd)
	if err != nil {
		return 0, fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(fd, old) //nolint:errcheck

	buf := make([]byte, 1)
	if _, err := os.Stdin.Read(buf); err != nil {
		return 0, fmt.Errorf("read key: %w", err)
	}
	return buf[0], nil
}
