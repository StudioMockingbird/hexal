package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestViewFromPointerCompiles(t *testing.T) {
	source := "fun total(data: Ptr<Int32>, count: Size): Int32 do\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\n    mut sum: Int32 = 0\n    for value in items do\n        sum = sum + value\n    end\n    return sum\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "(hex_view_Int32){") {
		t.Fatalf("generated C lacks the View descriptor initialization:\n%s", rootC(t, result))
	}
}

func TestViewFromPointerAcceptsMutPtr(t *testing.T) {
	source := "fun total(data: MutPtr<Int32>, count: Size): Int32 do\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\n    return items[0]\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestViewEmptyCompiles(t *testing.T) {
	source := "fun empty_demo(): View<Int32> do\n    return View<Int32>.empty()\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "NULL, 0") {
		t.Fatalf("generated C lacks the empty descriptor:\n%s", rootC(t, result))
	}
}

func TestViewFromPointerRequiresMatchingPointer(t *testing.T) {
	source := "fun bad(data: Ptr<Float64>, count: Size) do\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires Ptr<Int32> or MutPtr<Int32>") {
		t.Fatalf("want pointer-type diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestViewFromPointerRejectsNullablePointer(t *testing.T) {
	source := "fun bad(data: Ptr<Int32> | Nil, count: Size) do\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "must be narrowed") {
		t.Fatalf("want nullable diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestViewFromPointerRejectsNonSizeLength(t *testing.T) {
	source := "fun bad(data: Ptr<Int32>, count: Int64) do\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Size") {
		t.Fatalf("want length diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestViewFromPointerRejectsStringPointee(t *testing.T) {
	// The source fails because Ptr<String> is an invalid pointee, not
	// because View<String> is invalid.
	source := "fun bad(data: Ptr<String>, count: Size) do\n    items: View<String> = View<String>.from_pointer(data, count)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "could not construct pointer type") {
		t.Fatalf("want pointee diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestFromPointerRejectsStackRoots(t *testing.T) {
	rejected := []string{
		"fun f() do\n    mut value: Int32 = 1\n    view: View<Int32> = View<Int32>.from_pointer(ref value, 1)\nend\n",
		"fun f() do\n    value: Int32 = 1\n    p: Ptr<Int32> = ref value\n    view: View<Int32> = View<Int32>.from_pointer(p, 1)\nend\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want reject, got accept:\n%s", source)
		}
	}
	accepted := []string{
		"fun f(h: Heap) do\n    p: MutPtr<Int32> = h.allocate<Int32>(0)\n    view: View<Int32> = View<Int32>.from_pointer(p, 1)\nend\n",
		"fun wrap(p: Ptr<Int32>, n: Size): View<Int32> do\n    return View<Int32>.from_pointer(p, n)\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
	// Documented caller-side hole: wrap(ref local, 1) is not caught because
	// the callee sees an opaque parameter; lifetime safety is the caller's
	// responsibility at the from_pointer trust boundary.
	caller := "fun wrap(p: Ptr<Int32>, n: Size): View<Int32> do\n    return View<Int32>.from_pointer(p, n)\nend\nfun f() do\n    value: Int32 = 1\n    view: View<Int32> = wrap(ref value, 1)\nend\n"
	if result := compileSource(caller); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("caller-side from_pointer hole must compile by design: %v", result.Stderr)
	}
}
