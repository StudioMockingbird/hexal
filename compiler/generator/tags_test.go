package generator

import (
	"testing"

	compilerTypes "hexal/compiler/types"
)

// A registry lookup for an identity that was never collected is a compiler
// defect, never a reason to reconstruct a name or ordinal locally.
func TestRegistryMissingLookupFailsClosed(t *testing.T) {
	registry := buildTagRegistry(nil, nil)
	defer func() {
		recovered := recover()
		diagnostic, ok := recovered.(compilerTypes.Diagnostic)
		if !ok || diagnostic.Category != compilerTypes.UnknownError {
			t.Fatalf("missing lookup must panic an UnknownError diagnostic, got %v", recovered)
		}
	}()
	registry.unionMemberTag(compilerTypes.Type{Name: "Int32", CName: "int32_t", CanonicalKey: "Int32", ScalarKind: compilerTypes.ScalarSignedInteger, Bits: 32})
}
