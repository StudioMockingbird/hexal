package compiler

import (
	"strings"
	"testing"
)

// RFC 0042 integration tests: size_of<T>()/align_of<T>() layout queries and
// volatile integer pointer accesses.

func TestLayoutQueriesCompile(t *testing.T) {
	source := "type Node = {\n    x: Int32,\n    y: Float64,\n}\nfun layout_demo(): Size\n    a: Size = size_of<Int32>()\n    b: Size = align_of<Node>()\n    c: Size = size_of<String>()\n    d: Size = size_of<Array<UInt8, 4>>()\n    return a + b + c + d\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{
		"(size_t)sizeof(int32_t)",
		"(size_t)alignof(hex_t_Node)",
		"(size_t)sizeof(const hex_string *)",
	} {
		if !strings.Contains(result.MainC, fragment) {
			t.Fatalf("generated C lacks %s:\n%s", fragment, result.MainC)
		}
	}
}

func TestVolatileAccessCompiles(t *testing.T) {
	source := "fun volatile_demo(register: MutPtr<UInt32>, flag: Ptr<Int8>): UInt32\n    register.write_volatile(0x10)\n    status: UInt32 = register.read_volatile()\n    marker: Int8 = flag.read_volatile()\n    return status + marker.to<UInt32>()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{
		"*(volatile uint32_t *)",
		"*(volatile const int8_t *)",
	} {
		if !strings.Contains(result.MainC, fragment) {
			t.Fatalf("generated C lacks %s:\n%s", fragment, result.MainC)
		}
	}
}

func TestVolatileWriteRequiresMutPtr(t *testing.T) {
	source := "fun bad(register: Ptr<UInt32>)\n    register.write_volatile(1)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "read-only") {
		t.Fatalf("want read-only diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

// RFC 0049 item 8.2: write_volatile produces no value, so it is valid as a
// statement and rejected in value positions with the standard diagnostic.
func TestVolatileWriteProducesNoValue(t *testing.T) {
	valid := Compile("mut value: Int32 = 1 slot: MutPtr<Int32> = ref value slot.write_volatile(2)")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("write_volatile as a statement = %v", valid.Stderr)
	}
	invalid := Compile("mut value: Int32 = 1 slot: MutPtr<Int32> = ref value bad: Int32 = slot.write_volatile(2)")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) == 0 || !strings.Contains(strings.Join(invalid.Stderr, "\n"), "write_volatile produces no value") {
		t.Fatalf("write_volatile as an initializer = %#v, want produces-no-value diagnostic", invalid.Stderr)
	}
}

func TestVolatileRejectsNonIntegerElements(t *testing.T) {
	for _, element := range []string{"Float64", "Bool", "Ptr<Int32>"} {
		source := "fun bad(pointer: MutPtr<" + element + ">)\n    value: " + element + " = pointer.read_volatile()\nend\n"
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "volatile access is supported only for integer storage types") {
			t.Fatalf("want volatile eligibility diagnostic for %s, got exit=%d stderr=%v", element, result.ExitCode, result.Stderr)
		}
	}
}

func TestVolatileNullablePointerRequiresNarrowing(t *testing.T) {
	source := "fun bad(pointer: Ptr<UInt32> | Nil)\n    value: UInt32 = pointer.read_volatile()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("want nullable diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestLayoutQueriesRejectIneligibleTypes(t *testing.T) {
	source := "fun bad(): Size\n    return size_of<Unknown>()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "complete finite-sized") {
		t.Fatalf("want layout eligibility diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestLayoutQueriesRequireOneTypeArgument(t *testing.T) {
	source := "fun bad(): Size\n    return size_of()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "exactly one type argument") {
		t.Fatalf("want arity diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
	source = "fun bad(): Size\n    return size_of<Int32>(1)\nend\n"
	result = Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "no value arguments") {
		t.Fatalf("want value-argument diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestLayoutQueriesAreProtectedNames(t *testing.T) {
	source := "fun size_of(): Int32\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "protected built-in name") {
		t.Fatalf("want protected-name diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestLayoutQueryInGenericBodyDefersToSpecialization(t *testing.T) {
	source := "fun storage_size<T>(): Size\n    return size_of<T>()\nend\nfun demo(): Size\n    return storage_size<Int64>()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "sizeof(int64_t)") {
		t.Fatalf("generated C lacks the specialized sizeof:\n%s", result.MainC)
	}
}
