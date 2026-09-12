//go:build !windows

package driver

// isJunction always fails closed to false off Windows: only the Windows
// build can encounter a junction, and only it compiles this file's caller.
func isJunction(path string) bool {
	return false
}
