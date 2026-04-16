//go:build !linux && !darwin && !windows

package lock

// isNetworkFS always returns false on unsupported platforms.
func isNetworkFS(_ string) bool { return false }
