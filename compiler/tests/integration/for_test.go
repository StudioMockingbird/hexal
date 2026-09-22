package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestForInSequenceLoops(t *testing.T) {
	result := compileSource("fun demo() do\n    let fixed: Array<Int32, 3> = [10, 20, 30]\n    let mut total: Int32 = 0\n    for value in fixed do\n        total = total + value\n    end\n    for i, value in fixed do\n        total = total + value + i.to<Int32>()\n    end\n    let view: Slice<Int32> = fixed.slice(0, 2)\n    for value in view do\n        total = total + value\n    end\nend\nfun list_sum(h: Heap): Int32 do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    let mut total: Int32 = 0\n    for i, value in values do\n        total = total + value + i.to<Int32>()\n    end\n    return total\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_array_Int32_3 *const hex_for_1 = &(hex_v_fixed);",
		"for (size_t hex_for_1_index = 0; hex_for_1_index < (size_t)(3); hex_for_1_index++) {",
		"const int32_t hex_v_value = hex_for_1->data[hex_for_1_index];",
		"const size_t hex_v_i = hex_for_2_index;",
		"const hex_slice_Int32 hex_for_3 = hex_v_view;",
		"const hex_list_Int32 *const hex_for_1 = hex_v_values;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestForInTemporaryArraySource(t *testing.T) {
	result := compileSource("fun make_fixed(): Array<Int32, 2> do\n    return [1, 2]\nend\nfun demo() do\n    let mut total: Int32 = 0\n    for value in make_fixed() do\n        total = total + value\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "const hex_array_Int32_2 hex_for_1 = hex_f_m3_app_make_fixed();") {
		t.Fatalf("modules/app.c = %q, want materialized temporary Array source", rootC(t, result))
	}
}

func TestForInTextBytes(t *testing.T) {
	result := compileSource("fun demo() do\n    let text: String = \"caf\u00e9\"\n    let mut count: Int32 = 0\n    for b: Byte in text do\n        count = count + 1\n    end\n    for i, b: Byte in text do\n        count = count + 1\n    end\n    let inline: String<8> = \"hi\"\n    for i, b: Byte in inline do\n        count = count + 1\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_string *const hex_for_1 = hex_v_text;",
		"for (size_t hex_for_1_index = 0; hex_for_1_index < hex_for_1->byte_length; hex_for_1_index++) {",
		"const uint8_t hex_v_b = hex_for_1->data[hex_for_1_index];",
		"const size_t hex_v_i = hex_for_2_index;",
		"const hex_string_8 hex_for_3 = hex_v_inline;",
		"hex_for_3_index < hex_for_3.byte_length",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	if strings.Contains(rootC(t, result), "hex_utf8_next") {
		t.Fatalf("text iteration still decodes UTF-8")
	}
}

func TestForInDictEntries(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    let mut total: Int32 = 0\n    for key, value in scores do\n        total = total + key + value\n    end\n    for i, key, value in scores do\n        total = total + value + i.to<Int32>()\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_dict_Int32_Int32 *const hex_for_1 = hex_v_scores;",
		"for (size_t hex_for_1_bucket = 0; hex_for_1_bucket < hex_for_1->capacity; hex_for_1_bucket++) {",
		"if (!hex_for_1->buckets[hex_for_1_bucket].active) {",
		"const int32_t hex_v_key = hex_for_1->buckets[hex_for_1_bucket].key;",
		"const int32_t hex_v_value = hex_for_1->buckets[hex_for_1_bucket].value;",
		"const size_t hex_v_i = hex_for_2_ordinal;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestForInBinderShadowingAndImmutability(t *testing.T) {
	result := compileSource("fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    let value: Int32 = 100\n    for value in fixed do\n        let current: Int32 = value\n    end\n    for value in fixed do\n        value = 10\n    end\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "loop binder value is immutable") {
		t.Fatalf("Compile stderr = %#v, want binder immutability diagnostic", result.Stderr)
	}
}

func TestForInDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"not iterable", "fun demo() do\n    let count: Int32 = 3\n    for value in count do\n    end\nend", "value of type Int32 is not iterable"},
		{"sequence arity", "fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    for a, b, c in fixed do\n    end\nend", "sequence iteration requires one value binder or index and value binders"},
		{"dict arity", "fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    for key in scores do\n    end\nend", "dictionary iteration requires key and value binders or index, key, and value binders"},
		{"excess binders", "fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    for i, key, value, extra in scores do\n    end\nend", "a for-in loop takes at most 3 binders"},
		{"duplicate binder", "fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    for value, value in fixed do\n    end\nend", "duplicate loop binder name value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestForInParserErrors(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"for value in values print(value) end", "expected 'do' after for source"},
		{"while ready print(\"waiting\") end", "expected 'do' after while condition"},
		{"for value values do end", "expected 'in' after loop binders"},
		{"for in values do end", "expected a loop binder name after 'for'"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, " "), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestForInSourceEvaluatedOnce(t *testing.T) {
	result := compileSource("fun count_calls(): Array<Int32, 2> do\n    return [1, 2]\nend\nfun demo() do\n    let mut total: Int32 = 0\n    for value in count_calls() do\n        total = total + value\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Count(rootC(t, result), "hex_f_m3_app_count_calls()") != 1 {
		t.Fatalf("modules/app.c = %q, want exactly one source evaluation", rootC(t, result))
	}
}

func TestForInRejectsKnownCollectionMutations(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "list push",
			source: "fun demo(h: Heap) do\n" +
				"    let values: List<Int32> = List<Int32>(h)\n" +
				"    values.push(1)\n" +
				"    for value in values do\n" +
				"        values.push(value)\n" +
				"    end\n" +
				"end",
			want: "cannot mutate collection during iteration",
		},
		{
			name: "list free",
			source: "fun demo(h: Heap) do\n" +
				"    let values: List<Int32> = List<Int32>(h)\n" +
				"    values.push(1)\n" +
				"    for value in values do\n" +
				"        values.free(h)\n" +
				"    end\n" +
				"end",
			want: "cannot free collection during iteration",
		},
		{
			name: "dict insert",
			source: "fun demo(h: Heap) do\n" +
				"    let values: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
				"    values.insert(1, 10)\n" +
				"    for key, value in values do\n" +
				"        values.insert(key, value)\n" +
				"    end\n" +
				"end",
			want: "cannot mutate collection during iteration",
		},
		{
			name: "dict free",
			source: "fun demo(h: Heap) do\n" +
				"    let values: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
				"    values.insert(1, 10)\n" +
				"    for key, value in values do\n" +
				"        values.free(h)\n" +
				"    end\n" +
				"end",
			want: "cannot free collection during iteration",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}

func TestForInRejectsAliasedFreeAndUnprovenCalls(t *testing.T) {
	source := "fun release(values: List<Int32>, h: Heap): Int32 do\n" +
		"    values.free(h)\n" +
		"    return 0\n" +
		"end\n" +
		"fun demo(h: Heap) do\n" +
		"    let values: List<Int32> = List<Int32>(h)\n" +
		"    let alias: List<Int32> = values\n" +
		"    values.push(1)\n" +
		"    for value in values do\n" +
		"        let ignored: Int32 = release(alias, h)\n" +
		"    end\n" +
		"end"
	assertRejects(t, source, "cannot pass traversed collection to call during iteration")

	freeSource := "fun demo(h: Heap) do\n" +
		"    let values: List<Int32> = List<Int32>(h)\n" +
		"    let alias: List<Int32> = values\n" +
		"    values.push(1)\n" +
		"    for value in values do\n" +
		"        alias.free(h)\n" +
		"    end\n" +
		"end"
	assertRejects(t, freeSource, "cannot free collection during iteration")
}

func TestForInCopiedMutationUsesVersionCheck(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap) do\n"+"    let values: List<Int32> = List<Int32>(h)\n"+"    let alias: List<Int32> = values\n"+"    values.push(1)\n"+"    for value in values do\n"+"        alias.push(value)\n"+"    end\n"+"end")
	c := rootC(t, result)
	version := strings.Index(c, "if (hex_for_1->version != hex_for_1_version)")
	access := strings.Index(c, "hex_list_at_Int32(hex_for_1")
	mutation := strings.Index(c, "hex_list_push_Int32(hex_v_alias")
	if version < 0 || access < 0 || mutation < 0 || version > access || mutation < access {
		t.Fatalf("modules/app.c = %q, want version check before access and copied mutation in the loop", c)
	}
}

func TestForInDictChecksVersionBeforeBuckets(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap) do\n"+"    let values: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n"+"    values.insert(1, 10)\n"+"    for key, value in values do\n"+"        let total: Int32 = key + value\n"+"    end\n"+"end")
	c := rootC(t, result)
	version := strings.Index(c, "if (hex_for_1->version != hex_for_1_version)")
	active := strings.Index(c, "if (!hex_for_1->buckets[hex_for_1_bucket].active)")
	if version < 0 || active < 0 || version > active {
		t.Fatalf("modules/app.c = %q, want version check before bucket access", c)
	}
}

func TestForInNestedTraversalsCaptureIndependentVersions(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap) do\n"+
		"    let outer: List<Int32> = List<Int32>(h)\n"+"    let inner: List<Int32> = List<Int32>(h)\n"+"    outer.push(1)\n"+"    inner.push(2)\n"+"    for a in outer do\n"+"        for b in inner do\n"+"            let total: Int32 = a + b\n"+"        end\n"+"    end\n"+"end")
	c := rootC(t, result)
	if !strings.Contains(c, "hex_for_1_version") || !strings.Contains(c, "hex_for_2_version") {
		t.Fatalf("modules/app.c = %q, want independent List traversal versions", c)
	}
}

// A binder annotation is accepted exactly where it matches, at one, two, and
// three binders, for every iterable; Byte and UInt8 are the same annotation.
func TestForInBinderAnnotationsAgree(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n" +
		"    let list: List<Int32> = List<Int32>(h)\n    defer list.free(h)\n" +
		"    let fixed: Array<Int32, 2> = [1, 2]\n" +
		"    let view: Slice<Int32> = fixed.slice(0, 2)\n" +
		"    let bytes: Slice<Byte> = \"ab\".bytes()\n" +
		"    let table: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer table.free(h)\n" +
		"    for x: Int32 in list do\n    end\n" +
		"    for i: Size, x: Int32 in list do\n    end\n" +
		"    for x: Int32 in fixed do\n    end\n" +
		"    for i: Size, x: Int32 in view do\n    end\n" +
		"    for b: UInt8 in bytes do\n    end\n" +
		"    for b: Byte in bytes do\n    end\n" +
		"    for k: Int32, v: Int32 in table do\n    end\n" +
		"    for i: Size, k: Int32, v: Int32 in table do\n    end\n" +
		"end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// A wrong value or index annotation, and a Slice<Byte> annotation over a mutable
// element, report the disagreement; the binder stays immutable when annotated.
func TestForInBinderAnnotationDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"value", "fun demo(h: Heap) do\n    let list: List<Int32> = List<Int32>(h)\n    for x: Int64 in list do\n    end\nend", "for binder x is annotated Int64, but List<Int32> yields Int32 there"},
		{"index", "fun demo(h: Heap) do\n    let list: List<Int32> = List<Int32>(h)\n    for i: Int32, x: Int32 in list do\n    end\nend", "for binder i is annotated Int32, but List<Int32> yields Size there"},
		{"dict key", "fun demo(h: Heap) do\n    let table: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    for k: Int64, v: Int32 in table do\n    end\nend", "for binder k is annotated Int64, but Dict<Int32, Int32> yields Int32 there"},
		{"mutable element", "fun demo(h: Heap) do\n    let views: List<Slice<mut Byte>> = List<Slice<mut Byte>>(h)\n    for v: Slice<Byte> in views do\n    end\nend", "for binder v is annotated Slice<UInt8>, but List<Slice<mut UInt8>> yields Slice<mut UInt8> there"},
		{"binder immutable", "fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    for x: Int32 in fixed do\n        x = 3\n    end\nend", "loop binder x is immutable"},
	} {
		result := compileSource(tc.source)
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), tc.want) {
			t.Fatalf("%s: stderr = %#v, want %q", tc.name, result.Stderr, tc.want)
		}
	}
}

// Text iteration has no default element type: the value binder must say what
// it reads, for the heap form, every inline capacity, and the two-binder form
// where only the index is annotated.
func TestForInTextRequiresAnnotatedValueBinder(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"heap", "fun demo() do\n    let text: String = \"x\"\n    for b in text do\n    end\nend", "for binder b over String has an ambiguous element type; annotate it, for example for b: Byte in ..."},
		{"inline", "fun demo() do\n    let text: String<8> = \"x\"\n    for b in text do\n    end\nend", "for binder b over String<8> has an ambiguous element type; annotate it, for example for b: Byte in ..."},
		{"index only", "fun demo() do\n    let text: String<8> = \"x\"\n    for i: Size, b in text do\n    end\nend", "for binder b over String<8> has an ambiguous element type; annotate it, for example for b: Byte in ..."},
		{"index unannotated", "fun demo() do\n    let text: String = \"x\"\n    for i, b in text do\n    end\nend", "for binder b over String has an ambiguous element type"},
	} {
		result := compileSource(tc.source)
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), tc.want) {
			t.Fatalf("%s: stderr = %#v, want %q", tc.name, result.Stderr, tc.want)
		}
	}
}

// Text iteration's element type belongs to the binder: Byte reads storage
// units and Rune decodes scalars, on the heap and inline forms alike.
func TestForInTextElementBinder(t *testing.T) {
	for _, source := range []string{
		"fun demo() do\n    let text: String = \"x\"\n    for b: Byte in text do\n    end\nend",
		"fun demo() do\n    let text: String = \"x\"\n    for r: Rune in text do\n    end\nend",
		"fun demo() do\n    let text: String<8> = \"x\"\n    for i: Size, r: Rune in text do\n    end\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) = %v", source, result.Stderr)
		}
	}
}

// Rune iteration decodes one scalar per step through the shared utf8proc step;
// Byte iteration selects nothing.
func TestForInTextRuneDecodes(t *testing.T) {
	runes := assertCompiles(t, "fun demo() do\n    let text: String = \"h\\u{e9}\"\n    for r: Rune in text do\n    end\nend\n")
	if !strings.Contains(rootC(t, runes), "hex_utf8_decode_step") {
		t.Fatalf("Rune iteration did not decode through the utf8proc step:\n%s", rootC(t, runes))
	}
	bytes := assertCompiles(t, "fun demo() do\n    let text: String = \"x\"\n    for b: Byte in text do\n    end\nend\n")
	if strings.Contains(rootC(t, bytes), "hex_utf8_decode_step") {
		t.Fatalf("Byte iteration selected the Rune decode step:\n%s", rootC(t, bytes))
	}
}

// A List of a union keeps the plain binder and accepts the whole union as the// annotation; annotating one member is rejected because the element may be any.
func TestForInBinderOverUnionElements(t *testing.T) {
	prefix := "fun demo(h: Heap) do\n    let l: List<Int32 | Bool> = List<Int32 | Bool>(h)\n    defer l.free(h)\n"
	for _, body := range []string{"    for a in l do\n    end\n", "    for a: Int32 | Bool in l do\n    end\n"} {
		if result := compileSource(prefix + body + "end"); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) = %v", body, result.Stderr)
		}
	}
	result := compileSource(prefix + "    for a: Int32 in l do\n    end\nend")
	if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "for binder a is annotated Int32, but List<Bool | Int32> yields Bool | Int32 there") {
		t.Fatalf("stderr = %#v, want the member-annotation diagnostic", result.Stderr)
	}
}

// The loop reads a snapshot: reassigning the inline text inside the body cannot
// change what is read or how often.
func TestForInInlineTextIsASnapshot(t *testing.T) {
	result := compileSource("fun demo() do\n    let mut text: String<8> = \"ab\"\n    let mut count: Int32 = 0\n    for b: Byte in text do\n        text = \"abcdefg\"\n        count = count + 1\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "const hex_string_8 hex_for_1 = hex_v_text;") {
		t.Fatalf("the loop does not read a copy of the text:\n%s", rootC(t, result))
	}
}
