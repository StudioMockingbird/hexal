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
	if !strings.Contains(header, "void hex_print_int32(hex_print_buffer *out, int32_t value);") {
		t.Fatalf("hexal/print.h lacks hex_print_int32 declaration: %q", header)
	}
	for _, want := range []string{
		"typedef struct hex_print_buffer {",
		"uint8_t inline_storage[256];",
		"void hex_print_begin(hex_print_buffer *out);",
		"void hex_print_commit(hex_print_buffer *out);",
		"void hex_print_destroy(hex_print_buffer *out);",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal/print.h lacks %q: %q", want, header)
		}
	}
	if !strings.HasPrefix(source, "#include \"hexal/print.h\"") {
		t.Fatalf("hexal/print.c must include its header first: %q", source)
	}
	if !strings.Contains(source, "void hex_print_int32(hex_print_buffer *out, int32_t value) {") {
		t.Fatalf("hexal/print.c lacks hex_print_int32 definition: %q", source)
	}
	// No formatter reaches a sink: only the commit transfers, and it does so
	// exactly once per source call.
	if strings.Contains(source, "hex_io_write_all(") || strings.Contains(source, "WriteFile(") {
		t.Fatalf("a print formatter still writes directly: %q", source)
	}
	if strings.Count(source, "hex_io_stdout_write_all(") != 1 {
		t.Fatalf("hexal/print.c must transfer through exactly one standard-output entry: %q", source)
	}
	if !strings.Contains(source, "[Runtime Error] print buffer allocation failed\\n") {
		t.Fatalf("hexal/print.c lacks the builder allocation trap: %q", source)
	}
	// Console classification and conversion happen once per complete call,
	// behind the commit, never per formatted fragment.
	if strings.Count(source, "GetConsoleMode(") != 1 || strings.Count(source, "WriteConsoleW(") != 1 {
		t.Fatalf("hexal/print.c must classify and submit to a console exactly once per call: %q", source)
	}
	commit := strings.Index(source, "static bool hex_print_commit_native(")
	classify := strings.Index(source, "hex_print_console_handle(&console)")
	if commit < 0 || classify < commit {
		t.Fatalf("console classification must sit behind the commit: %q", source)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/print.h\"") {
		t.Fatalf("modules/app.h = %q, want the print component include", files["modules/app.h"])
	}
}

// A program without print emits no print component.
func TestPrintComponentAbsentWithoutPrint(t *testing.T) {
	program := checkedGeneratorSource(t, "let x: Int32 = 1\n")
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
