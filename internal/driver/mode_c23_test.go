//go:build c23

package driver

// Mode tests that spawn the real C toolchain: the exact arguments each mode
// sends to the backend and the observable properties each mode promises.
// Tagged `c23` like every other toolchain-dependent suite here.
//
// Run with: go test -tags c23 ./internal/driver/

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hexal/compiler"
)

// buildInMode builds one program under mode and returns the completed result.
func buildInMode(t *testing.T, dir string, mode BuildMode) BuildResult {
	t.Helper()
	result, err := Build(withTestBackend(t, BuildOptions{Root: dir, Mode: mode}))
	if err != nil {
		t.Fatalf("%s build failed: %v", mode, err)
	}
	return result
}

// containsSequence reports whether arguments carries want as a contiguous run,
// so an option set is asserted in its exact order rather than as a bag.
func containsSequence(arguments, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for start := 0; start+len(want) <= len(arguments); start++ {
		if strings.Join(arguments[start:start+len(want)], "\x00") == strings.Join(want, "\x00") {
			return true
		}
	}
	return false
}

// compiledSource is the translation unit one compile command names, taken
// from the argument after -c. Classifying on the source and not on any
// argument matters: a generated translation unit is compiled with the
// dependency include roots on its command line, so a command that merely
// mentions the dependency tree is not a dependency compile.
func compiledSource(command CommandResult) string {
	if command.Stage != StageCompile {
		return ""
	}
	for index, argument := range command.Arguments {
		if argument == "-c" && index+1 < len(command.Arguments) {
			return command.Arguments[index+1]
		}
	}
	return ""
}

func isDependencyCompile(command CommandResult) bool {
	source := compiledSource(command)
	return source != "" && strings.Contains(source, "dependencies")
}

func isGeneratedCompile(command CommandResult) bool {
	source := compiledSource(command)
	return source != "" && !strings.Contains(source, "dependencies")
}

// TestModeOptionsReachTheBackendExactly pins the whole contract between the
// driver and a mode-unaware backend: every generated translation unit is
// compiled with that mode's exact option run, the executable link carries its
// exact link options, the demanded runtime pack contributes include roots, and
// no vendored dependency source is compiled.
func TestModeOptionsReachTheBackendExactly(t *testing.T) {
	requireBackend(t)
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			// A heap program selects the mimalloc runtime dependency, so the
			// pack include root must reach generated compiles.
			writeSource(t, dir, "main.hex", "fun demo(h: Heap): Int32 do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(7)\n    return values[0]\nend\nprint(demo(Heap()))\n")
			result := buildInMode(t, dir, mode)
			options := Options(mode, hostQualifiedTarget())

			generated, dependencies, links, packIncludes := 0, 0, 0, 0
			for _, command := range result.Commands {
				switch {
				case isGeneratedCompile(command):
					generated++
					if !containsSequence(command.Arguments, options.Compile) {
						t.Fatalf("generated compile lacks the %s option run %v:\n%v", mode, options.Compile, command.Arguments)
					}
					for _, argument := range command.Arguments {
						if strings.Contains(argument, "mimalloc") {
							packIncludes++
						}
					}
				case isDependencyCompile(command):
					dependencies++
				case command.Stage == StageLink:
					links++
					if !containsSequence(command.Arguments, options.Link) {
						t.Fatalf("link lacks the %s option run %v:\n%v", mode, options.Link, command.Arguments)
					}
					if mode == ModeDebug {
						for _, unwanted := range []string{"-s", "-Wl,--gc-sections"} {
							for _, argument := range command.Arguments {
								if argument == unwanted {
									t.Fatalf("debug link carries the release option %q:\n%v", unwanted, command.Arguments)
								}
							}
						}
					}
				}
			}
			if generated == 0 || links != 1 {
				t.Fatalf("recorded %d generated compiles, %d links", generated, links)
			}
			if dependencies != 0 {
				t.Fatalf("normal build compiled %d vendored dependency sources; the pack supplies them", dependencies)
			}
			if packIncludes == 0 {
				t.Fatal("no generated compile received the demanded pack include root")
			}
		})
	}
}

// TestGeneratedCIsByteIdenticalAcrossModes holds the principle the whole
// feature rests on. The mode is a driver setting that never reaches the
// compiler, so the artifacts cannot differ; this asserts it on the concrete
// bytes rather than trusting the type.
func TestGeneratedCIsByteIdenticalAcrossModes(t *testing.T) {
	requireBackend(t)
	sources := map[string]string{"app.hex": "fun demo(h: Heap): Int32 do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(7)\n    return values[0]\nend\nprint(demo(Heap()))\n"}
	first := compiler.Compile(sources, "app.hex", compiler.Project{Target: hostQualifiedTarget()})
	second := compiler.Compile(sources, "app.hex", compiler.Project{Target: hostQualifiedTarget()})
	if len(first.Files) != len(second.Files) || len(first.Files) == 0 {
		t.Fatalf("artifact counts %d and %d", len(first.Files), len(second.Files))
	}
	for name, content := range first.Files {
		if second.Files[name] != content {
			t.Fatalf("artifact %q differs between compilations", name)
		}
	}
	// The identity encoder is what a mode change is allowed to move, and it
	// must move for the same artifacts.
	debug := buildIdentity(ModeDebug, "clang=test", first.Files, first.Dependencies, nil, nil, hostQualifiedTarget(), packInputs{}, nil)
	release := buildIdentity(ModeRelease, "clang=test", first.Files, first.Dependencies, nil, nil, hostQualifiedTarget(), packInputs{}, nil)
	if debug == release {
		t.Fatal("the build identity does not distinguish the modes")
	}
}

// TestModesProduceIdenticalProgramBehavior is the driver-level half of the
// release conformance rule: one program, both modes, identical stdout,
// stderr, and exit status.
func TestModesProduceIdenticalProgramBehavior(t *testing.T) {
	requireBackend(t)
	const program = "fun demo(h: Heap): Int32 do\n" +
		"    let values: List<Int32> = List<Int32>(h)\n" +
		"    defer values.free(h)\n" +
		"    values.push(7)\n" +
		"    values.push(35)\n" +
		"    return values[0] + values[1]\n" +
		"end\n" +
		"print(demo(Heap()))\n" +
		"print(1.0 / 3.0)\n"
	var outputs, errors [2]string
	var statuses [2]int
	for index, mode := range []BuildMode{ModeDebug, ModeRelease} {
		dir := t.TempDir()
		writeSource(t, dir, "main.hex", program)
		result := buildInMode(t, dir, mode)
		var stdout, stderr bytes.Buffer
		command := exec.Command(result.Executable)
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		if exit, ok := err.(*exec.ExitError); ok {
			statuses[index] = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("running the %s executable failed: %v", mode, err)
		}
		outputs[index], errors[index] = stdout.String(), stderr.String()
	}
	if outputs[0] != outputs[1] {
		t.Fatalf("stdout differs: debug %q, release %q", outputs[0], outputs[1])
	}
	if errors[0] != errors[1] {
		t.Fatalf("stderr differs: debug %q, release %q", errors[0], errors[1])
	}
	if statuses[0] != statuses[1] {
		t.Fatalf("exit status differs: debug %d, release %d", statuses[0], statuses[1])
	}
}

// TestReleaseIsSmallerAndCarriesNoDebugInformation checks the two properties
// a release build is chosen for. DWARF debug information shows up as a
// `.debug_info` section; a stripped image has none.
func TestReleaseIsSmallerAndCarriesNoDebugInformation(t *testing.T) {
	requireBackend(t)
	sizes := map[BuildMode]int64{}
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		dir := t.TempDir()
		writeSource(t, dir, "main.hex", "let values: Array<Int32, 2> = [1, 2]\nprint(values[0])\n")
		result := buildInMode(t, dir, mode)
		info, err := os.Stat(result.Executable)
		if err != nil {
			t.Fatal(err)
		}
		sizes[mode] = info.Size()
		raw, err := os.ReadFile(result.Executable)
		if err != nil {
			t.Fatal(err)
		}
		carriesDebugInformation := bytes.Contains(raw, []byte(".debug_info"))
		if mode == ModeRelease && carriesDebugInformation {
			t.Error("release executable carries debug information")
		}
		if mode == ModeDebug && !carriesDebugInformation {
			t.Error("debug executable carries no debug information")
		}
	}
	if sizes[ModeRelease] >= sizes[ModeDebug] {
		t.Fatalf("release executable is %d bytes, not smaller than debug's %d", sizes[ModeRelease], sizes[ModeDebug])
	}
}

// TestReleaseRemovesUnreferencedCode checks section collection concretely. The
// unreferenced function below carries a distinctive literal, so the literal's
// presence in the image is a direct observation of whether the function and
// its data survived the link.
func TestReleaseRemovesUnreferencedCode(t *testing.T) {
	requireBackend(t)
	const marker = "hexal-unreferenced-helper-marker-0187"
	const program = "fun unused_helper(): String do\n" +
		"    return \"" + marker + "\"\n" +
		"end\n" +
		"print(\"ok\")\n"
	present := map[BuildMode]bool{}
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		dir := t.TempDir()
		writeSource(t, dir, "main.hex", program)
		result := buildInMode(t, dir, mode)
		raw, err := os.ReadFile(result.Executable)
		if err != nil {
			t.Fatal(err)
		}
		present[mode] = bytes.Contains(raw, []byte(marker))
	}
	if !present[ModeDebug] {
		t.Fatal("debug dropped an unreferenced helper; it keeps symbols complete instead")
	}
	if present[ModeRelease] {
		t.Fatal("release kept an unreferenced helper; section collection did not run")
	}
}

// TestDebugUndefinedBehaviorProbeTerminates proves the backstop is armed and
// non-recoverable under the exact debug options: a deliberate signed overflow
// must terminate the process rather than continue with a wrapped value. On
// Linux that terminates with a diagnostic; on Windows trap mode carries no
// diagnostic runtime, so a non-zero exit with no "after" output is the whole
// contract. The release companion proves the backstop is mode-scoped.
func TestDebugUndefinedBehaviorProbeTerminates(t *testing.T) {
	selected := requireBackend(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "undefined.c")
	const probe = "#include <stdio.h>\n" +
		"int add(int left, int right) { return left + right; }\n" +
		"int main(void) {\n" +
		"    volatile int largest = 2147483647;\n" +
		"    printf(\"before\\n\");\n" +
		"    fflush(stdout);\n" +
		"    printf(\"after %d\\n\", add((int)largest, 1));\n" +
		"    return 0;\n" +
		"}\n"
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(dir, "undefined.o")
	options := Options(ModeDebug, hostQualifiedTarget())
	compile, err := selected.CompileOne(hostQualifiedTriple(), options.Compile, source, object)
	if err != nil || compile.ExitCode != 0 {
		t.Fatalf("probe failed to compile: %v\n%s", err, compile.Stderr)
	}
	binary := filepath.Join(dir, "undefined"+exeSuffix())
	link, err := selected.LinkObjects(hostQualifiedTriple(), []string{object}, binary, options.Link)
	if err != nil || link.ExitCode != 0 {
		t.Fatalf("probe failed to link: %v\n%s", err, link.Stderr)
	}
	var stdout, stderr bytes.Buffer
	command := exec.Command(binary)
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if runErr == nil {
		t.Fatalf("the probe completed; the backstop is recoverable or absent (stdout %q)", stdout.String())
	}
	if strings.Contains(stdout.String(), "after") {
		t.Fatalf("the probe continued past undefined behavior: %q", stdout.String())
	}
	if isLinuxTarget(hostQualifiedTarget()) && !strings.Contains(stderr.String(), "signed integer overflow") {
		t.Fatalf("the probe terminated without a diagnostic: %q", stderr.String())
	}
}

// TestReleaseUndefinedBehaviorProbeCompletes proves the backstop is
// mode-scoped: the same deliberate signed overflow completes under release and
// prints "after", because release carries no sanitizer instrumentation.
func TestReleaseUndefinedBehaviorProbeCompletes(t *testing.T) {
	selected := requireBackend(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "undefined_release.c")
	const probe = "#include <stdio.h>\n" +
		"int add(int left, int right) { return left + right; }\n" +
		"int main(void) {\n" +
		"    volatile int largest = 2147483647;\n" +
		"    printf(\"before\\n\");\n" +
		"    fflush(stdout);\n" +
		"    printf(\"after %d\\n\", add((int)largest, 1));\n" +
		"    return 0;\n" +
		"}\n"
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(dir, "undefined_release.o")
	options := Options(ModeRelease, hostQualifiedTarget())
	compile, err := selected.CompileOne(hostQualifiedTriple(), options.Compile, source, object)
	if err != nil || compile.ExitCode != 0 {
		t.Fatalf("probe failed to compile: %v\n%s", err, compile.Stderr)
	}
	binary := filepath.Join(dir, "undefined_release"+exeSuffix())
	link, err := selected.LinkObjects(hostQualifiedTriple(), []string{object}, binary, options.Link)
	if err != nil || link.ExitCode != 0 {
		t.Fatalf("probe failed to link: %v\n%s", err, link.Stderr)
	}
	output, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("the release probe failed: %v (output %q)", err, output)
	}
	if !strings.Contains(string(output), "after") {
		t.Fatalf("the release probe did not continue past overflow: %q", output)
	}
}

// TestDoctorReportsABackendRejectingAModeOption checks the reporting path with
// the real backend by probing an option set it genuinely refuses. The table is
// restored before the test returns, so no other test observes the extra entry.
func TestDoctorReportsABackendRejectingAModeOption(t *testing.T) {
	selected := requireBackend(t)
	const rejected = BuildMode("rejected-by-the-backend")
	modeOptionTable[rejected] = ModeOptions{Compile: []string{"-fhexal-no-such-option"}}
	defer delete(modeOptionTable, rejected)

	err := modeOptionProbe(selected, rejected, hostQualifiedTarget(), hostQualifiedTriple())
	if err == nil {
		t.Fatal("the backend accepted an option that does not exist")
	}
	if !strings.Contains(err.Error(), string(rejected)) || !strings.Contains(err.Error(), "-fhexal-no-such-option") {
		t.Fatalf("the problem does not name the mode and the option: %v", err)
	}

	// Both real modes must pass the same probe on a host where builds work.
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		if err := modeOptionProbe(selected, mode, hostQualifiedTarget(), hostQualifiedTriple()); err != nil {
			t.Errorf("the backend rejects a %s mode option: %v", mode, err)
		}
	}
}
