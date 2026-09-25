package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hexal/compiler"
	"hexal/internal/backend"
	"hexal/internal/version"

	compilerTypes "hexal/compiler/types"
)

// DoctorOptions selects the configuration doctor verifies. It carries the
// same required compiler and target as a build.
type DoctorOptions struct {
	CompilerPath string
	Target       compilerTypes.TargetProfileID
}

// Doctor verifies the local setup without building the user's project and
// without discovering it: there is no project lookup here at all. It returns
// a version report alongside the problems. The Hexal version line is always
// present, even when backend selection fails; the Clang and target lines
// appear only once a backend resolves. Problems accumulate every independently
// checkable failure rather than stopping at the first. Only a missing backend
// short-circuits the checks, because every check below needs one; even then
// the failure is reported once as a list, not an error.
func Doctor(options DoctorOptions) (report []string, problems []string) {
	report = []string{"Hexal: " + version.String()}

	if err := checkHost(options.Target); err != nil {
		problems = append(problems, err.Error())
		return report, problems
	}
	profile, profileErr := resolveProfile(options.Target)
	if profileErr != nil {
		problems = append(problems, profileErr.Error())
		return report, problems
	}
	selected, err := resolveBackend(options.CompilerPath)
	if err != nil {
		problems = append(problems, err.Error())
		return report, problems
	}
	report = append(report, "Clang: "+selected.Version, "Target: "+string(profile.profile))

	if probeErr := facilityProbe(selected, profile.triple, options.Target); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	if probeErr := linkProbe(selected, profile.triple); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		if probeErr := modeOptionProbe(selected, mode, options.Target, profile.triple); probeErr != nil {
			problems = append(problems, probeErr.Error())
		}
	}
	if probeErr := ubsanProbe(selected, options.Target, profile.triple); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	if probeErr := headerImportProbe(selected, profile.triple); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	if probeErr := foreignObjectProbe(selected, profile.triple); probeErr != nil {
		problems = append(problems, probeErr.Error())
	}
	if packErr := doctorRuntimePack(selected, options, profile.triple, featureDefines(options.Target)); packErr != nil {
		problems = append(problems, packErr.Error())
	}

	return report, problems
}

// doctorRuntimePack fully verifies the selected profile's embedded pack and
// proves both archives and every declared system library compile, link, and
// run through the selected compiler. A normal build never does this.
func doctorRuntimePack(selected *backend.Backend, options DoctorOptions, triple string, defines []string) error {
	fsys, err := runtimePackFS(options.Target)
	if err != nil {
		return err
	}
	manifest, pack, err := loadRuntimeManifest(fsys, options.Target, []compiler.RuntimeDependency{compiler.RuntimeLibuv, compiler.RuntimeMimalloc, compiler.RuntimeUtf8proc})
	if err != nil {
		return err
	}
	if err := verifyRuntimePack(fsys, manifest); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "hexal-doctor-pack-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the pack probe: %v", err)
	}
	defer os.RemoveAll(dir)
	includeDirs, archives, err := materializePack(dir, fsys, pack)
	if err != nil {
		return err
	}
	return runPackConsumptionProbe(selected, dir, includeDirs, archives, pack.SystemLibraries, triple, defines)
}

// runPackConsumptionProbe compiles and links a program that includes the
// packaged libuv, mimalloc, and utf8proc headers, calls a representative symbol
// from each archive, and uses every declared system library in manifest order.
func runPackConsumptionProbe(selected *backend.Backend, dir string, includeDirs, archives, systemLibraries []string, triple string, defines []string) error {
	const probe = "#include <uv.h>\n#include <mimalloc.h>\n#include <utf8proc.h>\n\nint main(void) {\n    void *memory = mi_malloc(16);\n    mi_free(memory);\n    return uv_version() == 0 || utf8proc_version() == nullptr ? 1 : 0;\n}\n"
	source := filepath.Join(dir, "pack.c")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		return fmt.Errorf("could not write the pack probe source: %v", err)
	}
	includeOptions := make([]string, 0, len(includeDirs)+len(defines))
	includeOptions = append(includeOptions, defines...)
	for _, include := range includeDirs {
		includeOptions = append(includeOptions, "-I"+include)
	}
	libraryOptions := make([]string, 0, len(systemLibraries))
	for _, library := range systemLibraries {
		libraryOptions = append(libraryOptions, backend.SystemLibraryArgument(library))
	}
	binary := filepath.Join(dir, "pack"+exeSuffix())
	compile, err := selected.CompileOne(triple, includeOptions, source, binary+".o")
	if err != nil {
		return fmt.Errorf("pack probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("pack probe failed to compile: %s", firstLine(compile.Stderr))
	}
	objects := append([]string{binary + ".o"}, archives...)
	link, err := selected.LinkObjects(triple, objects, binary, libraryOptions)
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
func modeOptionProbe(selected *backend.Backend, mode BuildMode, target compilerTypes.TargetProfileID, triple string) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-mode-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the %s mode probe: %v", mode, err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "mode.c")
	if err := os.WriteFile(source, []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
		return fmt.Errorf("could not write the %s mode probe source: %v", mode, err)
	}
	options := Options(mode, target)
	binary := filepath.Join(dir, "mode"+exeSuffix())
	compile, err := selected.CompileOne(triple, options.Compile, source, binary+".o")
	if err != nil {
		return fmt.Errorf("%s mode probe failed to run: %v", mode, err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("backend rejects a %s mode compile option (%s): %s", mode, strings.Join(options.Compile, " "), firstLine(compile.Stderr))
	}
	link, err := selected.LinkObjects(triple, []string{binary + ".o"}, binary, options.Link)
	if err != nil {
		return fmt.Errorf("%s mode probe failed to run: %v", mode, err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("backend rejects a %s mode link option (%s): %s", mode, strings.Join(options.Link, " "), firstLine(link.Stderr))
	}
	return nil
}

// ubsanProbe proves the selected installed Clang can link and execute the
// debug mode's undefined-behavior backstop for this target. On Linux it runs
// a trivial program under the diagnostic runtime, so a Clang without a usable
// runtime is not qualified. On Windows it compiles, links, and runs a signed-
// overflow program under trap mode: link without a MinGW UBSan runtime and a
// non-zero exit prove the trap fires and no diagnostic runtime is required.
func ubsanProbe(selected *backend.Backend, target compilerTypes.TargetProfileID, triple string) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-ubsan-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the UBSan probe: %v", err)
	}
	defer os.RemoveAll(dir)

	program := "int main(void) { return 0; }\n"
	if !isLinuxTarget(target) {
		program = "int main(void) { volatile int left = 2147483647; volatile int right = 1; volatile int sum = left + right; (void)sum; return 0; }\n"
	}
	source := filepath.Join(dir, "ubsan.c")
	if err := os.WriteFile(source, []byte(program), 0o644); err != nil {
		return fmt.Errorf("could not write the UBSan probe source: %v", err)
	}
	options := Options(ModeDebug, target)
	binary := filepath.Join(dir, "ubsan"+exeSuffix())
	compile, err := selected.CompileOne(triple, options.Compile, source, binary+".o")
	if err != nil {
		return fmt.Errorf("UBSan probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("backend rejects the debug UBSan compile options: %s", firstLine(compile.Stderr))
	}
	link, err := selected.LinkObjects(triple, []string{binary + ".o"}, binary, options.Link)
	if err != nil {
		return fmt.Errorf("UBSan probe failed to run: %v", err)
	}
	if link.ExitCode != 0 {
		if isLinuxTarget(target) {
			return fmt.Errorf("backend cannot link the debug UBSan runtime: %s", firstLine(link.Stderr))
		}
		return fmt.Errorf("backend cannot link the debug trap options: %s", firstLine(link.Stderr))
	}
	runErr := exec.Command(binary).Run()
	if isLinuxTarget(target) {
		if runErr != nil {
			return fmt.Errorf("UBSan probe built but did not run cleanly: %v", runErr)
		}
		return nil
	}
	if runErr == nil {
		return fmt.Errorf("UBSan trap probe completed without trapping")
	}
	if exit, ok := runErr.(*exec.ExitError); !ok || exit.ExitCode() == 0 {
		return fmt.Errorf("UBSan trap probe did not exit non-zero: %v", runErr)
	}
	return nil
}

// headerImportProbe preprocesses and inspects a representative imported header
// with the selected Clang, proving the automatic C-import path can read a
// header that declares a record, a typedef, an enum, and a function.
func headerImportProbe(selected *backend.Backend, triple string) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-header-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the header probe: %v", err)
	}
	defer os.RemoveAll(dir)

	const header = "typedef struct probe_point { int x; int y; } probe_point;\nenum probe_color { PROBE_RED, PROBE_GREEN };\nint probe_add(int left, int right);\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.h"), []byte(header), 0o644); err != nil {
		return fmt.Errorf("could not write the header probe: %v", err)
	}
	source := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(source, []byte("#include \"probe.h\"\n"), 0o644); err != nil {
		return fmt.Errorf("could not write the header probe source: %v", err)
	}
	preprocessed := filepath.Join(dir, "probe.i")
	preprocess, err := selected.Run("--target="+triple, "-std=c23", "-I"+dir, "-E", source, "-o", preprocessed)
	if err != nil {
		return fmt.Errorf("header probe failed to run: %v", err)
	}
	if preprocess.ExitCode != 0 {
		return fmt.Errorf("header probe failed to preprocess: %s", firstLine(preprocess.Stderr))
	}
	ast, err := selected.Run("--target="+triple, "-x", "c", "-std=c23", "-Xclang", "-ast-dump=json", "-fsyntax-only", preprocessed)
	if err != nil {
		return fmt.Errorf("header probe failed to run: %v", err)
	}
	if ast.ExitCode != 0 {
		return fmt.Errorf("header probe failed to inspect: %s", firstLine(ast.Stderr))
	}
	if !strings.Contains(ast.Stdout, "probe_add") {
		return fmt.Errorf("header probe inspected no declaration")
	}
	return nil
}

// foreignObjectProbe links an object produced by the same selected Clang
// alongside another translation unit, proving the backend can consume an
// object it did not produce in the same link step as Hexal-generated objects.
func foreignObjectProbe(selected *backend.Backend, triple string) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-foreign-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the foreign-object probe: %v", err)
	}
	defer os.RemoveAll(dir)

	helper := filepath.Join(dir, "helper.c")
	if err := os.WriteFile(helper, []byte("int probe_helper(void) { return 7; }\n"), 0o644); err != nil {
		return fmt.Errorf("could not write the foreign-object helper: %v", err)
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte("int probe_helper(void);\nint main(void) { return probe_helper() == 7 ? 0 : 1; }\n"), 0o644); err != nil {
		return fmt.Errorf("could not write the foreign-object main: %v", err)
	}
	helperObject := filepath.Join(dir, "helper.o")
	compileHelper, err := selected.CompileOne(triple, nil, helper, helperObject)
	if err != nil {
		return fmt.Errorf("foreign-object probe failed to run: %v", err)
	}
	if compileHelper.ExitCode != 0 {
		return fmt.Errorf("foreign-object probe failed to compile the helper: %s", firstLine(compileHelper.Stderr))
	}
	mainObject := filepath.Join(dir, "main.o")
	compileMain, err := selected.CompileOne(triple, nil, main, mainObject)
	if err != nil {
		return fmt.Errorf("foreign-object probe failed to run: %v", err)
	}
	if compileMain.ExitCode != 0 {
		return fmt.Errorf("foreign-object probe failed to compile the main: %s", firstLine(compileMain.Stderr))
	}
	binary := filepath.Join(dir, "foreign"+exeSuffix())
	link, err := selected.LinkObjects(triple, []string{mainObject, helperObject}, binary, nil)
	if err != nil {
		return fmt.Errorf("foreign-object probe failed to run: %v", err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("foreign-object probe failed to link: %s", firstLine(link.Stderr))
	}
	if err := exec.Command(binary).Run(); err != nil {
		return fmt.Errorf("foreign-object probe built but did not run cleanly: %v", err)
	}
	return nil
}

// facilityProbe compiles, links, and runs the generated facility/header
// probe through the exact path a real build uses. It proves the backend
// accepts every standard facility generated code relies on for this target,
// not just that a trivial program links.
func facilityProbe(selected *backend.Backend, triple string, target compilerTypes.TargetProfileID) error {
	dir, err := os.MkdirTemp("", "hexal-doctor-facility-")
	if err != nil {
		return fmt.Errorf("could not create a temporary directory for the facility probe: %v", err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "facility.c")
	if err := os.WriteFile(source, []byte(backend.QualificationProbe(target)), 0o644); err != nil {
		return fmt.Errorf("could not write the facility probe source: %v", err)
	}
	binary := filepath.Join(dir, "facility"+exeSuffix())
	compile, err := selected.CompileOne(triple, nil, source, binary+".o")
	if err != nil {
		return fmt.Errorf("facility probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("facility probe failed to compile: %s", firstLine(compile.Stderr))
	}
	link, err := selected.LinkObjects(triple, []string{binary + ".o"}, binary, nil)
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

// linkProbe compiles, links, and runs a minimal C program through the exact
// path a real build uses. Running the probe is valid because this release
// targets the host; a cross-target probe could be built but not executed.
func linkProbe(selected *backend.Backend, triple string) error {
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

	compile, err := selected.CompileOne(triple, nil, source, binary+".o")
	if err != nil {
		return fmt.Errorf("link probe failed to run: %v", err)
	}
	if compile.ExitCode != 0 {
		return fmt.Errorf("link probe failed to build for %s: %s", triple, firstLine(compile.Stderr))
	}
	link, err := selected.LinkObjects(triple, []string{binary + ".o"}, binary, nil)
	if err != nil {
		return fmt.Errorf("link probe failed to run: %v", err)
	}
	if link.ExitCode != 0 {
		return fmt.Errorf("link probe failed to link for %s: %s", triple, firstLine(link.Stderr))
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
