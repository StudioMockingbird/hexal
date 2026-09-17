package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hexal/internal/backend"
	"hexal/internal/version"

	compilerTypes "hexal/compiler/types"
)

// DoctorOptions selects the configuration doctor verifies. It carries the
// same required compiler and target as a build plus the optional runtime-root
// override.
type DoctorOptions struct {
	CompilerPath string
	Target       compilerTypes.TargetProfileID
	RuntimeDir   string
}

// Doctor verifies the local setup without building the user's project and
// without discovering it: there is no project lookup here at all. It
// returns a version report alongside the problems. The Hexal version line
// is always present, even when backend selection fails; the Zig and target
// lines appear only once a backend resolves. Problems accumulate every
// independently checkable failure rather than stopping at the first. Only
// a missing backend short-circuits the checks, because every check below
// needs one; even then the failure is reported once as a list, not an
// error.
func Doctor(options DoctorOptions) (report []string, problems []string) {
	report = []string{"Hexal: " + version.String()}

	profile, profileErr := resolveZigProfile(options.Target)
	if profileErr != nil {
		problems = append(problems, profileErr.Error())
		return report, problems
	}
	if err := checkHost(); err != nil {
		problems = append(problems, err.Error())
	}
	backend, err := resolveBackend(options.CompilerPath, profile)
	if err != nil {
		problems = append(problems, err.Error())
		return report, problems
	}
	report = append(report, "Zig: "+backend.Version, "Target: "+string(profile.profile))

	pinned := backendPkgPinnedVersion()
	if backend.Version != pinned {
		problems = append(problems, fmt.Sprintf(
			"backend is Zig %s, but this release pins %s (a newer version does not satisfy the pin; bumping it is a deliberate re-qualification)",
			backend.Version, pinned))
	}

	// A Zig distribution is an executable plus a required lib/ tree. A zig
	// whose lib/ is missing or unreadable fails at compile time with a far
	// less obvious message than this one.
	switch {
	case backend.LibDir == "":
		problems = append(problems, "could not read lib_dir from `zig env`; the distribution may be incomplete")
	default:
		if info, statErr := os.Stat(backend.LibDir); statErr != nil || !info.IsDir() {
			problems = append(problems, fmt.Sprintf(
				"backend lib_dir %q is not a readable directory; the distribution is incomplete and cannot be relocated away from its lib/ tree",
				backend.LibDir))
		}
	}

	if probeErr := facilityProbe(backend); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	if probeErr := linkProbe(backend); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		if probeErr := modeOptionProbe(backend, mode); probeErr != nil {
			problems = append(problems, probeErr.Error())
		}
	}
	if packErr := doctorRuntimePack(backend, options); packErr != nil {
		problems = append(problems, packErr.Error())
	}

	return report, problems
}

// doctorRuntimePack fully verifies the selected profile's checked-in pack and
// proves both archives and every declared system library compile, link, and
// run through the selected compiler. A normal build never does this.
func doctorRuntimePack(selected *backend.Backend, options DoctorOptions) error {
	profile, err := resolveZigProfile(options.Target)
	if err != nil {
		return err
	}
	root, err := resolveRuntimeRoot(options.RuntimeDir)
	if err != nil {
		return err
	}
	directory := packDirectory(root, profile)
	raw, readErr := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if readErr != nil {
		return fmt.Errorf("runtime pack for %s is missing; install the checked-in pack or pass -runtime-dir <path>", options.Target)
	}
	manifest, err := decodeRuntimeManifest(raw)
	if err != nil {
		return err
	}
	if err := validateRuntimeManifest(manifest, options.Target); err != nil {
		return err
	}
	if err := verifyRuntimePack(directory, manifest); err != nil {
		return err
	}
	return runPackConsumptionProbe(selected, directory, manifest)
}

// runPackConsumptionProbe compiles and links a program that includes the
// packaged libuv and mimalloc headers, calls a representative symbol from each
// archive, and uses every declared system library in manifest order.
func runPackConsumptionProbe(selected *backend.Backend, directory string, manifest runtimeManifest) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-pack-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the pack probe: %v", err)
	}
	defer os.RemoveAll(dir)

	const probe = "#include <uv.h>\n#include <mimalloc.h>\n\nint main(void) {\n    void *memory = mi_malloc(16);\n    mi_free(memory);\n    return uv_version() == 0 ? 1 : 0;\n}\n"
	source := filepath.Join(dir, "pack.c")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		return fmt.Errorf("could not write the pack probe source: %v", err)
	}
	includeOptions := make([]string, 0, len(manifest.Dependencies))
	archives := make([]string, 0, len(manifest.Dependencies))
	libraryOptions := make([]string, 0)
	for _, dependency := range manifest.Dependencies {
		include, pathErr := safeManifestPath(directory, dependency.IncludeRoot)
		if pathErr != nil {
			return pathErr
		}
		archive, pathErr := safeManifestPath(directory, dependency.Archive)
		if pathErr != nil {
			return pathErr
		}
		includeOptions = append(includeOptions, "-I"+include)
		archives = append(archives, archive)
		for _, library := range dependency.SystemLibraries {
			libraryOptions = append(libraryOptions, backend.SystemLibraryArgument(library))
		}
	}
	binary := filepath.Join(dir, "pack"+exeSuffix())
	compile, err := selected.CompileOne(qualifiedTriple, includeOptions, source, binary+".o")
	if err != nil {
		return fmt.Errorf("pack probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("pack probe failed to compile: %s", firstLine(compile.Stderr))
	}
	objects := append([]string{binary + ".o"}, archives...)
	link, err := selected.LinkObjects(qualifiedTriple, objects, binary, libraryOptions)
	if err != nil {
		return fmt.Errorf("pack probe failed to run: %v", err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("pack probe failed to link: %s", firstLine(link.Stderr))
	}
	if err := exec.Command(binary).Run(); err != nil {
		return fmt.Errorf("pack probe built but did not run cleanly: %v", err)
	}
	return nil
}

// modeOptionProbe compiles and links a minimal program with one mode's exact
// compile and link options, so a backend that rejects any option of any mode
// is reported here by mode and by the backend's own message, instead of
// surfacing much later as a confusing failure of a real build.
func modeOptionProbe(selected *backend.Backend, mode BuildMode) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-mode-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the %s mode probe: %v", mode, err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "mode.c")
	if err := os.WriteFile(source, []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
		return fmt.Errorf("could not write the %s mode probe source: %v", mode, err)
	}
	options := Options(mode)
	binary := filepath.Join(dir, "mode"+exeSuffix())
	compile, err := selected.CompileOne(qualifiedTriple, options.Compile, source, binary+".o")
	if err != nil {
		return fmt.Errorf("%s mode probe failed to run: %v", mode, err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("backend rejects a %s mode compile option (%s): %s", mode, strings.Join(options.Compile, " "), firstLine(compile.Stderr))
	}
	link, err := selected.LinkObjects(qualifiedTriple, []string{binary + ".o"}, binary, options.Link)
	if err != nil {
		return fmt.Errorf("%s mode probe failed to run: %v", mode, err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("backend rejects a %s mode link option (%s): %s", mode, strings.Join(options.Link, " "), firstLine(link.Stderr))
	}
	return nil
}

func backendPkgPinnedVersion() string {
	return backend.PinnedZigWindows().Version
}

// facilityProbe compiles, links, and runs the generated facility/header
// probe through the exact path a real build uses. It proves the backend
// accepts every standard facility generated code relies on, not just that a
// trivial program links.
func facilityProbe(backend *backend.Backend) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-facility-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the facility probe: %v", err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "facility.c")
	if err := os.WriteFile(source, []byte(backendPkgQualificationProbe()), 0o644); err != nil {
		return fmt.Errorf("could not write the facility probe source: %v", err)
	}
	binary := filepath.Join(dir, "facility"+exeSuffix())
	compile, err := backend.CompileOne(qualifiedTriple, nil, source, binary+".o")
	if err != nil {
		return fmt.Errorf("facility probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("facility probe failed to compile: %s", firstLine(compile.Stderr))
	}
	link, err := backend.LinkObjects(qualifiedTriple, []string{binary + ".o"}, binary, nil)
	if err != nil {
		return fmt.Errorf("facility probe failed to run: %v", err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("facility probe failed to link: %s", firstLine(link.Stderr))
	}
	if err := exec.Command(binary).Run(); err != nil {
		return fmt.Errorf("facility probe built but did not run cleanly: %v", err)
	}
	return nil
}

func backendPkgQualificationProbe() string {
	return backend.QualificationProbe()
}

// linkProbe compiles, links, and runs a minimal C program through the exact
// path a real build uses. Running the probe is valid because v1 targets the
// host; a cross-target probe could be built but not executed.
func linkProbe(backend *backend.Backend) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the link probe: %v", err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(source, []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
		return fmt.Errorf("could not write the link probe source: %v", err)
	}
	binary := filepath.Join(dir, "probe"+exeSuffix())

	compile, err := backend.CompileOne(qualifiedTriple, nil, source, binary+".o")
	if err != nil {
		return fmt.Errorf("link probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("link probe failed to build for %s: %s", qualifiedTriple, firstLine(compile.Stderr))
	}
	link, err := backend.LinkObjects(qualifiedTriple, []string{binary + ".o"}, binary, nil)
	if err != nil {
		return fmt.Errorf("link probe failed to run: %v", err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("link probe failed to link for %s: %s", qualifiedTriple, firstLine(link.Stderr))
	}
	if err := exec.Command(binary).Run(); err != nil {
		return fmt.Errorf("link probe built but did not run cleanly: %v", err)
	}
	return nil
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "no output"
}
