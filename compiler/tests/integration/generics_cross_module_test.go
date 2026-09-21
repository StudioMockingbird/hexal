package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// An exported generic function whose own body constructs its module's own
// exported generic type.
func TestCrossModuleGenericFunctionConstructsOwnGenericType(t *testing.T) {
	sources := map[string]string{
		"boxes.hex": "type Box<T> is struct\n    item: T,\nend\nfun new_box<T>(value: T): Box<T> do\n    return Box<T>(item = value)\nend\nexport\n    Box, new_box\nend\n",
		"app.hex":   "import\n    Boxes from \"./boxes\"\nend\nlet box = Boxes.new_box<Int32>(1)\nprint(box.item)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// A cross-module specialization's definition and prototype keep external
// linkage in its defining module, matching the compatible declaration every
// consumer's header carries; the generated program actually links.
func TestCrossModuleGenericFunctionSpecializationIsExternalLinkage(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "fun identity<T>(value: T): T do\n    return value\nend\nexport\n    identity\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet result: Int32 = Lib.identity<Int32>(1)\nprint(result)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	libC := result.Files["modules/lib.c"]
	const symbol = "hex_f_m3_lib_identity_Int32"
	if strings.Contains(libC, "static "+standaloneSpellingPrefix(libC, symbol)) {
		t.Fatalf("defining module kept static linkage for an imported specialization: %q", libC)
	}
	if !strings.Contains(libC, symbol+"(") {
		t.Fatalf("defining module is missing the specialization entirely: %q", libC)
	}
	appH := result.Files["modules/app.h"]
	if !strings.Contains(appH, symbol) {
		t.Fatalf("importer header is missing the compatible foreign prototype: %q", appH)
	}
}

// standaloneSpellingPrefix isolates whatever precedes symbol on its own
// definition line, so the static-linkage check above does not also match an
// unrelated "static" earlier in the file.
func standaloneSpellingPrefix(source, symbol string) string {
	index := strings.Index(source, symbol+"(")
	if index < 0 {
		return ""
	}
	lineStart := strings.LastIndex(source[:index], "\n") + 1
	return source[lineStart:index]
}

// An exported generic function's body resolves its own module's private
// type -- previously rechecked in the importer's environment, where the
// private type does not exist.
func TestCrossModuleGenericFunctionUsesOwnPrivateType(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "type Wrapper is struct\n    value: Int32,\nend\nfun make<T>(seed: T): Int32 do\n    let w: Wrapper = Wrapper(value = 1)\n    return w.value\nend\nexport\n    make\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet result: Int32 = Lib.make<Int32>(1)\nprint(result)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// A defining-module diagnostic (a type error inside the generic body) names
// the defining module's own logical key, never the requester's.
func TestCrossModuleGenericFunctionDiagnosticUsesDefiningLogicalKey(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "fun broken<T>(value: T): Int32 do\n    return \"not a number\"\nend\nexport\n    broken\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet result: Int32 = Lib.broken<Int32>(1)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("expected the specialized body's type error to fail compilation")
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "lib.hex") {
		t.Fatalf("stderr = %#v, want the defining module's logical key (lib.hex)", result.Stderr)
	}
}

// Qualified generic type syntax: an explicit annotation and a caller-owned
// (importer-defined) nominal argument, the generated-C stress case.
func TestQualifiedGenericTypeWithImporterOwnedArgument(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "type Box<T> is struct\n    item: T,\nend\nexport\n    Box\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\ntype Point is struct\n    x: Int32,\nend\nlet box: Lib.Box<Point> = Lib.Box<Point>(item = Point(x = 1))\nprint(box.item.x)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// A qualified generic method call on an imported specialized receiver
// resolves through the defining module's own generic method table.
func TestQualifiedGenericMethodOnImportedSpecialization(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "type Box<T> is struct\n    item: T,\nend\nmethod Box<T>.get(): T do\n    return self.item\nend\nexport\n    Box, Box.get\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet box = Lib.Box<Int32>(item = 5)\nlet result: Int32 = box.get()\nprint(result)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// Nominal identity is unaffected: two modules' own same-named Point types
// specializing the same imported generic stay distinct.
func TestQualifiedGenericTypeKeepsDistinctNominalIdentity(t *testing.T) {
	sources := map[string]string{
		"a.hex": "type Point is struct\n    x: Int32,\nend\nexport\n    Point\nend\n",
		"b.hex": "type Point is struct\n    y: Int32,\nend\nexport\n    Point\nend\n",
		"app.hex": "import\n    A from \"./a\",\n    B from \"./b\"\nend\n" +
			"let pa: A.Point = A.Point(x = 1)\nlet pb: B.Point = B.Point(y = 2)\nprint(pa.x)\nprint(pb.y)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// An unknown qualified generic type keeps the same visibility diagnostic
// shape as an unknown non-generic qualified type.
func TestQualifiedGenericTypeUnknownNameDiagnostic(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "type Box<T> is struct\n    item: T,\nend\nexport\n    Box\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet box: Lib.Missing<Int32> = box\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("expected an unknown qualified generic type to fail")
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "private") {
		t.Fatalf("stderr = %#v, want a visibility diagnostic for the unknown name", result.Stderr)
	}
}

// A wrong explicit generic argument count on a qualified type keeps the
// exact arity diagnostic, anchored at the qualified use.
func TestQualifiedGenericTypeWrongArityDiagnostic(t *testing.T) {
	sources := map[string]string{
		"lib.hex": "type Pair<A, B> is struct\n    first: A,\n    second: B,\nend\nexport\n    Pair\nend\n",
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet value: Lib.Pair<Int32> = value\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("expected a wrong-arity qualified generic type to fail")
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expects 2 type arguments; got 1") {
		t.Fatalf("stderr = %#v, want the exact arity diagnostic", result.Stderr)
	}
}
