package driver

// The driver's private qualification record for the one backend/profile pair.
// The core compiler owns only the target profile's semantic and generation
// facts; facts the compiler does not consume live here: Zig's toolchain target
// spelling and the checked-in runtime-pack directory.

import (
	"fmt"

	compilerTypes "hexal/compiler/types"
)

// zigTargetSpelling is Zig's toolchain target spelling for the qualified
// profile. It intentionally omits the Hexal CRT suffix; the profile record
// establishes that this Zig target is MinGW-w64 over UCRT.
const zigTargetSpelling = "x86_64-windows-gnu"

// zigProfile is one qualified (target profile, Zig) pair.
type zigProfile struct {
	profile   compilerTypes.TargetProfileID
	zigTarget string
	// packDir is the directory name under the runtime root that holds this
	// profile's checked-in pack.
	packDir string
}

// zigProfiles is the registry of qualified pairs. It holds exactly the one
// pair this compiler implements.
var zigProfiles = map[compilerTypes.TargetProfileID]zigProfile{
	compilerTypes.TargetX86_64WindowsGNU: {
		profile:   compilerTypes.TargetX86_64WindowsGNU,
		zigTarget: zigTargetSpelling,
		packDir:   string(compilerTypes.TargetX86_64WindowsGNU),
	},
}

// resolveZigProfile maps a requested target profile to its qualified Zig pair.
// A profile with no pair fails before any compilation.
func resolveZigProfile(target compilerTypes.TargetProfileID) (zigProfile, error) {
	profile, ok := zigProfiles[target]
	if !ok {
		return zigProfile{}, fmt.Errorf("target profile %s is not qualified", target)
	}
	return profile, nil
}
