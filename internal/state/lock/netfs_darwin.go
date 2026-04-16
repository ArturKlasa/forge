//go:build darwin

package lock

import (
	"strings"

	"golang.org/x/sys/unix"
)

var networkFSTypes = []string{"nfs", "smbfs", "macfuse", "afpfs", "osxfuse", "fuse"}

// isNetworkFS reports whether path is on a network or FUSE filesystem.
func isNetworkFS(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	// Fstypename is [16]int8; convert to string.
	b := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	fstype := strings.ToLower(string(b))
	for _, t := range networkFSTypes {
		if strings.HasPrefix(fstype, t) {
			return true
		}
	}
	return false
}
