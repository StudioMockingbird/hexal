package integration

// Module/import/export keywords, module-path literals, and dotted qualified
// types in type position parse here. Grammar failures surface as Syntax
// Errors; well-formed imports resolve and fail only when their target module
// is absent.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestModuleImportResolvesToNotFound(t *testing.T) {
	// "./math" is a valid module-path literal that resolves; no source
	// provides it, so the build fails with the resolution diagnostic rather
	// than a grammar error: the grammar itself parsed.
	result := compileSource("import\n    Math from \"./math\"\nend\nresult: Int32 := Math.add(2, 3)\n")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want failure until ./math exists; got %#v", result)
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "imported module ./math was not found") {
		t.Fatalf("first diagnostic = %#v, want the resolution error", result.Stderr)
	}
}

func TestModulePathWithHexSuffixParses(t *testing.T) {
	// The .hex suffix is a path spelling, not grammar; both spellings
	// resolve (a bare "./math" canonicalizes the same) and both fail only
	// at resolution, proving both parsed.
	assertRejects(t, "import\n    M from \"./math.hex\"\nend\n", "imported module ./math.hex was not found")
	assertRejects(t, "import\n    M from \"./math\"\nend\n", "imported module ./math was not found")
}

func TestModuleImportMissingPieces(t *testing.T) {
	assertRejects(t, "import end", "import block requires at least one entry")
	assertRejects(t, "import\n    Math\nend", "expected 'from' after an import alias")
	assertRejects(t, "import\n    Math from\nend", "a module path literal after 'from'")
	assertRejects(t, "import\n    from \"./math\"\nend", "an import alias")
}

func TestModulePathRejectsBackslashesAndEscapes(t *testing.T) {
	assertRejects(t, "import\n    M from \".\\\\math\"\nend", "invalid module-path literal")
	assertRejects(t, "import\n    M from \"./ma\\u0074h\"\nend", "invalid module-path literal")
	assertRejects(t, "import\n    M from \"./math\nend", "unterminated module path literal")
}

func TestExportRequiresModuleLevelDeclaration(t *testing.T) {
	assertRejects(t, "export x: Int32 := 1", "'end' to close the export block")
	assertRejects(t, "export x = 1", "'end' to close the export block")
	assertRejects(t, "fun f() do\n    export g: Int32 := 1\nend", "export may prefix only a module-level type, function, or implementation declaration")
}

func TestExportPrefixesDeclarations(t *testing.T) {
	assertCompiles(t, "fun f(): Int32 do\n    return 1\nend\nexport\n    f\nend\n")
	assertCompiles(t, "type Point is struct x: Int32, end\npoint: Point := Point(x = 1)\nexport\n    Point\nend\n")
	// An exported method needs an exported receiver type.
	assertCompiles(t, "type Point is struct x: Int32, end\nmethod Point.getX(): Int32 do\n    return self.x\nend\np: Point := Point(x = 1)\nv: Int32 := p.getX()\nexport\n    Point,\n    Point.getX\nend\n")
}

func TestQualifiedTypeParsesToUnknownAlias(t *testing.T) {
	assertRejects(t, "x: M.T := 1", "unknown module alias M")
	assertRejects(t, "x: M.A.B := 1", "unknown module alias M")
}

func TestPropertyChainsStillParseAsExpressions(t *testing.T) {
	assertCompiles(t, "type Point is struct x: Int32, end\npoint: Point := Point(x = 1)\nvalue: Int32 := point.x\n")
}
