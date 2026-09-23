package driver

import (
	"fmt"
	"testing"

	"hexal/compiler"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// scalarTargetProfiles are the profile identities whose data model the registry
// describes, each beside its registry key.
var scalarTargetProfiles = []struct {
	profile compilerTypes.TargetProfileID
	target  specdata.TargetID
}{
	{compilerTypes.TargetX86_64WindowsGNU, specdata.TargetWindowsUCRT},
	{compilerTypes.TargetX86_64LinuxGNU, specdata.TargetLinuxGNU},
}

// The C scalar mapping has one owner. The driver's header normalizer and the
// checker's foreign scalar resolver must both reproduce the target-qualified
// registry exactly; this test drives the driver's own resolution and the
// checker through the exported compiler API, so a private table reappearing on
// either side fails here. The checker's own package test asserts the same
// equality without the compile round trip.
func TestScalarMappingsAreTheSingleOwner(t *testing.T) {
	for _, testCase := range scalarTargetProfiles {
		importer := newImporter(compiler.CImportRequest{Header: "probe.h"}, newLineIndex(""), string(testCase.profile), nil)
		for _, mapping := range specdata.CScalars(testCase.target) {
			hexal, ok := importer.resolveBase(mapping.CSpelling)
			if !ok {
				t.Fatalf("target %s: the driver reports %q unsupported", testCase.target, mapping.CSpelling)
			}
			if hexal != string(mapping.HexalType) {
				t.Fatalf("target %s: the driver maps %q to %s, the registry says %s", testCase.target, mapping.CSpelling, hexal, mapping.HexalType)
			}
			// The checker resolves the same spelling through the public API: a
			// foreign parameter of the record's own Hexal type must prove.
			source := fmt.Sprintf("extern c from <probe.h> do\n    fun probe(value: %s as \"%s\"): Int32\nend\nlet keep: Int32 = 1\n", mapping.HexalType, mapping.CSpelling)
			result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{Target: testCase.profile})
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("target %s: the checker rejects %q for %s: %v", testCase.target, mapping.CSpelling, mapping.HexalType, result.Stderr)
			}
		}
	}
}

// An empty target is unreachable in production, but pure-Go normalization
// tests pass it; it keeps the historical LLP64 selection through the registry
// rather than a driver conditional.
func TestEmptyTargetSelectsLLP64Records(t *testing.T) {
	importer := newImporter(compiler.CImportRequest{Header: "probe.h"}, newLineIndex(""), "", nil)
	long, ok := importer.resolveBase("long")
	if !ok || long != "Int32" {
		t.Fatalf("resolveBase(long) = %q, %v, want Int32, true", long, ok)
	}
}
