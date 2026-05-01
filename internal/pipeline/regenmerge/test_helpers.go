package regenmerge

import "os"

// osMkdirAll is a thin wrapper used by tests; isolated here so it can be
// removed if a test-only file replaces it.
func osMkdirAll(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
