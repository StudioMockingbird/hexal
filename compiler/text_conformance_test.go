package compiler

import (
	"strings"
	"testing"
)

// RFC 0044 integration tests: Byte alias, byte/rune literals, the String and
// Strand surfaces, RuneCursor, and the from_bytes/from_runes constructors.

func TestByteAliasIsUInt8(t *testing.T) {
	source := "byte: Byte = b'A'\nnumber: UInt8 = byte\nagain: Byte = number\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "const uint8_t sw_v_byte = 65;") {
		t.Fatalf("generated C lacks the Byte value:\n%s", result.MainC)
	}
}

func TestByteLiteralsCompile(t *testing.T) {
	source := "ascii: UInt8 = b'A'\nnewline: Byte = b'\\n'\nraw: Byte = b'\\xFF'\nzero: Byte = b'\\0'\nquote: Byte = b'\\''\nbackslash: Byte = b'\\\\'\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, want := range []string{"= 65;", "= 10;", "= 255;", "= 0;", "= 39;", "= 92;"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("generated C lacks %s:\n%s", want, result.MainC)
		}
	}
}

func TestRuneLiteralsCompile(t *testing.T) {
	source := "letter: Rune = '\\u{00E9}'\ncrab: Rune = '\\u{1F980}'\nplain: Rune = 'A'\nnul: Rune = '\\0'\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, want := range []string{"= 233;", "= 129408;", "= 65;", "= 0;"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("generated C lacks %s:\n%s", want, result.MainC)
		}
	}
}

func TestStringUnicodeEscapesCompile(t *testing.T) {
	source := "fun demo(): Bool\n    text: String = \"caf\\u{00E9} \\u{1F980}\\0\"\n    return text.is_empty()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestStringSurfaceCompiles(t *testing.T) {
	source := "fun demo(): Size\n    text: String = \"hello\"\n    count: Size = text.length()\n    empty: Bool = text.is_empty()\n    first: Rune = text.at(0)\n    indexed: Rune = text[1]\n    cursor: RuneCursor = text.rune_cursor()\n    return count\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{
		"sw_string_rune_length(",
		"sw_string_is_empty(",
		"sw_string_at_rune(",
		"sw_string_rune_cursor(",
		"sw_rune_cursor_has_next(",
		"sw_rune_cursor_next(",
	} {
		if !strings.Contains(result.MainC, fragment) && !strings.Contains(result.MainH, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestRuneCursorIterationCompiles(t *testing.T) {
	source := "fun demo(): Int32\n    text: String = \"caf\\u{00E9}\"\n    cursor: RuneCursor = text.rune_cursor()\n    mut count: Int32 = 0\n    while cursor.has_next() do\n        value: Rune = cursor.next()\n        count = count + 1\n    end\n    return count\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestStrandSurfaceCompiles(t *testing.T) {
	source := "fun demo(h: Heap): Bool\n    label: Strand = \"Seawitch\"\n    count: Size = label.length()\n    empty: Bool = label.is_empty()\n    first: Rune = label.at(0)\n    indexed: Rune = label[1]\n    text: String = label.to_string(h)\n    text.free(h)\n    return empty\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "(sw_strand){{ 83, 101, 97, 119, 105, 116, 99, 104, 0 }}") {
		t.Fatalf("generated C lacks the inline Strand literal:\n%s", result.MainC)
	}
}

func TestStrandRejectsInvalidLiterals(t *testing.T) {
	tooLong := strings.Repeat("a", 32)
	source := "label: Strand = \"" + tooLong + "\"\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "exceeds 31 UTF-8 bytes") {
		t.Fatalf("want Strand size diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
	source = "label: Strand = \"a\\0b\"\n"
	result = Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot contain NUL") {
		t.Fatalf("want Strand NUL diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestStrandRejectsStringOnlyMethods(t *testing.T) {
	source := "fun demo()\n    label: Strand = \"x\"\n    view: View<Byte> = label.bytes()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Strand has no method bytes") {
		t.Fatalf("want Strand method diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestStringFromBytesAndFromRunesCompile(t *testing.T) {
	source := "fun demo(h: Heap): Bool\n    bytes: Array<UInt8, 3> = [97, 98, 99]\n    view: View<UInt8> = bytes.slice(0, 3)\n    made: String = String.from_bytes(h, view)\n    made.free(h)\n    runes: Array<Rune, 2> = ['a', '\\u{1F980}']\n    rune_view: View<Rune> = runes.slice(0, 2)\n    encoded: String = String.from_runes(h, rune_view)\n    encoded.free(h)\n    return true\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{"sw_string_from_bytes(", "sw_string_from_runes("} {
		if !strings.Contains(result.MainC, fragment) {
			t.Fatalf("generated C lacks %s:\n%s", fragment, result.MainC)
		}
	}
}

func TestStringFromBytesRejectsWrongView(t *testing.T) {
	source := "fun demo(h: Heap)\n    runes: Array<Rune, 1> = ['a']\n    view: View<Rune> = runes.slice(0, 1)\n    made: String = String.from_bytes(h, view)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires View<Byte>") {
		t.Fatalf("want from_bytes view diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestByteAndRuneLiteralDiagnostics(t *testing.T) {
	for _, source := range []string{
		"x: UInt8 = b'ab'",
		"x: UInt8 = b'\\u{41}'",
		"x: Rune = 'ab'",
		"x: Rune = '\\u{D800}'",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want literal diagnostic for %q, got exit=%d stderr=%v", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestRuneCursorHasNoUnknownMethods(t *testing.T) {
	source := "fun demo(): Rune\n    text: String = \"a\"\n    cursor: RuneCursor = text.rune_cursor()\n    return cursor.rewind()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "RuneCursor has no method rewind") {
		t.Fatalf("want cursor method diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}
