package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestGenericAliasSpecializesTransparently(t *testing.T) {
	result := compileSource("type Pointer<T> = Ptr<T> mut value: Int32 = 1 pointer: Pointer<Int32> = ref value")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "const int32_t *") {
		t.Fatalf("generated C = %q, want transparent pointer specialization", rootC(t, result))
	}
}

func TestGenericObjectSpecializesWithSubstitutedMembers(t *testing.T) {
	result := compileSource("type Box<T> = { value: T } box: Box<Int32> = Box<Int32> { value = 42 }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "typedef struct hex_t_m3_app_Box_Int32_ hex_t_m3_app_Box_Int32_;") || !strings.Contains(rootC(t, result), ".hex_m_value = 42") {
		t.Fatalf("generated output = H:%q C:%q, want specialized object", rootH(t, result), rootC(t, result))
	}
}

func TestGenericFunctionCallAndExplicitArguments(t *testing.T) {
	result := compileSource("fun identity<T>(value: T): T do\nreturn value\nend first: Int32 = identity(42) second: Int64 = identity<Int64>(42)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Count(rootC(t, result), "hex_f_m3_app_identity_Int32") < 2 || strings.Count(rootC(t, result), "hex_f_m3_app_identity_Int64") < 2 {
		t.Fatalf("generated C = %q, want two specialized definitions with prototypes", rootC(t, result))
	}
}

func TestGenericMethodWithReceiverAndMethodArguments(t *testing.T) {
	result := compileSource("type Box<T> = { value: T }\nimpl Box<T>.get(): T do\nreturn self.value\nend box: Box<Int32> = Box<Int32> { value = 42 }\nvalue: Int32 = box.get()")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_Box_Int32__get") {
		t.Fatalf("generated C = %q, want specialized method", rootC(t, result))
	}
}

func TestGenericFunctionValueReferenceInfersFromFunTarget(t *testing.T) {
	result := compileSource("fun identity<T>(value: T): T do\nreturn value\nend callback: Fun<(Int32) : Int32> = identity")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_identity_Int32") {
		t.Fatalf("generated C = %q, want inferred function reference", rootC(t, result))
	}
}

func TestGenericObjectLiteralInfersFromExpectedType(t *testing.T) {
	result := compileSource("type Box<T> = { value: T } box: Box<Int32> = Box { value = 42 }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_t_m3_app_Box_Int32_") {
		t.Fatalf("generated header = %q, want expected-type inference", rootH(t, result))
	}
}

func TestGenericNestedSpecializationsReuseOneCName(t *testing.T) {
	result := compileSource("fun identity<T>(value: T): T do\nreturn value\nend first: Int32 = identity(1) second: Int32 = identity(2)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Count(rootC(t, result), "static int32_t hex_f_m3_app_identity_Int32(const int32_t hex_v_value) {") != 1 {
		t.Fatalf("generated C = %q, want one specialized definition", rootC(t, result))
	}
}

func TestGenericPointerIndirectedRecursionIsFinite(t *testing.T) {
	result := compileSource("type Link<T> = { value: T, mut next: MutPtr<Link<T>> | Nil, } link: Link<Int32> = Link<Int32> { value = 1, next = nil }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_t_m3_app_Link_Int32_") {
		t.Fatalf("generated header = %q, want recursive specialized object", rootH(t, result))
	}
}

func TestGenericRecursiveSpecializationChangesArgumentsRejected(t *testing.T) {
	result := compileSource("fun expand<T>(value: T): T do\nreturn expand<Ptr<T>>(value)\nend bad: Int32 = expand(1)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "recursive specialization changes generic arguments") {
		t.Fatalf("diagnostics = %#v, want recursive-specialization error", result.Stderr)
	}
}

func TestGenericUnusedDeclarationEmitsNoC(t *testing.T) {
	result := compileSource("fun identity<T>(value: T): T do\nreturn value\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Contains(rootC(t, result), "identity") {
		t.Fatalf("generated C = %q, want no unused generic body", rootC(t, result))
	}
}

func TestGenericSpecializationTimeOperationDiagnostic(t *testing.T) {
	result := compileSource("fun maximum<T>(left: T, right: T): T do\nif left > right then\nreturn left\nelse\nreturn right\nend\nend bad: Bool = maximum(true, false)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ordering is unavailable for Bool values") {
		t.Fatalf("diagnostics = %#v, want specialization-time operation error", result.Stderr)
	}
}

func TestGenericArityAndInferenceDiagnostics(t *testing.T) {
	result := compileSource("fun identity<T>(value: T): T do\nreturn value\nend bad: Int32 = identity<Int32, Bool>(42)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "explicit generic argument count does not match declaration") {
		t.Fatalf("diagnostics = %#v, want arity error", result.Stderr)
	}
	result = compileSource("fun same<T>(left: T, right: T): Bool do\nreturn left == right\nend bad: Bool = same(1, true)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "conflicting inferred types for generic parameter T") {
		t.Fatalf("diagnostics = %#v, want inference conflict", result.Stderr)
	}
}

// Generic bodies containing for, errdefer, defer, while, break, and continue
// pass generator preflight for every specialization: the validation walk must
// not fail closed on statement shapes the checker accepts.
func TestGenericBodiesWithControlFlowSpecialize(t *testing.T) {
	source := "fun cleanup(v: Int32) do\nend\nfun sweep<T>(values: List<T>): Int32 | Error do\n    errdefer cleanup(9)\n    defer cleanup(8)\n    mut total: Int32 = 0\n    for value in values do\n        while total < 10 do\n            total = total + 1\n            if total > 100 then\n                break\n            end\n            continue\n        end\n    end\n    return total\nend\nfun demo(h: Heap): Int32 | Error do\n    ints: List<Int32> = List<Int32>.new(h)\n    defer ints.free(h)\n    ints.push(1)\n    a: Int32 = try sweep<Int32>(ints)\n    strands: List<Strand> = List<Strand>.new(h)\n    defer strands.free(h)\n    strands.push(\"s\")\n    b: Int32 = try sweep<Strand>(strands)\n    return a + b\nend\n"
	result := assertCompiles(t, source)
	if strings.Count(rootC(t, result), "hex_f_m3_app_sweep_Int32") < 1 || strings.Count(rootC(t, result), "hex_f_m3_app_sweep_Strand") < 1 {
		t.Fatalf("modules/app.c = %q, want both specializations", rootC(t, result))
	}
}
