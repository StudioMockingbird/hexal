package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// A named entry function captures an earlier root binding by reference; the
// binding lowers into one stack-owned environment field.
func TestEntryFunctionCapturesRootBinding(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun bump() do\n    count = count + 1\nend\nbump()\nprint(count)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	for _, want := range []string{
		"typedef struct {",
		"hex_entry_env env;",
		"static void hex_f_m3_app_bump(hex_entry_env *env)",
		"env->hex_v_count = hex_wrap_add_int32_t(env->hex_v_count, 1);",
		"env.hex_v_count = 0;",
		"hex_f_m3_app_bump(&env);",
	} {
		if !strings.Contains(appC, want) {
			t.Fatalf("captured C missing %q:\n%s", want, appC)
		}
	}
}

// Environment-dependence is transitive: a function that calls a capturing
// function is itself environment-dependent and passes the environment on.
func TestEnvironmentDependenceIsTransitive(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun bump() do\n    count = count + 1\nend\nfun twice() do\n    bump()\n    bump()\nend\ntwice()\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	if !strings.Contains(appC, "static void hex_f_m3_app_twice(hex_entry_env *env)") {
		t.Fatalf("transitive caller is not environment-dependent:\n%s", appC)
	}
	if !strings.Contains(appC, "hex_f_m3_app_bump(env);") {
		t.Fatalf("transitive call does not forward the environment:\n%s", appC)
	}
}

// A generic transitive caller inherits the entry environment from its
// environment-dependent callee after specialization.
func TestGenericEnvironmentDependenceIsTransitive(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun bump() do\n    count = count + 1\nend\nfun relay<T>(value: T) do\n    bump()\nend\nrelay(1)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generic transitive capture failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	if !strings.Contains(appC, "hex_entry_env *env") || !strings.Contains(appC, "hex_f_m3_app_bump(env)") {
		t.Fatalf("generic transitive caller did not forward the environment:\n%s", appC)
	}
}

// A method in the entry module may capture an earlier root binding.
func TestEntryMethodCapturesRootBinding(t *testing.T) {
	result := compileSource("type Counter is struct n: Int32 end\nlet mut count: Int32 = 0\nmethod Counter.bump() do\n    count = count + 1\nend\nlet c: Counter = Counter(n = 0)\nc.bump()\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	if !strings.Contains(appC, "hex_f_m3_app_Counter_bump(hex_entry_env *env") {
		t.Fatalf("capturing method lacks the environment parameter:\n%s", appC)
	}
	if !strings.Contains(appC, "hex_f_m3_app_Counter_bump(&env, ") {
		t.Fatalf("capturing method call does not pass the environment:\n%s", appC)
	}
}

// A binding declared after a function is not visible for capture: entry
// bindings keep source-order visibility.
func TestCaptureIsSourceOrdered(t *testing.T) {
	result := compileSource("fun read(): Int32 do\n    return count\nend\nlet count: Int32 = 1\n")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot access module data binding count") {
		t.Fatalf("want source-order rejection; got %v", result.Stderr)
	}
}

// A local function literal never captures an enclosing entry binding.
func TestLocalFunctionLiteralDoesNotCapture(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun outer() do\n    let callback = fun (): Int32 do\n        return count\n    end\nend\n")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot access module data binding count") {
		t.Fatalf("want non-capturing literal; got %v", result.Stderr)
	}
}

// A program with no capture emits no environment and keeps automatic locals.
func TestNoCaptureEmitsNoEnvironment(t *testing.T) {
	result := compileSource("let count: Int32 = 0\nfun plain(): Int32 do\n    return 1\nend\nprint(count)\nprint(plain())\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	if strings.Contains(appC, "hex_entry_env") {
		t.Fatalf("uncaptured program emitted an environment:\n%s", appC)
	}
	if !strings.Contains(appC, "const int32_t hex_v_count = 0;") {
		t.Fatalf("uncaptured root binding is not an automatic local:\n%s", appC)
	}
}

// An imported module's function cannot capture: only the entry module has an
// entry environment.
func TestImportedFunctionCannotCapture(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\n",
		"lib.hex": "let value: Int32 = 1\nfun read(): Int32 do\n    return value\nend\nexport\n    read\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("imported constant read failed: %v", result.Stderr)
	}
	if strings.Contains(result.Files["modules/lib.c"], "hex_entry_env") {
		t.Fatalf("imported module emitted an entry environment:\n%s", result.Files["modules/lib.c"])
	}
}

// A root direct call before a captured binding is initialized is rejected.
func TestCaptureInitializationSafety(t *testing.T) {
	result := compileSource("tick()\nlet mut count: Int32 = 0\nfun tick() do\n    count = count + 1\nend\n")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "function tick may access entry binding count before count is initialized") {
		t.Fatalf("want initialization diagnostic; got %v", result.Stderr)
	}
	ok := compileSource("let mut count: Int32 = 0\nfun tick() do\n    count = count + 1\nend\ntick()\n")
	if ok.ExitCode != compiler.ExitSuccess {
		t.Fatalf("call after initialization failed: %v", ok.Stderr)
	}
}

// An environment-dependent function cannot escape as a Fun value, be spawned,
// or be exported.
func TestCaptureNonEscaping(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"function value", "let mut count: Int32 = 0\nfun tick() do\n    count = count + 1\nend\nlet f: Fun<()> = tick\n"},
		{"spawn", "let mut count: Int32 = 0\nfun tick(): Bool do\n    count = count + 1\n    return true\nend\nlet t: Task<Bool> | Error = spawn tick()\n"},
		{"export", "let mut count: Int32 = 0\nfun tick() do\n    count = count + 1\nend\nexport\n    tick\nend\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "uses the entry environment and is valid only as a direct entry-module call") {
				t.Fatalf("want non-escaping diagnostic; got %v", result.Stderr)
			}
		})
	}
}

// A declaration with an empty capture summary retains its ordinary
// function-value and spawn behavior.
func TestNonCapturingDeclarationKeepsEscaping(t *testing.T) {
	result := compileSource("fun plain(): Int32 do\n    return 1\nend\nlet f: Fun<() : Int32> = plain\nlet t: Task<Int32> | Error = spawn plain()\nprint(f())\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("non-capturing function lost its value/spawn behavior: %v", result.Stderr)
	}
}

// A generic entry function may capture an earlier root binding; every
// specialization inherits the capture and the hidden environment parameter.
func TestGenericEntryFunctionCaptures(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun bump<T>(value: T) do\n    count = count + 1\nend\nbump(1)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	appC := rootC(t, result)
	if !strings.Contains(appC, "hex_entry_env *env") || !strings.Contains(appC, "env->hex_v_count") {
		t.Fatalf("generic capture did not lower into the environment:\n%s", appC)
	}
}

// A direct inferred fixed function-literal declaration sugar captures under the
// same rule as the equivalent written named function.
func TestSugarFunctionCaptures(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nlet bump = fun () do\n    count = count + 1\nend\nbump()\nprint(count)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_entry_env *env") {
		t.Fatalf("sugar function did not capture:\n%s", rootC(t, result))
	}
}

// Taking the address of a captured binding follows the ordinary pointer rules.
func TestCapturedBindingAddressable(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun peek(): Int32 do\n    let p: Ptr<Int32> = @count\n    return ^p\nend\npeek()\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

// A function-body errdefer to an environment-dependent function propagates the
// environment.
func TestErrdeferPropagatesEnvironment(t *testing.T) {
	result := compileSource("let mut count: Int32 = 0\nfun tick(): Nil | Error do\n    count = count + 1\n    return nil\nend\nfun run(): Nil | Error do\n    errdefer tick()\n    return Error(ErrorKind.Other(header = \"x\"), \"y\")\nend\nrun()\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_tick(env)") {
		t.Fatalf("errdefer did not forward the environment:\n%s", rootC(t, result))
	}
}
