package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestInlineListValueOperations(t *testing.T) {
	result := compileSource("fun demo() do\n    let mut values: List<Int32, 4> = []\n    values.push(7)\n    values[0] = 9\n    let copy: List<Int32, 4> = values\n    let size: Size = copy.length()\n    let same: Bool = values == copy\n    values.clear()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_list_inline_Int32_4 {",
		"size_t length;",
		"size_t version;",
		"int32_t data[4];",
		".length = 0, .version = 0",
		"hex_list_inline_push_Int32_4(&( hex_v_values ), 7)",
		"hex_list_inline_at_mut_Int32_4(&(hex_v_values), (size_t)(0))",
		"hex_list_inline_clear_Int32_4(&( hex_v_values ))",
		"hex_equal_hex_list_inline_Int32_4",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(listH(t, result), want) {
			t.Fatalf("generated output does not contain %q:\n%s\n%s\n%s", want, rootC(t, result), rootH(t, result), listH(t, result))
		}
	}
}

func TestInlineListLiteralsAndCapacityDiagnostics(t *testing.T) {
	for _, source := range []string{
		"fun demo() do\n    let empty: List<Int32, 3> = []\n    let partial: List<Int32, 3> = [1]\n    let full: List<Int32, 3> = [1, 2, 3]\nend",
		"fun demo() do\n    let values: List<Int32, 3> = List<Int32, 3>()\nend",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) stderr = %#v, want success", source, result.Stderr)
		}
	}
	for _, testCase := range []struct{ source, want string }{
		{"let value: Int32 = [1, 2]", "an inline List literal requires an expected List<T, N> destination type"},
		{"let empty = []", "an inline List literal requires an expected List<T, N> destination type"},
		{"let too_many: List<Int32, 2> = [1, 2, 3]", "List<T, N> literal has 3 elements but capacity is 2"},
		{"let wrong: List<Int32, 2> = [1, true]", "expected Int32 initializer; got Bool"},
		{"type Bad is List<Int32, 0>", "List capacity must be a positive integer literal"},
		{"fun demo() do\n    let bad: List<Int32, 2> = List<Int32, 2>(1)\nend", "List<T, N>() takes no arguments; use a contextual [value, ...] literal"},
	} {
		result := compileSource(testCase.source)
		matched := false
		for _, diagnostic := range result.Stderr {
			matched = matched || strings.Contains(diagnostic, testCase.want)
		}
		if result.ExitCode != compiler.ExitFailure || !matched {
			t.Errorf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestInlineListMutationRequiresWritableStorage(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"fun demo() do\n    let values: List<Int32, 2> = [1]\n    values.push(2)\nend", "inline List mutation requires a writable receiver"},
		{"fun demo(h: Heap) do\n    let values: List<Int32, 2> = [1]\n    values.free(h)\nend", "inline List storage cannot be freed"},
		{"fun demo() do\n    let values: List<Int32, 2> = [1]\n    let bad: Int32 = values[2]\nend", "out of bounds"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Errorf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}
