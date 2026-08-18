package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// Hexal has no module-global or process-global values. Root executable
// bindings lower as entry-body locals, functions cannot capture root locals,
// and no accepted declaration emits user value storage at C file scope.

func TestRootBindingsLowerAsLocals(t *testing.T) {
	source := "fun run(value: MutPtr<Int32>) do\n    value.value = 1\nend\nmut counter: Int32 = 0\nrun(ref counter)\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "int32_t hex_v_counter") {
		t.Fatalf("root binding did not lower as a local:\n%s", rootC(t, result))
	}
	if strings.Contains(rootH(t, result), "hex_v_counter") {
		t.Fatalf("root binding leaked into the header (file scope):\n%s", rootH(t, result))
	}
}

func TestFunctionCannotCaptureRootLocal(t *testing.T) {
	source := "mut counter: Int32 = 0\nfun increment() do\n    counter = counter + 1\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("want capture diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestNoNativeModuleConstantsOrStatics(t *testing.T) {
	// No global/static syntax exists: the words are ordinary identifiers or
	// rejected, and a root binding never produces C file-scope storage.
	source := "static: Int32 = 1\nglobal: Int32 = 2\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if strings.Contains(rootH(t, result), "hex_v_static") || strings.Contains(rootH(t, result), "hex_v_global") {
		t.Fatalf("root bindings emitted file-scope storage:\n%s", rootH(t, result))
	}
}
