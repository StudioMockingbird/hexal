package compiler

import (
	compilerTypes "hexal/compiler/types"
)

// targetProfile is the compiler-private record for one qualified target.
// It carries only facts consumed by current checking or generation; a new
// fact enters here only when checking or generation consumes it. The public
// identity is compilerTypes.TargetProfileID. Callers select identities,
// never facts.
type targetProfile struct {
	identity      compilerTypes.TargetProfileID
	os            string
	architecture  string
	littleEndian  bool
	pointerWidth  int
	sizeWidth     int
	windowsTarget bool
	threading     string
	tls           bool
	fibers        bool
	nativeIO      bool
}

// targetProfiles is the registry of qualified targets, keyed by public
// identity. It currently holds exactly the one qualified profile.
var targetProfiles = map[compilerTypes.TargetProfileID]targetProfile{
	compilerTypes.TargetX86_64WindowsGNU: {
		identity:      compilerTypes.TargetX86_64WindowsGNU,
		os:            "windows",
		architecture:  "x86_64",
		littleEndian:  true,
		pointerWidth:  64,
		sizeWidth:     64,
		windowsTarget: true,
		threading:     "windows",
		tls:           true,
		fibers:        true,
		nativeIO:      true,
	},
}

// resolveTargetProfile maps a Project.Target identity to its private record.
// The empty identity selects no profile: the zero Project value stays
// host-neutral and preserves the existing deterministic generated-C
// contract. Any other identity must name a qualified target, or compilation
// fails before lexing.
func resolveTargetProfile(target compilerTypes.TargetProfileID) (targetProfile, error) {
	if target == "" {
		return targetProfile{}, nil
	}
	profile, ok := targetProfiles[target]
	if !ok {
		return targetProfile{}, projectDiagnostic("unknown target profile " + string(target))
	}
	return profile, nil
}
