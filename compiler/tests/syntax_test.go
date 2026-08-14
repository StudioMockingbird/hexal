package tests

// Statement sequencing, comments, whitespace, and cross-stage diagnostics.

import (
	"hexal/compiler"
	"testing"
)

func TestReportsIndependentCheckerErrors(t *testing.T) {
	result := compileSource("x: Bogus = 2147483648")
	want := []string{
		"[Type Error] unknown type Bogus at 1:4",
		"[Type Error] given value is outside the Int32 range at 1:12",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestCollectsLexerDiagnostics(t *testing.T) {
	result := compileSource("x: Int32 = @ #")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{
		"[Syntax Error] unexpected character '@' at 1:12",
		"[Syntax Error] unexpected character '#' at 1:14",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsInvalidDeclarationSyntax(t *testing.T) {
	result := compileSource("9x:Int32 = 13")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Syntax Error] identifiers must begin with a letter at 1:1"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestMultipleStatements(t *testing.T) {
	result := compileSource("mut x: Int32 = 13 x = 14\nmut flag: Bool = true flag = false")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	want := "#include \"main.h\"\n#include \"modules/app.h\"\n\nint hex_module_root_run(void) {\n#line 1 \"app.hex\"\n    int32_t hex_v_x = 13;\n#line 1 \"app.hex\"\n    hex_v_x = 14;\n#line 2 \"app.hex\"\n    bool hex_v_flag = true;\n#line 2 \"app.hex\"\n    hex_v_flag = false;\n    return EXIT_SUCCESS;\n}\n"
	if rootC(t, result) != want {
		t.Fatalf("main.c = %q, want %q", rootC(t, result), want)
	}
}

func TestWhitespaceDoesNotAffectStatements(t *testing.T) {
	withNewline := compileSource("mut x: Int32 = 13\nx = 14")
	withSpace := compileSource("mut x: Int32 = 13 x = 14")
	if withNewline.ExitCode != compiler.ExitSuccess || withSpace.ExitCode != compiler.ExitSuccess {
		t.Fatalf("whitespace variants failed: newline=%v space=%v", withNewline.Stderr, withSpace.Stderr)
	}
	if withoutLineDirectives(rootC(t, withNewline)) != withoutLineDirectives(rootC(t, withSpace)) {
		t.Fatalf("whitespace changed generated C:\nnewline=%q\nspace=%q", rootC(t, withNewline), rootC(t, withSpace))
	}
}

func TestComments(t *testing.T) {
	result := compileSource("--- counter\nmut x: Int32 = --[ value\n  follows ]-- 13 -- update next\nx = 14")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile comments exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if len(result.Stderr) != 0 {
		t.Fatalf("Compile comments stderr = %#v, want empty", result.Stderr)
	}

	want := "#include \"main.h\"\n#include \"modules/app.h\"\n\nint hex_module_root_run(void) {\n#line 2 \"app.hex\"\n    int32_t hex_v_x = 13;\n#line 4 \"app.hex\"\n    hex_v_x = 14;\n    return EXIT_SUCCESS;\n}\n"
	if rootC(t, result) != want {
		t.Fatalf("commented main.c = %q, want %q", rootC(t, result), want)
	}
}

func TestRejectsSemicolon(t *testing.T) {
	result := compileSource("x: Int32 = 13;")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Syntax Error] unexpected character ';' at 1:14"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsTypedReassignment(t *testing.T) {
	result := compileSource("x: Int32 = 13 x: Int32 = 14")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Type Error] variable x is already declared; reassignment must omit the type annotation at 1:15"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsUnknownAssignment(t *testing.T) {
	result := compileSource("x = 13")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Type Error] unknown variable x at 1:1"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestReportsIndependentStatementErrors(t *testing.T) {
	result := compileSource("x: Bogus = 1 y: Bogus = 2")
	want := []string{
		"[Type Error] unknown type Bogus at 1:4",
		"[Type Error] unknown type Bogus at 1:17",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestReportsParserAndCheckerErrorsTogether(t *testing.T) {
	result := compileSource("x: Int32 = 13 y z: Bogus = 1")
	want := []string{
		"[Syntax Error] expected ':' for a declaration or '=' for an assignment at 1:17",
		"[Type Error] unknown type Bogus at 1:20",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestDoesNotBindFailedDeclaration(t *testing.T) {
	result := compileSource("bad: Bogus = 1 x = 2")
	want := []string{
		"[Type Error] unknown type Bogus at 1:6",
		"[Type Error] unknown variable x at 1:16",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}
