package modules

import "embed"

// Files contains the build inputs selected from the pinned upstream module.
//
//go:embed MANIFEST.sha256 MIMALLOC.md mimalloc/LICENSE mimalloc/include mimalloc/src
var Files embed.FS
