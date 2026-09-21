package integration

// Statement sequencing, comments, whitespace, and cross-stage diagnostics.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestReportsIndependentCheckerErrors(t *testing.T) {
	result := compileSource("let x: Bogus = 2147483648")
	want := []string{
		"[Type Error] unknown type Bogus at app.hex:1:8",
		"[Type Error] given value is outside the Int32 range at app.hex:1:16",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestCollectsLexerDiagnostics(t *testing.T) {
	result := compileSource("let x: Int32 = $ #")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{
		"[Syntax Error] unexpected character '$' at app.hex:1:16",
		"[Syntax Error] unexpected character '#' at app.hex:1:18",
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
	want := []string{"[Syntax Error] identifiers must begin with a letter at app.hex:1:1"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsEqualsInValueBinding(t *testing.T) {
	for _, testCase := range []struct {
		name, source, want string
	}{
		{"typed binding", "x: Int32 = 13", "[Syntax Error] declarations require 'let' at app.hex:1:1"},
		{"mutable binding", "mut x = 13", "[Syntax Error] 'mut' appears only immediately after 'let' in a declaration at app.hex:1:1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
				t.Fatalf("compile result = %#v, want one diagnostic %q", result, testCase.want)
			}
		})
	}
}

func TestMultipleStatements(t *testing.T) {
	result := compileSource("let mut x: Int32 = 13 x = 14\nlet mut flag: Bool = true flag = false")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	want := "#include \"modules/app.h\"\n\nint main(void) {\n#line 1 \"app.hex\"\n    int32_t hex_v_x = 13;\n#line 1 \"app.hex\"\n    hex_v_x = 14;\n#line 2 \"app.hex\"\n    bool hex_v_flag = true;\n#line 2 \"app.hex\"\n    hex_v_flag = false;\n    return 0;\n}\n"
	if rootC(t, result) != want {
		t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
	}
}

func TestWhitespaceDoesNotAffectStatements(t *testing.T) {
	withNewline := compileSource("let mut x: Int32 = 13\nx = 14")
	withSpace := compileSource("let mut x: Int32 = 13 x = 14")
	if withNewline.ExitCode != compiler.ExitSuccess || withSpace.ExitCode != compiler.ExitSuccess {
		t.Fatalf("whitespace variants failed: newline := %v space=%v", withNewline.Stderr, withSpace.Stderr)
	}
	if withoutLineDirectives(rootC(t, withNewline)) != withoutLineDirectives(rootC(t, withSpace)) {
		t.Fatalf("whitespace changed generated C:\nnewline=%q\nspace=%q", rootC(t, withNewline), rootC(t, withSpace))
	}
}

func TestComments(t *testing.T) {
	result := compileSource("--- counter\nlet mut x: Int32 = --[ value\n  follows ]-- 13 -- update next\nx = 14")
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
	result := compileSource("let x: Int32 = 13;")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Syntax Error] unexpected character ';' at app.hex:1:18"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsTypedReassignment(t *testing.T) {
	result := compileSource("let x: Int32 = 13 let x: Int32 = 14")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Type Error] variable x is already declared in this scope; use '=' for reassignment at app.hex:1:23"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestRejectsUnknownAssignment(t *testing.T) {
	result := compileSource("x = 13")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Type Error] unknown variable x at app.hex:1:1"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestReportsIndependentStatementErrors(t *testing.T) {
	result := compileSource("let x: Bogus = 1 let y: Bogus = 2")
	want := []string{
		"[Type Error] unknown type Bogus at app.hex:1:8",
		"[Type Error] unknown type Bogus at app.hex:1:25",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestParseErrorsAbortBeforeChecking(t *testing.T) {
	// A parse failure in any module fails the build with the parse
	// diagnostics; the checker never sees invalid syntax, so its diagnostics
	// are not stacked on top (earliest diagnostic ownership).
	result := compileSource("let x: Int32 = 13 y let z: Bogus = 1")
	want := []string{
		"[Syntax Error] expected '=' for an assignment at app.hex:1:21",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

func TestDoesNotBindFailedDeclaration(t *testing.T) {
	result := compileSource("let bad: Bogus = 1 x = 2")
	want := []string{
		"[Type Error] unknown type Bogus at app.hex:1:10",
		"[Type Error] unknown variable x at app.hex:1:20",
	}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] || result.Stderr[1] != want[1] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}

// Every structured body opens with an explicit delimiter. `do` opens
// function, method, while, and for bodies; `then` opens if, elseif, and
// match-arm bodies; `else` is its own opener.
func TestExplicitBlockOpenersAccepted(t *testing.T) {
	assertCompiles(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nlet value: Int32 = identity(1)\n")
	assertCompiles(t, "fun generic<T>(value: T): T do\n    return value\nend\nexport\n    generic\nend\n")
	assertCompiles(t, "fun recursive(value: Int32): Int32 do\n    return recursive(value)\nend\n")
	assertCompiles(t, "type Point is struct x: Int32, end\nmethod Point.getX(): Int32 do\n    return self.x\nend\nlet p: Point = Point(x = 1,)\nlet value: Int32 = p.getX()\n")
	assertCompiles(t, "fun reset() do\nend\nreset()\n")
	assertCompiles(t, "let mut value: Int32 = 1 if value > 0 then value = 0 elseif value == 0 then value = 1 else value = 2 end\n")
	assertCompiles(t, "let mut value: Int32 = 1 while value > 0 do value = 0 end\n")
	assertCompiles(t, "fun sum(): Int32 do\n    let values: Array<Int32, 2> = [1, 2]\n    let mut total: Int32 = 0\n    for item in values do\n        total = item\n    end\n    return total\nend\nsum()\n")
	assertCompiles(t, "let value: Int32 = match 1\n| else then 1\nend\n")
	assertCompiles(t, "fun choose(flag: Bool): Int32 do\n    if flag then\n        return 1\n    elseif !flag then\n        return 2\n    else\n        return 3\n    end\nend\n")
}

// The opener may sit on the next line or after a comment: it is separated
// by ordinary lexical separation, not a newline rule.
func TestExplicitBlockOpenerPlacement(t *testing.T) {
	assertCompiles(t, "fun identity(value: Int32): Int32\n    do\n    return value\nend\nlet value: Int32 = identity(1)\n")
	assertCompiles(t, "fun identity(value: Int32): Int32 do -- opener after comment\n    return value\nend\nlet value: Int32 = identity(1)\n")
	assertCompiles(t, "let mut value: Int32 = 1 if value > 0 -- condition\n    then\n    value = 0\nend\n")
}

func TestExplicitBlockOpenersRejectFormerForms(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun f()\nend", "expected 'do' after function signature"},
		{"fun f(): Int32\n    return 1\nend\n", "expected 'do' after function signature"},
		{"method Point.m()\nend", "expected 'do' after method signature"},
		{"if flag\n    let noop: Int32 = 1\nend", "expected 'then' after if condition"},
		{"if flag let noop: Int32 = 1 end", "expected 'then' after if condition"},
		{"let mut flag: Bool = true if flag then let noop: Int32 = 1 elseif !flag\n    let noop: Int32 = 2\nend", "expected 'then' after elseif condition"},
		// The delimiters are not interchangeable.
		{"fun f() then\nend", "expected 'do' after function signature"},
		{"if flag do\n    let noop: Int32 = 1\nend", "expected 'then' after if condition"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

// Missing-delimiter recovery keeps the following branches and sibling
// statements available: recovery must not consume elseif, else, or end as
// part of a malformed condition or body. The delimiters surface as
// statement-level diagnostics instead of being swallowed by the broken if.
func TestExplicitBlockOpenerRecoveryPreservesBranches(t *testing.T) {
	result := compileSource("if true\n    let noop: Int32 = 1\nelseif false\n    let noop: Int32 = 2\nelse\n    let noop: Int32 = 3\nend\nlet sibling: Int32 = 4\n")
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

// Every removed spelling produces a diagnostic naming its replacement; one
// negative test per removed operation.
func TestRemovedMethodSpellingsDiagnoseReplacement(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let fixed: Array<Int32, 2> = [1, 2] let bad: Int32 = fixed.at(0)", "Array<Int32, 2> has no method at"},
		{"fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    let first: Int32 = values.at(0)\nend", "List<Int32> has no method at"},
		{"let text: String = \"hi\" let first: Byte = text.at(0)", "String has no method at"},
		{"let label: String<8> = \"hi\" let first: Byte = label.at(0)", "String<8> has no method at"},
		{"let fixed: Array<Int32, 2> = [1, 2] let bad: Bool = fixed.is_empty()", "Array<Int32, 2> has no method is_empty"},
		{"let fixed: Array<Int32, 3> = [1, 2, 3] let view: Slice<Int32> = fixed.slice(0, 2) let bad: Bool = view.is_empty()", "Slice<Int32> has no method is_empty"},
		{"fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    let empty: Bool = values.is_empty()\nend", "List<Int32> has no method is_empty"},
		{"let text: String = \"hi\" let bad: Bool = text.is_empty()", "String has no method is_empty"},
		{"let label: String<8> = \"hi\" let bad: Bool = label.is_empty()", "String<8> has no method is_empty"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

// String.bytes is retained because its replacement would be asymptotically
// worse; it still lowers to its constant-time helper (positive test, not an
// absence check). is_empty was removed from every text form once the stored
// byte length made length() == 0 the identical O(1) test.
func TestRetainedTextOperationsKeepConstantTimeHelpers(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): Bool do\n    let text: String = \"hello\"\n    let raw: Slice<UInt8> = text.bytes()\n    let label: String<8> = \"hexal\"\n    return (text.length() == 0) and (label.length() == 0)\nend\n")
	for _, want := range []string{
		"hex_text_bytes(",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output lacks the retained constant-time helper %q", want)
		}
	}
}
