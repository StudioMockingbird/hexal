package lib

import "io/fs"

// RuntimePacks returns the embedded runtime-pack filesystem, rooted at the
// repository's lib/ directory. Callers select one target-profile
// subdirectory with fs.Sub; the driver never reads an adjacent file.
func RuntimePacks() fs.FS {
	return runtimePacks
}
