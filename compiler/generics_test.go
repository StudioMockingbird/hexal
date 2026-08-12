package compiler

import (
	"strings"
	"testing"
)

func TestGenericAliasSpecializesTransparently(t *testing.T) {
	result := Compile("type Pointer<T> = Ptr<T> mut value: Int32 = 1 pointer: Pointer<Int32> = ref value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "const int32_t *") {
		t.Fatalf("generated C = %q, want transparent pointer specialization", result.MainC)
	}
}

func TestGenericObjectSpecializesWithSubstitutedMembers(t *testing.T) {
	result := Compile("type Box<T> = { value: T } box: Box<Int32> = Box<Int32> { value = 42 }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "typedef struct hex_t_Box_Int32_ hex_t_Box_Int32_;") || !strings.Contains(result.MainC, ".hex_m_value = 42") {
		t.Fatalf("generated output = H:%q C:%q, want specialized object", result.MainH, result.MainC)
	}
}

func TestGenericFunctionCallAndExplicitArguments(t *testing.T) {
	result := Compile("fun identity<T>(value: T): T\nreturn value\nend first: Int32 = identity(42) second: Int64 = identity<Int64>(42)")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if strings.Count(result.MainC, "hex_f_identity_Int32") < 2 || strings.Count(result.MainC, "hex_f_identity_Int64") < 2 {
		t.Fatalf("generated C = %q, want two specialized definitions with prototypes", result.MainC)
	}
}

func TestGenericMethodWithReceiverAndMethodArguments(t *testing.T) {
	result := Compile("type Box<T> = { value: T }\nimpl Box<T>.get(): T\nreturn self.value\nend box: Box<Int32> = Box<Int32> { value = 42 }\nvalue: Int32 = box.get()")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_f_Box_Int32__get") {
		t.Fatalf("generated C = %q, want specialized method", result.MainC)
	}
}

func TestGenericFunctionValueReferenceInfersFromFunTarget(t *testing.T) {
	result := Compile("fun identity<T>(value: T): T\nreturn value\nend callback: Fun<(Int32) : Int32> = identity")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_f_identity_Int32") {
		t.Fatalf("generated C = %q, want inferred function reference", result.MainC)
	}
}

func TestGenericObjectLiteralInfersFromExpectedType(t *testing.T) {
	result := Compile("type Box<T> = { value: T } box: Box<Int32> = Box { value = 42 }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "hex_t_Box_Int32_") {
		t.Fatalf("generated header = %q, want expected-type inference", result.MainH)
	}
}

func TestGenericNestedSpecializationsReuseOneCName(t *testing.T) {
	result := Compile("fun identity<T>(value: T): T\nreturn value\nend first: Int32 = identity(1) second: Int32 = identity(2)")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if strings.Count(result.MainC, "static int32_t hex_f_identity_Int32(const int32_t hex_v_value) {") != 1 {
		t.Fatalf("generated C = %q, want one specialized definition", result.MainC)
	}
}

func TestGenericPointerIndirectedRecursionIsFinite(t *testing.T) {
	result := Compile("type Link<T> = { value: T, mut next: MutPtr<Link<T>> | Nil, } link: Link<Int32> = Link<Int32> { value = 1, next = nil }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "hex_t_Link_Int32_") {
		t.Fatalf("generated header = %q, want recursive specialized object", result.MainH)
	}
}

func TestGenericRecursiveSpecializationChangesArgumentsRejected(t *testing.T) {
	result := Compile("fun expand<T>(value: T): T\nreturn expand<Ptr<T>>(value)\nend bad: Int32 = expand(1)")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "recursive specialization changes generic arguments") {
		t.Fatalf("diagnostics = %#v, want recursive-specialization error", result.Stderr)
	}
}

func TestGenericUnusedDeclarationEmitsNoC(t *testing.T) {
	result := Compile("fun identity<T>(value: T): T\nreturn value\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if strings.Contains(result.MainC, "identity") {
		t.Fatalf("generated C = %q, want no unused generic body", result.MainC)
	}
}

func TestGenericSpecializationTimeOperationDiagnostic(t *testing.T) {
	result := Compile("fun maximum<T>(left: T, right: T): T\nif left > right\nreturn left\nelse\nreturn right\nend\nend bad: Bool = maximum(true, false)")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ordering is unavailable for Bool values") {
		t.Fatalf("diagnostics = %#v, want specialization-time operation error", result.Stderr)
	}
}

func TestGenericArityAndInferenceDiagnostics(t *testing.T) {
	result := Compile("fun identity<T>(value: T): T\nreturn value\nend bad: Int32 = identity<Int32, Bool>(42)")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "explicit generic argument count does not match declaration") {
		t.Fatalf("diagnostics = %#v, want arity error", result.Stderr)
	}
	result = Compile("fun same<T>(left: T, right: T): Bool\nreturn left == right\nend bad: Bool = same(1, true)")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "conflicting inferred types for generic parameter T") {
		t.Fatalf("diagnostics = %#v, want inference conflict", result.Stderr)
	}
}
