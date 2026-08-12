package compiler

// Transparent type aliases, canonical lowering, and type-name resolution. Spec 0005.

import (
	"strings"
	"testing"
)

func TestAliasesLowerCanonically(t *testing.T) {
	result := Compile("type Coordinate = Int32 type CoordinatePtr = Ptr<Coordinate> mut value: Coordinate = 1 pointer: CoordinatePtr = ref value read: Coordinate = pointer.value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"int32_t hex_v_value = 1;",
		"const int32_t *const hex_v_pointer = &hex_v_value;",
		"const int32_t hex_v_read = *hex_v_pointer;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
	if strings.Contains(result.MainC, "typedef") || strings.Contains(result.MainC, "Coordinate") {
		t.Fatalf("alias spelling leaked into generated C: %q", result.MainC)
	}
}

func TestNestedPointerAliasesLowerCanonically(t *testing.T) {
	result := Compile("type Pointer = MutPtr<Int32> type PointerPointer = Ptr<Pointer> mut value: Int32 = 1 mut pointer: Pointer = ref value pointerPointer: PointerPointer = ref pointer read: Int32 = pointerPointer.value.value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"int32_t *hex_v_pointer = &hex_v_value;",
		"int32_t *const *const hex_v_pointerPointer = &hex_v_pointer;",
		"const int32_t hex_v_read = *(*hex_v_pointerPointer);",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestTypeOnlyProgram(t *testing.T) {
	result := Compile("type Coordinate = Int32 type CoordinatePtr = Ptr<Coordinate>")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful type-only program", result)
	}
	want := "#include \"main.h\"\n\nint main(void) {\n    return EXIT_SUCCESS;\n}\n"
	if result.MainC != want {
		t.Fatalf("main.c = %q, want empty executable program", result.MainC)
	}
}

func TestRejectsAliasResolutionErrors(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"type Distance = Coordinate", "[Type Error] unknown type Coordinate at 1:17"},
		{"type Coordinate = Coordinate", "[Type Error] type alias Coordinate cannot reference itself at 1:6"},
		{"type Coordinate = Ptr<Coordinate>", "[Type Error] type alias Coordinate cannot reference itself at 1:6"},
		{"type Int32 = UInt32", "[Type Error] built-in type Int32 cannot be redeclared at 1:6"},
		{"type Ptr = UInt64", "[Type Error] built-in type constructor Ptr cannot be redeclared at 1:6"},
		{"type MutPtr = UInt64", "[Type Error] built-in type constructor MutPtr cannot be redeclared at 1:6"},
		{"Ptr: Int32 = 1", "[Type Error] built-in type constructor Ptr cannot be redeclared at 1:1"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Fatalf("Compile(%q) stderr = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestRejectsTypeValueCollisions(t *testing.T) {
	for _, source := range []string{
		"type Coordinate = Int32 Coordinate: Int32 = 1",
		"distance: Int32 = 1 type distance = Int32",
		"Int32: UInt32 = 1",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "already declared") {
			t.Fatalf("Compile(%q) stderr = %#v, want declaration collision", source, result.Stderr)
		}
	}
}

func TestTypeEnvironmentDoesNotLeakAcrossCompilations(t *testing.T) {
	first := Compile("type Pointer = MutPtr<Int32> mut value: Int32 = 1 pointer: Pointer = ref value")
	second := Compile("type Pointer = MutPtr<Bool> mut value: Bool = true pointer: Pointer = ref value")
	if first.ExitCode != ExitSuccess || second.ExitCode != ExitSuccess {
		t.Fatalf("compilations failed: first=%#v second=%#v", first, second)
	}
	if !strings.Contains(first.MainC, "int32_t *const hex_v_pointer") || !strings.Contains(second.MainC, "bool *const hex_v_pointer") {
		t.Fatalf("pointer type leaked across compilations: first=%q second=%q", first.MainC, second.MainC)
	}
}

func TestRejectsUnknownType(t *testing.T) {
	result := Compile("x: Bogus = 13")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	wantErrors := []string{"[Type Error] unknown type Bogus at 1:4"}
	if len(result.Stderr) != len(wantErrors) || result.Stderr[0] != wantErrors[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, wantErrors)
	}
	if result.MainC != "#include \"main.h\"\n\nint main(void) {\n    return EXIT_FAILURE;\n}\n" {
		t.Fatalf("failure main.c = %q", result.MainC)
	}
}

func TestRejectsUnknownNamedType(t *testing.T) {
	result := Compile("x: yyy = 1")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	want := []string{"[Type Error] unknown type yyy at 1:4"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}
