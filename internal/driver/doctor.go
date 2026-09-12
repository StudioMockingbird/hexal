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

// Doctor verifies the local setup without building the user's project and
// without discovering it: there is no project lookup here at all. It
// returns a version report alongside the problems. The Hexal version line
// is always present, even when backend discovery fails; the Zig and target
// lines appear only once a backend resolves. Problems accumulate every
// independently checkable failure rather than stopping at the first. Only
// a missing backend short-circuits the checks, because every check below
// needs one; even then the failure is reported once as a list, not an
// error.
func Doctor() (report []string, problems []string) {
	report = []string{"Hexal: " + version.String()}

	if err := checkHost(); err != nil {
		problems = append(problems, err.Error())
	}
	backend, err := resolveBackend()
	if err != nil {
		problems = append(problems, err.Error())
		return report, problems
	}
	report = append(report, "Zig: "+backend.Version, "Target: "+string(compilerTypes.TargetX86_64WindowsGNU))

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

	return report, problems
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
