//go:build windows

package driver

import (
	"syscall"
	"unsafe"
)

// isJunction reports whether path is a directory junction or any other
// reparse point. Go's Lstat names symlinks but reports junctions as plain
// directories, and the walker would otherwise descend into them. The
// reparse-point attribute is the one marker both kinds share.
func isJunction(path string) bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getAttributes := kernel32.NewProc("GetFileAttributesW")
	name16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, _, _ := getAttributes.Call(uintptr(unsafe.Pointer(name16)))
	if attributes == uintptr(syscall.INVALID_FILE_ATTRIBUTES) {
		return false
	}
	const fileAttributeReparsePoint = 0x400
	return attributes&fileAttributeReparsePoint != 0
}
