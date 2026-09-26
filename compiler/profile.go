package compiler

import (
	diagnostics "hexal/compiler/diagnostics"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// targetFactID maps each language-visible target identity to its specdata
// facts record. Identity stays in compiler/types and facts stay in specdata;
// this table is the only place the two names meet.
var targetFactID = map[compilerTypes.TargetProfileID]specdata.TargetID{
	compilerTypes.TargetX86_64WindowsGNU: specdata.TargetWindowsUCRT,
	compilerTypes.TargetX86_64LinuxGNU:   specdata.TargetLinuxGNU,
}

// resolveTargetProfile maps a Project.Target identity to its facts record.
// The empty identity selects no profile: the zero Project value stays
// host-neutral and preserves the existing deterministic generated-C
// contract. Any other identity must name a qualified target, or compilation
// fails before lexing.
//
// The record's ten facts describe the target rather than steer generation:
// generated C carries both platform branches for the C compiler's own macros to
// select, and size_of and align_of lower to C sizeof and alignof, so reading
// them from Go would mean deleting a branch the C compiler still needs. The
// record is still load-bearing, because this lookup is what proves a requested
// target is one the compiler holds facts for, and it fails closed when it is
// not.
func resolveTargetProfile(target compilerTypes.TargetProfileID) (specdata.TargetFacts, error) {
	if target == "" {
		return specdata.TargetFacts{}, nil
	}
	id, known := targetFactID[target]
	if !known {
		return specdata.TargetFacts{}, compilerTypes.Locationless(diagnostics.UnknownTargetProfile(string(target)))
	}
	facts, ok := specdata.Target(id)
	if !ok {
		// Validate() rejects a registry missing a key its consumers name, so
		// only a compiler defect reaches here; report it as one rather than as
		// an unknown target the caller could act on.
		return specdata.TargetFacts{}, compilerTypes.Locationless(diagnostics.MissingTargetRecord())
	}
	return facts, nil
}
