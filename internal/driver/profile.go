package driver

// The driver's private qualification record for the one backend/target pair
// this release builds. The core compiler owns the target profile's semantic and
// generation facts; facts the compiler does not consume live here: Clang's
// toolchain target triple and the checked-in runtime-pack directory.
//
// There is exactly one qualified pair. Windows remains a core compiler target
// whose generated C is checked by pure-Go tests, but this release has no
// native Windows driver, so a Windows target request fails before any external
// command runs.

import (
	"fmt"

	compilerTypes "hexal/compiler/types"
)

// qualifiedTriple is Clang's toolchain target spelling for the one qualified
// profile. The Hexal identity and the triple are currently identical.
const qualifiedTriple = "x86_64-linux-gnu"

// driverProfile is one qualified (target profile, Clang triple) pair.
type driverProfile struct {
	profile compilerTypes.TargetProfileID
	triple  string
	// packDir is the directory name under the embedded runtime filesystem that
	// holds this profile's pack.
	packDir string
}

// driverProfiles is the registry of qualified pairs. It holds exactly the one
// pair this release implements.
var driverProfiles = map[compilerTypes.TargetProfileID]driverProfile{
	compilerTypes.TargetX86_64LinuxGNU: {
		profile: compilerTypes.TargetX86_64LinuxGNU,
		triple:  qualifiedTriple,
		packDir: string(compilerTypes.TargetX86_64LinuxGNU),
	},
}

// resolveProfile maps a requested target profile to its qualified pair. Every
// other identity, including the Windows compiler target, fails before any
// compilation with the one stable diagnostic.
func resolveProfile(target compilerTypes.TargetProfileID) (driverProfile, error) {
	profile, ok := driverProfiles[target]
	if !ok {
		return driverProfile{}, fmt.Errorf("target profile %s is not qualified for native builds in this release", target)
	}
	return profile, nil
}
