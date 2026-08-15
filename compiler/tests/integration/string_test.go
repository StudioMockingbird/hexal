package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0020 Phase C: String revision — literals, shallow handle copy, the
// hex_string handle representation, and the byte-view operations.

func TestStringLiteralBinding(t *testing.T) {
	result := compileSource("greeting: String = \"hello\"")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_string {",
		"const uint8_t *data;",
		"size_t byte_length;",
		"static const uint8_t hex_lit_0_bytes[6] = { 104, 101, 108, 108, 111, 0 };",
		"static const hex_string hex_lit_0 = { hex_lit_0_bytes, 5 };",
		"const hex_string *const hex_v_greeting = &hex_lit_0;",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestStringLiteralEscapes(t *testing.T) {
	result := compileSource("text: String = \"a\\\"b\\\\c\\nd\\te\\rf\"")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(hexalH(t, result), "static const uint8_t hex_lit_0_bytes[12] = { 97, 34, 98, 92, 99, 10, 100, 9, 101, 13, 102, 0 };") {
		t.Fatalf("hexal.h = %q, want escaped payload bytes", hexalH(t, result))
	}
}

func TestStringBytesAndSlice(t *testing.T) {
	result := compileSource("fun demo() do\n    text: String = \"hello\"\n    raw: View<UInt8> = text.bytes()\n    first: UInt8 = raw[0]\n    part: View<UInt8> = text.slice(1, 3)\n    second: UInt8 = part[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_view_UInt8 hex_v_raw = hex_string_bytes(hex_v_text);",
		"*hex_view_at_UInt8(hex_v_raw, (size_t)(0))",
		"const hex_view_UInt8 hex_v_part = hex_string_slice(hex_v_text, (size_t)(1), (size_t)(3));",
		"*hex_view_at_UInt8(hex_v_part, (size_t)(0))",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestStringOwningLifecycle(t *testing.T) {
	result := compileSource("fun make_text(h: Heap): String do\n    return \"ready\".to_string(h)\nend\nfun demo(h: Heap) do\n    text: String = make_text(h)\n    defer text.free(h)\n    loud: String = text.concat(h, \"!\")\n    loud.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"return hex_string_to_string(hex_v_h, &hex_lit_0);",
		"hex_v_text = hex_f_m3_app_make_text(hex_v_h);",
		"hex_v_loud = hex_string_concat(hex_v_h, hex_v_text, &hex_lit_1);",
		"hex_string_free(hex_v_h, hex_v_loud);",
		"hex_string_free(hex_defer_capture_2, hex_defer_capture_1);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestStringFromBytes(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    text: String = \"abc\"\n    raw: View<UInt8> = text.bytes()\n    copy: String = String.from_bytes(h, raw)\n    copy.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_v_copy = hex_string_from_bytes(hex_v_h, (hex_v_raw).data, (hex_v_raw).length);") {
		t.Fatalf("modules/app.c = %q, want from_bytes call", rootC(t, result))
	}
}

// RFC 0069: String construction and concatenation check the complete
// storage-header + payload + terminator chain with ckd_add before the raw
// allocator sees any sum, and each overflow stage selects its exact message.
func TestStringAllocationSizeArithmetic(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    text: String = \"abc\"\n    raw: View<UInt8> = text.bytes()\n    copy: String = String.from_bytes(h, raw)\n    copy.free(h)\n    runes: Array<Rune, 1> = ['a']\n    rune_view: View<Rune> = runes.slice(0, 1)\n    encoded: String = String.from_runes(h, rune_view)\n    encoded.free(h)\n    loud: String = text.concat(h, \"!\")\n    loud.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	output := hexalH(t, result)
	for _, want := range []string{
		"ckd_add(&total, sizeof(hex_string_storage), length)",
		"ckd_add(&total, total, 1)",
		"ckd_add(&bytes, bytes, width)",
		"ckd_add(&total, sizeof(hex_string_storage), bytes)",
		"ckd_add(&length, left->byte_length, right->byte_length)",
		"[Runtime Error] string allocation size overflow\\n",
		"[Runtime Error] string concatenation length overflow\\n",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("hexal.h = %q, want %q", output, want)
		}
	}
	// RFC 0069 Amendment 2: the validated payload and each concatenation
	// input copy with guarded memcpy calls (a zero-length input never passes
	// a possibly invalid pointer to a standard memory function), diagnostics
	// report through hex_runtime_trap, and no raw fputs or compiler-owned
	// NULL remains in the String machinery.
	for _, want := range []string{
		"if (length != 0) {",
		"memcpy(storage->bytes, data, length);",
		"if (left->byte_length != 0) {",
		"memcpy(storage->bytes, left->data, left->byte_length);",
		"if (right->byte_length != 0) {",
		"memcpy(storage->bytes + left->byte_length, right->data, right->byte_length);",
		"hex_runtime_trap(\"[Runtime Error] string allocation size overflow\\n\")",
		"hex_runtime_trap(\"[Runtime Error] string concatenation length overflow\\n\")",
		"hex_runtime_trap(\"[Runtime Error] invalid UTF-8 in String\\n\")",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("hexal.h = %q, want %q", output, want)
		}
	}
	for _, banned := range []string{
		"SIZE_MAX - ",
		"sizeof(hex_string_storage) + ",
		"fputs(",
		"NULL",
	} {
		if strings.Contains(output, banned) {
			t.Fatalf("hexal.h = %q, contains banned %q", output, banned)
		}
	}
}

// RFC 0035: String handles copy by value and cleanup is manual; every
// formerly-ownership program below is now valid C-style code.

func TestStringShallowCopySemantics(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    owned: String = \"x\".to_string(h)\n    other: String = owned\nend",
		"fun demo(h: Heap) do\n    owned: String = \"x\".to_string(h)\nend",
		"fun demo(h: Heap) do\n    owned: String = \"x\".to_string(h)\n    owned.free(h)\n    owned.free(h)\nend",
		"fun demo(h: Heap) do\n    mut owned: String = \"x\".to_string(h)\n    owned = \"y\".to_string(h)\nend",
		"fun demo(h: Heap) do\n    mut owned: String = \"x\".to_string(h)\n    owned.free(h)\n    owned = \"y\"\nend",
		"fun demo(h: Heap) do\n    text: String = \"x\"\n    text.free(h)\nend",
		"fun make_text(h: Heap): String do\n    return \"ready\"\nend",
		"fun make_text(h: Heap, source: String): String do\n    return source\nend",
		"owned: String = \"x\"",
		"fun demo(h: Heap, source: String) do\n    source.free(h)\nend",
		"fun demo(h: Heap, release: Bool) do\n    owned: String = \"x\".to_string(h)\n    if release then\n        owned.free(h)\n    end\n    owned.free(h)\nend",
		"fun demo(h: Heap, release: Bool) do\n    owned: String = \"x\".to_string(h)\n    if release then\n        defer owned.free(h)\n    else\n        defer owned.free(h)\n    end\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestStringReturnHandoff(t *testing.T) {
	result := compileSource("fun make_text(h: Heap): String do\n    owned: String = \"x\".to_string(h)\n    return owned\nend\nfun demo(h: Heap) do\n    text: String = make_text(h)\n    text.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestStringStaticReassignment(t *testing.T) {
	result := compileSource("fun demo() do\n    mut greeting: String = \"hello\"\n    greeting = \"world\"\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestStringParameterCopiesAreValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(source: String) do\n    other: String = source\nend",
		"fun demo(source: String) do\n    mut copy: String = \"x\"\n    copy = source\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestStringInArrayIsStoredAndCopiedShallow(t *testing.T) {
	// RFC 0048: Array<String, N> is valid; element copies share the String
	// handle, and the array never frees a stored literal.
	result := compileSource("fun demo() do\n    texts: Array<String, 2> = [\"a\", \"b\"]\n    copy: Array<String, 2> = texts\n    first: String = texts[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_array_String_2 hex_v_copy = hex_v_texts;",
		"const hex_string *const hex_v_first = *hex_array_at_String_2",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}
