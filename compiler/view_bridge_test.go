package compiler

import (
	"strings"
	"testing"
)

// RFC 0043 integration tests: View<T>.from_pointer and View<T>.empty().

func TestViewFromPointerCompiles(t *testing.T) {
	source := "fun total(data: Ptr<Int32>, count: Size): Int32\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\n    mut sum: Int32 = 0\n    for value in items do\n        sum = sum + value\n    end\n    return sum\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "(sw_view_Int32){") {
		t.Fatalf("generated C lacks the View descriptor initialization:\n%s", result.MainC)
	}
}

func TestViewFromPointerAcceptsMutPtr(t *testing.T) {
	source := "fun total(data: MutPtr<Int32>, count: Size): Int32\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\n    return items[0]\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestViewEmptyCompiles(t *testing.T) {
	source := "fun empty_demo(): View<Int32>\n    return View<Int32>.empty()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "NULL, 0") {
		t.Fatalf("generated C lacks the empty descriptor:\n%s", result.MainC)
	}
}

func TestViewFromPointerRequiresMatchingPointer(t *testing.T) {
	source := "fun bad(data: Ptr<Float64>, count: Size)\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires Ptr<Int32> or MutPtr<Int32>") {
		t.Fatalf("want pointer-type diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestViewFromPointerRejectsNullablePointer(t *testing.T) {
	source := "fun bad(data: Ptr<Int32> | Nil, count: Size)\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "must be narrowed") {
		t.Fatalf("want nullable diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestViewFromPointerRejectsNonSizeLength(t *testing.T) {
	source := "fun bad(data: Ptr<Int32>, count: Int64)\n    items: View<Int32> = View<Int32>.from_pointer(data, count)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Size") {
		t.Fatalf("want length diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestViewFromPointerRejectsManagedElements(t *testing.T) {
	source := "fun bad(data: Ptr<String>, count: Size)\n    items: View<String> = View<String>.from_pointer(data, count)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("want element diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}
