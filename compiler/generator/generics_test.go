package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

func TestGenerateGenericFunctionSpecialization(t *testing.T) {
	program := checkedGeneratorSource(t, "fun identity<T>(value: T): T\nreturn value\nend answer: Int32 = identity(42)")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_f_m3_app_identity_Int32") {
		t.Fatalf("modules/app.c = %q, want specialized function name", rootC)
	}
	if strings.Count(rootC, "hex_f_m3_app_identity_Int32") < 2 {
		t.Fatalf("modules/app.c = %q, want prototype and definition", rootC)
	}
}

func TestGenerateGenericObjectSpecialization(t *testing.T) {
	program := checkedGeneratorSource(t, "type Box<T> = { value: T } box: Box<Int32> = Box<Int32> { value = 42 }")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootH, "hex_t_m3_app_Box_Int32") {
		t.Fatalf("modules/app.h = %q, want specialized object struct", rootH)
	}
	if !strings.Contains(rootC, "hex_v_box") {
		t.Fatalf("modules/app.c = %q, want specialized binding", rootC)
	}
}

func TestGenerateGenericMethodSpecialization(t *testing.T) {
	program := checkedGeneratorSource(t, "type Box<T> = { value: T }\nimpl Box<T>.get(): T\nreturn self.value\nend box: Box<Int32> = Box<Int32> { value = 42 }\nvalue: Int32 = box.get()")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootH, "hex_t_m3_app_Box_Int32") {
		t.Fatalf("modules/app.h = %q, want specialized object struct", rootH)
	}
	if !strings.Contains(rootC, "hex_f_m3_app_Box_Int32__get") && !strings.Contains(rootC, "hex_f_m3_app_Box_Int32_get") {
		t.Fatalf("modules/app.c = %q, want specialized method definition", rootC)
	}
}
