package integration

import (
	"strings"
	"testing"
)

// A Rune literal is a Unicode scalar value that lowers to the uint32_t scalar
// and carries its decoded value into the generated C.
func TestRuneLiteralLowersToScalar(t *testing.T) {
	result := assertCompiles(t, "let r: Rune = 'a'\nlet n: UInt32 = r.value()\n")
	if !strings.Contains(rootC(t, result), "97") {
		t.Fatalf("Rune literal lost its scalar value:\n%s", rootC(t, result))
	}
}

// A Rune literal accepts one-, two-, three-, and four-byte scalars, and
// rejects a surrogate escape and a multi-scalar body.
func TestRuneLiteralScalarRange(t *testing.T) {
	assertCompiles(t, "let a: Rune = 'a'\nlet b: Rune = '\\u{7FF}'\nlet c: Rune = '\\u{20AC}'\nlet d: Rune = '\\u{1F600}'\n")
	assertRejects(t, "let a: Rune = '\\u{D800}'\n", "invalid Unicode scalar value")
	assertRejects(t, "let a: Rune = 'ab'\n", "Rune literal must contain exactly one Unicode scalar")
}

// Rune values compare for equality and order by scalar value.
func TestRuneOrderingAndEquality(t *testing.T) {
	assertCompiles(t, "let a: Rune = 'a'\nlet b: Rune = 'b'\nlet lt: Bool = a < b\nlet eq: Bool = a == b\nlet ne: Bool = a != b\nlet le: Bool = a <= b\n")
}

// A Rune is a valid match scrutinee and a valid interpolation value.
func TestRuneMatchAndInterpolation(t *testing.T) {
	assertCompiles(t, "let r: Rune = 'a'\nlet n: Int32 = match r\n| 'a' then 1\n| else then 0\nend\n")
	assertCompiles(t, "fun demo(h: Heap) do\n    let r: Rune = 'a'\n    let s: String = String.interpolate(h, \"r={{ r }}\")\nend\n")
}

// Rune.value is the identity conversion to UInt32 and takes no arguments.
func TestRuneValueMethod(t *testing.T) {
	assertCompiles(t, "let r: Rune = '\\u{1F600}'\nlet v: UInt32 = r.value()\n")
	assertRejects(t, "let r: Rune = 'a'\nlet v: UInt32 = r.value(1)\n", "value expects no arguments")
}

// Rune.category yields the closed UnicodeCategory value, and a type-mode match
// over it is exhaustive without a final else.
func TestRuneCategory(t *testing.T) {
	variants := []string{"Cn", "Lu", "Ll", "Lt", "Lm", "Lo", "Mn", "Mc", "Me", "Nd", "Nl", "No", "Pc", "Pd", "Ps", "Pe", "Pi", "Pf", "Po", "Sm", "Sc", "Sk", "So", "Zs", "Zl", "Zp", "Cc", "Cf", "Cs", "Co"}
	exhaustive := "fun demo(r: Rune): Int32 do\n    return match r.category() is\n"
	for _, variant := range variants {
		exhaustive += "    | UnicodeCategory." + variant + " then 1\n"
	}
	exhaustive += "    end\nend\n"
	result := assertCompiles(t, exhaustive)
	if !strings.Contains(rootC(t, result), "hex_rune_categories") {
		t.Fatalf("Rune.category did not emit its tag table:\n%s", rootC(t, result))
	}
	assertRejects(t, "fun demo(r: Rune): Int32 do\n    return match r.category() is\n    | UnicodeCategory.Lu then 1\n    | UnicodeCategory.Ll then 2\n    end\nend\n", "match is not exhaustive")
}

// The Tier 2 Rune properties map onto utf8proc and select the property
// helpers, even in a program with no text type.
func TestRuneProperties(t *testing.T) {
	result := assertCompiles(t, "let r: Rune = 'a'\nlet a: Bool = r.is_lower()\nlet b: Bool = r.is_upper()\nlet c: Bool = r.is_alphabetic()\nlet d: Bool = r.is_numeric()\nlet e: Bool = r.is_whitespace()\nlet f: Rune = r.to_lower()\nlet g: Rune = r.to_upper()\nlet h: Rune = r.to_title()\nlet i: Int32 = r.display_width()\nlet j: UInt8 = r.combining_class()\n")
	if !strings.Contains(stringC(t, result), "hex_rune_is_lower") || !strings.Contains(stringC(t, result), "utf8proc") {
		t.Fatalf("Rune properties did not select their utf8proc helpers:\n%s", stringC(t, result))
	}
	assertRejects(t, "let r: Rune = 'a'\nlet b: Bool = r.is_lower(1)\n", "is_lower expects no arguments")
}

// String.from_runes encodes a Slice<Rune> into owned text and rejects an
// invalid scalar as an Error rather than trapping.
func TestStringFromRunes(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): String | Error do\n    let values: Array<Rune, 2> = ['a', '\\u{1F600}']\n    let view: Slice<Rune> = values.slice(0, 2)\n    return String.from_runes(h, view)\nend\n")
	if !strings.Contains(rootC(t, result), "hex_string_from_runes_") {
		t.Fatalf("String.from_runes did not emit its adapter:\n%s", rootC(t, result))
	}
	assertRejects(t, "fun demo(h: Heap): String | Error do\n    return String.from_runes(h, 1)\nend\n", "String.from_runes requires Slice<Rune>")
	assertRejects(t, "fun demo(h: Heap): String | Error do\n    let bytes: Slice<Byte> = \"x\".bytes()\n    return String.from_runes(h, bytes)\nend\n", "String.from_runes requires Slice<Rune>")
}

// Rune.from turns a UInt32 into a checked scalar and yields Rune | Error.
func TestRuneFrom(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Rune | Error do\n    return Rune.from(0x1F600)\nend\n")
	if !strings.Contains(rootC(t, result), "hex_rune_from_") {
		t.Fatalf("Rune.from did not emit its adapter:\n%s", rootC(t, result))
	}
	assertRejects(t, "let r: Rune | Error = Rune.from(1, 2)\n", "Rune.from expects 1 argument")
	assertRejects(t, "let r: Rune | Error = Rune.from(\"x\")\n", "Rune.from requires a UInt32")
	assertRejects(t, "let r: Rune | Error = Rune.nope(1)\n", "Rune has no such operation")
}

// Rune.utf8_length reports the encoded width 1 through 4, and the module emits
// its helper only where the method is used.
func TestRuneUtf8Length(t *testing.T) {
	result := assertCompiles(t, "let a: Rune = 'a'\nlet b: Rune = '\\u{7FF}'\nlet c: Rune = '\\u{20AC}'\nlet d: Rune = '\\u{1F600}'\nlet wa: Size = a.utf8_length()\nlet wb: Size = b.utf8_length()\nlet wc: Size = c.utf8_length()\nlet wd: Size = d.utf8_length()\n")
	if !strings.Contains(rootC(t, result), "hex_rune_utf8_length") {
		t.Fatalf("utf8_length did not emit its helper:\n%s", rootC(t, result))
	}
	plain := assertCompiles(t, "let r: Rune = 'a'\n")
	if strings.Contains(rootC(t, plain), "hex_rune_utf8_length") {
		t.Fatalf("a program without utf8_length emitted its helper:\n%s", rootC(t, plain))
	}
	assertRejects(t, "let r: Rune = 'a'\nlet n: Size = r.utf8_length(1)\n", "utf8_length expects no arguments")
}

// String.rune_length counts scalars and selects the utf8proc adapter; byte
// length and literal-only programs select nothing.
func TestRuneLengthDemand(t *testing.T) {
	withRunes := assertCompiles(t, "let s: String = \"hello\"\nlet n: Size = s.rune_length()\n")
	if !strings.Contains(stringC(t, withRunes), "hex_text_rune_length") || !strings.Contains(stringC(t, withRunes), "utf8proc_iterate") {
		t.Fatalf("rune_length did not emit its utf8proc-stepping helper:\n%s", stringC(t, withRunes))
	}
	withoutRunes := assertCompiles(t, "let s: String = \"hello\"\nlet n: Size = s.length()\n")
	if strings.Contains(stringC(t, withoutRunes), "utf8proc") || strings.Contains(stringC(t, withoutRunes), "hex_text_rune_length") {
		t.Fatalf("byte length selected the utf8proc adapter:\n%s", stringC(t, withoutRunes))
	}
}

// String.rune_length is available on the heap and inline forms alike, takes no
// arguments, and yields Size.
func TestRuneLengthSurface(t *testing.T) {
	assertCompiles(t, "let a: String = \"hello\"\nlet b: String<16> = \"hello\"\nlet x: Size = a.rune_length()\nlet y: Size = b.rune_length()\n")
	assertRejects(t, "let s: String = \"abc\"\nlet n: Size = s.rune_length(1)\n", "rune_length expects no arguments")
}
