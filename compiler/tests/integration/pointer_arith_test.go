package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// No source-level pointer arithmetic. Pointers refer to and dereference one
// typed object; bounds-carrying types own sequence access.

func TestPointerArithmeticRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"plus count", "fun demo() do\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Ptr<Int32> = pointer + 1\nend", "operator + requires numeric operands"},
		{"count plus", "fun demo() do\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Ptr<Int32> = 1 + pointer\nend", "operator + requires numeric operands"},
		{"minus count", "fun demo() do\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Ptr<Int32> = pointer - 1\nend", "operator - requires numeric operands"},
		{"distance", "fun demo() do\n    value: Int32 = 1\n    other: Int32 = 2\n    left: Ptr<Int32> = ref value\n    right: Ptr<Int32> = ref other\n    bad: Int32 = left - right\nend", "operator - requires numeric operands"},
		{"mut pointer plus", "fun demo() do\n    mut value: Int32 = 1\n    pointer: MutPtr<Int32> = ref value\n    bad: MutPtr<Int32> = pointer + 1\nend", "operator + requires numeric operands"},
		{"alias plus", "type Handle = Ptr<Int32>\nfun demo() do\n    value: Int32 = 1\n    pointer: Handle = ref value\n    bad: Handle = pointer + 1\nend", "operator + requires numeric operands"},
		{"nested pointer", "type Node = { next: Ptr<Node>, }\nfun demo(node: Ptr<Node>) do\n    bad: Ptr<Node> = node.value.next + 1\nend", "operator + requires numeric operands"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestPointerOrderingRejected(t *testing.T) {
	for _, operator := range []string{"<", "<=", ">", ">="} {
		source := "fun demo() do\n    value: Int32 = 1\n    other: Int32 = 2\n    left: Ptr<Int32> = ref value\n    right: Ptr<Int32> = ref other\n    bad: Bool = left " + operator + " right\nend"
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ordering is unavailable for Ptr<Int32>") {
			t.Fatalf("Compile(%q) stderr = %#v, want ordering rejection", source, result.Stderr)
		}
	}
}

func TestPointerIndexingRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"read", "fun demo() do\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Int32 = pointer[0]\nend"},
		{"write", "fun demo() do\n    mut value: Int32 = 1\n    pointer: MutPtr<Int32> = ref value\n    pointer[0] = 5\nend"},
		{"nullable narrowed", "fun demo() do\n    value: Int32 = 1\n    mut pointer: Ptr<Int32> | Nil = ref value\n    if pointer != nil then\n        bad: Int32 = pointer[0]\n    end\nend"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot index") {
				t.Fatalf("Compile(%q) stderr = %#v, want indexing rejection", testCase.source, result.Stderr)
			}
		})
	}
}

func TestPointerDereferenceThenCheckedIndexIsValid(t *testing.T) {
	result := compileSource("fun demo() do\n    mut values: Array<Int32, 4> = [10, 20, 30, 40]\n    array_pointer: MutPtr<Array<Int32, 4>> = ref values\n    item: Int32 = array_pointer.value[2]\n    element: MutPtr<Int32> = ref values[2]\n    copy: Int32 = element.value\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestPointerIdentityEqualityRemainsValid(t *testing.T) {
	result := compileSource("fun demo() do\n    value: Int32 = 1\n    other: Int32 = 2\n    left: Ptr<Int32> = ref value\n    right: Ptr<Int32> = ref other\n    same: Bool = left == right\n    different: Bool = left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestPointerIntegerConversionsRejected(t *testing.T) {
	result := compileSource("fun demo() do\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: UInt64 = pointer\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected UInt64 initializer, got Ptr<Int32>") {
		t.Fatalf("Compile stderr = %#v, want pointer-to-integer rejection", result.Stderr)
	}
	result = compileSource("fun demo() do\n    address: UInt64 = 42\n    bad: Ptr<Int32> = address\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected Ptr<Int32> initializer, got UInt64") {
		t.Fatalf("Compile stderr = %#v, want integer-to-pointer rejection", result.Stderr)
	}
}

func TestPointerCompoundAssignmentsAreSyntaxErrors(t *testing.T) {
	for _, source := range []string{
		"fun demo()\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    pointer += 1\nend",
		"fun demo()\n    pointer: Ptr<Int32> = nil\n    pointer++\nend",
		"fun demo()\n    count: Int32 = 1\n    count++\nend",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("Compile(%q) stderr = %#v, want syntax error", source, result.Stderr)
		}
	}
}

func TestPointerUnknownErasureAddsNoCapability(t *testing.T) {
	result := compileSource("fun demo() do\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    erased: Ptr<Unknown> = pointer\n    bad: Int32 = erased[0]\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot index") {
		t.Fatalf("Compile stderr = %#v, want indexing rejection on erased pointer", result.Stderr)
	}
}
