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
	source := "fun run(value: Ptr<mut Int32>) do\n    ^value = 1\nend\nmut counter: Int32 := 0\nrun(@counter)\n"
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
	source := "mut counter: Int32 := 0\nfun increment() do\n    counter = counter + 1\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("want capture diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestNoNativeModuleConstantsOrStatics(t *testing.T) {
	// `global` remains an ordinary identifier and a root binding never
	// produces C file-scope storage; only an explicit `static` declaration
	// does (TestStaticModuleValueLowersAsFileScopeStorage below).
	source := "global: Int32 := 2\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if strings.Contains(rootH(t, result), "hex_v_global") {
		t.Fatalf("root binding emitted file-scope storage:\n%s", rootH(t, result))
	}
}

// A `static` module value, unlike an ordinary root binding, is the one
// spelling that does emit program-lifetime C file-scope storage.
func TestStaticModuleValueLowersAsFileScopeStorage(t *testing.T) {
	source := "static count: Int32 := 1\nread: Int32 := count\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "const int32_t hex_v_") {
		t.Fatalf("static module value did not lower as fixed C file-scope storage:\n%s", rootC(t, result))
	}
}
