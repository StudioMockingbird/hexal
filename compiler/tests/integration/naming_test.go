package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestPrivateValueNames(t *testing.T) {
	longName := "long_identifier_name_with_more_than_one_hundred_characters_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := "main: Int32 = 1 int: Int32 = 2 restrict: Int32 = 3 INT32_MAX: Int32 = 4 hex_v_score: Int32 = 5 " + longName + ": Int32 = 6"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const int32_t hex_v_main = 1;",
		"const int32_t hex_v_int = 2;",
		"const int32_t hex_v_restrict = 3;",
		"const int32_t hex_v_INT32_MAX = 4;",
		"const int32_t hex_v_hex_v_score = 5;",
		"const int32_t hex_v_" + longName + " = 6;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestReferencesUsePrivateValueNames(t *testing.T) {
	result := compileSource("mut int: Int32 = 1 int = 2 pointer: Ptr<Int32> = ref int value: Int32 = pointer.value")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"int32_t hex_v_int = 1;",
		"hex_v_int = 2;",
		"const int32_t *const hex_v_pointer = &hex_v_int;",
		"const int32_t hex_v_value = *hex_v_pointer;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

// Every built-in type name is protected from redeclaration. Aliases,
// scalars, collections, handles, and marker types all share the same
// diagnostic.
func TestBuiltinTypeNamesAreProtected(t *testing.T) {
	names := map[string]string{
		"Int8": "", "Int16": "", "Int32": "", "Int64": "",
		"UInt8": "", "UInt16": "", "UInt32": "", "UInt64": "",
		"Float32": "", "Float64": "", "Bool": "", "Rune": "", "Byte": "", "Size": "",
		"String": "", "Strand": "", "RuneCursor": "",
		"List": "", "Dict": "", "View": "", "Array": "",
		"Fun": "", "Unknown": "", "Nil": "",
		"Task": "", "Channel": "", "Mutex": "", "Atomic": "", "Heap": "",
		"EoS": "", "Error": "",
		"Ptr":    "built-in type constructor Ptr cannot be redeclared",
		"MutPtr": "built-in type constructor MutPtr cannot be redeclared",
	}
	for name, want := range names {
		t.Run(name, func(t *testing.T) {
			if want == "" {
				want = "built-in type " + name + " cannot be redeclared"
			}
			assertRejects(t, "type "+name+" = Int32", want)
		})
	}
}
