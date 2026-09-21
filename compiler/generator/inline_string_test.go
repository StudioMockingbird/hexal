package generator

import (
	"regexp"
	"strings"
	"testing"
)

// Each demanded capacity is one struct defined exactly once in hexal/string.h,
// however many modules, types, and positions name it, and an undemanded
// capacity has no struct and no helper.
func TestInlineStringCapacitiesAreDefinedOncePerDemand(t *testing.T) {
	program := checkedGeneratorSource(t, "type Box is struct name: String<16>, label: String<16> end\n"+
		"fun demo(h: Heap) do\n"+
		"    let a: String<16> = \"a\"\n    let b: String<16> = \"b\"\n"+
		"    let c: String<40> = a.widen<40>()\n"+
		"    let list: List<String<16>> = List<String<16>>(h)\n    defer list.free(h)\n"+
		"    list.push(a)\nend\n")
	files := generateOne(t, program)
	header := files["hexal/string.h"]
	for _, capacity := range []string{"16", "40"} {
		if count := strings.Count(header, "typedef struct hex_string_"+capacity+" {"); count != 1 {
			t.Fatalf("hex_string_%s is defined %d times in hexal/string.h, want once:\n%s", capacity, count, header)
		}
	}
	for name, content := range files {
		for _, undemanded := range []string{"hex_string_8", "hex_string_31", "hex_string_128", "hex_string_256"} {
			if strings.Contains(content, undemanded+" ") || strings.Contains(content, undemanded+"(") || strings.Contains(content, undemanded+"*") {
				t.Fatalf("%s names the undemanded capacity %s", name, undemanded)
			}
		}
	}
	// No struct definition occurs outside the string component.
	for name, content := range files {
		if name != "hexal/string.h" && strings.Contains(content, "typedef struct hex_string_16 {") {
			t.Fatalf("%s redefines hex_string_16", name)
		}
	}
}

// A program that uses no text type emits no string component and none of the
// removed text helpers appears in any file of any program shape.
func TestNoTextComponentAndNoRemovedHelpersWithoutText(t *testing.T) {
	files := generateOne(t, checkedGeneratorSource(t, "let x: Int32 = 1\n"))
	for name := range files {
		if strings.HasPrefix(name, "hexal/string") {
			t.Fatalf("scalar program emitted %s", name)
		}
	}
	shapes := []string{
		"let a: String<16> = \"a\"\nfor b: Byte in a do\nend\n",
		"fun demo(h: Heap) do\n    let d: Dict<String<16>, Int32> = Dict<String<16>, Int32>(h)\n    defer d.free(h)\n    d.insert(\"k\", 1)\nend\n",
		"fun demo(): Int32 | Error do\n    return Error(ErrorKind.Other(header = \"h\"), \"m\")\nend\n",
		"fun demo(h: Heap): String do\n    return String.interpolate(h, \"n={{ 1 }}\")\nend\n",
	}
	for _, source := range shapes {
		for name, content := range generateOne(t, checkedGeneratorSource(t, source)) {
			for _, removed := range []string{"hex_rune_cursor", "hex_strand", "hex_hash_Strand", "rune_length", "hex_utf8_decode", "hex_utf8_encode", "from_runes"} {
				if strings.Contains(content, removed) {
					t.Fatalf("%s carries the removed helper %s for %q", name, removed, source)
				}
			}
		}
	}
}

// The inline structs come before every header that names them: error.h and
// dict.h include the string component first, and a module header naming a
// capacity includes it before its own declarations.
func TestInlineStringIncludeOrderAndLinkage(t *testing.T) {
	program := checkedGeneratorSource(t, "type Box is struct name: String<24> end\n"+
		"fun demo(h: Heap): Int32 | Error do\n"+
		"    let d: Dict<String<24>, Int32> = Dict<String<24>, Int32>(h)\n    defer d.free(h)\n"+
		"    d.insert(\"k\", 1)\n"+
		"    return Error(ErrorKind.Other(header = \"h\"), \"m\")\nend\n")
	files := generateOne(t, program)
	for _, name := range []string{"hexal/error.h", "hexal/dict.h", "modules/app.h"} {
		content, exists := files[name]
		if !exists {
			t.Fatalf("%s was not emitted", name)
		}
		include := strings.Index(content, "#include \"hexal/string.h\"")
		use := regexp.MustCompile(`hex_string_(24|128|256)\b`).FindStringIndex(content)
		if include < 0 {
			t.Fatalf("%s does not include hexal/string.h", name)
		}
		if use != nil && use[0] < include {
			t.Fatalf("%s names an inline text struct before it includes hexal/string.h", name)
		}
	}
	// Every helper that string.h defines is static inline in the header, so it
	// has internal linkage and cannot collide across the modules that include it.
	header := files["hexal/string.h"]
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "hex_string_") && strings.Contains(line, "(") && !strings.HasPrefix(line, "static") {
			t.Fatalf("hexal/string.h defines a function without internal linkage: %q", line)
		}
	}
}

// slice and bytes are constant-time on every form: the shared view helpers are
// a bounds check and an offset, with no loop over the text.
func TestTextSliceAndBytesAreConstantTime(t *testing.T) {
	files := generateOne(t, checkedGeneratorSource(t, "fun demo() do\n    let a: String<16> = \"abc\"\n    let s: Slice<Byte> = a.slice(0, 2)\n    let b: Slice<Byte> = a.bytes()\nend\n"))
	header := files["hexal/string.h"]
	for _, name := range []string{"hex_text_slice", "hex_text_bytes"} {
		start := strings.Index(header, "static inline hex_slice_UInt8 "+name+"(")
		if start < 0 {
			t.Fatalf("hexal/string.h lacks %s:\n%s", name, header)
		}
		body := header[start:]
		body = body[:strings.Index(body, "\n}\n")]
		for _, loop := range []string{"for (", "while (", "do {", "hex_utf8", "memchr", "strlen"} {
			if strings.Contains(body, loop) {
				t.Fatalf("%s is not constant time (%q):\n%s", name, loop, body)
			}
		}
	}
}

// An unused capacity contributes no helper: a program that only stores and
// reads capacity-16 text emits no capacity-16 conversion or interpolation
// adapters.
func TestInlineStringEmitsNoHelperForUnusedOperations(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    let a: String<16> = \"a\"\n    let n: Size = a.length()\nend\n")
	files := generateOne(t, program)
	all := ""
	for _, content := range files {
		all += content
	}
	for _, unused := range []string{"hex_string_from_bytes_", "hex_string_concat_", "hex_string_interpolate_"} {
		if strings.Contains(all, unused) {
			t.Fatalf("an unused operation %s was emitted", unused)
		}
	}
	if strings.Contains(files["modules/app.c"], "hex_text_fill_checked") {
		t.Fatalf("modules/app.c carries a checked fill for a program that never builds text")
	}
}
