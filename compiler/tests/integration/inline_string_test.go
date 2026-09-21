package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// rejectedWith requires each source to fail with the exact diagnostic text
// among its stderr lines.
func rejectedWith(t *testing.T, cases []struct{ source, want string }) {
	t.Helper()
	for _, testCase := range cases {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) exit=%d stderr=%#v, want %q", testCase.source, result.ExitCode, result.Stderr, testCase.want)
		}
	}
}

// String<N> resolves for a positive decimal literal capacity up to 4096, in
// every spelling the decimal grammar allows, and rejects everything else with
// one diagnostic naming what a capacity is.
func TestInlineStringCapacityRules(t *testing.T) {
	assertCompiles(t, "let a: String<16> = \"hello\"\n")
	assertCompiles(t, "let a: String<4096> = \"x\"\n")
	assertCompiles(t, "let a: String<1_024> = \"x\"\nlet b: String<1024> = a\n")
	rejectedWith(t, []struct{ source, want string }{
		{"let a: String<0> = \"x\"\n", "String capacity must be a positive integer literal"},
		{"let n: Int32 = 5\nlet a: String<n> = \"x\"\n", "String capacity must be a positive integer literal"},
		{"fun f<T>(v: String<T>) do\nend\n", "String capacity must be a positive integer literal"},
		{"let a: String<3.5> = \"x\"\n", "String capacity must be a positive integer literal"},
		{"let a: String<4097> = \"x\"\n", "String capacity 4097 exceeds the maximum of 4096"},
		{"let a: String<1, 2> = \"x\"\n", "String takes at most one capacity argument"},
	})
}

// A literal is measured in bytes against N at compile time; invalid UTF-8 in
// the source keeps its own diagnostic; a bare literal is still not a binding
// initializer.
func TestInlineStringLiteralCapacityAndContext(t *testing.T) {
	assertCompiles(t, "let a: String<5> = \"hello\"\n")
	assertCompiles(t, "let a: String<2> = \"é\"\n")
	rejectedWith(t, []struct{ source, want string }{
		{"let a: String<4> = \"hello\"\n", "String<4> literal exceeds 4 UTF-8 bytes"},
		{"let a: String<1> = \"é\"\n", "String<1> literal exceeds 1 UTF-8 bytes"},
		{"let a = \"hi\"\n", "`let` requires an initializer whose type does not depend on context"},
	})
	// A literal holding invalid UTF-8 is refused at compile time for both forms.
	for _, source := range []string{"let a: String<8> = \"\xff\"\n", "let a: String = \"\xff\"\n"} {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "string literal contains invalid UTF-8") {
			t.Fatalf("Compile(%q) stderr = %#v, want the invalid UTF-8 diagnostic", source, result.Stderr)
		}
	}
	result := assertCompiles(t, "fun demo() do\n    let a: String<8> = \"a\\0b\"\n    let b: String = \"a\\0b\"\nend\n")
	if !strings.Contains(rootC(t, result), ".byte_length = 3, .data = { 97, 0, 98, }") {
		t.Fatalf("embedded NUL was not kept in the inline literal:\n%s", rootC(t, result))
	}
}

// Every capacity is one canonical type, and List<String<N>> at two capacities
// are different types.
func TestInlineStringTypesInternOnce(t *testing.T) {
	assertCompiles(t, "let a: String<16> = \"x\"\nlet b: String<16> = a\n")
	assertRejects(t, "let l: List<String<16>> = List<String<16>>(Heap())\nlet m: List<String<32>> = l\n", "expected List<String<32>> initializer; got List<String<16>>")
}

// widen<M> is explicit and infallible: it requires M >= N and returns a plain
// String<M>. Nothing else converts between capacities.
func TestInlineStringWiden(t *testing.T) {
	result := assertCompiles(t, "fun demo() do\n    let small: String<16> = \"hi\"\n    let large: String<64> = small.widen<64>()\n    let same: String<16> = small.widen<16>()\nend\n")
	if !strings.Contains(rootC(t, result), "(*(hex_string_64 *)hex_text_fill(&(hex_string_64){0}, hex_text_inline(&(hex_v_small))))") {
		t.Fatalf("widen did not lower to one fill of the destination:\n%s", rootC(t, result))
	}
	rejectedWith(t, []struct{ source, want string }{
		{"let a: String<16> = \"hello\"\nlet b: String<8> = a.widen<8>()\n", "widen<M> requires M greater than or equal to N"},
		{"let a: String<16> = \"hello\"\nlet b: String<64> = a\n", "expected String<64> initializer; got String<16>; use widen<64>()"},
		{"let a: String = \"hello\"\nlet b: String<8> = a.widen<8>()\n", "String has no method widen"},
	})
}

// Every position requires the exact type and names the explicit conversion.
func TestInlineStringPositionsRequireExactType(t *testing.T) {
	rejectedWith(t, []struct{ source, want string }{
		{"fun take(v: String<32>) do\nend\nfun demo() do\n    let a: String<16> = \"x\"\n    take(a)\nend\n", "take argument 1 requires String<32>; got String<16>; use widen<32>()"},
		{"fun take(v: String<16>) do\nend\nfun demo() do\n    let a: String = \"x\"\n    take(a)\nend\n", "take argument 1 requires String<16>; got String; use String<16>.from_bytes(...) for a checked conversion"},
		{"fun take(v: String) do\nend\nfun demo() do\n    let a: String<16> = \"x\"\n    take(a)\nend\n", "take argument 1 requires String; got String<16>; use copy(heap)"},
		{"fun give(): String<32> do\n    let a: String<16> = \"x\"\n    return a\nend\n", "give returns String<32>; got String<16>; use widen<32>()"},
		{"type Box is struct name: String<32> end\nfun demo() do\n    let a: String<16> = \"x\"\n    let box: Box = Box(name = a)\nend\n", "; use widen<32>()"},
		{"type W is union | A as text: String<32> end | B as n: Int32 end end\nfun demo() do\n    let a: String<16> = \"x\"\n    let w: W = W.A(text = a)\nend\n", "; use widen<32>()"},
		{"fun demo(h: Heap) do\n    let l: List<String<32>> = List<String<32>>(h)\n    let a: String<16> = \"x\"\n    l.push(a)\nend\n", "list element requires String<32>; got String<16>; use widen<32>()"},
	})
}

// Text values are complete, copyable values valid in every position the type
// system names; nothing needs cleanup.
func TestInlineStringIsAValueInEveryPosition(t *testing.T) {
	source := "type Box is struct name: String<16>, other: String end\n" +
		"type W is union | A as text: String<8> end | B as n: Int32 end end\n" +
		"fun make(): String<8> do\n    return \"task\"\nend\n" +
		"fun demo(h: Heap): Bool do\n" +
		"    let texts: Array<String<8>, 2> = [\"a\", \"bc\"]\n" +
		"    let view: Slice<String<8>> = texts.slice(0, 2)\n" +
		"    let items: List<String<8>> = List<String<8>>(h)\n    defer items.free(h)\n    items.push(view[1])\n" +
		"    let box: Box = Box(name = \"hi\", other = \"x\")\n" +
		"    let w: W = W.A(text = \"yo\")\n" +
		"    let table: Dict<Int32, String<8>> = Dict<Int32, String<8>>(h)\n    defer table.free(h)\n    table.insert(1, \"v\")\n" +
		"    let pool: Pool<String<8>> = Pool<String<8>>(2)\n" +
		"    let slot: Ptr<mut String<8>> = pool.allocate(\"pooled\")\n    pool.free(slot)\n    pool.destroy()\n" +
		"    let stash: Stash<String<8>> = Stash<String<8>>()\n    let slot2: Ptr<mut String<8>> = stash.allocate(\"stashed\")\n    stash.destroy()\n" +
		"    let allocated: Ptr<mut String<8>> = h.allocate<String<8>>(\"heaped\")\n    h.free(allocated)\n" +
		"    let channel: Channel<String<8>> | Error = Channel<String<8>>(h, 1)\n" +
		"    let task: Task<String<8>> | Error = spawn make()\n" +
		"    return true\n" +
		"end\n"
	result := assertCompiles(t, source)
	// Copying an inline value never allocates and never adds a free: the only
	// releases are the collections' own.
	if strings.Contains(rootC(t, result), "hex_string_free(hex_v_h, hex_v_box") {
		t.Fatalf("an inline text member was released as a heap String:\n%s", rootC(t, result))
	}
}

// A union injects only the member it holds; a contextual literal picks the
// first written member whose capacity holds it; is and match tell members apart.
func TestInlineStringUnionInjection(t *testing.T) {
	assertCompiles(t, "let a: String<16> | String<32> = \"hello\"\nlet b: String<32> | String<16> = \"hello\"\nlet c: String<4> | String<32> = \"hello\"\n"+
		"let x: Bool = a is String<16>\nlet y: Bool = b is String<32>\nlet z: Bool = c is String<32>\n"+
		"let label: Int32 = match a is\n| String<16> then 16\n| String<32> then 32\nend\n")
	assertCompiles(t, "let a: String<16> = \"x\"\nlet u: String<16> | Nil = a\nlet w: String<16> | String<32> = a\n")
	rejectedWith(t, []struct{ source, want string }{
		{"let a: String<8> = \"hello world\"\n", "String<8> literal exceeds 8 UTF-8 bytes"},
		{"let b: String<16> = \"x\"\nlet c: String<32> | Nil = b\n", "no member of String<32> | Nil accepts this expression"},
		{"let a: String<2> | String<3> = \"hello\"\n", "no member of String<2> | String<3> accepts this expression"},
	})
	// Written order decides the member; the two spellings hold different types.
	first := assertCompiles(t, "fun demo(): Int32 do\n    let a: String<16> | String<32> = \"hello\"\n    return match a is\n    | String<16> then 16\n    | String<32> then 32\n    end\nend\n")
	second := assertCompiles(t, "fun demo(): Int32 do\n    let a: String<32> | String<16> = \"hello\"\n    return match a is\n    | String<16> then 16\n    | String<32> then 32\n    end\nend\n")
	if strings.Contains(rootC(t, first), "hex_string_32){") && !strings.Contains(rootC(t, first), "hex_string_16){") {
		t.Fatalf("String<16> | String<32> did not select the first written member")
	}
	if !strings.Contains(rootC(t, second), "hex_string_32){") {
		t.Fatalf("String<32> | String<16> did not select its first written member:\n%s", rootC(t, second))
	}
}

// No function is generic over a capacity, but a capacity type substitutes
// like any other and specializations stay distinct.
func TestInlineStringGenericsAndInference(t *testing.T) {
	result := assertCompiles(t, "fun same<T>(a: T, b: T): Bool do\n    return a == b\nend\nfun demo() do\n    let a: String<16> = \"x\"\n    let r: Bool = same(a, a)\n    let s = String<16>.from_bytes(\"x\".bytes())\nend\n")
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_same_String_16_") {
		t.Fatalf("the generic specialization lost its capacity:\n%s", rootC(t, result))
	}
	rejectedWith(t, []struct{ source, want string }{
		{"fun f<N>(a: String<N>) do\nend\n", "String capacity must be a positive integer literal"},
		{"let a = \"hi\"\n", "`let` requires an initializer whose type does not depend on context"},
	})
}

// String<N> owns nothing: it has neither free nor c_pointer, and its bytes()
// and slice() need a place, since a slice into a temporary would dangle at once.
func TestInlineStringMethodSurfaceAndProvenance(t *testing.T) {
	rejectedWith(t, []struct{ source, want string }{
		{"fun demo(h: Heap) do\n    let a: String<16> = \"x\"\n    a.free(h)\nend\n", "String<16> has no method free"},
		{"fun demo() do\n    let a: String<16> = \"x\"\n    let p: Ptr<Byte> = a.c_pointer()\nend\n", "String<16> has no method c_pointer"},
		{"fun f(): String<16> do\n    return \"x\"\nend\nfun demo() do\n    let b: Slice<Byte> = f().bytes()\nend\n", "a Slice cannot be rooted in a temporary String<16>"},
		{"fun f(): String<16> do\n    return \"x\"\nend\nfun demo() do\n    let b: Slice<Byte> = f().slice(0, 1)\nend\n", "a Slice cannot be rooted in a temporary String<16>"},
		{"let a: String<16> = \"x\"\nlet c: Slice<mut Byte> = a.bytes()\n", "expected Slice<mut UInt8> initializer; got Slice<UInt8>"},
		{"let a: String<8> = \"x\"\nlet q: Ptr<String> = nil\n", "could not construct pointer type"},
	})
	// Bound first, the same call compiles, on a binding, a member, and a mut binding.
	assertCompiles(t, "type Box is struct name: String<16> end\nfun f(): String<16> do\n    return \"x\"\nend\nfun demo() do\n    let a: String<16> = f()\n    let b: Slice<Byte> = a.bytes()\n    let box: Box = Box(name = \"y\")\n    let c: Slice<Byte> = box.name.slice(0, 1)\n    let mut m: String<16> = \"z\"\n    let d: Slice<Byte> = m.bytes()\nend\n")
}

// A pointer to inline text is valid and its layout is measurable; a heap
// String is still no pointee. Neither form has a foreign ABI mapping, but a
// pointer to one crosses as an address.
func TestInlineStringLayoutPointersAndForeign(t *testing.T) {
	assertCompiles(t, "fun demo() do\n    let a: String<8> = \"x\"\n    let p: Ptr<String<8>> = @a\n    let s: Size = size_of<String<8>>()\n    let al: Size = align_of<String<8>>()\n    let h: Size = size_of<String>()\nend\n")
	rejectedWith(t, []struct{ source, want string }{
		{"let s: Size = size_of<String<n>>()\n", "String capacity must be a positive integer literal"},
	})
	target := func(source string) compiler.CompilationResult {
		return compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{Target: "x86_64-linux-gnu"})
	}
	for _, source := range []string{
		"extern c from <string.h> do\n    fun c_bad as \"strlen\"(text: String<8> as \"const char *\"): Size as \"size_t\"\nend\n",
		"extern c from <stdlib.h> do\n    global c_counter as \"counter\": String<8>\nend\n",
	} {
		if result := target(source); result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "String<8> has no supported C ABI mapping for target x86_64-linux-gnu") {
			t.Fatalf("Compile(%q) stderr = %#v, want the ABI rejection", source, result.Stderr)
		}
	}
	if result := target("extern c from <string.h> do\n    fun c_touch as \"memset\"(target: Ptr<mut String<8>>, value: Int32 as \"int\", count: Size as \"size_t\"): Ptr<mut Unknown> | Nil\nend\nfun demo() do\n    let mut text: String<8> = \"abc\"\n    unsafe do\n        let r: Ptr<mut Unknown> | Nil = c_touch(@text, 0, 1)\n    end\nend\n"); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a pointer to inline text must cross the ABI: %#v", result.Stderr)
	}
}

// Every producing operation that can fail returns its success type joined with
// Error; heap interpolate and copy cannot fail and stay plain. The inline form
// takes no Heap.
func TestTextProducingOperationResults(t *testing.T) {
	assertCompiles(t, "fun demo(h: Heap): Int32 | Error do\n"+
		"    let small: String<16> = \"hi\"\n"+
		"    let a: String<16> | Error = String<16>.from_bytes(small.bytes())\n"+
		"    let b: String<48> | Error = String<48>.concat(small.bytes(), small.bytes())\n"+
		"    let c: String<48> | Error = String<48>.interpolate(\"n={{ 1 }}\")\n"+
		"    let d: String | Error = String.from_bytes(h, small.bytes())\n"+
		"    let e: String | Error = small.concat(h, small.bytes())\n"+
		"    let f: String = String.interpolate(h, \"n={{ 1 }}\")\n"+
		"    let g: String = small.copy(h)\n"+
		"    let widened: String<32> | Error = String<32>.from_bytes(g.bytes())\n"+
		"    return 0\nend\n")
	rejectedWith(t, []struct{ source, want string }{
		{"fun demo(h: Heap): Int32 | Error do\n    let f: String = try String.interpolate(h, \"n={{ 1 }}\")\n    return 0\nend\n", "try requires a union containing Error and a success member; got String"},
		{"fun demo(h: Heap) do\n    let d: String = String.from_bytes(h, \"x\".bytes())\nend\n", "expected String initializer; got Error | String"},
		{"fun demo(h: Heap) do\n    let s: String = \"x\".to_string(h)\nend\n", "String has no method to_string"},
		{"fun demo() do\n    let a: String<4> | Error = String<4>.interpolate(\"plain\")\nend\n", "String<4>.interpolate requires at least one interpolation"},
		{"fun demo(h: Heap) do\n    let a = String<4>.from_bytes(h, \"x\".bytes())\nend\n", "String<4>.from_bytes expects 1 arguments; got 2"},
		{"fun demo() do\n    let a = String<4>.from_runes(1)\nend\n", "String<4> has no such operation; use String<4>.from_bytes(view), String<4>.concat(left, right), or String<4>.interpolate(template)"},
	})
}

// A multi-byte sequence split across the two operands of String<N>.concat is
// valid once joined, so validation covers the result, not each operand; a heap
// concat appends to text that is already valid and validates only what it
// appends.
func TestInlineConcatValidatesTheJoinedResult(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Bool do\n    let lead: Array<Byte, 1> = [0xC3]\n    let tail: Array<Byte, 1> = [0xA9]\n    let joined: String<4> | Error = String<4>.concat(lead.slice(0, 1), tail.slice(0, 1))\n    return joined is String<4>\nend\n")
	header := rootH(t, result)
	if !strings.Contains(header, "hex_utf8_valid(value.data, total)") {
		t.Fatalf("inline concat does not validate the joined bytes:\n%s", header)
	}
	if strings.Index(header, "total > 4") > strings.Index(header, "hex_utf8_valid(value.data, total)") {
		t.Fatalf("capacity must be checked before content:\n%s", header)
	}
}

// A template is meaningful only as the argument of an interpolate call: in any
// other position, including where an inline String<N> is expected, it is
// rejected rather than allocating or building text implicitly.
func TestInterpolationTemplateOnlyInsideInterpolateCalls(t *testing.T) {
	const want = "string interpolation requires String.interpolate(heap, template)"
	rejectedWith(t, []struct{ source, want string }{
		{"let a: String<16> = \"n={{ 1 }}\"\n", want},
		{"let a: String = \"n={{ 1 }}\"\n", want},
		{"print(\"n={{ 1 }}\")\n", want},
		{"fun demo(h: Heap) do\n    let a = String<16>.from_bytes(\"n={{ 1 }}\")\nend\n", want},
	})
	assertCompiles(t, "fun demo(): String<16> | Error do\n    return String<16>.interpolate(\"n={{ 1 }}\")\nend\n")
}

// Comparing text of different forms compiles wherever the operands are calls,
// and each call is evaluated once, left before right.
func TestMixedTextComparisonsSequenceCalls(t *testing.T) {
	prelude := "fun small(): String<16> do\n    return \"ab\"\nend\nfun large(): String<64> do\n    return \"ab\"\nend\nfun heap(h: Heap): String do\n    return \"ab\".copy(h)\nend\n"
	for _, expression := range []string{
		"small() == large()", "large() != small()", "small() < large()", "large() >= small()",
		"small() == heap(h)", "heap(h) <= large()", "heap(h) > small()",
	} {
		result := assertCompiles(t, prelude+"fun demo(h: Heap): Bool do\n    return "+expression+"\nend\n")
		if strings.Count(rootC(t, result), "hex_f_m3_app_") < 3 {
			t.Fatalf("%s lost a call:\n%s", expression, rootC(t, result))
		}
	}
	result := assertCompiles(t, prelude+"fun demo(h: Heap): Bool do\n    return small() == large()\nend\n")
	source := rootC(t, result)
	left, right := strings.Index(source, "hex_f_m3_app_small()"), strings.Index(source, "hex_f_m3_app_large()")
	if left < 0 || right < 0 || strings.Count(source, "hex_f_m3_app_small()") != 1 || strings.Count(source, "hex_f_m3_app_large()") != 1 || left > right {
		t.Fatalf("operands are not each evaluated once, left first:\n%s", source)
	}
}
