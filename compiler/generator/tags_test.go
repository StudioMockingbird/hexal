package generator

import (
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

// A registry lookup for an identity that was never collected is a compiler
// defect, never a reason to reconstruct a name or ordinal locally.
func TestRegistryMissingLookupFailsClosed(t *testing.T) {
	registry := buildTagRegistry(nil, nil)
	// A missing identity renders a stable placeholder, records the first
	// miss, and fails the phase when the registry settles; artifacts from
	// the doomed run are discarded.
	placeholder := registry.unionMemberTag(compilerTypes.Type{Name: "Int32", CName: "int32_t", CanonicalKey: "Int32", ScalarKind: compilerTypes.ScalarSignedInteger, Bits: 32})
	if placeholder != "hex_tag_type:Int32" {
		t.Fatalf("placeholder = %q", placeholder)
	}
	err := registry.settled()
	diagnostic, ok := err.(compilerTypes.Diagnostic)
	if !ok || diagnostic.Category != compilerTypes.UnknownError {
		t.Fatalf("settled() = %v, want an UnknownError diagnostic", err)
	}
	if !strings.Contains(diagnostic.Message, "missing from the program-wide registry") {
		t.Fatalf("message = %q", diagnostic.Message)
	}
}
