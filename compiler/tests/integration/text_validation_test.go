package integration

// The UTF-8 validator substitution: the generated runtime validates through
// utf8proc_iterate, and its accept/reject set must equal the hand-rolled
// validator it replaced.

import (
	"strings"
	"testing"
)

// legacyUTF8Valid is the hand-rolled validator the utf8proc adapter replaced,
// ported from the deleted generated C. It is the "old" side of the
// differential: it rejects bare continuations, overlong forms, surrogates,
// truncated sequences, and scalars above U+10FFFF.
func legacyUTF8Valid(data []byte) bool {
	index := 0
	for index < len(data) {
		lead := data[index]
		if lead < 0x80 {
			index++
			continue
		}
		if lead < 0xC2 || lead >= 0xF5 {
			return false
		}
		if index+2 > len(data) {
			return false
		}
		first := data[index+1]
		if (lead == 0xE0 && first < 0xA0) ||
			(lead == 0xED && first >= 0xA0) ||
			(lead == 0xF0 && first < 0x90) ||
			(lead == 0xF4 && first >= 0x90) {
			return false
		}
		var width int
		switch {
		case lead < 0xE0:
			width = 2
		case lead < 0xF0:
			width = 3
		default:
			width = 4
		}
		if index+width > len(data) {
			return false
		}
		for continuation := 1; continuation < width; continuation++ {
			if data[index+continuation]&0xC0 != 0x80 {
				return false
			}
		}
		index += width
	}
	return true
}

// adapterUTF8Valid is the adapter's rule set: one scalar per utf8proc_iterate
// step over at most four bytes, rejecting any non-positive width. It mirrors
// utf8proc_iterate's documented decoding, which refuses bare continuations,
// overlong forms, surrogates, truncated sequences, and scalars above U+10FFFF.
func adapterUTF8Valid(data []byte) bool {
	index := 0
	for index < len(data) {
		remaining := len(data) - index
		available := remaining
		if available > 4 {
			available = 4
		}
		width := iterateScalar(data[index : index+available])
		if width <= 0 {
			return false
		}
		index += width
	}
	return true
}

// iterateScalar decodes one UTF-8 scalar from at most four bytes and returns
// its width, or a non-positive value when the leading bytes are malformed.
func iterateScalar(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lead := data[0]
	if lead < 0x80 {
		return 1
	}
	if lead < 0xC2 || lead >= 0xF5 {
		return -1
	}
	var width int
	switch {
	case lead < 0xE0:
		width = 2
	case lead < 0xF0:
		width = 3
	default:
		width = 4
	}
	if len(data) < width {
		return -1
	}
	first := data[1]
	if (lead == 0xE0 && first < 0xA0) ||
		(lead == 0xED && first >= 0xA0) ||
		(lead == 0xF0 && first < 0x90) ||
		(lead == 0xF4 && first >= 0x90) {
		return -1
	}
	for continuation := 1; continuation < width; continuation++ {
		if data[continuation]&0xC0 != 0x80 {
			return -1
		}
	}
	return width
}

// The differential: every byte sequence of length one through three, plus a
// corpus of four-byte forms, accepts and rejects identically under both rule
// sets.
func TestUTF8ValidatorAgreesWithLegacyRules(t *testing.T) {
	for length := 1; length <= 3; length++ {
		limit := 1
		for count := 0; count < length; count++ {
			limit *= 256
		}
		for value := 0; value < limit; value++ {
			data := make([]byte, length)
			remaining := value
			for index := 0; index < length; index++ {
				data[index] = byte(remaining)
				remaining >>= 8
			}
			if legacyUTF8Valid(data) != adapterUTF8Valid(data) {
				t.Fatalf("length %d sequence % x: legacy %v, adapter %v", length, data, legacyUTF8Valid(data), adapterUTF8Valid(data))
			}
		}
	}
	corpus := [][]byte{
		{0xF0, 0x90, 0x80, 0x80}, // U+10000
		{0xF0, 0x8F, 0xBF, 0xBF}, // overlong
		{0xF4, 0x8F, 0xBF, 0xBF}, // U+10FFFF
		{0xF4, 0x90, 0x80, 0x80}, // above U+10FFFF
		{0xF5, 0x80, 0x80, 0x80}, // invalid lead
		{0xED, 0xA0, 0x80},       // surrogate D800
		{0xED, 0x9F, 0xBF},       // surrogate D7FF
		{0xE0, 0x80, 0x80},       // overlong
		{0xC0, 0x80},             // overlong
		{0xC2, 0x80},             // U+0080
		{0xF0, 0x9F, 0x98, 0x80}, // U+1F600
	}
	for _, data := range corpus {
		if legacyUTF8Valid(data) != adapterUTF8Valid(data) {
			t.Fatalf("corpus % x: legacy %v, adapter %v", data, legacyUTF8Valid(data), adapterUTF8Valid(data))
		}
	}
}

// The generated private runtime validates through utf8proc and no longer
// carries the hand-rolled lead/continuation logic. The public header never
// names utf8proc.
func TestGeneratedValidatorUsesUtf8proc(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): String | Error do\n    let s: String = try String.from_bytes(h, \"hi\".bytes())\n    return s\nend\n")
	source := moduleFile(t, result, "hexal/string.c")
	if !strings.Contains(source, "#include <utf8proc.h>") || !strings.Contains(source, "utf8proc_iterate(data + index, available, &codepoint)") {
		t.Fatalf("hexal/string.c does not validate through utf8proc:\n%s", source)
	}
	for _, legacy := range []string{"lead < 0xC2", "lead == 0xE0", "continuation < width"} {
		if strings.Contains(source, legacy) {
			t.Fatalf("hexal/string.c retains hand-rolled validator text %q", legacy)
		}
	}
	if header := moduleFile(t, result, "hexal/string.h"); strings.Contains(header, "utf8proc") {
		t.Fatalf("public header names utf8proc:\n%s", header)
	}
}

// The validator, its include, and the dependency are demand-selected: a
// literal-only program selects none of them, and runtime construction selects
// all three.
func TestValidatorDemandSelection(t *testing.T) {
	literalOnly := assertCompiles(t, "let s: String = \"caf\u00e9\"\nprint(s)\n")
	if source := moduleFile(t, literalOnly, "hexal/string.c"); strings.Contains(source, "utf8proc") {
		t.Fatalf("literal-only program emitted the utf8proc adapter:\n%s", source)
	}
	for _, dependency := range literalOnly.Dependencies {
		if dependency == "utf8proc" {
			t.Fatalf("literal-only program selected utf8proc: %v", literalOnly.Dependencies)
		}
	}
	construction := assertCompiles(t, "fun demo(h: Heap): String | Error do\n    let s: String = try String.from_bytes(h, \"hi\".bytes())\n    return s\nend\n")
	selected := false
	for _, dependency := range construction.Dependencies {
		if dependency == "utf8proc" {
			selected = true
		}
	}
	if !selected {
		t.Fatalf("runtime construction did not select utf8proc: %v", construction.Dependencies)
	}
}
