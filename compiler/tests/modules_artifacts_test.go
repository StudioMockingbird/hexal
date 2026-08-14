package tests

// RFC 0034 Task 2: the entrypoint module's generated C/header (modules/app.c,
// modules/app.h) hold every user-facing declaration, statement, and #line
// mapping, while main.c/main.h hold only the runtime machinery and the thin
// entry. This split is the ground contract Tasks 3-8 extend to per-module
// artifacts.

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

	if !strings.Contains(rootC, "typedef struct hex_t_Point") && !strings.Contains(rootH, "typedef struct hex_t_Point") {
		t.Fatalf("user type must live in the entrypoint module artifacts, got C=%q H=%q", rootC, rootH)
	}
	if !strings.Contains(rootC, "static int32_t hex_f_area(") {
		t.Fatalf("user function must live in modules/app.c, got %q", rootC)
	}
	if !strings.Contains(rootC, "int hex_module_root_run(void)") {
		t.Fatalf("modules/app.c must define hex_module_root_run, got %q", rootC)
	}
	if !strings.Contains(rootC, "#line 5 \"app.hex\"") {
		t.Fatalf("module statements must carry app.hex line mappings, got %q", rootC)
	}
	if strings.Contains(result.MainC, "hex_f_area") || strings.Contains(result.MainC, "hex_t_Point") {
		t.Fatalf("user code leaked into main.c: %q", result.MainC)
	}
	if strings.Contains(result.MainH, "hex_t_Point") {
		t.Fatalf("user type leaked into main.h: %q", result.MainH)
	}

	if !strings.Contains(result.MainC, "int main(void)") {
		t.Fatalf("main.c must hold the thin entry, got %q", result.MainC)
	}
	if !strings.Contains(result.MainC, "hex_module_root_run()") {
		t.Fatalf("main.c must call the entrypoint module's root run, got %q", result.MainC)
	}
	if !strings.Contains(rootH, "int hex_module_root_run(void);") {
		t.Fatalf("modules/app.h must declare hex_module_root_run, got %q", rootH)
	}
	if result.MainC != result.Files["main.c"] || result.MainH != result.Files["main.h"] {
		t.Fatalf("MainC/MainH must mirror Files[\"main.c\"]/Files[\"main.h\"]")
	}
	if len(result.Files) != 4 {
		t.Fatalf("successful compilation must produce exactly 4 artifacts, got %v", sortedKeys(result.Files))
	}
}

func TestFailureReturnsOnlyEntrypointFiles(t *testing.T) {
	result := compileSource("x: Int32 = true")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want failure, got %#v", result)
	}
	if len(result.Files) != 2 {
		t.Fatalf("failure must produce only main.c and main.h, got %v", sortedKeys(result.Files))
	}
	if _, ok := result.Files["main.c"]; !ok {
		t.Fatalf("failure output lacks main.c: %v", sortedKeys(result.Files))
	}
	if _, ok := result.Files["main.h"]; !ok {
		t.Fatalf("failure output lacks main.h: %v", sortedKeys(result.Files))
	}
	if _, ok := result.Files["modules/app.c"]; ok {
		t.Fatalf("failure output must not contain entrypoint module artifacts: %v", sortedKeys(result.Files))
	}
}
