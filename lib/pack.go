// Package lib embeds the checked-in native runtime packs in the compiler
// binary. A release build carries its native inputs, so a user build needs no
// adjacent lib/ tree and no -runtime-dir override. The embedded filesystem is
// immutable; the driver materializes only demanded entries into its private
// staging directory.
//
// A pack directory must exist at build time; an absent embed target fails the
// whole module build rather than producing a compiler with no runtime inputs.
package lib

import "embed"

//go:embed x86_64-linux-gnu
var runtimePacks embed.FS
