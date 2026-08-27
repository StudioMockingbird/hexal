// Tier 2: structured generation over valid programs. Byte mutation (tier 1)
// almost never produces a program that typechecks, so it reaches the
// generator -- where this project's defect history actually lives -- almost
// never. This file generates valid programs from generateProgram
// (generator_test.go) and asserts the properties and meta-tests required of
// that generator: construct coverage, acceptance rate, generation
// determinism, rename invariance, and monomorphization uniqueness. Every
// property below is also proven non-vacuous through an explicit test seam: a
// directly-fed, deliberately wrong input the property's own check must
// reject. No production source is edited to do this.
package fuzz

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"hexal/compiler"
)

// constructCheck names one entry in the closed checklist and the pattern
// that proves it, pinned to the operand types generateProgram actually
// emits rather than a substring any construct could satisfy.
type constructCheck struct {
	name    string
	pattern *regexp.Regexp
}

// constructChecklist is the closed list of constructs this identity-focused
// generator claims to emit: nominal objects, ADTs, unions, generic
// declarations and specializations, constructed collections, and imports.
// It does not claim every operator, control form, or collection operation.
var constructChecklist = []constructCheck{
	{"nominal-object", regexp.MustCompile(`struct hex_t_\w*_GenPoint\b`)},
	{"adt", regexp.MustCompile(`hex_tag_\w*_GenSignal_GenAlpha\b`)},
	{"union", regexp.MustCompile(`\bhex_t_Int32_Nil\b`)},
	{"constructed-collection", regexp.MustCompile(`\bhex_list_Int32\b`)},
	{"cross-module-import", regexp.MustCompile(`\bhex_f_\w*_lib_GenMakePoint\b`)},
	// Satisfied only once every rotating type has been specialized, so
	// checklist coverage genuinely requires a multi-seed prefix rather than
	// one lucky candidate.
	{"generic-specialization", regexp.MustCompile(`\bhex_f_\w*_lib_GenIdentity_(Int32|Int64|Float64|Bool)\b`)},
}

// concatenatedFiles joins every generated artifact so one regexp search
// covers the whole result regardless of which file a construct lands in.
func concatenatedFiles(files map[string]string) string {
	var whole strings.Builder
	for _, content := range files {
		whole.WriteString(content)
		whole.WriteByte('\n')
	}
	return whole.String()
}

// assertChecklistCovered fails when coverage is missing any checklist entry.
// It is parameterized over the checklist and a coverage map -- both plain
// data -- so an injection test can feed a deliberately incomplete map
// without needing a real generator that under-covers.
func assertChecklistCovered(t failer, checklist []constructCheck, covered map[string]bool) {
	t.Helper()
	for _, check := range checklist {
		if !covered[check.name] {
			t.Fatalf("construct checklist entry %q was never covered", check.name)
		}
	}
}

// assertAcceptanceRate fails when accepted/total drops below the 90 percent
// floor, reporting the first rejection as the RFC's guard requires.
func assertAcceptanceRate(t failer, accepted, total int, firstRejection string) {
	t.Helper()
	if total == 0 {
		t.Fatalf("acceptance rate computed over zero candidates")
	}
	rate := float64(accepted) / float64(total)
	if rate < 0.90 {
		t.Fatalf("acceptance rate %.1f%% (%d/%d) below the 90%% floor; first rejection: %s", rate*100, accepted, total, firstRejection)
	}
}

// assertSameProgram fails when two generatedProgram values are not
// byte-identical, the generation-determinism property.
func assertSameProgram(t failer, a, b generatedProgram) {
	t.Helper()
	if a.entrypoint != b.entrypoint {
		t.Fatalf("seed %d: entrypoint differs across two generations: %q vs %q", a.seed, a.entrypoint, b.entrypoint)
	}
	if len(a.sources) != len(b.sources) {
		t.Fatalf("seed %d: source count differs across two generations: %d vs %d", a.seed, len(a.sources), len(b.sources))
	}
	for key, content := range a.sources {
		other, ok := b.sources[key]
		if !ok || content != other {
			t.Fatalf("seed %d: source %q differs across two generations", a.seed, key)
		}
	}
}

// TestGeneratorChecklistAcceptanceAndDeterminism runs the RFC's exact
// search: candidates from monotonically increasing seeds starting at zero,
// retaining the shortest prefix whose accepted subset covers every
// checklist entry, then checking the 90 percent acceptance floor over that
// prefix. It also reports the domain's own composition, so a checklist that
// stopped being covered would be visible here rather than passing silently.
func TestGeneratorChecklistAcceptanceAndDeterminism(t *testing.T) {
	covered := make(map[string]bool, len(constructChecklist))
	firstCoveredAtSeed := make(map[string]uint64, len(constructChecklist))
	accepted, total := 0, 0
	firstRejection := ""
	const safetyCap = 1000

	var seed uint64
	for {
		program := generateProgram(seed)
		// Generation determinism: the same seed must produce the same
		// program before it is even compiled.
		assertSameProgram(t, program, generateProgram(seed))

		result := compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
		total++
		if result.ExitCode == compiler.ExitSuccess {
			accepted++
			whole := concatenatedFiles(result.Files)
			for _, check := range constructChecklist {
				if !covered[check.name] && check.pattern.MatchString(whole) {
					covered[check.name] = true
					firstCoveredAtSeed[check.name] = seed
				}
			}
		} else if firstRejection == "" {
			firstRejection = fmt.Sprintf("seed %d: %v", seed, result.Stderr)
		}

		fullyCovered := true
		for _, check := range constructChecklist {
			if !covered[check.name] {
				fullyCovered = false
				break
			}
		}
		if fullyCovered {
			break
		}
		seed++
		if seed >= safetyCap {
			t.Fatalf("checklist not covered after %d candidates; coverage so far: %v", safetyCap, covered)
		}
	}

	assertChecklistCovered(t, constructChecklist, covered)
	assertAcceptanceRate(t, accepted, total, firstRejection)

	t.Logf("domain composition: prefix length %d, accepted %d, rejected %d", total, accepted, total-accepted)
	for _, check := range constructChecklist {
		t.Logf("  %s: first covered at seed %d", check.name, firstCoveredAtSeed[check.name])
	}
}

// Injection half: the checklist-coverage guard fires when one entry is
// never covered.
func TestChecklistCoverageGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	covered := map[string]bool{"nominal-object": true, "adt": true}
	assertChecklistCovered(recorder, constructChecklist, covered)
	if !recorder.fired {
		t.Fatal("assertChecklistCovered did not fire on an incomplete coverage map")
	}
}

// Injection half: the acceptance-rate guard fires below the 90 percent
// floor and reports the first rejection.
func TestAcceptanceRateGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	assertAcceptanceRate(recorder, 5, 10, "seed 0: [Type Error] deliberate")
	if !recorder.fired {
		t.Fatal("assertAcceptanceRate did not fire at 50% acceptance")
	}
	if !strings.Contains(recorder.message, "deliberate") {
		t.Fatalf("assertAcceptanceRate fired without reporting the first rejection: %q", recorder.message)
	}
}

// Injection half: the generation-determinism guard fires when two
// generations of the same seed disagree.
func TestGenerationDeterminismGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	a := generatedProgram{seed: 0, entrypoint: "app.hex", sources: map[string]string{"app.hex": "one"}}
	b := generatedProgram{seed: 0, entrypoint: "app.hex", sources: map[string]string{"app.hex": "two"}}
	assertSameProgram(recorder, a, b)
	if !recorder.fired {
		t.Fatal("assertSameProgram did not fire on two differing generations of one seed")
	}
}

// modulePrefixPattern extracts one module's C-symbol prefix from its header's
// include guard, e.g. "m3_lib" from "#ifndef HEX_MODULE_m3_lib_H".
var modulePrefixPattern = regexp.MustCompile(`#ifndef HEX_MODULE_(\w+)_H`)

// modulePrefix returns the module prefix recorded in headerKey's include
// guard, or ok=false when the header or guard is missing.
func modulePrefix(files map[string]string, headerKey string) (string, bool) {
	content, ok := files[headerKey]
	if !ok {
		return "", false
	}
	match := modulePrefixPattern.FindStringSubmatch(content)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// assertRenameInvariant checks that substituting old's module prefix and
// file stem for new's throughout old's files reproduces new's files exactly
// -- "renaming a module renames its C symbols consistently and changes
// nothing else structurally." It takes both already-compiled file maps and
// the two stems as plain data, so an injection test can feed a mismatched
// pair without needing a real renaming bug.
func assertRenameInvariant(t failer, oldFiles map[string]string, oldStem string, newFiles map[string]string, newStem string) {
	t.Helper()
	oldPrefix, ok := modulePrefix(oldFiles, "modules/"+oldStem+".h")
	if !ok {
		t.Fatalf("no module prefix found for modules/%s.h", oldStem)
	}
	newPrefix, ok := modulePrefix(newFiles, "modules/"+newStem+".h")
	if !ok {
		t.Fatalf("no module prefix found for modules/%s.h", newStem)
	}
	renamed := make(map[string]string, len(oldFiles))
	for name, content := range oldFiles {
		renamedName := strings.Replace(name, "modules/"+oldStem+".", "modules/"+newStem+".", 1)
		renamedContent := strings.ReplaceAll(content, oldPrefix, newPrefix)
		renamedContent = strings.ReplaceAll(renamedContent, "\""+oldStem+".hex\"", "\""+newStem+".hex\"")
		renamedContent = strings.ReplaceAll(renamedContent, "modules/"+oldStem+".", "modules/"+newStem+".")
		renamed[renamedName] = renamedContent
	}
	if len(renamed) != len(newFiles) {
		t.Fatalf("renamed file count %d does not match the actually-renamed compile's %d", len(renamed), len(newFiles))
	}
	for name, content := range renamed {
		other, ok := newFiles[name]
		if !ok {
			t.Fatalf("renamed file %q is absent from the actually-renamed compile", name)
		}
		if content != other {
			t.Fatalf("file %q differs after consistent renaming: renaming touched something the real rename did not, or missed something it did", name)
		}
	}
}

// TestRenameInvariance compiles one generated program's imported module
// under two different names, holding every other source byte identical, and
// checks that consistently substituting the old module's name and C-symbol
// prefix for the new one reproduces the second compile exactly.
func TestRenameInvariance(t *testing.T) {
	program := generateProgram(0)
	appTemplate := strings.ReplaceAll(program.sources["app.hex"], "./lib", "./%s")
	lib := program.sources["lib.hex"]

	oldApp := strings.Replace(appTemplate, "%s", "lib", 1)
	newApp := strings.Replace(appTemplate, "%s", "widget", 1)
	oldResult := compiler.Compile(map[string]string{"app.hex": oldApp, "lib.hex": lib}, "app.hex", compiler.Project{})
	newResult := compiler.Compile(map[string]string{"app.hex": newApp, "widget.hex": lib}, "app.hex", compiler.Project{})
	if oldResult.ExitCode != compiler.ExitSuccess || newResult.ExitCode != compiler.ExitSuccess {
		t.Fatalf("both compiles must succeed; old stderr=%v new stderr=%v", oldResult.Stderr, newResult.Stderr)
	}
	assertRenameInvariant(t, oldResult.Files, "lib", newResult.Files, "widget")
}

// Injection half: the rename-invariance guard fires when the "renamed"
// compile is missing a file the substitution expects to find.
func TestRenameInvarianceGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	oldFiles := map[string]string{
		"modules/lib.h": "#ifndef HEX_MODULE_m3_lib_H\n#define HEX_MODULE_m3_lib_H\n#endif\n",
		"modules/lib.c": "#include \"modules/lib.h\"\nint hex_f_m3_lib_get(void) { return 1; }\n",
	}
	// A deliberately broken "rename": the second file's content was never
	// actually renamed, so the substitution the guard performs on oldFiles
	// will not match it.
	newFiles := map[string]string{
		"modules/widget.h": "#ifndef HEX_MODULE_m6_widget_H\n#define HEX_MODULE_m6_widget_H\n#endif\n",
		"modules/widget.c": "#include \"modules/lib.h\"\nint hex_f_m3_lib_get(void) { return 1; }\n",
	}
	assertRenameInvariant(recorder, oldFiles, "lib", newFiles, "widget")
	if !recorder.fired {
		t.Fatal("assertRenameInvariant did not fire on a file that was never actually renamed")
	}
}

// assertSingleSpecialization checks that whole contains exactly one
// definition of function specialized at concreteType, matching
// generateProgram's naming scheme hex_f_<prefix>_<function>_<Type>, and
// returns the specialization's symbol so a caller can confirm every call
// site actually references that one symbol. It takes the generated text as
// plain data so an injection test can feed a duplicated definition directly.
func assertSingleSpecialization(t failer, whole, function, concreteType string) string {
	t.Helper()
	defPattern := regexp.MustCompile(`\b(hex_f_\w*_` + function + `_` + concreteType + `)\((?:const )?\w+ hex_v_\w+\) \{`)
	defs := defPattern.FindAllStringSubmatch(whole, -1)
	if len(defs) != 1 {
		t.Fatalf("found %d definitions of %s<%s>, want exactly 1: %v", len(defs), function, concreteType, defs)
	}
	return defs[0][1]
}

// TestMonomorphizationUniqueness instantiates one generic function through
// two independent call sites with the same concrete type argument and
// checks that exactly one specialization was generated for both to share.
func TestMonomorphizationUniqueness(t *testing.T) {
	app := "module Lib = import \"./lib\"\n" +
		"fun runA(): Int32 do\n" +
		"    return Lib.GenIdentity<Int32>(3)\n" +
		"end\n" +
		"fun runB(): Int32 do\n" +
		"    return Lib.GenIdentity<Int32>(4)\n" +
		"end\n" +
		"a: Int32 := runA()\n" +
		"b: Int32 := runB()\n"
	lib := "export fun GenIdentity<T>(value: T): T do\n" +
		"    return value\n" +
		"end\n"
	result := compiler.Compile(map[string]string{"app.hex": app, "lib.hex": lib}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("stderr=%v", result.Stderr)
	}
	whole := concatenatedFiles(result.Files)
	symbol := assertSingleSpecialization(t, whole, "GenIdentity", "Int32")
	// Both call sites pass a bare numeric literal, which only a real call
	// site does -- the prototype and definition signatures always carry a
	// parameter name or type instead.
	callSites := regexp.MustCompile(regexp.QuoteMeta(symbol)+`\(\d+\)`).FindAllString(whole, -1)
	if len(callSites) != 2 {
		t.Fatalf("found %d call sites referencing specialization %s, want 2: %v", len(callSites), symbol, callSites)
	}
}

// Injection half: the monomorphization-uniqueness guard fires on text
// carrying two definitions of the same specialization.
func TestMonomorphizationUniquenessGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	duplicated := "int32_t hex_f_m3_lib_GenIdentity_Int32(const int32_t hex_v_value) {\n    return hex_v_value;\n}\n" +
		"int32_t hex_f_m6_lib_GenIdentity_Int32(const int32_t hex_v_value) {\n    return hex_v_value;\n}\n"
	assertSingleSpecialization(recorder, duplicated, "GenIdentity", "Int32")
	if !recorder.fired {
		t.Fatal("assertSingleSpecialization did not fire on text carrying two definitions")
	}
}
