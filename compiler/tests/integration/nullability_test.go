package integration

// Explicit nullability: Nil/nil lowering, the null niche for pointer-like
// unions, null tests, branch narrowing, and the erased Unknown pointee.
// Spec 0010.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestNilValueAndBindingLowerToNullptr(t *testing.T) {
	// RFC 0048: standalone `nothing: Nil = nil` is invalid; null-pointer
	// lowering survives through the nullable union form.
	assertRejects(t, "nothing: Nil = nil", "Nil is valid only as a member of a union with a non-Nil type")
	result := compileSource("maybe: Ptr<Int32> | Nil = nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"#include <stddef.h>",
		"const int32_t *const hex_v_maybe = nullptr;",
	} {
		if !strings.Contains(rootH(t, result), want) && !strings.Contains(rootC(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootH(t, result), rootC(t, result), want)
		}
	}
}

func TestNullablePointerUsesTheNullNiche(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = nil present: Ptr<Int32> | Nil = ref value")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const int32_t *const hex_v_maybe = nullptr;",
		"const int32_t *const hex_v_present = &hex_v_value;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

// RFC 0049 item 8.7: the pointer-plus-Nil null niche applies only to
// pointers. Handle types like String have no null representation, so their
// unions lower to tagged unions instead.
func TestNullableHandleUnionDoesNotUseTheNullNiche(t *testing.T) {
	result := compileSource("text: String | Nil = nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_union_10_hex_string9_nullptr_t hex_v_text",
		".tag = hex_union_10_hex_string9_nullptr_t_tag_member_1",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	if strings.Contains(rootC(t, result), " == nullptr") || strings.Contains(rootC(t, result), " != nullptr") || strings.Contains(rootC(t, result), "(nullptr)") {
		t.Fatalf("handle union must not lower through the null niche:\n%s", rootC(t, result))
	}
}

func TestNullTestsLowerToNullPointerComparison(t *testing.T) {
	result := compileSource("mut maybe: Ptr<Int32> | Nil = nil equal: Bool = maybe == nil notEqual: Bool = maybe != nil commuted: Bool = nil == maybe")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const bool hex_v_equal = hex_v_maybe == nullptr;",
		"const bool hex_v_notEqual = hex_v_maybe != nullptr;",
		"const bool hex_v_commuted = hex_v_maybe == nullptr;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestNullTestAsConditionNarrowsReads(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil then result: Int32 = maybe.value end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"if (hex_v_maybe != nullptr) {",
		"const int32_t hex_v_result = *hex_v_maybe;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestNullableAssignmentStoresNullAndPointer(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 other: Int32 = 2 mut maybe: Ptr<Int32> | Nil = nil maybe = ref value maybe = nil maybe = ref other")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"int32_t *hex_v_maybe = nullptr;",
		"hex_v_maybe = &hex_v_value;",
		"hex_v_maybe = nullptr;",
		"hex_v_maybe = &hex_v_other;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestNullableObjectMemberUsesNullNiche(t *testing.T) {
	result := compileSource("type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } mut tail: Node = Node { value = 3, next = nil, }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_t_m3_app_Node *hex_m_next;",
		".hex_m_next = nullptr,",
	} {
		if !strings.Contains(rootH(t, result), want) && !strings.Contains(rootC(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootH(t, result), rootC(t, result), want)
		}
	}
}

func TestNullableFunctionResultReturnsNullptr(t *testing.T) {
	result := compileSource("fun absent(): MutPtr<Int32> | Nil do\n    return nil\nend\nnothing: MutPtr<Int32> | Nil = absent()")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static int32_t * hex_f_m3_app_absent(void) {",
		"return nullptr;",
		"int32_t *const hex_v_nothing = hex_f_m3_app_absent();",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestErasedUnknownPointersLowerToVoidPointers(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 reader: Ptr<Int32> = ref value erased: Ptr<Unknown> = reader restored: Ptr<Int32> = erased maybe_erased: MutPtr<Unknown> | Nil = nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const void *const hex_v_erased = hex_v_reader;",
		"const int32_t *const hex_v_restored = hex_v_erased;",
		"void *const hex_v_maybe_erased = nullptr;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestStddefIncludedOnlyWhenNullUsed(t *testing.T) {
	withNull := compileSource("mut maybe: Ptr<Int32> | Nil = nil if maybe != nil then noop: Int32 = 0 end")
	if withNull.ExitCode != compiler.ExitSuccess || !strings.Contains(hexalH(t, withNull), "#include <stddef.h>") {
		t.Fatalf("null-using program = %#v, want <stddef.h>", withNull)
	}

	withoutNull := compileSource("mut value: Int32 = 1 reader: Ptr<Int32> = ref value")
	if withoutNull.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", withoutNull.ExitCode, withoutNull.Stderr, compiler.ExitSuccess)
	}
	if strings.Contains(rootC(t, withoutNull), "#include <stddef.h>") || strings.Contains(rootH(t, withoutNull), "#include <stddef.h>") {
		t.Fatalf("null-free program must not include <stddef.h>: C=%q H=%q", rootC(t, withoutNull), rootH(t, withoutNull))
	}
}

func TestNullabilityDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"mut value: Int32 = 1 bad: Ptr<Int32> = nil", "nil requires an expected union containing Nil"},
		{"mut value: Int32 = 1 node: MutPtr<Int32> = ref value bad: Bool = node == nil", "nil requires an expected union containing Nil"},
		{"maybe: Ptr<Int32> | Nil = nil bad: Int32 = maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value"},
		{"mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil then bad: Int32 = maybe.value end", ""},
	} {
		result := compileSource(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile(%q) = %#v, want success", testCase.source, result.Stderr)
			}
			continue
		}
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

// RFC 0049 item 8.1 regression at the integration level: standalone Nil is
// rejected through generic substitution and spawn arguments.
func TestNilRejectedThroughSubstitution(t *testing.T) {
	assertRejects(t, "type Box<T> = { value: T }\nbad: Box<Nil> = Box<Nil> { value = nil }\n", "Nil is valid only as a member of a union with a non-Nil type")
	assertRejects(t, "fun worker(flag: Nil): Bool do\n    return true\nend\nfun f(h: Heap): Int32 | Error do\n    task: Task<Bool> = try spawn worker(nil)\n    return 0\nend\n", "Nil is valid only as a member of a union with a non-Nil type")
}

// A branch-established narrowing survives on the sole continuing path when
// every alternative terminates with return, break, or continue.
func TestSoleContinuingPathNarrowing(t *testing.T) {
	assertCompiles(t, "fun f(): Int32 do\n    mut maybe: Ptr<Int32> | Nil = nil\n    if maybe == nil then\n        return 0\n    end\n    return maybe.value\nend\n")
	assertCompiles(t, "fun f(): Int32 do\n    mut maybe: Ptr<Int32> | Nil = nil\n    while true do\n        if maybe == nil then\n            break\n        end\n        return maybe.value\n    end\n    return 0\nend\n")
	assertRejects(t, "fun f(): Int32 do\n    mut maybe: Ptr<Int32> | Nil = nil\n    if maybe != nil then\n        print(maybe.value)\n    end\n    return maybe.value\nend\n", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}
