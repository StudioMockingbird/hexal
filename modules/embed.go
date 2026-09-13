package modules

import "embed"

// Files contains the build inputs selected from the pinned upstream module.
//
//go:embed MANIFEST.sha256 MIMALLOC.md LIBUV.md mimalloc/LICENSE mimalloc/include mimalloc/src libuv/LICENSE libuv/LICENSE-extra libuv/include libuv/src/*.c libuv/src/*.h libuv/src/win
var Files embed.FS
