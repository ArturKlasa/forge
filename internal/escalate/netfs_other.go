//go:build !linux && !windows

package escalate

// IsNetworkFS returns false on platforms without statfs detection.
// fsnotify polling fallback can be forced via SetNetworkFSOverride.
func IsNetworkFS(_ string) bool { return false }
