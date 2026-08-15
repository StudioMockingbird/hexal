package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestUnionAliasesNormalizeAndInject(t *testing.T) {
	result := compileSource("type Number = Int32 | Float64 mut value: Number = 1 value = 2.5")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "typedef enum hex_union_7_int32_t6_double_tag") || !strings.Contains(rootC(t, result), "hex_v_value") {
		t.Fatalf("generated union output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionContextUsesWrittenCandidateOrder(t *testing.T) {
	result := compileSource("first: UInt8 | UInt16 = 7 second: Int64 | Int32 = 7")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected candidate-order source: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_union_7_uint8_t8_uint16_t_tag_member_0") || !strings.Contains(rootC(t, result), "hex_union_7_int32_t7_int64_t_tag_member_1") {
		t.Fatalf("candidate tags missing from generated C: %q", rootC(t, result))
	}
}

func TestUnionWideningPreservesSourceEvaluation(t *testing.T) {
	result := compileSource("small: Int32 | Bool = true wide: Int32 | Bool | Nil = small")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected widening source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "hex_internal_widen_") || strings.Count(rootC(t, result), "hex_internal_widen_") != 1 {
		t.Fatalf("widening helper output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionIsNarrowsIfElseAndWhile(t *testing.T) {
	// RFC 0048: the else arm narrows value to Nil, which is printable but
	// cannot initialize a standalone Nil binding.
	result := compileSource("value: Int32 | Float64 | Nil = 1 if value is Int32 then integer: Int32 = value elseif value != nil then floating: Float64 = value else print(value) end mut state: Int32 | Float64 = 1 while state is Int32 do current: Int32 = state state = 2 end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected flow narrowing source: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), ".payload.member_") {
		t.Fatalf("narrowed payload missing from generated C: %q", rootC(t, result))
	}
}

func TestUnionNullTestsAndTruthiness(t *testing.T) {
	result := compileSource("value: Int32 | Bool | Nil = true present: Bool = value != nil if value then end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected Nil/truthiness source: %v", result.Stderr)
	}
	if !strings.Contains(hexalH(t, result), "#include <stddef.h>") || !strings.Contains(rootH(t, result), "_truthy") || !strings.Contains(rootC(t, result), ".tag !=") {
		t.Fatalf("null/truthiness output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionEqualityUsesTagsAndPayloads(t *testing.T) {
	result := compileSource("left: Int32 | Bool = true right: Bool | Int32 = false same: Bool = left == right")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected union equality source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "_equal(") || !strings.Contains(rootC(t, result), "_equal(") {
		t.Fatalf("union equality helper missing: H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestNullablePointerUnionKeepsNullNiche(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil then result: Int32 = maybe.value end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nullable pointer source: %v", result.Stderr)
	}
	if strings.Contains(rootH(t, result), "hex_union_") || !strings.Contains(rootC(t, result), "nullptr") {
		t.Fatalf("nullable pointer was tagged: H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionNestedPointerAndFunctionPositions(t *testing.T) {
	result := compileSource("fun identity(value: Int32 | Bool): Int32 | Bool do return value end mut value: Int32 | Bool = true slot: MutPtr<Int32 | Bool> = ref value result: Int32 | Bool = identity(value)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nested/function union source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "hex_union_") || !strings.Contains(rootC(t, result), "hex_f_m3_app_identity") {
		t.Fatalf("nested/function union output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionDiagnosticsFailAtTheEarliestPhase(t *testing.T) {
	result := compileSource("value: UInt8 | UInt16 = missing + 1")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "unknown variable missing") {
		t.Fatalf("diagnostics = %#v, want earliest unknown-variable error", result.Stderr)
	}
}

func TestGeneratedUnionNamesAreDeterministic(t *testing.T) {
	source := "first: Int32 | Float64 = 1 second: Bool | Int32 = true"
	first := compileSource(source)
	second := compileSource(source)
	if first.ExitCode != compiler.ExitSuccess || second.ExitCode != compiler.ExitSuccess || rootC(t, first) != rootC(t, second) || rootH(t, first) != rootH(t, second) {
		t.Fatalf("repeated union output differs: first=%q/%q second=%q/%q", rootC(t, first), rootH(t, first), rootC(t, second), rootH(t, second))
	}
}
