//go:build linux

package escalate

import (
	"golang.org/x/sys/unix"
)

// known network/remote filesystem magic numbers on Linux.
var networkMagic = map[int64]bool{
	0x6969:     true, // NFS
	0xFF534D42: true, // CIFS/SMB
	0x517B:     true, // SMB (older)
	0x65735546: true, // FUSE
	0x65735543: true, // FUSE_CTL
}

// IsNetworkFS returns true when path is on a remote or FUSE filesystem.
func IsNetworkFS(path string) bool {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false
	}
	return networkMagic[stat.Type]
}
