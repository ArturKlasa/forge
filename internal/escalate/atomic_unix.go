//go:build !windows

package escalate

import "github.com/google/renameio/v2"

func platformAtomicWrite(path string, data []byte) error {
	return renameio.WriteFile(path, data, 0o644)
}
