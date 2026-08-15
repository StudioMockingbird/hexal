package integration

// RFC 0060: the selected root module's C/header pair (modules/app.c,
// modules/app.h) holds every user-facing declaration, statement, #line
// mapping, the process-wide runtime definitions, and the process entry point.
// hexal.h holds only the shared program-support machinery. This split is the
// ground contract for per-module artifacts; the thin-entry pair no longer
// exists.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestRootModuleArtifactsSplit(t *testing.T) {
	source := "type Point = { x: Int32, }\nfun area(point: Point): Int32\n    return point.x\nend\nvalue: Int32 = 13\n"
	result := assertCompiles(t, source)

	rootC := rootC(t, result)
	rootH := rootH(t, result)
	hexalH := hexalH(t, result)

	if !strings.Contains(rootH, "typedef struct hex_t_m3_app_Point") {
		t.Fatalf("user type must live in the entrypoint module header, got C=%q H=%q", rootC, rootH)
	}
	if !strings.Contains(rootC, "static int32_t hex_f_m3_app_area(") {
		t.Fatalf("user function must live in modules/app.c, got %q", rootC)
	}
	if !strings.Contains(rootC, "int main(void)") {
		t.Fatalf("modules/app.c must define the process entry point, got %q", rootC)
	}
	if !strings.Contains(rootC, "#line 5 \"app.hex\"") {
		t.Fatalf("module statements must carry app.hex line mappings, got %q", rootC)
	}
	if strings.Contains(hexalH, "hex_f_m3_app_area") || strings.Contains(hexalH, "hex_t_m3_app_Point") {
		t.Fatalf("user code leaked into hexal.h: %q", hexalH)
	}
	if strings.Contains(rootH, "int main(void)") {
		t.Fatalf("the module header must not declare the process entry point, got %q", rootH)
	}
	generated := hexalH + "\n" + rootC + "\n" + rootH
	for _, forbidden := range []string{"#include \"main.h\"", "HEXAL_MAIN_H", "hex_module_root_run"} {
		if strings.Contains(generated, forbidden) {
			t.Fatalf("generated content must not contain %q:\n%s", forbidden, generated)
		}
	}
	if !strings.Contains(hexalH, "#ifndef HEXAL_H") {
		t.Fatalf("hexal.h must carry the HEXAL_H guard, got %q", hexalH)
	}
	if len(result.Files) != 3 {
		t.Fatalf("successful compilation must produce exactly 3 artifacts, got %v", sortedKeys(result.Files))
	}
}

func TestFailureReturnsNoArtifacts(t *testing.T) {
	result := compileSource("x: Int32 = true")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want failure, got %#v", result)
	}
	if result.Files == nil {
		t.Fatalf("Files must be non-nil on failure")
	}
	if len(result.Files) != 0 {
		t.Fatalf("failure must produce no artifacts, got %v", sortedKeys(result.Files))
	}
}
