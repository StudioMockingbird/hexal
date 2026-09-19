package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// A fixed top-level `let` in an imported module is a module constant: one
// immutable object readable by the module's own functions.
func TestModuleConstantReadByImportedFunction(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Limits from \"./limits\"\nend\nlet ok: Bool = Limits.valid(5)\n",
		"limits.hex": "let MaxTouchPoints: Int32 = 10\n" +
			"fun valid(count: Int32): Bool do\n    return count <= MaxTouchPoints\nend\n" +
			"export\n    valid\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.Files["modules/limits.c"], "static const int32_t hex_v_") {
		t.Fatalf("private constant did not lower to static const:\n%s", result.Files["modules/limits.c"])
	}
}

// An imported module may not declare mutable top-level state.
func TestImportedMutableTopLevelRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\n",
		"lib.hex": "let mut count: Int32 = 0\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot declare mutable top-level binding count") {
		t.Fatalf("want mutable-top-level rejection; got %v", result.Stderr)
	}
}

// A fixed imported declaration whose initializer is not in the closed static
// set is rejected.
func TestImportedNonStaticConstantRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\n",
		"lib.hex": "fun make(): Int32 do\n    return 1\nend\nlet value: Int32 = make()\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "module constant value must be statically initialized") {
		t.Fatalf("want static-initializer rejection; got %v", result.Stderr)
	}
}

// A constant whose type contains Atomic is rejected; Atomic is mutable runtime
// state even when its binding is fixed.
func TestImportedAtomicConstantRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\n",
		"lib.hex": "let counter: Atomic<Int32> = Atomic<Int32>(0)\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot contain mutable Atomic state") {
		t.Fatalf("want Atomic rejection; got %v", result.Stderr)
	}
}

// An exported constant emits one external definition plus a matching
// `extern const` header declaration; a private one is `static const`.
func TestExportedConstantLowersWithExternConst(t *testing.T) {
	sources := map[string]string{
		"app.hex":    "import\n    Limits from \"./limits\"\nend\nlet port: Int32 = Limits.DefaultPort\n",
		"limits.hex": "let DefaultPort: Int32 = 8080\nexport\n    DefaultPort\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.Files["modules/limits.h"], "extern const int32_t hex_v_") {
		t.Fatalf("exported constant header missing extern const:\n%s", result.Files["modules/limits.h"])
	}
	if strings.Contains(result.Files["modules/limits.c"], "static const") {
		t.Fatalf("exported constant must not be static:\n%s", result.Files["modules/limits.c"])
	}
}

// A constant initializer may not reference a binding, including another
// constant, so no dependency graph or cycle rule is needed.
func TestConstantCannotReferenceBinding(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\n",
		"lib.hex": "let a: Int32 = 1\nlet b: Int32 = a\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "module constant b must be statically initialized") {
		t.Fatalf("want binding-reference rejection; got %v", result.Stderr)
	}
}

// A constant's annotation obeys source-order type visibility even though its
// value name is visible to every function in the module.
func TestConstantTypeVisibilityIsSourceOrdered(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\n",
		"lib.hex": "let origin: Point = Point(x = 1)\ntype Point is struct x: Int32 end\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "unknown type Point") {
		t.Fatalf("want source-order type rejection; got %v", result.Stderr)
	}
}

// `static` is an ordinary identifier now, valid in every declaration role.
func TestStaticIsAnOrdinaryIdentifier(t *testing.T) {
	result := compileSource("let static: Int32 = 1\nprint(static)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}
