package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// stringH returns the generated hexal/string.h component artifact.
// stringC returns the generated hexal/string.c component artifact.
func TestStringLiteralBinding(t *testing.T) {
	result := compileSource("let greeting: String = \"hello\"")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The representation and the literal object pair live in the String
	// component: hexal/string.h declares each object once with external
	// const linkage, hexal/string.c defines it once. The handle stores a byte
	// length and no rune count.
	for _, want := range []string{
		"typedef struct hex_string {",
		"const uint8_t *data;",
		"size_t byte_length;",
		"extern const uint8_t hex_lit_0_bytes[6];",
		"extern const hex_string hex_lit_0;",
	} {
		if !strings.Contains(stringH(t, result), want) {
			t.Fatalf("hexal/string.h = %q, want %q", stringH(t, result), want)
		}
	}
	if strings.Contains(stringH(t, result), "rune_length") || strings.Contains(stringC(t, result), "rune_length") {
		t.Fatalf("the String component still carries a rune count")
	}
	for _, want := range []string{
		"const uint8_t hex_lit_0_bytes[6] = { 104, 101, 108, 108, 111, 0 };",
		"const hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 5 };",
	} {
		if !strings.Contains(stringC(t, result), want) {
			t.Fatalf("hexal/string.c = %q, want %q", stringC(t, result), want)
		}
	}
	if !strings.Contains(rootC(t, result), "const hex_string *const hex_v_greeting = &hex_lit_0;") {
		t.Fatalf("modules/app.c = %q, want the literal object reference", rootC(t, result))
	}
}

func TestStringLiteralEscapes(t *testing.T) {
	result := compileSource("let text: String = \"a\\\"b\\\\c\\nd\\te\\rf\"")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(stringC(t, result), "const uint8_t hex_lit_0_bytes[12] = { 97, 34, 98, 92, 99, 10, 100, 9, 101, 13, 102, 0 };") {
		t.Fatalf("hexal/string.c = %q, want escaped payload bytes", stringC(t, result))
	}
}

func TestStringBytesAndSlice(t *testing.T) {
	result := compileSource("fun demo() do\n    let text: String = \"hello\"\n    let raw: Slice<UInt8> = text.bytes()\n    let first: UInt8 = raw[0]\n    let part: Slice<UInt8> = text.slice(1, 3)\n    let second: UInt8 = part[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_slice_UInt8 hex_v_raw = hex_text_bytes(hex_text_heap(hex_v_text));",
		"*hex_slice_at_UInt8(hex_v_raw, (size_t)(0))",
		"const hex_slice_UInt8 hex_v_part = hex_text_slice(hex_text_heap(hex_v_text), (size_t)(1), (size_t)(3));",
		"*hex_slice_at_UInt8(hex_v_part, (size_t)(0))",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestStringOwningLifecycle(t *testing.T) {
	result := compileSource("fun make_text(h: Heap): String do\n    return \"ready\".copy(h)\nend\nfun demo(h: Heap): Int32 | Error do\n    let text: String = make_text(h)\n    defer text.free(h)\n    let loud: String = try text.concat(h, \"!\".bytes())\n    loud.free(h)\n    return 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"return hex_string_make(hex_v_h, hex_text_heap(&hex_lit_0));",
		"hex_v_text = hex_f_m3_app_make_text(hex_v_h);",
		"hex_string_concat_Error_String(hex_v_h, hex_text_heap(hex_v_text), hex_text_bytes(hex_text_heap(&hex_lit_1)), 7, 33)",
		"hex_string_free(hex_v_h, hex_v_loud);",
		"hex_string_free(hex_defer_capture_2, hex_defer_capture_1);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestStringFromBytes(t *testing.T) {
	result := compileSource("fun demo(h: Heap): Int32 | Error do\n    let text: String = \"abc\"\n    let raw: Slice<UInt8> = text.bytes()\n    let copy: String = try String.from_bytes(h, raw)\n    copy.free(h)\n    return 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_string_from_bytes_Error_String(hex_v_h, hex_v_raw, 4, 28)") {
		t.Fatalf("modules/app.c = %q, want from_bytes adapter call", rootC(t, result))
	}
	if !strings.Contains(rootH(t, result), "hex_utf8_valid(bytes.data, bytes.length)") {
		t.Fatalf("modules/app.h = %q, want the validating adapter", rootH(t, result))
	}
}

// String construction and concatenation check the complete storage-header +
// payload + terminator chain with ckd_add before the raw allocator sees any
// sum, and the one shared join owns the overflow message.
func TestStringAllocationSizeArithmetic(t *testing.T) {
	result := compileSource("fun demo(h: Heap): Int32 | Error do\n    let text: String = \"abc\"\n    let raw: Slice<UInt8> = text.bytes()\n    let copy: String = try String.from_bytes(h, raw)\n    copy.free(h)\n    let loud: String = try text.concat(h, \"!\".bytes())\n    loud.free(h)\n    return 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The checked arithmetic, guarded copies, and diagnostics all move with
	// the non-specialized bodies into hexal/string.c.
	output := stringC(t, result)
	for _, want := range []string{
		"ckd_add(&total, sizeof(hex_string_storage), length)",
		"ckd_add(&total, total, 1)",
		"ckd_add(&length, left.length, right.length)",
		"[Runtime Error] string concatenation length overflow\\n",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("hexal/string.c = %q, want %q", output, want)
		}
	}
	// Each input copies with a guarded memcpy, so a zero-length input never
	// passes a possibly invalid pointer to a standard memory function;
	// diagnostics report through hex_runtime_trap, and no raw fputs or
	// compiler-owned NULL remains in the String machinery.
	for _, want := range []string{
		"if (left.length != 0) {",
		"memcpy(storage->bytes, left.data, left.length);",
		"if (right.length != 0) {",
		"memcpy(storage->bytes + left.length, right.data, right.length);",
		"hex_runtime_trap(\"[Runtime Error] string concatenation length overflow\\n\")",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("hexal/string.c = %q, want %q", output, want)
		}
	}
	for _, banned := range []string{
		"SIZE_MAX - ",
		"sizeof(hex_string_storage) + ",
		"fputs(",
		"NULL",
		"invalid UTF-8 in string",
	} {
		if strings.Contains(output, banned) {
			t.Fatalf("hexal/string.c = %q, contains banned %q", output, banned)
		}
	}
}

// A free whose receiver is statically proven literal storage is rejected;
// opaque handles still free at runtime.
func TestStringShallowCopySemantics(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    let owned: String = \"x\".copy(h)\n    let other: String = owned\nend",
		"fun demo(h: Heap) do\n    let owned: String = \"x\".copy(h)\nend",
		"fun demo(h: Heap) do\n    let owned: String = \"x\".copy(h)\n    owned.free(h)\n    owned.free(h)\nend",
		"fun demo(h: Heap) do\n    let mut owned: String = \"x\".copy(h)\n    owned = \"y\".copy(h)\nend",
		"fun demo(h: Heap) do\n    let mut owned: String = \"x\".copy(h)\n    owned.free(h)\n    owned = \"y\"\nend",
		"fun make_text(h: Heap): String do\n    return \"ready\"\nend",
		"fun make_text(h: Heap, source: String): String do\n    return source\nend",
		"let owned: String = \"x\"",
		"fun demo(h: Heap, source: String) do\n    source.free(h)\nend",
		"fun demo(h: Heap, release: Bool) do\n    let owned: String = \"x\".copy(h)\n    if release then\n        owned.free(h)\n    end\n    owned.free(h)\nend",
		"fun demo(h: Heap, release: Bool) do\n    let owned: String = \"x\".copy(h)\n    if release then\n        defer owned.free(h)\n    else\n        defer owned.free(h)\n    end\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestStringReturnHandoff(t *testing.T) {
	result := compileSource("fun make_text(h: Heap): String do\n    let owned: String = \"x\".copy(h)\n    return owned\nend\nfun demo(h: Heap) do\n    let text: String = make_text(h)\n    text.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// A free whose receiver may statically be a literal is rejected, however the
// literal reaches the receiver: direct binding, object member, ADT payload,
// array element, replacement, branch merge, or dynamic-index join. Opaque
// origins (parameters, call results, handle reads) still compile and trap
// at runtime instead.
func TestStringLiteralFreeRejected(t *testing.T) {
	rejected := []string{
		"fun demo(h: Heap) do\n    let text: String = \"x\"\n    text.free(h)\nend",
		"fun demo(h: Heap) do\n    let text: String = r\"x\"\n    text.free(h)\nend",
		"fun demo(h: Heap) do\n    let text: String = \"x\"\n    let alias: String = text\n    alias.free(h)\nend",
		"type Box is struct text: String end\nfun demo(h: Heap) do\n    let box: Box = Box(text = \"x\")\n    box.text.free(h)\nend",
		"type W is union | A as text: String end | B as x: Int32 end end\nlet h: Heap = Heap()\nlet w: W = W.A(text = \"x\")\nlet label: Int32 = match w is\n| W.A then w.text.free(h)\n| W.B then 0\nend",
		"fun demo(h: Heap) do\n    let texts: Array<String, 2> = [\"a\", \"b\"]\n    texts[0].free(h)\nend",
		"fun demo(h: Heap) do\n    let mut text: String = \"x\".copy(h)\n    text = \"y\"\n    text.free(h)\nend",
		"fun demo(h: Heap, release: Bool) do\n    let mut text: String = \"x\".copy(h)\n    if release then\n        text = \"y\"\n    end\n    text.free(h)\nend",
		"fun demo(h: Heap) do\n    let mut texts: Array<String, 2> = [\"a\".copy(h), \"b\".copy(h)]\n    let i: Size = 0\n    texts[i] = \"lit\"\n    texts[0].free(h)\nend",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot free a String literal") {
			t.Fatalf("want literal-free rejection; got %#v:\n%s", result.Stderr, source)
		}
	}
	accepted := []string{
		"fun demo(h: Heap) do\n    let mut text: String = \"x\"\n    text = \"y\".copy(h)\n    text.free(h)\nend",
		"fun demo(h: Heap, source: String) do\n    source.free(h)\nend",
		"fun make_text(): String do\n    return \"ready\"\nend\nfun demo(h: Heap) do\n    let text: String = make_text()\n    text.free(h)\nend",
		"fun demo(h: Heap) do\n    let values: List<String> = List<String>(h)\n    values.push(\"lit\")\n    let first: String = values[0]\n    first.free(h)\nend",
		"fun demo(h: Heap): Int32 | Error do\n    let raw: Slice<Byte> = \"hi\".bytes()\n    let text: String = try String.from_bytes(h, raw)\n    text.free(h)\n    return 0\nend",
		"fun demo(h: Heap) do\n    let text: String = String.interpolate(h, \"n={{ 1 }}\")\n    text.free(h)\nend",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}

func TestStringStaticReassignment(t *testing.T) {
	result := compileSource("fun demo() do\n    let mut greeting: String = \"hello\"\n    greeting = \"world\"\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestStringParameterCopiesAreValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(source: String) do\n    let other: String = source\nend",
		"fun demo(source: String) do\n    let mut copy: String = \"x\"\n    copy = source\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestStringInArrayIsStoredAndCopiedShallow(t *testing.T) {
	// Array<String, N> is valid; element copies share the String handle,
	// and the array never frees a stored literal.
	result := compileSource("fun demo() do\n    let texts: Array<String, 2> = [\"a\", \"b\"]\n    let copy: Array<String, 2> = texts\n    let first: String = texts[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_array_String_2 hex_v_copy = hex_v_texts;",
		"const hex_string *const hex_v_first = hex_v_texts.data[0];",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

// length() counts bytes on both forms, in constant time, and slice() takes byte
// bounds: a range that splits a UTF-8 sequence is legal and yields bytes.
func TestTextLengthAndSliceAreBytes(t *testing.T) {
	result := compileSource("fun demo() do\n    let heap: String = \"h\u00e9llo\"\n    let inline: String<16> = \"\\u{1F600}\"\n    let a: Size = heap.length()\n    let b: Size = inline.length()\n    let part: Slice<Byte> = heap.slice(1, 2)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"(hex_text_heap(hex_v_heap)).length",
		"(hex_text_inline(&(hex_v_inline))).length",
		"hex_text_slice(hex_text_heap(hex_v_heap), (size_t)(1), (size_t)(2))",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	if strings.Contains(stringH(t, result), "hex_utf8_next") || strings.Contains(stringC(t, result), "hex_utf8_next") {
		t.Fatalf("byte slicing still walks UTF-8 sequences")
	}
}
