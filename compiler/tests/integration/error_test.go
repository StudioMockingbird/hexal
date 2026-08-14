package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0029: Error values, T | Error results, try propagation, and errdefer.

func TestErrorNewConstruction(t *testing.T) {
	result := compileSource("fun demo()\n    err: Error = Error.new(\"File Error\", \"file not found\")\n    header: Strand = err.header\n    message: String = err.message\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_t_Error hex_t_Error;",
		"struct hex_t_Error {",
		"const hex_string *hex_m_file;",
		"size_t hex_m_line;",
		"hex_strand hex_m_header;",
		"const hex_string *hex_m_message;",
		"hex_v_err = (hex_t_Error){",
		".hex_m_file = &hex_lit_0,",
		".hex_m_line = 2,",
		".hex_m_column = 18,",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestTryExpression(t *testing.T) {
	result := compileSource("fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error\n    count: Int32 = try read_count()\n    total: Int32 = count + try read_count()\n    return total\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_union_7_int32_t11_hex_t_Error hex_try_1 = hex_f_m3_app_read_count();",
		"if (hex_try_1.tag == hex_union_7_int32_t11_hex_t_Error_tag_member_1) {",
		"return (hex_union_7_int32_t11_hex_t_Error){ .tag = hex_union_7_int32_t11_hex_t_Error_tag_member_1, .payload.member_1 = hex_try_1.payload.member_1 };",
		"hex_v_count = hex_try_1.payload.member_0;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("main.c = %q, want %q", rootC(t, result), want)
		}
	}
}

// RFC 0049 item 8.3: try is a statement as well as an expression. The
// prologue hoists and propagates Error; the success value is discarded with
// no normalization temporary.
func TestTryStatement(t *testing.T) {
	nilSuccess := compileSource("fun fail(): Nil | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    try fail()\n    return 1\nend\n")
	if nilSuccess.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Nil-success try statement = %v", nilSuccess.Stderr)
	}
	for _, want := range []string{
		"const hex_union_11_hex_t_Error9_nullptr_t hex_try_1 = hex_f_m3_app_fail();",
		"if (hex_try_1.tag == ",
	} {
		if !strings.Contains(rootC(t, nilSuccess), want) {
			t.Fatalf("main.c = %q, want %q", rootC(t, nilSuccess), want)
		}
	}
	if strings.Contains(rootC(t, nilSuccess), "hex_try_result_") {
		t.Fatalf("try statement must not normalize a discarded success value")
	}

	payload := compileSource("fun read(): Int32 | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    try read()\n    return 1\nend\n")
	if payload.ExitCode != compiler.ExitSuccess {
		t.Fatalf("payload-success try statement = %v", payload.Stderr)
	}
}

// RFC 0049 item 8.3: a try statement requires a compatible enclosing
// function and a union operand with exactly one Error member, and does not
// admit arbitrary value expressions as statements.
func TestTryStatementDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo(): Int32 | Error\n    value: Int32 = 1\n    try value\nend\n", "try requires a union containing Error"},
		{"fun read(): Int32 | Error\n    return Error.new(\"x\", \"y\")\nend\ntry read()\n", "try requires an enclosing function whose result accepts Error"},
		{"fun demo(): Int32 | Error\n    try Error.new(\"x\", \"y\")\nend\n", "try requires a union containing Error"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}

	valueStatement := compileSource("fun demo(): Int32 | Error\n    5\nend\n")
	if valueStatement.ExitCode != compiler.ExitFailure {
		t.Fatalf("a bare value must not be a statement, got accept")
	}
}

func TestTryMultipleSuccessMembers(t *testing.T) {
	result := compileSource("fun read_number(): Int32 | Float32 | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    value: Int32 | Float32 = try read_number()\n    return 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_union_7_int32_t5_float11_hex_t_Error hex_try_1 = hex_f_m3_app_read_number();",
		"hex_union_7_int32_t5_float hex_try_result_2;",
		"switch (hex_try_1.tag) {",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("main.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestTryErrorReturnType(t *testing.T) {
	result := compileSource("fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun fallback(): Int32 | Error\n    count: Int32 = try read_count()\n    return count\nend\nfun demo(): Int32 | Error\n    return fallback()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestErrdeferRunsOnErrorReturn(t *testing.T) {
	result := compileSource("fun cleanup(value: Int32)\nend\nfun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error\n    errdefer cleanup(1)\n    defer cleanup(2)\n    count: Int32 = try read_count()\n    return count\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The try error path unwinds with errorExit=true: both the errdefer and
	// the defer run.
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_cleanup(hex_defer_capture_1);") || !strings.Contains(rootC(t, result), "hex_f_m3_app_cleanup(hex_defer_capture_2);") {
		t.Fatalf("main.c = %q, want errdefer and defer on the error path", rootC(t, result))
	}
}

func TestErrdeferSkippedOnSuccessReturn(t *testing.T) {
	result := compileSource("fun cleanup(value: Int32)\nend\nfun demo(): Int32 | Error\n    errdefer cleanup(1)\n    defer cleanup(2)\n    return 7\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The success return classifies the exit: the defer runs unconditionally
	// and the errdefer is guarded by the runtime Error test.
	if !strings.Contains(rootC(t, result), "const bool hex_err_2 = (hex_return_1.tag == hex_union_7_int32_t11_hex_t_Error_tag_member_1);") {
		t.Fatalf("main.c = %q, want success exit classification", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "if (hex_err_2) {") {
		t.Fatalf("main.c = %q, want guarded errdefer", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_cleanup(hex_defer_capture_2);") {
		t.Fatalf("main.c = %q, defer must run on success", rootC(t, result))
	}
}

func TestErrdeferRuntimeUnionReturn(t *testing.T) {
	result := compileSource("fun cleanup(value: Int32)\nend\nfun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error\n    errdefer cleanup(1)\n    result: Int32 | Error = read_count()\n    if release\n        return result\n    end\n    return 3\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "const bool hex_err_") {
		t.Fatalf("main.c = %q, want runtime exit classification", rootC(t, result))
	}
}

func TestErrorDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo(): Int32 | Error\n    value: Int32 = 1\n    bad: Int32 = try value\nend", "try requires a union containing Error"},
		{"fun demo(): Int32\n    value: Int32 = 1\n    bad: Int32 = try value\nend", "try requires an enclosing function whose result accepts Error"},
		{"fun read_count(): Int32 | Error\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32\n    defer try read_count()\n    return 1\nend", "try is not permitted inside defer or errdefer"},
		{"fun demo(): Int32\n    errdefer cleanup()\n    return 1\nend\nfun cleanup()\nend", "errdefer requires an enclosing function whose result accepts Error"},
		{"fun demo()\n    err: Error = Error { file = \"x\", line = 1, column = 1, header = \"h\", message = \"m\" }\nend", "Error must be created with Error.new(header, message)"},
		{"fun demo()\n    err: Error = Error.new(1, \"m\")\nend", "Error.new expects header: Strand and message: String"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
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
	result := compileSource("fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error\n    mut total: Int32 = 0\n    while true do\n        total = total + try read_count()\n        break\n    end\n    if total > 0\n        total = total + try read_count()\n    end\n    return total\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestErrdeferAtRootScope(t *testing.T) {
	bad := "errdefer cleanup()\nfun cleanup()\nend\n"
	result := compileSource(bad)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 ||
		!strings.Contains(result.Stderr[0], "errdefer requires an enclosing function whose result accepts Error") {
		t.Fatalf("want root errdefer Type Error, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
	for _, diagnostic := range result.Stderr {
		if strings.Contains(diagnostic, "Unknown Error") {
			t.Fatalf("root errdefer must not be Unknown Error: %v", result.Stderr)
		}
	}
	// Function-scoped errdefer remains valid, and root defer is unchanged.
	valid := "fun cleanup()\nend\nfun run(): Int32 | Error\n    errdefer cleanup()\n    return 1\nend\n"
	if result := compileSource(valid); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("function errdefer must compile: %v", result.Stderr)
	}
	rootDefer := "fun cleanup()\nend\ndefer cleanup()\n"
	if result := compileSource(rootDefer); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("root defer must still compile: %v", result.Stderr)
	}
}
