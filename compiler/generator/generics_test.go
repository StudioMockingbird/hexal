package generator

import (
	"strings"
	"testing"
)

func TestGenerateGenericFunctionSpecialization(t *testing.T) {
	program := checkedGeneratorSource(t, "fun identity<T>(value: T): T\nreturn value\nend answer: Int32 = identity(42)")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, "hex_f_identity_Int32") {
		t.Fatalf("main.c = %q, want specialized function name", mainC)
	}
	if strings.Count(mainC, "hex_f_identity_Int32") < 2 {
		t.Fatalf("main.c = %q, want prototype and definition", mainC)
	}
}

func TestGenerateGenericObjectSpecialization(t *testing.T) {
	program := checkedGeneratorSource(t, "type Box<T> = { value: T } box: Box<Int32> = Box<Int32> { value = 42 }")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainH, "hex_t_Box_Int32") {
		t.Fatalf("main.h = %q, want specialized object struct", mainH)
	}
	if !strings.Contains(mainC, "hex_v_box") {
		t.Fatalf("main.c = %q, want specialized binding", mainC)
	}
}

func TestGenerateGenericMethodSpecialization(t *testing.T) {
	program := checkedGeneratorSource(t, "type Box<T> = { value: T }\nimpl Box<T>.get(): T\nreturn self.value\nend box: Box<Int32> = Box<Int32> { value = 42 }\nvalue: Int32 = box.get()")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainH, "hex_t_Box_Int32") {
		t.Fatalf("main.h = %q, want specialized object struct", mainH)
	}
	if !strings.Contains(mainC, "hex_f_Box_Int32__get") && !strings.Contains(mainC, "hex_f_Box_Int32_get") {
		t.Fatalf("main.c = %q, want specialized method definition", mainC)
	}
}
