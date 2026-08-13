package compiler

// Explicit nullability: Nil/nil lowering, the null niche for pointer-like
// unions, null tests, branch narrowing, and the erased Unknown pointee.
// Spec 0010.

import (
	"strings"
	"testing"
)

func TestNilValueAndBindingLowerToNullptr(t *testing.T) {
	// RFC 0048: standalone `nothing: Nil = nil` is invalid; null-pointer
	// lowering survives through the nullable union form.
	result := Compile("maybe: Ptr<Int32> | Nil = nil")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"#include <stddef.h>",
		"const int32_t *const hex_v_maybe = nullptr;",
	} {
		if !strings.Contains(result.MainH, want) && !strings.Contains(result.MainC, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainH, result.MainC, want)
		}
	}
}

func TestNullablePointerUsesTheNullNiche(t *testing.T) {
	result := Compile("mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = nil present: Ptr<Int32> | Nil = ref value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const int32_t *const hex_v_maybe = nullptr;",
		"const int32_t *const hex_v_present = &hex_v_value;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

// RFC 0049 item 8.7: the pointer-plus-Nil null niche applies only to
// pointers. Handle types like String have no null representation, so their
// unions lower to tagged unions instead.
func TestNullableHandleUnionDoesNotUseTheNullNiche(t *testing.T) {
	result := Compile("text: String | Nil = nil")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const hex_internal_union_1 hex_v_text",
		".tag = hex_internal_union_1_tag_member_1",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
	if strings.Contains(result.MainC, "nullptr") {
		t.Fatalf("handle union must not lower through the null niche:\n%s", result.MainC)
	}
}

func TestNullTestsLowerToNullPointerComparison(t *testing.T) {
	result := Compile("mut maybe: Ptr<Int32> | Nil = nil equal: Bool = maybe == nil notEqual: Bool = maybe != nil commuted: Bool = nil == maybe")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const bool hex_v_equal = hex_v_maybe == nullptr;",
		"const bool hex_v_notEqual = hex_v_maybe != nullptr;",
		"const bool hex_v_commuted = hex_v_maybe == nullptr;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestNullTestAsConditionNarrowsReads(t *testing.T) {
	result := Compile("mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value end")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"if (hex_v_maybe != nullptr) {",
		"const int32_t hex_v_result = *hex_v_maybe;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestNullableAssignmentStoresNullAndPointer(t *testing.T) {
	result := Compile("mut value: Int32 = 1 other: Int32 = 2 mut maybe: Ptr<Int32> | Nil = nil maybe = ref value maybe = nil maybe = ref other")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"int32_t *hex_v_maybe = nullptr;",
		"hex_v_maybe = &hex_v_value;",
		"hex_v_maybe = nullptr;",
		"hex_v_maybe = &hex_v_other;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestNullableObjectMemberUsesNullNiche(t *testing.T) {
	result := Compile("type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } mut tail: Node = Node { value = 3, next = nil, }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"hex_t_Node *hex_m_next;",
		".hex_m_next = nullptr,",
	} {
		if !strings.Contains(result.MainH, want) && !strings.Contains(result.MainC, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainH, result.MainC, want)
		}
	}
}

func TestNullableFunctionResultReturnsNullptr(t *testing.T) {
	result := Compile("fun absent(): MutPtr<Int32> | Nil\n    return nil\nend\nnothing: MutPtr<Int32> | Nil = absent()")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static int32_t * hex_f_absent(void) {",
		"return nullptr;",
		"int32_t *const hex_v_nothing = hex_f_absent();",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestErasedUnknownPointersLowerToVoidPointers(t *testing.T) {
	result := Compile("mut value: Int32 = 1 reader: Ptr<Int32> = ref value erased: Ptr<Unknown> = reader restored: Ptr<Int32> = erased maybe_erased: MutPtr<Unknown> | Nil = nil")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const void *const hex_v_erased = hex_v_reader;",
		"const int32_t *const hex_v_restored = hex_v_erased;",
		"void *const hex_v_maybe_erased = nullptr;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestStddefIncludedOnlyWhenNullUsed(t *testing.T) {
	withNull := Compile("mut maybe: Ptr<Int32> | Nil = nil if maybe != nil noop: Int32 = 0 end")
	if withNull.ExitCode != ExitSuccess || !strings.Contains(withNull.MainH, "#include <stddef.h>") {
		t.Fatalf("null-using program = %#v, want <stddef.h>", withNull)
	}

	withoutNull := Compile("mut value: Int32 = 1 reader: Ptr<Int32> = ref value")
	if withoutNull.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", withoutNull.ExitCode, withoutNull.Stderr, ExitSuccess)
	}
	if strings.Contains(withoutNull.MainC, "#include <stddef.h>") || strings.Contains(withoutNull.MainH, "#include <stddef.h>") {
		t.Fatalf("null-free program must not include <stddef.h>: C=%q H=%q", withoutNull.MainC, withoutNull.MainH)
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
		{"mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil bad: Int32 = maybe.value end", ""},
	} {
		result := Compile(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != ExitSuccess {
				t.Fatalf("Compile(%q) = %#v, want success", testCase.source, result.Stderr)
			}
			continue
		}
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}
