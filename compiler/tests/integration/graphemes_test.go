package integration

import (
	"strings"
	"testing"
)

// String.grapheme_length counts extended grapheme clusters and selects the
// utf8proc break-state helper; byte and rune lengths do not.
func TestGraphemeLengthDemand(t *testing.T) {
	clusters := assertCompiles(t, "let s: String = \"e\\u{301}\"\nlet n: Size = s.grapheme_length()\n")
	if !strings.Contains(stringC(t, clusters), "hex_text_grapheme_length") || !strings.Contains(stringC(t, clusters), "utf8proc_grapheme_break_stateful") {
		t.Fatalf("grapheme_length did not emit its break-state helper:\n%s", stringC(t, clusters))
	}
	plain := assertCompiles(t, "let s: String = \"abc\"\nlet n: Size = s.rune_length()\n")
	if strings.Contains(stringC(t, plain), "hex_text_grapheme_length") {
		t.Fatalf("rune_length selected the grapheme helper:\n%s", stringC(t, plain))
	}
	assertRejects(t, "let s: String = \"abc\"\nlet n: Size = s.grapheme_length(1)\n", "grapheme_length expects no arguments")
}

// A GraphemeCursor enumerates clusters and selects the utf8proc break-state
// segmenter; its methods and the Grapheme surface are checked.
func TestGraphemeCursor(t *testing.T) {
	result := assertCompiles(t, "fun demo() do\n    let text: String = \"e\\u{301}\"\n    let mut c: GraphemeCursor = text.grapheme_cursor()\n    if c.has_next() then\n        let g: Grapheme = c.next()\n        let n: Size = g.rune_length()\n        let b: Slice<Byte> = g.bytes()\n    end\nend\n")
	if !strings.Contains(stringC(t, result), "hex_grapheme_cursor_next") || !strings.Contains(stringC(t, result), "utf8proc_grapheme_break_stateful") {
		t.Fatalf("GraphemeCursor did not select the break-state segmenter:\n%s", stringC(t, result))
	}
	assertRejects(t, "fun demo() do\n    let text: String = \"abc\"\n    let mut c: GraphemeCursor = text.grapheme_cursor()\n    let g: Grapheme = c.next()\n    let x: Size = g.frob()\nend\n", "Grapheme has no method frob")
	assertRejects(t, "fun demo() do\n    let text: String = \"abc\"\n    let c: GraphemeCursor = text.grapheme_cursor()\n    let g: Grapheme = c.next()\nend\n", "next mutates its cursor")
	assertRejects(t, "fun demo(h: Heap) do\n    let d: Dict<Grapheme, Int32> = Dict<Grapheme, Int32>(h)\nend\n", "dictionary key type must be Int32 or String<N>")
}

// for g: Grapheme in text enumerates clusters through the stateful segmenter.
func TestForInTextGrapheme(t *testing.T) {
	result := assertCompiles(t, "fun demo() do\n    let text: String = \"e\\u{301}\"\n    for g: Grapheme in text do\n        let n: Size = g.rune_length()\n    end\nend\n")
	if !strings.Contains(rootC(t, result), "hex_grapheme_cursor_next") {
		t.Fatalf("Grapheme iteration did not segment through the cursor:\n%s", rootC(t, result))
	}
}

// String.casefold allocates through a module-local adapter and selects the
// utf8proc transform; the utf8proc buffer never escapes the runtime core.
func TestCasefold(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): String | Error do\n    let text: String = \"Stra\\u{df}e\"\n    return text.casefold(h)\nend\n")
	if !strings.Contains(rootC(t, result), "hex_string_casefold_") {
		t.Fatalf("casefold did not emit its adapter:\n%s", rootC(t, result))
	}
	if !strings.Contains(stringC(t, result), "utf8proc_map") || !strings.Contains(stringC(t, result), "free(mapped)") {
		t.Fatalf("casefold did not map and release the utf8proc buffer:\n%s", stringC(t, result))
	}
	assertRejects(t, "fun demo() do\n    let text: String = \"x\"\n    let folded: String | Error = text.casefold()\nend\n", "casefold expects 1 argument")
}

// String.normalize takes a NormalizationForm, allocates through its
// module-local adapter, and never normalizes implicitly.
func TestNormalize(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): String | Error do\n    let text: String = \"e\\u{301}\"\n    return text.normalize(h, NormalizationForm.NFC())\nend\n")
	if !strings.Contains(rootH(t, result), "hex_string_normalize_") || !strings.Contains(rootH(t, result), "hex_normalize_form") {
		t.Fatalf("normalize did not emit its adapter and form map:\n%s", rootH(t, result))
	}
	assertRejects(t, "fun demo(h: Heap) do\n    let text: String = \"x\"\n    let n: String | Error = text.normalize(h, 1)\nend\n", "normalize requires a NormalizationForm")
	assertRejects(t, "fun demo(h: Heap) do\n    let text: String = \"x\"\n    let n: String | Error = text.normalize(h)\nend\n", "normalize expects 2 arguments")
	assertRejects(t, "fun demo(h: Heap) do\n    let text: String = \"x\"\n    let n: String | Error = text.normalize(h, NormalizationForm.NFC(), h)\nend\n", "normalize expects 2 arguments")
}
