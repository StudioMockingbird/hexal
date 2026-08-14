package tests

// RFC 0034 Task 3 grammar: module/import/export keywords, module-path
// literals, and dotted qualified types in type position. Semantic import
// resolution arrives with the module phase; until then every import fails
// with the structured "imports are not resolved yet" Module Error, which
// also proves the grammar itself parsed.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestModuleImportParsesToModuleError(t *testing.T) {
	source := "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want failure until imports resolve, got %#v", result)
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "imports are not resolved yet") {
		t.Fatalf("first diagnostic = %#v, want the import module error", result.Stderr)
	}
}

func TestModulePathWithHexSuffixParses(t *testing.T) {
	// The .hex suffix is a path spelling, not grammar; both forms reach the
	// same resolution failure, proving both parsed.
	assertRejects(t, "module M = import \"./math.hex\"\n", "imports are not resolved yet")
	assertRejects(t, "module M = import \"./math\"\n", "imports are not resolved yet")
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
	assertCompiles(t, "type Point = { x: Int32, }\nexport impl Point.getX(): Int32\n    return self.x\nend\np: Point = Point { x = 1, }\nv: Int32 = p.getX()\n")
}

func TestQualifiedTypeParsesToUnknownAlias(t *testing.T) {
	assertRejects(t, "x: M.T = 1", "unknown module alias M")
	assertRejects(t, "x: M.A.B = 1", "unknown module alias M")
}

func TestPropertyChainsStillParseAsExpressions(t *testing.T) {
	assertCompiles(t, "type Point = { x: Int32, }\npoint: Point = Point { x = 1, }\nvalue: Int32 = point.x\n")
}
