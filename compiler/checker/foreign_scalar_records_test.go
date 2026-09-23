package checker

import (
	"testing"

	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// The checker's foreign scalar resolver must be exactly the target-qualified
// registry, asserted with type identity rather than the bridge tolerance
// foreignScalarCompatible allows. Together with the driver's test this is the
// duplicate-owner proof: neither site may keep a table the other lacks.
func TestForeignScalarFollowsTargetRecords(t *testing.T) {
	for _, testCase := range []struct {
		profile compilerTypes.TargetProfileID
		target  specdata.TargetID
	}{
		{compilerTypes.TargetX86_64WindowsGNU, specdata.TargetWindowsUCRT},
		{compilerTypes.TargetX86_64LinuxGNU, specdata.TargetLinuxGNU},
	} {
		for _, mapping := range specdata.CScalars(testCase.target) {
			want, ok := compilerTypes.ResolveSpecID(mapping.HexalType)
			if !ok {
				t.Fatalf("registry type %q does not resolve", mapping.HexalType)
			}
			got, ok := foreignScalarForSpelling(mapping.CSpelling, testCase.profile)
			if !ok {
				t.Fatalf("target %s: foreignScalarForSpelling(%q) reported unsupported", testCase.target, mapping.CSpelling)
			}
			if !compilerTypes.Equal(got, want) {
				t.Fatalf("target %s: foreignScalarForSpelling(%q) = %s, the registry says %s", testCase.target, mapping.CSpelling, got.Name, want.Name)
			}
		}
	}
}
