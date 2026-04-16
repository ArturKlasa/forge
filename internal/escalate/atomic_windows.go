//go:build windows

package escalate

import natom "github.com/natefinch/atomic"

func platformAtomicWrite(path string, data []byte) error {
	return natom.WriteFile(path, data)
}
