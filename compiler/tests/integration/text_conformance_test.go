package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestByteAliasIsUInt8(t *testing.T) {
	source := "let byte: Byte = b'A'\nlet number: UInt8 = byte\nlet again: Byte = number\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "const uint8_t hex_v_byte = 65;") {
		t.Fatalf("generated C lacks the Byte value:\n%s", rootC(t, result))
	}
}

func TestByteLiteralsCompile(t *testing.T) {
	source := "let ascii: UInt8 = b'A'\nlet newline: Byte = b'\\n'\nlet raw: Byte = b'\\xFF'\nlet zero: Byte = b'\\0'\nlet quote: Byte = b'\\''\nlet backslash: Byte = b'\\\\'\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, want := range []string{"= 65;", "= 10;", "= 255;", "= 0;", "= 39;", "= 92;"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("generated C lacks %s:\n%s", want, rootC(t, result))
		}
	}
}

func TestRuneLiteralsCompile(t *testing.T) {
	source := "let letter: Rune = '\\u{00E9}'\nlet crab: Rune = '\\u{1F980}'\nlet plain: Rune = 'A'\nlet nul: Rune = '\\0'\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, want := range []string{"= 233;", "= 129408;", "= 65;", "= 0;"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("generated C lacks %s:\n%s", want, rootC(t, result))
		}
	}
}

func TestStringUnicodeEscapesCompile(t *testing.T) {
	source := "fun demo(): Bool do\n    let text: String = \"caf\\u{00E9} \\u{1F980}\\0\"\n    return text.length() == 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestStringSurfaceCompiles(t *testing.T) {
	source := "fun demo(): Size do\n    let text: String = \"hello\"\n    let count: Size = text.length()\n    let cursor: RuneCursor = text.rune_cursor()\n    let first: Rune = cursor.next()\n    return count\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	// The String surface spells as calls in the module C and as declarations
	// and definitions in the String component pair.
	output := rootC(t, result) + rootH(t, result) + stringH(t, result) + stringC(t, result)
	for _, fragment := range []string{
		"hex_string_rune_length(",
		"hex_string_rune_cursor(",
		"hex_rune_cursor_has_next(",
		"hex_rune_cursor_next(",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestRuneCursorIterationCompiles(t *testing.T) {
	source := "fun demo(): Int32 do\n    let text: String = \"caf\\u{00E9}\"\n    let cursor: RuneCursor = text.rune_cursor()\n    let mut count: Int32 = 0\n    while cursor.has_next() do\n        let value: Rune = cursor.next()\n        count = count + 1\n    end\n    return count\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestStrandSurfaceCompiles(t *testing.T) {
	source := "fun demo(h: Heap): Bool do\n    let label: Strand = \"hexal\"\n    let count: Size = label.length()\n    let text: String = label.to_string(h)\n    text.free(h)\n    return count == 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "(hex_strand){{ 104, 101, 120, 97, 108, 0 }}") {
		t.Fatalf("generated C lacks the inline Strand literal:\n%s", rootC(t, result))
	}
}

func TestStrandRejectsInvalidLiterals(t *testing.T) {
	tooLong := strings.Repeat("a", 32)
	source := "let label: Strand = \"" + tooLong + "\"\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "exceeds 31 UTF-8 bytes") {
		t.Fatalf("want Strand size diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
	source = "let label: Strand = \"a\\0b\"\n"
	result = compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot contain NUL") {
		t.Fatalf("want Strand NUL diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestStrandRejectsStringOnlyMethods(t *testing.T) {
	source := "fun demo() do\n    let label: Strand = \"x\"\n    let view: Slice<Byte> = label.bytes()\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Strand has no method bytes") {
		t.Fatalf("want Strand method diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestStringFromBytesAndFromRunesCompile(t *testing.T) {
	source := "fun demo(h: Heap): Bool do\n    let bytes: Array<UInt8, 3> = [97, 98, 99]\n    let view: Slice<UInt8> = bytes.slice(0, 3)\n    let made: String = String.from_bytes(h, view)\n    made.free(h)\n    let runes: Array<Rune, 2> = ['a', '\\u{1F980}']\n    let rune_view: Slice<Rune> = runes.slice(0, 2)\n    let encoded: String = String.from_runes(h, rune_view)\n    encoded.free(h)\n    return true\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{"hex_string_from_bytes(", "hex_string_from_runes("} {
		if !strings.Contains(rootC(t, result), fragment) {
			t.Fatalf("generated C lacks %s:\n%s", fragment, rootC(t, result))
		}
	}
}

func TestStringFromBytesRejectsWrongView(t *testing.T) {
	source := "fun demo(h: Heap) do\n    let runes: Array<Rune, 1> = ['a']\n    let view: Slice<Rune> = runes.slice(0, 1)\n    let made: String = String.from_bytes(h, view)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires Slice<Byte>") {
		t.Fatalf("want from_bytes view diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestByteAndRuneLiteralDiagnostics(t *testing.T) {
	for _, source := range []string{
		"x: UInt8 := b'ab'",
		"x: UInt8 := b'\\u{41}'",
		"x: Rune := 'ab'",
		"x: Rune := '\\u{D800}'",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want literal diagnostic for %q; got exit=%d stderr=%v", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestRuneCursorHasNoUnknownMethods(t *testing.T) {
	source := "fun demo(): Rune do\n    let text: String = \"a\"\n    let cursor: RuneCursor = text.rune_cursor()\n    return cursor.rewind()\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "RuneCursor has no method rewind") {
		t.Fatalf("want cursor method diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}
