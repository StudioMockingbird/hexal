package driver

// The driver's private qualification record for each backend/target pair this
// release builds. The core compiler owns the target profile's semantic and
// generation facts; facts the compiler does not consume live here: Clang's
// toolchain target triple and the checked-in runtime-pack directory.
//
// Exactly two pairs are qualified: x86-64 Linux on linux/amd64 and x86-64
// Windows on windows/amd64. The driver links and runs what it produces, so
// each host builds only its own OS's target; cross-compilation is a separate
// decision that would need a bundled sysroot per target.

import (
	"fmt"

	compilerTypes "hexal/compiler/types"
)

// driverProfile is one qualified (target profile, Clang triple) pair.
type driverProfile struct {
	profile compilerTypes.TargetProfileID
	triple  string
	// packDir is the directory name under the embedded runtime filesystem that
	// holds this profile's pack.
	packDir string
}

// driverProfiles is the registry of qualified pairs.
var driverProfiles = map[compilerTypes.TargetProfileID]driverProfile{
	compilerTypes.TargetX86_64LinuxGNU: {
		profile: compilerTypes.TargetX86_64LinuxGNU,
		triple:  "x86_64-linux-gnu",
		packDir: string(compilerTypes.TargetX86_64LinuxGNU),
	},
	compilerTypes.TargetX86_64WindowsGNU: {
		profile: compilerTypes.TargetX86_64WindowsGNU,
		triple:  "x86_64-w64-windows-gnu",
		packDir: string(compilerTypes.TargetX86_64WindowsGNU),
	},
}

// qualifiedHosts pairs each target profile with the only host that builds it.
// The diagnostic names this set rather than one release's single option.
var qualifiedHosts = map[compilerTypes.TargetProfileID]string{
	compilerTypes.TargetX86_64LinuxGNU:   "linux/amd64",
	compilerTypes.TargetX86_64WindowsGNU: "windows/amd64",
}

// resolveProfile maps a requested target profile to its qualified pair. Every
// other identity fails before any compilation with the one stable diagnostic.
func resolveProfile(target compilerTypes.TargetProfileID) (driverProfile, error) {
	profile, ok := driverProfiles[target]
	if !ok {
		return driverProfile{}, fmt.Errorf("target profile %s is not qualified for native builds in this release", target)
	}
	return profile, nil
}

// featureDefines selects the feature-test preprocessor definitions generated C
// and the pack probe need under strict C23 for the selected target. Linux
// needs the POSIX feature-test level so pthread and netdb declarations are
// visible; Windows needs the pack's Windows defines so bundled headers and
// utf8proc accept static linkage and modern CRT defaults. It is a build
// selection, not a generated-C rule: the emitted source stays standard C23.
func featureDefines(target compilerTypes.TargetProfileID) []string {
	if isLinuxTarget(target) {
		return []string{"-D_POSIX_C_SOURCE=200809L"}
	}
	return []string{
		"-DWIN32_LEAN_AND_MEAN",
		"-D_WIN32_WINNT=0x0A00",
		"-D_CRT_DECLARE_NONSTDC_NAMES=0",
		"-D_CRT_SECURE_NO_WARNINGS",
		"-DUTF8PROC_STATIC",
	}
}
