package generator

import (
	"strings"
	"testing"
)

// utf8SequenceValid mirrors the hexal/string.c hex_utf8_next scalar
// validation so the boundary table can be evaluated without a C toolchain.
// The two must stay in lockstep; TestUTF8BoundaryTable asserts the emitted C
// carries the same guards.
func utf8SequenceValid(data []byte, index int) (int, bool) {
	length := len(data)
	lead := data[index]
	if lead < 0x80 {
		return 1, true
	}
	if lead < 0xC2 || lead >= 0xF5 {
		return 0, false
	}
	if index+2 > length {
		return 0, false
	}
	first := data[index+1]
	if (lead == 0xE0 && first < 0xA0) ||
		(lead == 0xED && first >= 0xA0) ||
		(lead == 0xF0 && first < 0x90) ||
		(lead == 0xF4 && first >= 0x90) {
		return 0, false
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
	if index+width > length {
		return 0, false
	}
	for i := 1; i < width; i++ {
		if data[index+i]&0xC0 != 0x80 {
			return 0, false
		}
	}
	return width, true
}

// The accept/reject classes hex_utf8_next must enforce: the shortest valid
// encoding at each width, the scalar boundary pairs, bare continuation leads,
// overlong encodings, leads past the scalar ceiling, truncated sequences, and
// a non-continuation continuation byte.
func TestUTF8BoundaryTable(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		valid bool
	}{
		{"ascii", []byte{0x41}, true},
		{"shortest width 1 U+0000", []byte{0x00}, true},
		{"shortest width 2 U+0080", []byte{0xC2, 0x80}, true},
		{"shortest width 3 U+0800", []byte{0xE0, 0xA0, 0x80}, true},
		{"shortest width 4 U+10000", []byte{0xF0, 0x90, 0x80, 0x80}, true},
		{"U+D7FF below surrogates", []byte{0xED, 0x9F, 0xBF}, true},
		{"U+D800 surrogate starts", []byte{0xED, 0xA0, 0x80}, false},
		{"U+DFFF surrogate ends", []byte{0xED, 0xBF, 0xBF}, false},
		{"U+E000 after surrogates", []byte{0xEE, 0x80, 0x80}, true},
		{"U+10FFFF ceiling", []byte{0xF4, 0x8F, 0xBF, 0xBF}, true},
		{"U+110000 above ceiling", []byte{0xF4, 0x90, 0x80, 0x80}, false},
		{"bare continuation 0x80", []byte{0x80}, false},
		{"bare continuation 0xBF", []byte{0xBF}, false},
		{"overlong 2-byte 0xC0", []byte{0xC0, 0x80}, false},
		{"overlong 2-byte 0xC1", []byte{0xC1, 0xBF}, false},
		{"overlong 3-byte", []byte{0xE0, 0x80, 0x80}, false},
		{"overlong 4-byte", []byte{0xF0, 0x80, 0x80, 0x80}, false},
		{"lead 0xF5", []byte{0xF5, 0x80, 0x80, 0x80}, false},
		{"lead 0xF7", []byte{0xF7, 0xBF, 0xBF, 0xBF}, false},
		{"lead 0xFF", []byte{0xFF}, false},
		{"bad continuation", []byte{0xC3, 0x28}, false},
		{"truncated 2-byte", []byte{0xC2}, false},
		{"truncated 3-byte", []byte{0xE0, 0xA0}, false},
		{"truncated 4-byte", []byte{0xF0, 0x90, 0x80}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := utf8SequenceValid(tc.bytes, 0)
			if ok != tc.valid {
				t.Fatalf("utf8SequenceValid(% x) = %v, want %v", tc.bytes, ok, tc.valid)
			}
		})
	}
}

// The generated hexal/string.c must carry the same scalar guards the boundary
// table evaluates, so template drift fails the suite rather than passing as a
// stale mirror.
func TestGeneratedUTF8ValidationGuards(t *testing.T) {
	program := checkedGeneratorSource(t, "greeting: String = \"hi\"\n")
	files := generateOne(t, program)
	source := files["hexal/string.c"]
	for _, guard := range []string{
		"lead < 0xC2 || lead >= 0xF5",
		"(lead == 0xE0 && first < 0xA0)",
		"(lead == 0xED && first >= 0xA0)",
		"(lead == 0xF0 && first < 0x90)",
		"(lead == 0xF4 && first >= 0x90)",
	} {
		if !strings.Contains(source, guard) {
			t.Fatalf("hexal/string.c lacks scalar guard %q", guard)
		}
	}
}
