package integration

// Statement sequencing, comments, whitespace, and cross-stage diagnostics.

import (
	"hexal/compiler"
	"strings"
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
	want := "#include \"modules/app.h\"\n\nint main(void) {\n#line 1 \"app.hex\"\n    int32_t hex_v_x = 13;\n#line 1 \"app.hex\"\n    hex_v_x = 14;\n#line 2 \"app.hex\"\n    bool hex_v_flag = true;\n#line 2 \"app.hex\"\n    hex_v_flag = false;\n    return 0;\n}\n"
	if rootC(t, result) != want {
		t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
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

	want := "#include \"modules/app.h\"\n\nint main(void) {\n#line 2 \"app.hex\"\n    int32_t hex_v_x = 13;\n#line 4 \"app.hex\"\n    hex_v_x = 14;\n    return 0;\n}\n"
	if rootC(t, result) != want {
		t.Fatalf("commented modules/app.c = %q, want %q", rootC(t, result), want)
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

func TestParseErrorsAbortBeforeChecking(t *testing.T) {
	// RFC 0034 Task 4: a parse failure in any module fails the build with
	// the parse diagnostics; the checker never sees invalid syntax, so its
	// diagnostics are not stacked on top (earliest diagnostic ownership).
	result := compileSource("x: Int32 = 13 y z: Bogus = 1")
	want := []string{
		"[Syntax Error] expected ':' for a declaration or '=' for an assignment at 1:17",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
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

// RFC 0061: every structured body opens with an explicit delimiter. `do`
// opens function, method, while, and for bodies; `then` opens if, elseif, and
// match-arm bodies; `else` is its own opener.
func TestExplicitBlockOpenersAccepted(t *testing.T) {
	assertCompiles(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nvalue: Int32 = identity(1)\n")
	assertCompiles(t, "export fun generic<T>(value: T): T do\n    return value\nend\n")
	assertCompiles(t, "fun recursive(value: Int32): Int32 do\n    return recursive(value)\nend\n")
	assertCompiles(t, "type Point = { x: Int32, }\nimpl Point.getX(): Int32 do\n    return self.x\nend\np: Point = Point { x = 1, }\nvalue: Int32 = p.getX()\n")
	assertCompiles(t, "fun reset() do\nend\nreset()\n")
	assertCompiles(t, "mut value: Int32 = 1 if value > 0 then value = 0 elseif value == 0 then value = 1 else value = 2 end\n")
	assertCompiles(t, "mut value: Int32 = 1 while value > 0 do value = 0 end\n")
	assertCompiles(t, "fun sum(): Int32 do\n    values: Array<Int32, 2> = [1, 2]\n    mut total: Int32 = 0\n    for item in values do\n        total = item\n    end\n    return total\nend\nsum()\n")
	assertCompiles(t, "value: Int32 = match 1\n| else then 1\nend\n")
	assertCompiles(t, "fun choose(flag: Bool): Int32 do\n    if flag then\n        return 1\n    elseif !flag then\n        return 2\n    else\n        return 3\n    end\nend\n")
}

// The opener may sit on the next line or after a comment (RFC 0061: the
// opener is separated by ordinary lexical separation, not a newline rule).
func TestExplicitBlockOpenerPlacement(t *testing.T) {
	assertCompiles(t, "fun identity(value: Int32): Int32\n    do\n    return value\nend\nvalue: Int32 = identity(1)\n")
	assertCompiles(t, "fun identity(value: Int32): Int32 do -- opener after comment\n    return value\nend\nvalue: Int32 = identity(1)\n")
	assertCompiles(t, "mut value: Int32 = 1 if value > 0 -- condition\n    then\n    value = 0\nend\n")
}

func TestExplicitBlockOpenersRejectFormerForms(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun f()\nend", "expected 'do' after function signature"},
		{"fun f(): Int32\n    return 1\nend\n", "expected 'do' after function signature"},
		{"impl Point.m()\nend", "expected 'do' after method signature"},
		{"if flag\n    noop: Int32 = 1\nend", "expected 'then' after if condition"},
		{"if flag noop: Int32 = 1 end", "expected 'then' after if condition"},
		{"mut flag: Bool = true if flag then noop: Int32 = 1 elseif !flag\n    noop: Int32 = 2\nend", "expected 'then' after elseif condition"},
		// The delimiters are not interchangeable.
		{"fun f() then\nend", "expected 'do' after function signature"},
		{"if flag do\n    noop: Int32 = 1\nend", "expected 'then' after if condition"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

// Missing-delimiter recovery keeps the following branches and sibling
// statements available (RFC 0061: recovery must not consume elseif, else, or
// end as part of a malformed condition or body). The delimiters surface as
// statement-level diagnostics instead of being swallowed by the broken if.
func TestExplicitBlockOpenerRecoveryPreservesBranches(t *testing.T) {
	result := compileSource("if true\n    noop: Int32 = 1\nelseif false\n    noop: Int32 = 2\nelse\n    noop: Int32 = 3\nend\nsibling: Int32 = 4\n")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected 'then' after if condition") {
		t.Fatalf("stderr = %#v, want the missing-then diagnostic", result.Stderr)
	}
	joined := strings.Join(result.Stderr, "\n")
	for _, want := range []string{
		"unexpected 'elseif' outside an if statement",
		"unexpected 'else' outside an if statement",
		"unexpected 'end' outside a block",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stderr = %#v, want branch/end delimiters preserved, not consumed: %q", result.Stderr, want)
		}
	}
}
