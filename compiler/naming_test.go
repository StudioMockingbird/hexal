package compiler

// Source-to-C identifier mapping and private name prefixes. Spec 0004.

import (
	"strings"
	"testing"
)

func TestPrivateValueNames(t *testing.T) {
	longName := "long_identifier_name_with_more_than_one_hundred_characters_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := "main: Int32 = 1 int: Int32 = 2 restrict: Int32 = 3 INT32_MAX: Int32 = 4 hex_v_score: Int32 = 5 " + longName + ": Int32 = 6"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
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
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestReferencesUsePrivateValueNames(t *testing.T) {
	result := Compile("mut int: Int32 = 1 int = 2 pointer: Ptr<Int32> = ref int value: Int32 = pointer.value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"int32_t hex_v_int = 1;",
		"hex_v_int = 2;",
		"const int32_t *const hex_v_pointer = &hex_v_int;",
		"const int32_t hex_v_value = *hex_v_pointer;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}
