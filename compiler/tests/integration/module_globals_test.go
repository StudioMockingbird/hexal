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
	source := "fun run(value: Ptr<mut Int32>) do\n    ^value = 1\nend\nlet mut counter: Int32 = 0\nrun(@counter)\n"
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

// An entry-module named function captures an earlier root binding by
// reference; the binding moves into the stack-owned entry environment.
func TestFunctionCapturesRootLocal(t *testing.T) {
	source := "let mut counter: Int32 = 0\nfun increment() do\n    counter = counter + 1\nend\nincrement()\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	if !strings.Contains(appC, "typedef struct {") || !strings.Contains(appC, "env->") || !strings.Contains(appC, "hex_entry_env env;") {
		t.Fatalf("root capture did not lower to a stack-owned environment:\n%s", appC)
	}
}

func TestNoNativeModuleConstantsOrStatics(t *testing.T) {
	// `global` remains an ordinary identifier and a root binding never
	// produces C file-scope storage; only an explicit `static` declaration
	// does (TestStaticModuleValueLowersAsFileScopeStorage below).
	source := "let global: Int32 = 2\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if strings.Contains(rootH(t, result), "hex_v_global") {
		t.Fatalf("root binding emitted file-scope storage:\n%s", rootH(t, result))
	}
}

// A fixed top-level declaration in an imported module is a module constant:
// one immutable C file-scope object with internal linkage.
func TestModuleConstantLowersAsFileScopedConst(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "let count: Int32 = 1\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	mathC := result.Files["modules/math.c"]
	if !strings.Contains(mathC, "static const int32_t hex_v_") || !strings.Contains(mathC, "_count = 1;") {
		t.Fatalf("module constant did not lower as static const storage:\n%s", mathC)
	}
}
