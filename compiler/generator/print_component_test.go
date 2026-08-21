package generator

import (
	"strings"
	"testing"
)

// A program using print emits both hexal/print.h and hexal/print.c; the
// module header includes the component.
func TestPrintComponentEmitsPair(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    print(42)\nend")
	files := generateOne(t, program)
	header, hExists := files["hexal/print.h"]
	source, sExists := files["hexal/print.c"]
	if !hExists {
		t.Fatalf("print program emitted no hexal/print.h: %v", files)
	}
	if !sExists {
		t.Fatalf("print program emitted no hexal/print.c: %v", files)
	}
	if !strings.HasPrefix(header, "#ifndef HEXAL_PRINT_H\n#define HEXAL_PRINT_H\n") || !strings.HasSuffix(header, "\n#endif\n") {
		t.Fatalf("hexal/print.h lost its guard: %q", header)
	}
	if !strings.Contains(header, "#include \"hexal.h\"") {
		t.Fatalf("hexal/print.h lacks hexal.h include: %q", header)
	}
	if !strings.Contains(header, "void hex_print_int32(int32_t value);") {
		t.Fatalf("hexal/print.h lacks hex_print_int32 declaration: %q", header)
	}
	if !strings.HasPrefix(source, "#include \"hexal/print.h\"") {
		t.Fatalf("hexal/print.c must include its header first: %q", source)
	}
	if !strings.Contains(source, "void hex_print_int32(int32_t value) {") {
		t.Fatalf("hexal/print.c lacks hex_print_int32 definition: %q", source)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/print.h\"") {
		t.Fatalf("modules/app.h = %q, want the print component include", files["modules/app.h"])
	}
}

// A program without print emits no print component.
func TestPrintComponentAbsentWithoutPrint(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 := 1\n")
	files := generateOne(t, program)
	if _, exists := files["hexal/print.h"]; exists {
		t.Fatalf("no-print program emitted hexal/print.h")
	}
	if _, exists := files["hexal/print.c"]; exists {
		t.Fatalf("no-print program emitted hexal/print.c")
	}
	if strings.Contains(files["modules/app.h"], "hexal/print.h") {
		t.Fatalf("modules/app.h = %q, must not include unselected print component", files["modules/app.h"])
	}
}
