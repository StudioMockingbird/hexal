package generator

import (
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
	if !ok || diagnostic.Message.Category() != compilerTypes.UnknownError {
		t.Fatalf("settled() = %v, want an UnknownError diagnostic", err)
	}
	if diagnostic.Message.ID() != "internal.generator-invariant" {
		t.Fatalf("identity = %q", diagnostic.Message.ID())
	}
}
