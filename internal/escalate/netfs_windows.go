//go:build windows

package escalate

import (
	"golang.org/x/sys/windows"
)

// IsNetworkFS returns true when path resolves to a network drive on Windows.
func IsNetworkFS(path string) bool {
	root, err := windows.UTF16PtrFromString(volumeRoot(path))
	if err != nil {
		return false
	}
	driveType := windows.GetDriveType(root)
	return driveType == windows.DRIVE_REMOTE
}

func volumeRoot(path string) string {
	if len(path) >= 3 && path[1] == ':' {
		return path[:3]
	}
	return path
}
