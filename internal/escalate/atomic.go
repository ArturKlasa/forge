package escalate

// AtomicWrite writes data to path atomically using a platform-specific shim.
// On Unix: google/renameio/v2 (write to temp in same dir, then rename).
// On Windows: natefinch/atomic (MoveFileEx with retry on sharing violations).
func AtomicWrite(path string, data []byte) error {
	return platformAtomicWrite(path, data)
}
