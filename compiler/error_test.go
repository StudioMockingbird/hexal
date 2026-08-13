package compiler

import (
	"strings"
	"testing"
)

// RFC 0029: Error values, T | Error results, try propagation, and errdefer.

func TestErrorNewConstruction(t *testing.T) {
	result := Compile("fun demo()\n    err: Error = Error.new(\"File Error\", \"file not found\")\n    header: Strand = err.header\n    message: String = err.message\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
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
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestTryExpression(t *testing.T) {
	result := Compile("fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error\n    count: Int32 = try read_count()\n    total: Int32 = count + try read_count()\n    return total\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const hex_internal_union_1 hex_try_1 = hex_f_read_count();",
		"if (hex_try_1.tag == hex_internal_union_1_tag_member_1) {",
		"return (hex_internal_union_1){ .tag = hex_internal_union_1_tag_member_1, .payload.member_1 = hex_try_1.payload.member_1 };",
		"hex_v_count = hex_try_1.payload.member_0;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestTryMultipleSuccessMembers(t *testing.T) {
	result := Compile("fun read_number(): Int32 | Float32 | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    value: Int32 | Float32 = try read_number()\n    return 1\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const hex_internal_union_1 hex_try_1 = hex_f_read_number();",
		"hex_internal_union_3 hex_try_result_2;",
		"switch (hex_try_1.tag) {",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestTryErrorReturnType(t *testing.T) {
	result := Compile("fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun fallback(): Int32 | Error\n    count: Int32 = try read_count()\n    return count\nend\nfun demo(): Int32 | Error\n    return fallback()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

func TestErrdeferRunsOnErrorReturn(t *testing.T) {
	result := Compile("fun cleanup(value: Int32)\nend\nfun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error\n    errdefer cleanup(1)\n    defer cleanup(2)\n    count: Int32 = try read_count()\n    return count\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	// The try error path unwinds with errorExit=true: both the errdefer and
	// the defer run.
	if !strings.Contains(result.MainC, "hex_f_cleanup(hex_defer_capture_1);") || !strings.Contains(result.MainC, "hex_f_cleanup(hex_defer_capture_2);") {
		t.Fatalf("main.c = %q, want errdefer and defer on the error path", result.MainC)
	}
}

func TestErrdeferSkippedOnSuccessReturn(t *testing.T) {
	result := Compile("fun cleanup(value: Int32)\nend\nfun demo(): Int32 | Error\n    errdefer cleanup(1)\n    defer cleanup(2)\n    return 7\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	// The success return classifies the exit: the defer runs unconditionally
	// and the errdefer is guarded by the runtime Error test.
	if !strings.Contains(result.MainC, "const bool hex_err_2 = (hex_return_1.tag == hex_internal_union_1_tag_member_1);") {
		t.Fatalf("main.c = %q, want success exit classification", result.MainC)
	}
	if !strings.Contains(result.MainC, "if (hex_err_2) {") {
		t.Fatalf("main.c = %q, want guarded errdefer", result.MainC)
	}
	if !strings.Contains(result.MainC, "hex_f_cleanup(hex_defer_capture_2);") {
		t.Fatalf("main.c = %q, defer must run on success", result.MainC)
	}
}

func TestErrdeferRuntimeUnionReturn(t *testing.T) {
	result := Compile("fun cleanup(value: Int32)\nend\nfun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error\n    errdefer cleanup(1)\n    result: Int32 | Error = read_count()\n    if release\n        return result\n    end\n    return 3\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "const bool hex_err_") {
		t.Fatalf("main.c = %q, want runtime exit classification", result.MainC)
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
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestErrorReservedName(t *testing.T) {
	result := Compile("type Error = { value: Int32, }")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("Compile stderr = %#v, want reserved-name rejection", result.Stderr)
	}
}

func TestTryInsideBranchAndLoop(t *testing.T) {
	result := Compile("fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(): Int32 | Error\n    mut total: Int32 = 0\n    while true do\n        total = total + try read_count()\n        break\n    end\n    if total > 0\n        total = total + try read_count()\n    end\n    return total\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

func TestErrdeferAtRootScope(t *testing.T) {
	bad := "errdefer cleanup()\nfun cleanup()\nend\n"
	result := Compile(bad)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 ||
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
	if result := Compile(valid); result.ExitCode != ExitSuccess {
		t.Fatalf("function errdefer must compile: %v", result.Stderr)
	}
	rootDefer := "fun cleanup()\nend\ndefer cleanup()\n"
	if result := Compile(rootDefer); result.ExitCode != ExitSuccess {
		t.Fatalf("root defer must still compile: %v", result.Stderr)
	}
}
