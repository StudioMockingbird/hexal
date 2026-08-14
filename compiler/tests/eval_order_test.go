package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0049 item 8.3: every argument position evaluates each argument exactly
// once, in source order. Hexal functions cannot mutate module data, so
// once-and-left-to-right is proven at compile time by counting call sites in
// the generated C; the print output order is proven at runtime in the C23
// suite (c23_print_smoke_test.go).

func TestArgumentsEvaluatedOnceInSourceOrder(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		count  int
	}{
		{"array literal elements", "fun bump(): Int32\n    return 1\nend\nfun f()\n    values: Array<Int32, 2> = [bump(), bump()]\nend\n", 2},
		{"print arguments", "fun bump(): Int32\n    return 1\nend\nfun f()\n    print(bump(), bump())\nend\n", 2},
		{"spawn arguments", "fun bump(): Int32\n    return 1\nend\nfun worker(a: Int32, b: Int32): Bool\n    return a == b\nend\nfun f(h: Heap): Int32 | Error\n    task: Task<Bool> = try spawn worker(bump(), bump())\n    return 0\nend\n", 2},
		{"from_pointer length", "fun bump(): Size\n    return 1\nend\nfun f(h: Heap)\n    p: MutPtr<Int32> = h.allocate<Int32>(1)\n    view: View<Int32> = View<Int32>.from_pointer(p, bump())\nend\n", 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compiler.Compile(testCase.source)
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
			}
			if got := strings.Count(result.MainC, "hex_f_bump()"); got != testCase.count {
				t.Fatalf("main.c = %q, want exactly %d evaluations, got %d", result.MainC, testCase.count, got)
			}
		})
	}
}
