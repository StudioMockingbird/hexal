package checker

// Module import scaffolding: alias registration, import ordering, and the
// declaration-only rule for imported modules.

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func requireMessage(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("CheckModules accepted the program, want %q", want)
	}
	diagnostics, ok := err.(compilerTypes.Diagnostics)
	if !ok {
		t.Fatalf("error type = %T, want Diagnostics", err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Message == want {
			return
		}
	}
	t.Fatalf("diagnostics = %v, want message %q", diagnostics, want)
}

// The single-module path registers an import alias and leaves the alias
// invisible to value lookup: a bare alias reference fails as an ordinary
// unknown variable until the missing path is reported.
func TestImportAliasIsNotAValue(t *testing.T) {
	requireAccepted(t, "module Math = import \"./math\"\n")
	requireDiagnostic(t, "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n", "unknown variable Math")
}

// An import alias colliding with an existing module binding is a Name Error
// at the alias; a later import redeclaring an alias is the same conflict.
func TestImportAliasConflictsWithExistingName(t *testing.T) {
	requireDiagnostic(t, "module Math = import \"./math\"\nmodule Math = import \"./math2\"\n", "import alias Math conflicts with an existing name")
}

// Imports must form the module's prefix; the parser ends the prefix at the
// first non-import top-level item and rejects any later import as a Syntax
// Error, so the checker never sees a misplaced import.
func TestImportsMustPrecedeAllOtherItems(t *testing.T) {
	tokens, lexErr := lexer.Lex("x: Int32 = 1\nmodule Math = import \"./math\"\n")
	if lexErr != nil {
		t.Fatalf("Lex returned an error: %v", lexErr)
	}
	if _, parseErr := parser.Parse(tokens); parseErr == nil {
		t.Fatal("Parse accepted an import after a declaration")
	} else if message := parseErr.Error(); !strings.Contains(message, "imports must precede all other top-level items") {
		t.Fatalf("Parse error = %v, want the misplaced-import Syntax Error", message)
	}
}

// An imported module's top level is declarations only; every executable
// statement is rejected and skipped entirely.
func TestImportedModuleRejectsExecutableStatements(t *testing.T) {
	app := parseProgram(t, "module Math = import \"./vec3\"\n")
	dep := parseProgram(t, "x: Int32 = 1\n")
	_, err := CheckModules(map[string]parser.Program{"app.hex": app, "vec3.hex": dep}, []string{"vec3", "app"}, "app")
	requireMessage(t, err, "imported module vec3 contains executable statements")
}

// A dependency with only declarations checks clean in its own scope.
func TestImportedModuleDeclarationsOnly(t *testing.T) {
	app := parseProgram(t, "module Math = import \"./vec3\"\n")
	dep := parseProgram(t, "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n")
	checked, err := CheckModules(map[string]parser.Program{"app.hex": app, "vec3.hex": dep}, []string{"vec3", "app"}, "app")
	if err != nil {
		t.Fatalf("CheckModules rejected declarations-only modules: %v", err)
	}
	if len(checked["app.hex"].Statements) != 0 {
		t.Fatalf("app statements = %d, want 0", len(checked["app.hex"].Statements))
	}
}

// A function parameter may not shadow an import alias.
func TestParameterCannotShadowImportAlias(t *testing.T) {
	requireDiagnostic(t, "module Math = import \"./math\"\nfun f(Math: Int32) do\nend\n", "import alias Math conflicts with an existing name")
}

func TestCanonicalModuleID(t *testing.T) {
	cases := []struct {
		fromModule string
		path       string
		want       string
	}{
		{"app", "./math", "math"},
		{"app", "./math.hex", "math"},
		{"app", "./graphics/shapes", "graphics/shapes"},
		{"graphics/app", "../shared/tools", "shared/tools"},
		{"graphics/app", "./shared/tools.hex", "graphics/shared/tools"},
		{"app", "math/vec3", "math/vec3"},
	}
	for _, item := range cases {
		if got := canonicalModuleID(item.fromModule, item.path); got != item.want {
			t.Fatalf("canonicalModuleID(%q, %q) = %q, want %q", item.fromModule, item.path, got, item.want)
		}
	}
}
