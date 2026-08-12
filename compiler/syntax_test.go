package compiler

// Statement sequencing, comments, whitespace, and cross-stage diagnostics.

import "testing"

func TestReportsIndependentCheckerErrors(t *testing.T) {
	result := Compile("x: Bogus = 2147483648")
	want := []string{
		"[Type Error] unknown type Bogus at 1:4",
		"[Type Error] given value is outside the Int32 range at 1:12",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestCollectsLexerDiagnostics(t *testing.T) {
	result := Compile("x: Int32 = @ #")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
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
	result := Compile("9x:Int32 = 13")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	want := []string{"[Syntax Error] identifiers must begin with a letter at 1:1"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestMultipleStatements(t *testing.T) {
	result := Compile("mut x: Int32 = 13 x = 14\nmut flag: Bool = true flag = false")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	want := "#include \"main.h\"\n\nint main(void) {\n#line 1 \"main.hex\"\n    int32_t hex_v_x = 13;\n#line 1 \"main.hex\"\n    hex_v_x = 14;\n#line 2 \"main.hex\"\n    bool hex_v_flag = true;\n#line 2 \"main.hex\"\n    hex_v_flag = false;\n    return EXIT_SUCCESS;\n}\n"
	if result.MainC != want {
		t.Fatalf("main.c = %q, want %q", result.MainC, want)
	}
}

func TestWhitespaceDoesNotAffectStatements(t *testing.T) {
	withNewline := Compile("mut x: Int32 = 13\nx = 14")
	withSpace := Compile("mut x: Int32 = 13 x = 14")
	if withNewline.ExitCode != ExitSuccess || withSpace.ExitCode != ExitSuccess {
		t.Fatalf("whitespace variants failed: newline=%v space=%v", withNewline.Stderr, withSpace.Stderr)
	}
	if withoutLineDirectives(withNewline.MainC) != withoutLineDirectives(withSpace.MainC) {
		t.Fatalf("whitespace changed generated C:\nnewline=%q\nspace=%q", withNewline.MainC, withSpace.MainC)
	}
}

func TestComments(t *testing.T) {
	result := Compile("--- counter\nmut x: Int32 = --[ value\n  follows ]-- 13 -- update next\nx = 14")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile comments exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if len(result.Stderr) != 0 {
		t.Fatalf("Compile comments stderr = %#v, want empty", result.Stderr)
	}

	want := "#include \"main.h\"\n\nint main(void) {\n#line 2 \"main.hex\"\n    int32_t hex_v_x = 13;\n#line 4 \"main.hex\"\n    hex_v_x = 14;\n    return EXIT_SUCCESS;\n}\n"
	if result.MainC != want {
		t.Fatalf("commented main.c = %q, want %q", result.MainC, want)
	}
}

func TestRejectsSemicolon(t *testing.T) {
	result := Compile("x: Int32 = 13;")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	want := []string{"[Syntax Error] unexpected character ';' at 1:14"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsTypedReassignment(t *testing.T) {
	result := Compile("x: Int32 = 13 x: Int32 = 14")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	want := []string{"[Type Error] variable x is already declared; reassignment must omit the type annotation at 1:15"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsUnknownAssignment(t *testing.T) {
	result := Compile("x = 13")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	want := []string{"[Type Error] unknown variable x at 1:1"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestReportsIndependentStatementErrors(t *testing.T) {
	result := Compile("x: Bogus = 1 y: Bogus = 2")
	want := []string{
		"[Type Error] unknown type Bogus at 1:4",
		"[Type Error] unknown type Bogus at 1:17",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestReportsParserAndCheckerErrorsTogether(t *testing.T) {
	result := Compile("x: Int32 = 13 y z: Bogus = 1")
	want := []string{
		"[Syntax Error] expected ':' for a declaration or '=' for an assignment at 1:17",
		"[Type Error] unknown type Bogus at 1:20",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestDoesNotBindFailedDeclaration(t *testing.T) {
	result := Compile("bad: Bogus = 1 x = 2")
	want := []string{
		"[Type Error] unknown type Bogus at 1:6",
		"[Type Error] unknown variable x at 1:16",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}
