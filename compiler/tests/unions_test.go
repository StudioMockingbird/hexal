package tests

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
	if !strings.Contains(result.MainH, "typedef enum hex_internal_union_1_tag") || !strings.Contains(result.MainC, "hex_v_value") {
		t.Fatalf("generated union output = H:%q C:%q", result.MainH, result.MainC)
	}
}

func TestUnionContextUsesWrittenCandidateOrder(t *testing.T) {
	result := compileSource("first: UInt8 | UInt16 = 7 second: Int64 | Int32 = 7")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected candidate-order source: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "hex_internal_union_1_tag_member_0") || !strings.Contains(result.MainC, "hex_internal_union_2_tag_member_1") {
		t.Fatalf("candidate tags missing from generated C: %q", result.MainC)
	}
}

func TestUnionWideningPreservesSourceEvaluation(t *testing.T) {
	result := compileSource("small: Int32 | Bool = true wide: Int32 | Bool | Nil = small")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected widening source: %v", result.Stderr)
	}
	if !strings.Contains(result.MainH, "hex_internal_widen_") || strings.Count(result.MainC, "hex_internal_widen_") != 1 {
		t.Fatalf("widening helper output = H:%q C:%q", result.MainH, result.MainC)
	}
}

func TestUnionIsNarrowsIfElseAndWhile(t *testing.T) {
	// RFC 0048: the else arm narrows value to Nil, which is printable but
	// cannot initialize a standalone Nil binding.
	result := compileSource("value: Int32 | Float64 | Nil = 1 if value is Int32 integer: Int32 = value elseif value != nil floating: Float64 = value else print(value) end mut state: Int32 | Float64 = 1 while state is Int32 do current: Int32 = state state = 2 end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected flow narrowing source: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, ".payload.member_") {
		t.Fatalf("narrowed payload missing from generated C: %q", result.MainC)
	}
}

func TestUnionNullTestsAndTruthiness(t *testing.T) {
	result := compileSource("value: Int32 | Bool | Nil = true present: Bool = value != nil if value end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected Nil/truthiness source: %v", result.Stderr)
	}
	if !strings.Contains(result.MainH, "#include <stddef.h>") || !strings.Contains(result.MainC, "_truthy") || !strings.Contains(result.MainC, ".tag !=") {
		t.Fatalf("null/truthiness output = H:%q C:%q", result.MainH, result.MainC)
	}
}

func TestUnionEqualityUsesTagsAndPayloads(t *testing.T) {
	result := compileSource("left: Int32 | Bool = true right: Bool | Int32 = false same: Bool = left == right")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected union equality source: %v", result.Stderr)
	}
	if !strings.Contains(result.MainH, "_equal(") || !strings.Contains(result.MainC, "_equal(") {
		t.Fatalf("union equality helper missing: H:%q C:%q", result.MainH, result.MainC)
	}
}

func TestNullablePointerUnionKeepsNullNiche(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nullable pointer source: %v", result.Stderr)
	}
	if strings.Contains(result.MainH, "hex_internal_union_") || !strings.Contains(result.MainC, "nullptr") {
		t.Fatalf("nullable pointer was tagged: H:%q C:%q", result.MainH, result.MainC)
	}
}

func TestUnionNestedPointerAndFunctionPositions(t *testing.T) {
	result := compileSource("fun identity(value: Int32 | Bool): Int32 | Bool return value end mut value: Int32 | Bool = true slot: MutPtr<Int32 | Bool> = ref value result: Int32 | Bool = identity(value)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nested/function union source: %v", result.Stderr)
	}
	if !strings.Contains(result.MainH, "hex_internal_union_") || !strings.Contains(result.MainC, "hex_f_identity") {
		t.Fatalf("nested/function union output = H:%q C:%q", result.MainH, result.MainC)
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
	if first.ExitCode != compiler.ExitSuccess || second.ExitCode != compiler.ExitSuccess || first.MainC != second.MainC || first.MainH != second.MainH {
		t.Fatalf("repeated union output differs: first=%q/%q second=%q/%q", first.MainC, first.MainH, second.MainC, second.MainH)
	}
}
