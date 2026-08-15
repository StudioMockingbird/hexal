//go:build c23

package c23validation

import "testing"

// RFC 0044 smoke tests: byte/rune literals, the RuneCursor, the Strand
// surface, and the from_bytes/from_runes constructors generate C that gcc
// accepts, and a compiled run computes the expected values.

func c23GeneratedTextConformanceRuns(t *testing.T) {
	source := "fun demo(h: Heap): Bool do\n    byte: UInt8 = b'\\xFF'\n    byte_ok: Bool = byte == 255\n    letter: Rune = '\\u{00E9}'\n    crab: Rune = '\\u{1F980}'\n    rune_ok: Bool = letter == 233 and crab == 129408\n    text: String = \"caf\\u{00E9} \\u{1F980}\"\n    count: Size = text.length()\n    length_ok: Bool = count == 6\n    first: Rune = text.at(0)\n    accented: Rune = text.at(3)\n    index_ok: Bool = first == 99 and accented == 233\n    cursor: RuneCursor = text.rune_cursor()\n    mut seen: Int32 = 0\n    while cursor.has_next() do\n        value: Rune = cursor.next()\n        seen = seen + 1\n    end\n    cursor_ok: Bool = seen == 6\n    label: Strand = \"hexal\"\n    strand_ok: Bool = label.length() == 5 and label[0] == 104\n    runes: Array<Rune, 2> = [letter, crab]\n    view: View<Rune> = runes.slice(0, 2)\n    encoded: String = String.from_runes(h, view)\n    encoded_ok: Bool = encoded.length() == 2 and encoded.at(0) == 233\n    encoded.free(h)\n    return byte_ok and rune_ok and length_ok and index_ok and cursor_ok and strand_ok and encoded_ok\nend\n\tprint(demo(Heap.new()))\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
}

// RFC 0044 runtime traps: invalid UTF-8 bytes, a String index outside its
// bounds, and RuneCursor exhaustion all terminate with a runtime
// diagnostic.
func c23GeneratedTextTraps(t *testing.T) {
	cases := map[string]string{
		"malformed utf8":    "fun demo(h: Heap) do\n    bytes: Array<UInt8, 2> = [0xC3, 0x28]\n    view: View<UInt8> = bytes.slice(0, 2)\n    text: String = String.from_bytes(h, view)\n    print(text)\nend\ndemo(Heap.new())\n",
		"string index":      "fun demo() do\n    text: String = \"hex\"\n    late: Rune = text.at(9)\n    print(late)\nend\ndemo()\n",
		"cursor exhaustion": "fun demo() do\n    text: String = \"hex\"\n    cursor: RuneCursor = text.rune_cursor()\n    cursor.next()\n    cursor.next()\n    cursor.next()\n    late: Rune = cursor.next()\n    print(late)\nend\ndemo()\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			trapGeneratedC(t, assertCompiles(t, source))
		})
	}
}
