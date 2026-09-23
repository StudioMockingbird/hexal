//go:build windows

package driver

import (
	"syscall"
	"unsafe"
)

// replaceFile atomically replaces any existing destination with the source
// using MoveFileEx with MOVEFILE_REPLACE_EXISTING. os.Rename refuses to
// replace an existing destination on Windows; this is the operation the
// publication rule requires. MoveFileEx carries the replace semantics on
// every Windows release the toolchain supports.
func replaceFile(source, destination string) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	moveFileEx := kernel32.NewProc("MoveFileExW")
	source16, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destination16, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	const movefileReplaceExisting = 0x1
	const movefileWriteThrough = 0x8
	result, _, errno := moveFileEx.Call(
		uintptr(unsafe.Pointer(source16)),
		uintptr(unsafe.Pointer(destination16)),
		movefileReplaceExisting|movefileWriteThrough,
	)
	if result == 0 {
		return errno
	}
	return nil
}
