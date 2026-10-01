package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestPointerToSliceCompiles(t *testing.T) {
	source := "fun total(data: Ptr<Int32>, count: Size): Int32 do\n    unsafe do\n        let items: Slice<Int32> = data.to_slice(count)\n        let mut sum: Int32 = 0\n        for value in items do\n            sum = sum + value\n        end\n        return sum\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "(hex_slice_Int32){") {
		t.Fatalf("generated C lacks the View descriptor initialization:\n%s", rootC(t, result))
	}
}

func TestPointerToSliceAcceptsWritablePointers(t *testing.T) {
	source := "fun total(data: Ptr<mut Int32>, count: Size): Int32 do\n    unsafe do\n        let items: Slice<Int32> = data.to_slice(count)\n        return items[0]\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

// from_pointer cannot prove the supplied region's length, lifetime,
// alignment, initialization, or provenance, so it is permitted only inside an
// explicit lexical permission region.
func TestPointerToSliceRequiresUnsafeBlock(t *testing.T) {
	sources := []string{
		"fun bad(data: Ptr<Int32>, count: Size) do\n    let items: Slice<Int32> = data.to_slice(count)\nend\n",
		"fun bad(data: Ptr<mut Int32>, count: Size) do\n    let items: Slice<mut Int32> = data.to_slice(count)\nend\n",
	}
	for _, source := range sources {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 ||
			!strings.Contains(result.Stderr[0], "Ptr.to_slice requires an unsafe do ... end block") {
			t.Fatalf("want unsafe-permission diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

// A safe wrapper may hold the permission region internally; its callers stay
// ordinary safe code.
func TestPointerToSliceWrapperKeepsCallersSafe(t *testing.T) {
	source := "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    unsafe do\n        return p.to_slice(n)\n    end\nend\nfun f() do\n    let value: Int32 = 1\n    let view: Slice<Int32> = wrap(@value, 1)\nend\n"
	if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("want accept; got %v", result.Stderr)
	}
}

// Permission is lexical: a callee written without its own region is rejected
// even when every caller is inside one.
func TestUnsafeDoesNotEscapeThroughCall(t *testing.T) {
	source := "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    return p.to_slice(n)\nend\nfun f() do\n    let value: Int32 = 1\n    unsafe do\n        let view: Slice<Int32> = wrap(@value, 1)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 ||
		!strings.Contains(result.Stderr[0], "Ptr.to_slice requires an unsafe do ... end block") {
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

func TestPointerToSliceRequiresMatchingPointer(t *testing.T) {
	source := "fun bad(data: Ptr<Float64>, count: Size) do\n    unsafe do\n        let items: Slice<Int32> = data.to_slice(count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected Slice<Int32> initializer; got Slice<Float64>") {
		t.Fatalf("want pointer-type diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestPointerToSliceRejectsReadOnlyPointerForWritableSlice(t *testing.T) {
	source := "fun bad(data: Ptr<Int32>, count: Size) do\n    unsafe do\n        let items: Slice<mut Int32> = data.to_slice(count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected Slice<mut Int32> initializer; got Slice<Int32>") {
		t.Fatalf("want pointer-mode diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestPointerToSliceRejectsNullablePointer(t *testing.T) {
	source := "fun bad(data: Ptr<Int32> | Nil, count: Size) do\n    unsafe do\n        let items: Slice<Int32> = data.to_slice(count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "narrow it before dereferencing") {
		t.Fatalf("want nullable diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestPointerToSliceRejectsNonSizeLength(t *testing.T) {
	source := "fun bad(data: Ptr<Int32>, count: Int64) do\n    unsafe do\n        let items: Slice<Int32> = data.to_slice(count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Size") {
		t.Fatalf("want length diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestPointerToSliceRejectsStringPointee(t *testing.T) {
	// The source fails because Ptr<String> is an invalid pointee, not
	// because Slice<String> is invalid.
	source := "fun bad(data: Ptr<String>, count: Size) do\n    unsafe do\n        let items: Slice<String> = data.to_slice(count)\n    end\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "could not construct pointer type") {
		t.Fatalf("want pointee diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestPointerToSliceAcceptsAllRoots(t *testing.T) {
	accepted := []string{
		"fun f() do\n    let mut value: Int32 = 1\n    unsafe do\n        let view: Slice<Int32> = (@value).to_slice(1)\n    end\nend\n",
		"fun f() do\n    let value: Int32 = 1\n    let p: Ptr<Int32> = @value\n    unsafe do\n        let view: Slice<Int32> = p.to_slice(1)\n    end\nend\n",
		"fun f() do\n    let mut value: Int32 = 1\n    let mut p: Ptr<Int32> = @value\n    let mut q: Ptr<Int32> = p\n    unsafe do\n        let view: Slice<Int32> = q.to_slice(1)\n    end\nend\n",
		"fun f() do\n    let value: Int32 = 1\n    let mut p: Ptr<Int32> = @value\n    let mut q: Ptr<Int32> = p\n    let mut r: Ptr<Int32> = q\n    unsafe do\n        let view: Slice<Int32> = r.to_slice(1)\n    end\nend\n",
		"fun f() do\n    let mut value: Int32 = 1\n    let mut p: Ptr<Int32> = @value\n    let mut q: Ptr<Int32> = p\n    unsafe do\n        let view: Slice<Int32> = q.to_slice(1)\n        p = @value\n        let view2: Slice<Int32> = p.to_slice(1)\n    end\nend\n",
		"fun f(h: Heap) do\n    let mut value: Int32 = 1\n    let mut p: Ptr<Int32> = h.allocate<Int32>(0)\n    p = @value\n    unsafe do\n        let view: Slice<Int32> = p.to_slice(1)\n    end\nend\n",
		"fun f(h: Heap) do\n    let p: Ptr<mut Int32> = h.allocate<Int32>(0)\n    unsafe do\n        let view: Slice<Int32> = p.to_slice(1)\n    end\nend\n",
		"fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    unsafe do\n        return p.to_slice(n)\n    end\nend\n",
		"fun f(h: Heap) do\n    let p: Ptr<mut Int32> = h.allocate<Int32>(0)\n    let q: Ptr<mut Int32> = p\n    unsafe do\n        let view: Slice<Int32> = q.to_slice(1)\n    end\nend\n",
		"fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    let q: Ptr<Int32> = p\n    unsafe do\n        return q.to_slice(n)\n    end\nend\n",
		"fun f(h: Heap) do\n    let mut value: Int32 = 1\n    let mut p: Ptr<Int32> = @value\n    p = h.allocate<Int32>(0)\n    unsafe do\n        let view: Slice<Int32> = p.to_slice(1)\n    end\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
	// from_pointer through a parameter is equally unchecked: backing storage
	// validity is the caller's responsibility at the trust boundary.
	caller := "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    unsafe do\n        return p.to_slice(n)\n    end\nend\nfun f() do\n    let value: Int32 = 1\n    let view: Slice<Int32> = wrap(@value, 1)\nend\n"
	if result := compileSource(caller); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("caller-side from_pointer must compile by design: %v", result.Stderr)
	}
}
