package tests

// RFC 0034 Task 3 grammar: module/import/export keywords, module-path
// literals, and dotted qualified types in type position. Grammar failures
// surface as Syntax Errors; well-formed imports resolve (Task 4) and fail
// only when their target module is absent.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestModuleImportResolvesToNotFound(t *testing.T) {
	// Task 4 resolution: "./math" is a valid module-path literal that
	// resolves; no source provides it, so the build fails with the
	// resolution diagnostic rather than a grammar error — the grammar
	// itself parsed.
	result := compileSource("module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want failure until ./math exists, got %#v", result)
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "imported module \"./math\" was not found") {
		t.Fatalf("first diagnostic = %#v, want the resolution error", result.Stderr)
	}
}

func TestModulePathWithHexSuffixParses(t *testing.T) {
	// The .hex suffix is a path spelling, not grammar; both spellings
	// resolve (a bare "./math" canonicalizes the same) and both fail only
	// at resolution, proving both parsed.
	assertRejects(t, "module M = import \"./math.hex\"\n", "imported module \"./math.hex\" was not found")
	assertRejects(t, "module M = import \"./math\"\n", "imported module \"./math\" was not found")
}

func TestModuleImportMissingPieces(t *testing.T) {
	assertRejects(t, "import \"./math\"", "'module'")
	assertRejects(t, "module = import \"./math\"", "an identifier after 'module'")
	assertRejects(t, "module Math import \"./math\"", "expected '='")
	assertRejects(t, "module Math = import", "a module path literal after 'import'")
}

func TestModulePathRejectsBackslashesAndEscapes(t *testing.T) {
	assertRejects(t, "module M = import \".\\\\math\"", "invalid module-path literal")
	assertRejects(t, "module M = import \"./ma\\u0074h\"", "invalid module-path literal")
	assertRejects(t, "module M = import \"./math", "unterminated module path literal")
}

func TestExportRequiresModuleLevelDeclaration(t *testing.T) {
	assertRejects(t, "export x: Int32 = 1", "export may prefix only a module-level type, function, or implementation declaration")
	assertRejects(t, "export x = 1", "export may prefix only a module-level type, function, or implementation declaration")
	assertRejects(t, "fun f()\n    export g: Int32 = 1\nend", "export may prefix only a module-level type, function, or implementation declaration")
}

func TestExportPrefixesDeclarations(t *testing.T) {
	assertCompiles(t, "export fun f(): Int32\n    return 1\nend\n")
	assertCompiles(t, "export type Point = { x: Int32, }\npoint: Point = Point { x = 1, }\n")
	// An exported method needs an exported receiver type (Task 5 closure).
	assertCompiles(t, "export type Point = { x: Int32, }\nexport impl Point.getX(): Int32\n    return self.x\nend\np: Point = Point { x = 1, }\nv: Int32 = p.getX()\n")
}

func TestQualifiedTypeParsesToUnknownAlias(t *testing.T) {
	assertRejects(t, "x: M.T = 1", "unknown module alias M")
	assertRejects(t, "x: M.A.B = 1", "unknown module alias M")
}

func TestPropertyChainsStillParseAsExpressions(t *testing.T) {
	assertCompiles(t, "type Point = { x: Int32, }\npoint: Point = Point { x = 1, }\nvalue: Int32 = point.x\n")
}
