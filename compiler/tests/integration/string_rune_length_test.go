package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// The cached rune count lives in the String header. These are the textual
// halves of the acceptance cases; the runtime halves (that a slice over
// multi-byte input yields identical bytes, that a concatenated count matches an
// independent scan) need an executed binary and stay unverified, as
// docs/status.md records for every generated artifact.

// The count reaches every construction path. Each assertion names the
// expression that supplies it, so a future constructor added without one is a
// visible omission rather than a silent zero.
func TestCachedRuneLengthSetAtEveryConstructionPath(t *testing.T) {
	source := "fun demo(h: Heap): Size do\n" +
		"    literal: String := \"hello\"\n" +
		"    joined: String := literal.concat(h, literal)\n" +
		"    copied: String := joined.to_string(h)\n" +
		"    raw: View<UInt8> := copied.bytes()\n" +
		"    rebuilt: String := String.from_bytes(h, raw)\n" +
		"    count: Size := rebuilt.length()\n" +
		"    rebuilt.free(h)\n" +
		"    copied.free(h)\n" +
		"    joined.free(h)\n" +
		"    return count\n" +
		"end\n"
	source_c := stringC(t, assertCompiles(t, source))
	for name, want := range map[string]string{
		"literal":    "const hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 5, .rune_length = 5 };",
		"from_bytes": ".data = storage->bytes, .byte_length = length, .rune_length = runes };",
		"from_runes": ".data = storage->bytes, .byte_length = bytes, .rune_length = length };",
		"concat":     ".rune_length = left->rune_length + right->rune_length };",
		"to_string":  "return hex_string_from_bytes(h, text->data, text->byte_length);",
	} {
		if !strings.Contains(source_c, want) {
			t.Fatalf("the %s construction path does not set rune_length: want %q in hexal/string.c", name, want)
		}
	}
}

// A literal's count is computed at compile time, so it is the one path where a
// byte count substituted for a rune count would go unnoticed. "cafe" plus a
// crab emoji is 6 runes across 10 bytes, which makes the two numbers impossible
// to confuse.
func TestCachedRuneLengthCountsRunesNotBytesInLiterals(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Size do\n    text: String := \"caf\\u{00E9} \\u{1F980}\"\n    return text.length()\nend\n")
	const want = "const hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 10, .rune_length = 6 };"
	if !strings.Contains(stringC(t, result), want) {
		t.Fatalf("hexal/string.c = %q, want %q", stringC(t, result), want)
	}
}

// length() and slice() are the two consumers. Neither may walk to derive a
// count now that the header carries one; slice still walks to reach a
// position, which is a different thing and stays.
func TestCachedRuneLengthConsumersDoNotScan(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Size do\n    text: String := \"hello\"\n    part: View<UInt8> := text.slice(1, 3)\n    return text.length()\nend\n")
	header := stringH(t, result)
	for _, want := range []string{
		"static inline size_t hex_string_rune_length(const hex_string *text) {\n    return text->rune_length;\n}",
		"if (!(start <= end && end <= text->rune_length)) {",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal/string.h = %q, want %q", header, want)
		}
	}
	// The validation scan is gone; the walk to `end` is not.
	if strings.Contains(header, "while (index < text->byte_length) {") {
		t.Fatalf("hexal/string.h still counts runes by scanning:\n%s", header)
	}
	if !strings.Contains(header, "for (size_t rune = 0; rune < end; rune++) {") {
		t.Fatalf("hexal/string.h lost the walk to the slice end:\n%s", header)
	}
}

// Invariant 1: the header grew, the handle did not. A List<String> element is
// still one pointer, so element size, copying, and passing are unchanged.
func TestCachedRuneLengthLeavesStringHandleSizeUnchanged(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): Size do\n    names: List<String> := List<String>.new(h)\n    names.push(\"hello\")\n    count: Size := names.length()\n    names.free(h)\n    return count\nend\n")
	header := listH(t, result)
	for _, want := range []string{
		"const hex_string * *data;",
		"sizeof(const hex_string *)",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("List<String> element is no longer a pointer-sized handle: want %q in\n%s", want, header)
		}
	}
}

// Invariant 4 and the Non-goals: a cached count is not an argument for
// reinstating indexing, which was removed. Nothing here reopens it.
func TestCachedRuneLengthDoesNotReopenTextIndexing(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"text: String := \"hi\" first: Rune := text[0]", "cannot index String"},
		{"label: Strand := \"hi\" first: Rune := label[0]", "cannot index Strand"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}
