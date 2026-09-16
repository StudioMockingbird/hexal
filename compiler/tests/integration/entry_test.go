package integration

// The entry-module return: status recording, cleanup label, defer unwinding,
// status typing, and the unchanged function return.

import (
	"strings"
	"testing"
)

func TestRootReturnLowersToStatusAndCleanup(t *testing.T) {
	root := rootC(t, assertCompiles(t, "if true then\n    return 7\nend\nreturn\n"))
	for _, want := range []string{
		"uint8_t hex_exit_status = 0;",
		"hex_exit_status = (uint8_t)(7);",
		"goto hex_exit;",
		"hex_exit:\n    return (int)hex_exit_status;",
		// A bare root return and root fallthrough both record zero.
		"hex_exit_status = 0;",
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root module lacks %q:\n%s", want, root)
		}
	}
}

// A root return with no host-invocation demand keeps the unconditional
// int main(void) spelling.
func TestRootReturnKeepsMainVoid(t *testing.T) {
	root := rootC(t, assertCompiles(t, "return 2\n"))
	if !strings.Contains(root, "int main(void) {") || strings.Contains(root, "argc") {
		t.Errorf("a root-return-only program must keep int main(void):\n%s", root)
	}
}

// The status value is evaluated once, every active defer runs innermost to
// outermost, and control reaches the shared cleanup label.
func TestRootReturnUnwindsDefersInReverseOrder(t *testing.T) {
	source := "fun first() do\n    print(\"first\")\nend\n" +
		"fun second() do\n    print(\"second\")\nend\n" +
		"defer first()\n" +
		"if true then\n    defer second()\n    return 7\nend\n" +
		"return 3\n"
	root := rootC(t, assertCompiles(t, source))
	second := strings.Index(root, "hex_f_m3_app_second();")
	first := strings.Index(root, "hex_f_m3_app_first();")
	exit := strings.Index(root, "goto hex_exit;")
	if second < 0 || first < 0 || exit < 0 || !(second < first && first < exit) {
		t.Fatalf("root return must unwind innermost (second) then outermost (first) before the cleanup jump:\n%s", root)
	}
	// The later root return 3 renders after the first cleanup jump.
	if later := strings.Index(root, "hex_exit_status = (uint8_t)(3);"); later < exit {
		t.Errorf("the later status must follow the first cleanup jump:\n%s", root)
	}
}

func TestRootReturnUnderWhile(t *testing.T) {
	root := rootC(t, assertCompiles(t, "mut count: Int32 := 0\nwhile count < 3 do\n    count = count + 1\n    return 2\nend\nreturn 0\n"))
	loop := strings.Index(root, "while (hex_v_count < 3) {")
	jump := strings.Index(root, "goto hex_exit;")
	close := strings.Index(root, "}")
	if loop < 0 || jump < loop {
		t.Fatalf("a root return inside a while body must jump from inside the loop:\n%s", root)
	}
	if close < jump {
		t.Fatalf("the loop must close after the root return's cleanup jump:\n%s", root)
	}
}

func TestRootReturnStatusTyping(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"return true\n", "entry-module return requires UInt8; got Bool"},
		{"return 1.0\n", "entry-module return requires UInt8; got Float64"},
		{"return 256\n", "given value is outside the UInt8 range"},
		{"return -1\n", "negated integer literal requires a signed destination"},
	} {
		assertRejects(t, testCase.source, testCase.want)
	}
}

// Function returns keep their ordinary lowering; entry status rules do not
// leak into a declaration's own result contract.
func TestRootReturnKeepsFunctionReturnsUnchanged(t *testing.T) {
	root := rootC(t, assertCompiles(t, "fun f(): Int32 do\n    return 1\nend\nreturn 0\n"))
	if !strings.Contains(root, "return 1;") {
		t.Errorf("function return lowering changed:\n%s", root)
	}
	if !strings.Contains(root, "goto hex_exit;") {
		t.Errorf("root return did not lower to the cleanup jump:\n%s", root)
	}
}

// The root has no Error result, so try and errdefer stay rejected there.
func TestRootTryAndErrdeferRejected(t *testing.T) {
	assertRejects(t, "fun f(): Int32 | Error do\n    return 1\nend\nx := try f()\n",
		"try requires an enclosing function whose result accepts Error")
	assertRejects(t, "errdefer print(\"x\")\n",
		"errdefer requires an enclosing function whose result accepts Error")
}

// A union narrowed by an if whose alternative ends in a root return is used
// as its remaining member without further narrowing.
func TestRootReturnStatusSurvivesNarrowing(t *testing.T) {
	source := programImport +
		"args := Prog.arguments()\n" +
		"if args is Error then\n    return 1\nend\n" +
		"print(args.length())\n"
	root := rootC(t, assertCompiles(t, source))
	if !strings.Contains(root, "hex_v_args.payload.hex_m_Slice_String_)") {
		t.Errorf("the narrowed slice member was not used directly:\n%s", root)
	}
	if strings.Count(root, "hex_exit_status = (uint8_t)(1);") != 1 {
		t.Errorf("the root return must record status 1 exactly once:\n%s", root)
	}
}
