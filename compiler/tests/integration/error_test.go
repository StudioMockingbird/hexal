package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestErrorNewConstruction(t *testing.T) {
	result := compileSource("fun demo() do\n    err: Error := Error.new(\"File Error\", \"file not found\")\n    header: Strand := err.header\n    message: String := err.message\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The Error representation is owned by hexal/error.h; hexal.h must not
	// define it and the module header must include the component.
	errorH := moduleFile(t, result, "hexal/error.h")
	for _, want := range []string{
		"typedef struct hex_t_Error hex_t_Error;",
		"struct hex_t_Error {",
		"const hex_string *hex_m_file;",
		"size_t hex_m_line;",
		"size_t hex_m_column;",
		"hex_strand hex_m_header;",
		"const hex_string *hex_m_message;",
	} {
		if !strings.Contains(errorH, want) {
			t.Fatalf("hexal/error.h = %q, want %q", errorH, want)
		}
	}
	if strings.Contains(hexalH(t, result), "hex_t_Error") {
		t.Fatalf("hexal.h must not carry the Error definition: %q", hexalH(t, result))
	}
	if !strings.Contains(rootH(t, result), "#include \"hexal/error.h\"") {
		t.Fatalf("modules/app.h must include hexal/error.h: %q", rootH(t, result))
	}
	for _, want := range []string{
		"hex_v_err = (hex_t_Error){",
		".hex_m_file = &hex_lit_0,",
		".hex_m_line = 2,",
		".hex_m_column = 19,",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestTryExpression(t *testing.T) {
	result := compileSource("fun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error do\n    count: Int32 := try read_count()\n    total: Int32 := count + try read_count()\n    return total\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_union_7_int32_t7_t_Error hex_try_1 = hex_f_m3_app_read_count();",
		"if (hex_try_1.tag == hex_union_7_int32_t7_t_Error_tag_member_1) {",
		"return (hex_union_7_int32_t7_t_Error){ .tag = hex_union_7_int32_t7_t_Error_tag_member_1, .payload.member_1 = hex_try_1.payload.member_1 };",
		"hex_v_count = hex_try_1.payload.member_0;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestTryStatement(t *testing.T) {
	nilSuccess := compileSource("fun fail(): Nil | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error do\n    try fail()\n    return 1\nend\n")
	if nilSuccess.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Nil-success try statement = %v", nilSuccess.Stderr)
	}
	for _, want := range []string{
		"const hex_union_7_t_Error9_nullptr_t hex_try_1 = hex_f_m3_app_fail();",
		"if (hex_try_1.tag == ",
	} {
		if !strings.Contains(rootC(t, nilSuccess), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, nilSuccess), want)
		}
	}
	if strings.Contains(rootC(t, nilSuccess), "hex_try_result_") {
		t.Fatalf("try statement must not normalize a discarded success value")
	}

	payload := compileSource("fun read(): Int32 | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error do\n    try read()\n    return 1\nend\n")
	if payload.ExitCode != compiler.ExitSuccess {
		t.Fatalf("payload-success try statement = %v", payload.Stderr)
	}
}

// A try statement requires a compatible enclosing function and a union
// operand with exactly one Error member, and does not admit arbitrary value
// expressions as statements.
func TestTryStatementDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo(): Int32 | Error do\n    value: Int32 := 1\n    try value\nend\n", "try requires a union containing Error"},
		{"fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\ntry read()\n", "try requires an enclosing function whose result accepts Error"},
		{"fun demo(): Int32 | Error do\n    try Error.new(\"x\", \"y\")\nend\n", "try requires a union containing Error"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}

	valueStatement := compileSource("fun demo(): Int32 | Error\n    5\nend\n")
	if valueStatement.ExitCode != compiler.ExitFailure {
		t.Fatalf("a bare value must not be a statement; got accept")
	}
}

func TestTryMultipleSuccessMembers(t *testing.T) {
	result := compileSource("fun read_number(): Int32 | Float32 | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error do\n    value: Int32 | Float32 := try read_number()\n    return 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_union_7_int32_t5_float7_t_Error hex_try_1 = hex_f_m3_app_read_number();",
		"hex_union_7_int32_t5_float hex_try_result_2;",
		"switch (hex_try_1.tag) {",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestTryErrorReturnType(t *testing.T) {
	result := compileSource("fun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun fallback(): Int32 | Error do\n    count: Int32 := try read_count()\n    return count\nend\nfun demo(): Int32 | Error do\n    return fallback()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestErrdeferRunsOnErrorReturn(t *testing.T) {
	result := compileSource("fun cleanup(value: Int32) do\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error do\n    errdefer cleanup(1)\n    defer cleanup(2)\n    count: Int32 := try read_count()\n    return count\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The try error path unwinds with errorExit=true: both the errdefer and
	// the defer run.
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_cleanup(hex_defer_capture_1);") || !strings.Contains(rootC(t, result), "hex_f_m3_app_cleanup(hex_defer_capture_2);") {
		t.Fatalf("modules/app.c = %q, want errdefer and defer on the error path", rootC(t, result))
	}
}

func TestErrdeferSkippedOnSuccessReturn(t *testing.T) {
	result := compileSource("fun cleanup(value: Int32) do\nend\nfun demo(): Int32 | Error do\n    errdefer cleanup(1)\n    defer cleanup(2)\n    return 7\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The success return classifies the exit: the defer runs unconditionally
	// and the errdefer is guarded by the runtime Error test.
	if !strings.Contains(rootC(t, result), "const bool hex_err_2 = (hex_return_1.tag == hex_union_7_int32_t7_t_Error_tag_member_1);") {
		t.Fatalf("modules/app.c = %q, want success exit classification", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "if (hex_err_2) {") {
		t.Fatalf("modules/app.c = %q, want guarded errdefer", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_cleanup(hex_defer_capture_2);") {
		t.Fatalf("modules/app.c = %q, defer must run on success", rootC(t, result))
	}
}

func TestErrdeferRuntimeUnionReturn(t *testing.T) {
	result := compileSource("fun cleanup(value: Int32) do\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error do\n    errdefer cleanup(1)\n    result: Int32 | Error := read_count()\n    if release then\n        return result\n    end\n    return 3\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "const bool hex_err_") {
		t.Fatalf("modules/app.c = %q, want runtime exit classification", rootC(t, result))
	}
}

// A deferred call through a Fun<> value captures the callee at registration
// and reuses ordinary call lowering at the scope exit: binding, parameter,
// and errdefer forms all compile instead of failing closed.
func TestDeferredFunValueCalls(t *testing.T) {
	compileCases := []string{
		"fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run() do\n    callback: Fun<(Int32) : Int32> := square\n    defer callback(3)\nend\n",
		"fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(callback: Fun<(Int32) : Int32>) do\n    defer callback(3)\nend\n",
		"fun cleanup(value: Int32): Nil | Error do\n    return nil\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun run(): Int32 | Error do\n    callback: Fun<(Int32) : Nil | Error> := cleanup\n    errdefer callback(1)\n    count: Int32 := try read_count()\n    return count\nend\n",
		"fun cleanup(value: Int32): Nil | Error do\n    return nil\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun run(callback: Fun<(Int32) : Nil | Error>): Int32 | Error do\n    errdefer callback(1)\n    count: Int32 := try read_count()\n    return count\nend\n",
	}
	for _, source := range compileCases {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("deferred Fun<> call rejected (%v):\n%s", result.Stderr, source)
		}
	}
	// The scope-exit call goes through the captured function value.
	result := compileSource("fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run() do\n    callback: Fun<(Int32) : Int32> := square\n    defer callback(3)\nend\n")
	root := rootC(t, result)
	if !strings.Contains(root, "= hex_v_callback;") {
		t.Fatalf("modules/app.c = %q, want the callee captured at registration", root)
	}
	if !strings.Contains(root, "hex_defer_capture_1(hex_defer_capture_2);") {
		t.Fatalf("modules/app.c = %q, want captured callee applied to the captured argument", root)
	}
}

func TestErrorDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo(): Int32 | Error do\n    value: Int32 := 1\n    bad: Int32 := try value\nend", "try requires a union containing Error"},
		{"fun demo(): Int32 do\n    value: Int32 := 1\n    bad: Int32 := try value\nend", "try requires an enclosing function whose result accepts Error"},
		{"fun read_count(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 do\n    defer try read_count()\n    return 1\nend", "try is not permitted inside defer or errdefer"},
		{"fun demo(): Int32 do\n    errdefer cleanup()\n    return 1\nend\nfun cleanup() do\nend", "errdefer requires an enclosing function whose result accepts Error"},
		{"fun demo() do\n    err: Error := Error { file = \"x\", line = 1, column = 1, header = \"h\", message = \"m\" }\nend", "Error must be created with Error.new(header, message)"},
		{"fun demo() do\n    err: Error := Error.new(1, \"m\")\nend", "Error.new expects header: Strand and message: String"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

// try and spawn inside errdefer are rejected exactly like their defer forms:
// the errdefer action is a cleanup context, so its direct expression and
// nested call arguments must not contain either construct.
func TestTryAndSpawnInsideErrdeferRejected(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"try direct", "fun read_count(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 | Error do\n    errdefer try read_count()\n    return 1\nend", "try is not permitted inside defer or errdefer"},
		{"try nested argument", "fun read_count(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun consume(v: Int32) do\nend\nfun demo(): Int32 | Error do\n    errdefer consume(try read_count())\n    return 1\nend", "try is not permitted inside defer or errdefer"},
		{"spawn direct", "fun worker(v: Int32): Int32 do\n    return v\nend\nfun demo(): Int32 | Error do\n    errdefer spawn worker(1)\n    return 1\nend", "spawn is not permitted inside defer or errdefer"},
		{"spawn nested argument", "fun worker(v: Int32): Int32 do\n    return v\nend\nfun consume(t: Task<Int32>) do\nend\nfun demo(): Int32 | Error do\n    errdefer consume(spawn worker(1))\n    return 1\nend", "spawn is not permitted inside defer or errdefer"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestErrorFileRecordsLogicalSourceKey(t *testing.T) {
	single := compileSource("fun demo() do\n    err: Error := Error.new(\"x\", \"y\")\n    again: Error := Error.new(\"x\", \"y\")\nend")
	if single.ExitCode != compiler.ExitSuccess {
		t.Fatalf("single-module compile failed: %v", single.Stderr)
	}
	pool := single.Files["hexal/string.c"]
	if !strings.Contains(pool, "97, 112, 112, 46, 104, 101, 120") {
		t.Fatalf("single-module literal pool must intern the app.hex file key:\n%s", pool)
	}
	if strings.Contains(pool, "109, 97, 105, 110, 46, 104, 101, 120") {
		t.Fatalf("single-module literal pool must not intern main.hex:\n%s", pool)
	}
	if count := strings.Count(pool, "97, 112, 112, 46, 104, 101, 120"); count != 1 {
		t.Fatalf("repeated Error.new must intern the file literal once; got %d app.hex literals", count)
	}

	multi := compiler.Compile(map[string]string{
		"a.hex":   "export fun fa(): Error do\n    return Error.new(\"a\", \"x\")\nend\n",
		"b.hex":   "export fun fb(): Error do\n    return Error.new(\"b\", \"y\")\nend\n",
		"app.hex": "module A = import \"./a\"\nmodule B = import \"./b\"\nfun demo() do\n    ea: Error := A.fa()\n    eb: Error := B.fb()\nend\n",
	}, "app.hex", compiler.Project{})
	if multi.ExitCode != compiler.ExitSuccess {
		t.Fatalf("multi-module compile failed: %v", multi.Stderr)
	}
	multiPool := multi.Files["hexal/string.c"]
	if !strings.Contains(multiPool, "97, 46, 104, 101, 120") || !strings.Contains(multiPool, "98, 46, 104, 101, 120") {
		t.Fatalf("multi-module pool must intern each module's own key:\n%s", multiPool)
	}
	if strings.Contains(multiPool, "109, 97, 105, 110, 46, 104, 101, 120") {
		t.Fatalf("multi-module pool must not intern main.hex:\n%s", multiPool)
	}
	for module, want := range map[string]string{"a.c": "a.hex", "b.c": "b.hex"} {
		artifact := multi.Files["modules/"+module]
		if !strings.Contains(artifact, ".hex_m_file = &hex_lit_") {
			t.Fatalf("%s must reference the shared pool's file literal:\n%s", module, artifact)
		}
		if strings.Contains(artifact, want) && !strings.Contains(artifact, "#line") {
			t.Fatalf("%s must not embed the file name outside #line directives:\n%s", module, artifact)
		}
	}
}

func TestErrorReservedName(t *testing.T) {
	result := compileSource("type Error = { value: Int32, }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("Compile stderr = %#v, want reserved-name rejection", result.Stderr)
	}
}

func TestTryInsideBranchAndLoop(t *testing.T) {
	result := compileSource("fun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error do\n    mut total: Int32 := 0\n    while true do\n        total = total + try read_count()\n        break\n    end\n    if total > 0 then\n        total = total + try read_count()\n    end\n    return total\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestErrdeferAtRootScope(t *testing.T) {
	bad := "errdefer cleanup()\nfun cleanup() do\nend\n"
	result := compileSource(bad)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 ||
		!strings.Contains(result.Stderr[0], "errdefer requires an enclosing function whose result accepts Error") {
		t.Fatalf("want root errdefer Type Error; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
	for _, diagnostic := range result.Stderr {
		if strings.Contains(diagnostic, "Unknown Error") {
			t.Fatalf("root errdefer must not be Unknown Error: %v", result.Stderr)
		}
	}
	// Function-scoped errdefer remains valid, and root defer is unchanged.
	valid := "fun cleanup() do\nend\nfun run(): Int32 | Error do\n    errdefer cleanup()\n    return 1\nend\n"
	if result := compileSource(valid); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("function errdefer must compile: %v", result.Stderr)
	}
	rootDefer := "fun cleanup() do\nend\ndefer cleanup()\n"
	if result := compileSource(rootDefer); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("root defer must still compile: %v", result.Stderr)
	}
}
