package compiler

// C interoperability's compiler-owned half: the deterministic reserved
// binding-key derivation. The compiler never reads a header; the driver
// prepares the binding module that `DiscoverCImports` requests and inserts it
// into a copied source map.

import (
	"crypto/sha256"
	"encoding/hex"
)

// CImportRequest is one reachable C header request: the header payload without
// delimiters and whether it was written in the system `<...>` form. The
// system and quoted forms are distinct identities.
type CImportRequest struct {
	Header string
	System bool
}

// CBindingKey derives the reserved logical key of one C header's prepared
// binding module: hexalc/h<sha256(target NUL form NUL payload)>.hex. The
// leading h keeps the digest component a Hexal identifier. The target
// participates because one header may normalize platform integers and
// conditional declarations differently per target.
func CBindingKey(profile string, request CImportRequest) string {
	form := "quoted"
	if request.System {
		form = "system"
	}
	digest := sha256.Sum256([]byte(profile + "\x00" + form + "\x00" + request.Header))
	return "hexalc/h" + hex.EncodeToString(digest[:]) + ".hex"
}
