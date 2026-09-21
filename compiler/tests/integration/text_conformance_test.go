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

func TestStringUnicodeEscapesCompile(t *testing.T) {
	source := "fun demo(): Bool do\n    let text: String = \"caf\\u{00E9} \\u{1F980}\\0\"\n    return text.length() == 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestStringSurfaceCompiles(t *testing.T) {
	source := "fun demo(): Size do\n    let text: String = \"hello\"\n    let count: Size = text.length()\n    let raw: Slice<Byte> = text.bytes()\n    let part: Slice<Byte> = text.slice(0, 2)\n    return count\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	// Every read operation is a byte view.
	output := rootC(t, result) + rootH(t, result) + stringH(t, result) + stringC(t, result)
	for _, fragment := range []string{
		"hex_text_heap(",
		".length",
		"hex_text_bytes(",
		"hex_text_slice(",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestStringFromBytesCompiles(t *testing.T) {
	source := "fun demo(h: Heap): Bool | Error do\n    let bytes: Array<UInt8, 3> = [97, 98, 99]\n    let view: Slice<UInt8> = bytes.slice(0, 3)\n    let made: String = try String.from_bytes(h, view)\n    made.free(h)\n    return true\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_string_from_bytes_") {
		t.Fatalf("generated C lacks the from_bytes adapter call:\n%s", rootC(t, result))
	}
}

func TestStringFromBytesRejectsWrongView(t *testing.T) {
	source := "fun demo(h: Heap) do\n    let words: Array<Int32, 1> = [1]\n    let view: Slice<Int32> = words.slice(0, 1)\n    let made = String.from_bytes(h, view)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires Slice<Byte>") {
		t.Fatalf("want from_bytes view diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestByteLiteralDiagnostics(t *testing.T) {
	for _, source := range []string{
		"x: UInt8 := b'ab'",
		"x: UInt8 := b'\\u{41}'",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want literal diagnostic for %q; got exit=%d stderr=%v", source, result.ExitCode, result.Stderr)
		}
	}
}

// Every removed text form reports its own diagnostic, and the syntax freed by
// removing the Rune literal is reserved rather than reused.
func TestRemovedTextFormsReportDiagnostics(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"let c: Rune = 1", "unknown type Rune; Rune was removed: text is bytes, use Byte"},
		{"fun f(r: Slice<Rune>) do\nend", "unknown type Rune; Rune was removed: text is bytes, use Byte"},
		{"fun f(c: RuneCursor) do\nend", "unknown type RuneCursor; RuneCursor was removed with Rune"},
		{"let s: Strand = \"x\"", "unknown type Strand; use String<N> (String<31> keeps the former capacity)"},
		{"let c: Int32 = 'a'", "bare-quote literals are reserved; use b'a' for a byte or \"a\" for text"},
		{"let c: Int32 = '\\u{41}'", "bare-quote literals are reserved; use b'a' for a byte or \"a\" for text"},
		{"let c: Int32 = 'unterminated", "bare-quote literals are reserved; use b'a' for a byte or \"a\" for text"},
		{"fun f(text: String) do\n    let cursor = text.rune_cursor()\nend", "String has no method rune_cursor"},
		{"fun f(h: Heap) do\n    let s = String.from_runes(h, 1)\nend", "String has no such operation; use String.from_bytes(heap, view) or String.interpolate(heap, template)"},
		{"fun f(h: Heap) do\n    let s: String = \"x\".to_string(h)\nend", "String has no method to_string"},
	} {
		result := compileSource(tc.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), tc.want) {
			t.Fatalf("Compile(%q) stderr = %v, want %q", tc.source, result.Stderr, tc.want)
		}
	}
}

// Byte literals, string escapes, and byte access are untouched by the removal:
// a string literal still produces the same UTF-8 bytes.
func TestUnaffectedTextSurfaceStillCompiles(t *testing.T) {
	source := "fun demo(): Size do\n    let accented: String = \"h\u00e9llo\"\n    let emoji: String = \"\\u{1F600}\"\n    let raw: Byte = b'\\xFF'\n    let bytes: Slice<Byte> = emoji.bytes()\n    return accented.length() + bytes.length()\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(stringC(t, result), "{ 104, 195, 169, 108, 108, 111, 0 }") || !strings.Contains(stringC(t, result), "{ 240, 159, 152, 128, 0 }") {
		t.Fatalf("string literals lost their UTF-8 bytes:\n%s", stringC(t, result))
	}
}
