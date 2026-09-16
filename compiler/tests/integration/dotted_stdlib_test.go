package integration

// Standard-library modules are reached through the unquoted dotted reference
// `Alias from std.<component>`, while relative source modules keep their
// quoted `./` and `../` paths. These tests pin resolution, identity,
// diagnostics, and the preserved generated-artifact behavior.

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// Every currently defined one-component stdlib module resolves through the
// dotted syntax.
func TestDottedStdlibModulesResolve(t *testing.T) {
	for _, module := range []string{
		"std.io", "std.fs", "std.time", "std.net", "std.process",
		"std.signal", "std.terminal", "std.program", "std.entropy", "std.ascii",
	} {
		source := "import\n    M from " + module + "\nend\nvalue: Int32 := 1\n"
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Errorf("import %s rejected: %v", module, result.Stderr)
		}
	}
}

// An absent standard-library module reports the exact unknown-module
// diagnostic using the dotted spelling the user wrote.
func TestDottedStdlibUnknownModule(t *testing.T) {
	assertRejects(t,
		"import\n    H from std.crypto.hash\nend\nvalue: Int32 := 1\n",
		"unknown stdlib module std.crypto.hash")
}

// The exact dotted-reference diagnostics. The parser test pins the anchors;
// this pins the messages the public compiler reports.
func TestDottedStdlibSyntaxDiagnostics(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"import\n    Io from \"std/io\"\nend\n", "standard-library imports use dotted paths; write std.io"},
		{"import\n    X from \"vendor/x\"\nend\n", "quoted import paths must begin with ./ or ../"},
		{"import\n    X from std\nend\n", "standard-library import requires a component after std."},
		{"import\n    X from std/io\nend\n", "standard-library imports use dots between components"},
		{"import\n    X from std..io\nend\n", "expected a standard-library module component after '.'"},
		{"import\n    X from std.for\nend\n", "expected a standard-library module component after '.'"},
		{"import\n    X from\n        std.io\nend\n", "module reference must begin on the same line as 'from'"},
	} {
		assertRejects(t, testCase.source, testCase.want)
	}
}

// Relative source paths keep their lexical resolution, .hex suffix rule, and
// above-root diagnostic unchanged.
func TestRelativeImportsRetainLexicalResolution(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    A from \"./math\"\n,\n    B from \"./math.hex\"\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "duplicate import of canonical module math")

	result = compiler.Compile(map[string]string{"app.hex": "import\n    Up from \"../up\"\nend\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "import resolves above the logical source-map root")
}

// A supplied std/program.hex reached through the relative "./std/program"
// spelling is rejected by the reserved-prefix Module Error.
func TestRelativeStdPathRetainsReservedPrefixError(t *testing.T) {
	result := compiler.Compile(map[string]string{
		"app.hex":         "import\n    P from \"./std/program\"\nend\nvalue: Int32 := 1\n",
		"std/program.hex": "value: Int32 := 1\nexport\n    value\nend\n",
	}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, `the "std" path prefix is reserved for the standard library`)
}

// A core-library module and an embedded source module resolve through the same
// dotted syntax while keeping their distinct artifact behavior.
func TestDottedStdlibArtifactOwnership(t *testing.T) {
	core := assertCompiles(t, "import\n    Io from std.io\nend\nout: Io.IO | Error := Io.stdout()\n")
	if _, ok := core.Files["hexal/io.h"]; !ok {
		t.Errorf("std.io must select the core component headers: %v", sortedKeys(core.Files))
	}
	for key := range core.Files {
		if strings.HasPrefix(key, "stdlib/") || strings.HasPrefix(key, "modules/std/") {
			t.Errorf("a core library must emit no module artifact, got %q", key)
		}
	}
	sourceModule := assertCompiles(t, "import\n    Ascii from std.ascii\nend\nd: Bool := Ascii.is_digit(48)\n")
	for _, key := range []string{"stdlib/ascii.c", "stdlib/ascii.h"} {
		if _, ok := sourceModule.Files[key]; !ok {
			t.Errorf("std.ascii must emit %s: %v", key, sortedKeys(sourceModule.Files))
		}
	}
}

// One canonical standard-library identity imported twice reports the existing
// duplicate-canonical diagnostic.
func TestDottedStdlibDuplicateImport(t *testing.T) {
	result := compileSource("import\n    A from std.io\n,\n    B from std.io\nend\n")
	assertStderrContains(t, result, "duplicate import of canonical module std/io")
}

// `std` stays contextual: it is legal as a declaration name, a member name,
// and an import alias outside the import-reference position.
func TestStdNameLegalOutsideImportReference(t *testing.T) {
	assertCompiles(t, "std: Int32 := 1\ntotal: Int32 := std + 1\n")
	assertCompiles(t, "type Box is struct std: Int32 end\nb: Box := Box(std = 1)\nvalue: Int32 := b.std\n")
	sources := map[string]string{
		"app.hex": "import\n    std from \"./lib\"\nend\nvalue: Int32 := std.answer()\n",
		"lib.hex": "fun answer(): Int32 do\n    return 42\nend\nexport\n    answer\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "lib")
}
