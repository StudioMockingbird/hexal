package generator

import (
	"strings"
	"testing"
)

// An Error-using program emits hexal/error.h with the canonical Error
// representation, the module header includes it, and hexal.h no longer
// carries the definition.
func TestErrorComponentSelectedByUse(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(): Int32 | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend")
	files := generateOne(t, program)
	errorH, exists := files["hexal/error.h"]
	if !exists {
		t.Fatalf("Error-using program emitted no hexal/error.h: %v", files)
	}
	if !strings.HasPrefix(errorH, "#ifndef HEXAL_ERROR_H\n#define HEXAL_ERROR_H\n") {
		t.Fatalf("hexal/error.h lost its guard: %q", errorH)
	}
	if !strings.HasSuffix(errorH, "\n#endif\n") {
		t.Fatalf("hexal/error.h must end with a single newline after #endif: %q", errorH)
	}
	includeOrder := strings.Index(errorH, "#include \"hexal.h\"")
	stringInclude := strings.Index(errorH, "#include \"hexal/string.h\"")
	if includeOrder < 0 || stringInclude < 0 || includeOrder > stringInclude {
		t.Fatalf("hexal/error.h must include hexal.h then hexal/string.h: %q", errorH)
	}
	for _, want := range []string{
		"typedef struct hex_t_Error hex_t_Error;",
		"struct hex_t_Error {",
		"const hex_string *hex_m_file;",
		"size_t hex_m_line;",
		"size_t hex_m_column;",
		"hex_strand hex_m_header;",
		"const hex_string *hex_m_message;",
		"};",
	} {
		if !strings.Contains(errorH, want) {
			t.Fatalf("hexal/error.h = %q, want %q", errorH, want)
		}
	}
	if strings.Contains(files["hexal.h"], "hex_t_Error") {
		t.Fatalf("hexal.h must not carry the Error definition: %q", files["hexal.h"])
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/error.h\"") {
		t.Fatalf("modules/app.h must include hexal/error.h: %q", files["modules/app.h"])
	}
}

// A program that never names Error emits no hexal/error.h artifact.
func TestErrorComponentAbsentWithoutError(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 = 1")
	files := generateOne(t, program)
	if _, exists := files["hexal/error.h"]; exists {
		t.Fatalf("scalar-only program emitted hexal/error.h: %v", files)
	}
	if strings.Contains(files["modules/app.h"], "hexal/error.h") {
		t.Fatalf("scalar-only module header includes hexal/error.h: %q", files["modules/app.h"])
	}
}

// Rendering the error component is deterministic for an equivalent program.
func TestErrorComponentDeterministic(t *testing.T) {
	merged := &programEmission{errorUsed: true}
	first, err := renderComponentArtifacts(merged, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	second, err := renderComponentArtifacts(merged, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	if first["hexal/error.h"] != second["hexal/error.h"] {
		t.Fatalf("equivalent programs rendered hexal/error.h differently")
	}
}
