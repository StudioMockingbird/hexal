package compiler

import (
	"strings"
	"testing"
)

// RFC 0028: for-in iteration over arrays, views, lists, text, and dicts.

func TestForInSequenceLoops(t *testing.T) {
	result := Compile("fun demo()\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    mut total: Int32 = 0\n    for value in fixed do\n        total = total + value\n    end\n    for i, value in fixed do\n        total = total + value + i.to<Int32>()\n    end\n    view: View<Int32> = fixed.slice(0, 2)\n    for value in view do\n        total = total + value\n    end\nend\nfun list_sum(h: Heap): Int32\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    mut total: Int32 = 0\n    for i, value in values do\n        total = total + value + i.to<Int32>()\n    end\n    return total\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"hex_array_Int32_3 *const hex_for_1 = &(hex_v_fixed);",
		"for (size_t hex_for_1_index = 0; hex_for_1_index < (size_t)(3); hex_for_1_index++) {",
		"const int32_t hex_v_value = *hex_array_at_Int32_3(hex_for_1, (size_t)(hex_for_1_index));",
		"const size_t hex_v_i = hex_for_2_index;",
		"const hex_view_Int32 hex_for_3 = hex_v_view;",
		"const hex_list_Int32 *const hex_for_1 = hex_v_values;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestForInTemporaryArraySource(t *testing.T) {
	result := Compile("fun make_fixed(): Array<Int32, 2>\n    return [1, 2]\nend\nfun demo()\n    mut total: Int32 = 0\n    for value in make_fixed() do\n        total = total + value\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "const hex_array_Int32_2 hex_for_1 = hex_f_make_fixed();") {
		t.Fatalf("main.c = %q, want materialized temporary Array source", result.MainC)
	}
}

func TestForInTextRunes(t *testing.T) {
	result := Compile("fun demo()\n    text: String = \"café\"\n    mut count: Int32 = 0\n    for rune in text do\n        count = count + 1\n    end\n    for i, rune in text do\n        count = count + 1\n    end\n    strand: Strand = \"hi\"\n    for i, rune in strand do\n        count = count + 1\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const hex_string *const hex_for_1 = hex_v_text;",
		"while (hex_for_1_offset < hex_for_1->byte_length) {",
		"hex_utf8_next(hex_for_1->data, hex_for_1->byte_length, &hex_for_1_offset)",
		"const uint32_t hex_v_rune = (hex_for_1_rune);",
		"const size_t hex_v_i = hex_for_2_ordinal;",
		"const hex_strand hex_for_3 = hex_v_strand;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestForInDictEntries(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    mut total: Int32 = 0\n    for key, value in scores do\n        total = total + key + value\n    end\n    for i, key, value in scores do\n        total = total + value + i.to<Int32>()\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const hex_dict_Int32_Int32 *const hex_for_1 = hex_v_scores;",
		"for (size_t hex_for_1_bucket = 0; hex_for_1_bucket < hex_for_1->capacity; hex_for_1_bucket++) {",
		"if (!hex_for_1->buckets[hex_for_1_bucket].active) {",
		"const int32_t hex_v_key = hex_for_1->buckets[hex_for_1_bucket].key;",
		"const int32_t hex_v_value = hex_for_1->buckets[hex_for_1_bucket].value;",
		"const size_t hex_v_i = hex_for_2_ordinal;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestForInBinderShadowingAndImmutability(t *testing.T) {
	result := Compile("fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    value: Int32 = 100\n    for value in fixed do\n        current: Int32 = value\n    end\n    for value in fixed do\n        value = 10\n    end\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "loop binder value is immutable") {
		t.Fatalf("Compile stderr = %#v, want binder immutability diagnostic", result.Stderr)
	}
}

func TestForInDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"not iterable", "fun demo()\n    count: Int32 = 3\n    for value in count do\n    end\nend", "value of type Int32 is not iterable"},
		{"sequence arity", "fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    for a, b, c in fixed do\n    end\nend", "sequence iteration requires one value binder or index and value binders"},
		{"dict arity", "fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    for key in scores do\n    end\nend", "dictionary iteration requires key and value binders or index, key, and value binders"},
		{"stream arity", "fun demo(h: Heap)\n    source: Stream<Int32> = Stream<Int32>.new()\n    for i, value, extra in source do\n    end\nend", "stream iteration requires one value binder or index and value binders"},
		{"excess binders", "fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    for i, key, value, extra in scores do\n    end\nend", "a for-in loop takes at most 3 binders"},
		{"duplicate binder", "fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    for value, value in fixed do\n    end\nend", "duplicate loop binder name value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := Compile(testCase.source)
			if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
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
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, " "), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestForInSourceEvaluatedOnce(t *testing.T) {
	result := Compile("fun count_calls(): Array<Int32, 2>\n    return [1, 2]\nend\nfun demo()\n    mut total: Int32 = 0\n    for value in count_calls() do\n        total = total + value\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if strings.Count(result.MainC, "hex_f_count_calls()") != 1 {
		t.Fatalf("main.c = %q, want exactly one source evaluation", result.MainC)
	}
}
