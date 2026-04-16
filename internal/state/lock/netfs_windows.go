//go:build windows

package lock

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// isNetworkFS reports whether path is on a network drive.
func isNetworkFS(path string) bool {
	// UNC paths (\\server\share) are always remote.
	if strings.HasPrefix(path, `\\`) {
		return true
	}
	root := filepath.VolumeName(path) + `\`
	driveType := windows.GetDriveType(windows.StringToUTF16Ptr(root))
	return driveType == windows.DRIVE_REMOTE
}
