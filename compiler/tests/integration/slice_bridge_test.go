package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestSliceFromPointerCompiles(t *testing.T) {
	source := "fun total(data: Ptr<Int32>, count: Size): Int32 do\n    unsafe do\n        items: Slice<Int32> := Slice<Int32>.from_pointer(data, count)\n        mut sum: Int32 := 0\n        for value in items do\n            sum = sum + value\n        end\n        return sum\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "(hex_slice_Int32){") {
		t.Fatalf("generated C lacks the View descriptor initialization:\n%s", rootC(t, result))
	}
}

func TestSliceFromPointerAcceptsWritablePointers(t *testing.T) {
	source := "fun total(data: Ptr<mut Int32>, count: Size): Int32 do\n    unsafe do\n        items: Slice<Int32> := Slice<Int32>.from_pointer(data, count)\n        return items[0]\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

// from_pointer cannot prove the supplied region's length, lifetime,
// alignment, initialization, or provenance, so it is permitted only inside an
// explicit lexical permission region.
func TestSliceFromPointerRequiresUnsafeBlock(t *testing.T) {
	sources := []string{
		"fun bad(data: Ptr<Int32>, count: Size) do\n    items: Slice<Int32> := Slice<Int32>.from_pointer(data, count)\nend\n",
		"fun bad(data: Ptr<mut Int32>, count: Size) do\n    items: Slice<mut Int32> := Slice<mut Int32>.from_pointer(data, count)\nend\n",
	}
	for _, source := range sources {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 ||
			!strings.Contains(result.Stderr[0], "Slice.from_pointer requires an unsafe do ... end block") {
			t.Fatalf("want unsafe-permission diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

// A safe wrapper may hold the permission region internally; its callers stay
// ordinary safe code.
func TestSliceFromPointerWrapperKeepsCallersSafe(t *testing.T) {
	source := "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    unsafe do\n        return Slice<Int32>.from_pointer(p, n)\n    end\nend\nfun f() do\n    value: Int32 := 1\n    view: Slice<Int32> := wrap(@value, 1)\nend\n"
	if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("want accept; got %v", result.Stderr)
	}
}

// Permission is lexical: a callee written without its own region is rejected
// even when every caller is inside one.
func TestUnsafeDoesNotEscapeThroughCall(t *testing.T) {
	source := "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    return Slice<Int32>.from_pointer(p, n)\nend\nfun f() do\n    value: Int32 := 1\n    unsafe do\n        view: Slice<Int32> := wrap(@value, 1)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 ||
		!strings.Contains(result.Stderr[0], "Slice.from_pointer requires an unsafe do ... end block") {
		t.Fatalf("want unsafe-permission diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSliceEmptyCompiles(t *testing.T) {
	source := "fun empty_demo(): Slice<Int32> do\n    return Slice<Int32>.empty()\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "nullptr, 0") {
		t.Fatalf("generated C lacks the empty descriptor:\n%s", rootC(t, result))
	}
}

func TestSliceFromPointerRequiresMatchingPointer(t *testing.T) {
	source := "fun bad(data: Ptr<Float64>, count: Size) do\n    unsafe do\n        items: Slice<Int32> := Slice<Int32>.from_pointer(data, count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires Ptr<Int32> or Ptr<mut Int32>") {
		t.Fatalf("want pointer-type diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSliceFromPointerRejectsReadOnlyPointerForWritableSlice(t *testing.T) {
	source := "fun bad(data: Ptr<Int32>, count: Size) do\n    unsafe do\n        items: Slice<mut Int32> := Slice<mut Int32>.from_pointer(data, count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires Ptr<mut Int32>") {
		t.Fatalf("want pointer-mode diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSliceFromPointerRejectsNullablePointer(t *testing.T) {
	source := "fun bad(data: Ptr<Int32> | Nil, count: Size) do\n    unsafe do\n        items: Slice<Int32> := Slice<Int32>.from_pointer(data, count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "must be narrowed") {
		t.Fatalf("want nullable diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSliceFromPointerRejectsNonSizeLength(t *testing.T) {
	source := "fun bad(data: Ptr<Int32>, count: Int64) do\n    unsafe do\n        items: Slice<Int32> := Slice<Int32>.from_pointer(data, count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Size") {
		t.Fatalf("want length diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSliceFromPointerRejectsStringPointee(t *testing.T) {
	// The source fails because Ptr<String> is an invalid pointee, not
	// because Slice<String> is invalid.
	source := "fun bad(data: Ptr<String>, count: Size) do\n    unsafe do\n        items: Slice<String> := Slice<String>.from_pointer(data, count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "could not construct pointer type") {
		t.Fatalf("want pointee diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestFromPointerAcceptsAllRoots(t *testing.T) {
	accepted := []string{
		"fun f() do\n    mut value: Int32 := 1\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(@value, 1)\n    end\nend\n",
		"fun f() do\n    value: Int32 := 1\n    p: Ptr<Int32> := @value\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
		"fun f() do\n    mut value: Int32 := 1\n    mut p: Ptr<Int32> := @value\n    mut q: Ptr<Int32> := p\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(q, 1)\n    end\nend\n",
		"fun f() do\n    value: Int32 := 1\n    mut p: Ptr<Int32> := @value\n    mut q: Ptr<Int32> := p\n    mut r: Ptr<Int32> := q\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(r, 1)\n    end\nend\n",
		"fun f() do\n    mut value: Int32 := 1\n    mut p: Ptr<Int32> := @value\n    mut q: Ptr<Int32> := p\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(q, 1)\n        p = @value\n        view2: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
		"fun f(h: Heap) do\n    mut value: Int32 := 1\n    mut p: Ptr<Int32> := h.allocate<Int32>(0)\n    p = @value\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
		"fun f(h: Heap) do\n    p: Ptr<mut Int32> := h.allocate<Int32>(0)\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
		"fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    unsafe do\n        return Slice<Int32>.from_pointer(p, n)\n    end\nend\n",
		"fun f(h: Heap) do\n    p: Ptr<mut Int32> := h.allocate<Int32>(0)\n    q: Ptr<mut Int32> := p\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(q, 1)\n    end\nend\n",
		"fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    q: Ptr<Int32> := p\n    unsafe do\n        return Slice<Int32>.from_pointer(q, n)\n    end\nend\n",
		"fun f(h: Heap) do\n    mut value: Int32 := 1\n    mut p: Ptr<Int32> := @value\n    p = h.allocate<Int32>(0)\n    unsafe do\n        view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
	// from_pointer through a parameter is equally unchecked: backing storage
	// validity is the caller's responsibility at the trust boundary.
	caller := "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    unsafe do\n        return Slice<Int32>.from_pointer(p, n)\n    end\nend\nfun f() do\n    value: Int32 := 1\n    view: Slice<Int32> := wrap(@value, 1)\nend\n"
	if result := compileSource(caller); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("caller-side from_pointer must compile by design: %v", result.Stderr)
	}
}
