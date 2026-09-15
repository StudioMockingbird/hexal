package integration

import (
	"strings"
	"testing"
)

// The permission region is an ordinary lexical scope: bindings declared inside
// are unavailable afterward, while an outer mutable binding assigned inside
// keeps its value.
func TestUnsafeBlockIsALexicalScope(t *testing.T) {
	assertRejects(t,
		"fun demo(): Int32 do\n    unsafe do\n        inner: Int32 := 1\n    end\n    return inner\nend\n",
		"unknown variable inner")
	assertCompiles(t,
		"fun demo(): Int32 do\n    mut total: Int32 := 0\n    unsafe do\n        total = 7\n    end\n    return total\nend\n")
}

func TestUnsafeBlockNests(t *testing.T) {
	assertCompiles(t,
		"fun demo(p: Ptr<Int32>, n: Size): Size do\n    unsafe do\n        unsafe do\n            view: Slice<Int32> := Slice<Int32>.from_pointer(p, n)\n            return view.length()\n        end\n    end\nend\n")
}

// An empty or capability-free region is valid and warns about nothing.
func TestEmptyUnsafeBlockCompiles(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Int32 do\n    unsafe do\n    end\n    unsafe do\n        x: Int32 := 1\n    end\n    return 0\nend\n")
	if len(result.Stderr) != 0 {
		t.Fatalf("capability-free unsafe block produced diagnostics: %v", result.Stderr)
	}
}

// Operations invalid regardless of safety keep their ordinary diagnostics
// inside the region.
func TestOrdinaryDiagnosticsSurviveInsideUnsafe(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"fun demo() do\n    unsafe do\n        bad: Int32 := true\n    end\nend\n", "expected Int32 initializer"},
		{"fun demo() do\n    unsafe do\n        values: Array<Int32, 2> := [1, 2]\n        bad: Int32 := values[5]\n    end\nend\n", "out of bounds"},
		{"fun demo(h: Heap) do\n    unsafe do\n        p: Ptr<mut Int32> := h.allocate<Int32>(1)\n        h.free(p)\n        h.free(p)\n    end\nend\n", "already released"},
		{"fun demo() do\n    unsafe do\n        bad: Int32 := missing\n    end\nend\n", "unknown variable missing"},
	} {
		assertRejects(t, testCase.source, testCase.want)
	}
}

// Stash, Pool, Slice.empty, and ordinary collection slicing gain no unsafe
// requirement.
func TestSafeOperationsDoNotRequireUnsafe(t *testing.T) {
	assertCompiles(t, "fun demo(): Slice<Int32> do\n    return Slice<Int32>.empty()\nend\n")
	assertCompiles(t, "fun demo(): Int32 do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    tail: Slice<Int32> := view.slice(1, 2)\n    return tail[0]\nend\n")
	assertCompiles(t, "type Node is struct amount: Int32 end\nfun demo(): Int32 do\n    stash := Stash<Node>()\n    defer stash.destroy()\n    first: Ptr<mut Node> := stash.allocate(Node(amount = 1))\n    result: Int32 := (^first).amount\n    stash.reset()\n    return result\nend\n")
	assertCompiles(t, "type Node is struct amount: Int32 end\nfun demo(): Int32 do\n    pool := Pool<Node>(2)\n    defer pool.destroy()\n    slot: Ptr<mut Node> := pool.allocate(Node(amount = 1))\n    pool.free(slot)\n    return 0\nend\n")
}

// Permission is checked where the operation is written, not where a deferred
// action later runs.
func TestDeferredUnsafeOperationFollowsItsWrittenPosition(t *testing.T) {
	assertCompiles(t,
		"fun consume(view: Slice<Int32>) do\nend\nfun demo(p: Ptr<Int32>, n: Size) do\n    unsafe do\n        defer consume(Slice<Int32>.from_pointer(p, n))\n    end\nend\n")
	assertRejects(t,
		"fun consume(view: Slice<Int32>) do\nend\nfun demo(p: Ptr<Int32>, n: Size) do\n    defer consume(Slice<Int32>.from_pointer(p, n))\nend\n",
		"Slice.from_pointer requires an unsafe do ... end block")
}

// The region emits no runtime artifact: the enclosed statements lower exactly
// as they would outside it, with no brace, guard, flag, or marker of its own.
func TestUnsafeBlockEmitsNoRuntimeArtifact(t *testing.T) {
	plain := assertCompiles(t, "fun demo(): Int32 do\n    mut total: Int32 := 0\n    total = total + 1\n    return total\nend\n")
	fenced := assertCompiles(t, "fun demo(): Int32 do\n    mut total: Int32 := 0\n    unsafe do\n        total = total + 1\n    end\n    return total\nend\n")
	strip := func(text string) string {
		lines := strings.Split(text, "\n")
		kept := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#line") {
				continue
			}
			kept = append(kept, line)
		}
		return strings.Join(kept, "\n")
	}
	if strip(rootC(t, plain)) != strip(rootC(t, fenced)) {
		t.Fatalf("unsafe lowering differs beyond source mapping:\n--- plain ---\n%s\n--- fenced ---\n%s", rootC(t, plain), rootC(t, fenced))
	}
	if strings.Contains(strings.ToLower(rootC(t, fenced)), "unsafe") {
		t.Fatalf("generated C mentions unsafe:\n%s", rootC(t, fenced))
	}
}

// A defer registered inside the region runs at the region's exit, exactly like
// any other block scope.
func TestUnsafeBlockOwnsItsDeferredActions(t *testing.T) {
	result := assertCompiles(t,
		"fun cleanup() do\nend\nfun demo(): Int32 do\n    unsafe do\n        defer cleanup()\n    end\n    return 0\nend\n")
	generated := rootC(t, result)
	if !strings.Contains(generated, "hex_f_m3_app_cleanup()") {
		t.Fatalf("generated C lacks the region's deferred call:\n%s", generated)
	}
}
