//go:build linux

package lock

import "golang.org/x/sys/unix"

// Network / FUSE filesystem magic numbers where flock is unreliable.
const (
	nfsSuperMagic  = 0x6969
	smbSuperMagic  = 0x517B
	cifsMagic      = 0xFF534D42
	fuseSuperMagic = 0x65735546
	smb2Magic      = 0xFE534D42
)

// isNetworkFS reports whether path is on a network or FUSE filesystem.
func isNetworkFS(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	switch st.Type {
	case nfsSuperMagic, smbSuperMagic, cifsMagic, fuseSuperMagic, smb2Magic:
		return true
	}
	return false
}
