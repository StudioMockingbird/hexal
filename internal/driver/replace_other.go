//go:build !windows

package driver

import "os"

// replaceFile atomically replaces any existing destination. POSIX rename is
// atomic over an existing destination; this file exists only so non-Windows
// builds compile.
func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
