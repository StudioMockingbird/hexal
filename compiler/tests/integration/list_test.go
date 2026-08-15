package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0020 Phase D: List<T> — owning growable sequences, shallow String
// handles, explicit cleanup, and container-only cleanup.
// and the List<String> nested-String element rules.

func TestListLifecycle(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    count: Size = values.length()\n    empty: Bool = values.length() == 0\n    first: Int32 = values[0]\n    second: Int32 = values[1]\n    values[1] = 5\n    last: Int32 = values.pop()\n    values.clear()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_list_Int32 {",
		"int32_t *data;",
		"size_t length;",
		"hex_v_values = hex_list_new_Int32(hex_v_h);",
		"hex_list_push_Int32(hex_v_values, 1);",
		"(hex_v_values)->length",
		"(hex_v_values)->length == 0",
		"*hex_list_at_mut_Int32(hex_v_values, (size_t)(0))",
		"*hex_list_at_mut_Int32(hex_v_values, (size_t)(1)) = 5;",
		"hex_v_last = hex_list_pop_Int32(hex_v_values);",
		"hex_list_clear_Int32(hex_v_values);",
		"hex_list_free_Int32(hex_defer_capture_2, hex_defer_capture_1);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestListViewDerivationAndInvalidation(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    view: View<Int32> = values.slice(0, 2)\n    total: Int32 = view[0] + view[1]\n    values.set(0, 9)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "const hex_view_Int32 hex_v_view = hex_list_slice_Int32(hex_v_values, (size_t)(0), (size_t)(2));") {
		t.Fatalf("modules/app.c = %q, want list slice", rootC(t, result))
	}
}

// RFC 0035: views are plain descriptors; mutating or freeing the source List
// while a view is live is now the programmer's responsibility.
func TestListViewAfterStructuralMutationIsValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    view: View<Int32> = values.slice(0, 1)\n    values.push(1)\nend",
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    view: View<Int32> = values.slice(0, 1)\n    dropped: Int32 = values.pop()\nend",
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    view: View<Int32> = values.slice(0, 1)\n    values.clear()\nend",
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    view: View<Int32> = values.slice(0, 1)\n    values.free(h)\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestListShallowCopySemantics(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    other: List<Int32> = values\nend",
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\nend",
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    values.free(h)\n    values.free(h)\nend",
		"fun demo(h: Heap) do\n    List<Int32>.new(h)\nend",
		"fun demo(h: Heap) do\n    mut values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values = List<Int32>.new(h)\nend",
		"fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    values.free(h)\n    values.push(1)\nend",
		"fun demo(h: Heap, values: List<Int32>) do\n    values.free(h)\nend",
		"fun make_values(h: Heap, values: List<Int32>): List<Int32> do\n    return values\nend",
		"fun demo(h: Heap, release: Bool) do\n    values: List<Int32> = List<Int32>.new(h)\n    if release then\n        values.free(h)\n    end\n    values.push(1)\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestListBorrowParameterMutatesCaller(t *testing.T) {
	result := compileSource("fun append_default(values: List<Int32>) do\n    values.push(0)\nend\nfun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    append_default(values)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestListReturnHandoff(t *testing.T) {
	result := compileSource("fun make_values(h: Heap): List<Int32> do\n    values: List<Int32> = List<Int32>.new(h)\n    values.push(1)\n    return values\nend\nfun demo(h: Heap) do\n    values: List<Int32> = make_values(h)\n    values.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestListOfStrings(t *testing.T) {
	// RFC 0048: a stored literal is never freed by the collection or by a
	// pop; a runtime String popped out of the list is freed explicitly.
	result := compileSource("fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    runtime: String = \"bob\".to_string(h)\n    names.push(runtime)\n    names.set(0, \"carol\")\n    popped: String = names.pop()\n    popped.free(h)\n    first: String = names[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_list_push_String(hex_v_names, &hex_lit_0);",
		"hex_list_push_String(hex_v_names, hex_v_runtime);",
		"hex_list_set_String(hex_v_names, (size_t)(0), &hex_lit_2);",
		"hex_v_popped = hex_list_pop_String(hex_v_names);",
		"hex_string_free(hex_v_h, hex_v_popped);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

// RFC 0035: List<String> reads copy String handles; destructive operations
// while a read handle is live are the programmer's responsibility.
func TestListStringMutationAfterReadIsValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"a\")\n    first: String = names[0]\n    names.set(0, \"b\")\nend",
		"fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"a\")\n    first: String = names[0]\n    dropped: String = names.pop()\nend",
		"fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"a\")\n    first: String = names[0]\n    names.clear()\nend",
		"fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    names.push(\"a\")\n    first: String = names[0]\n    names.free(h)\nend",
		"fun demo(h: Heap) do\n    mut names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"a\")\n    first: String = names[0]\n    names = List<String>.new(h)\nend",
		"fun inspect(names: List<String>) do\nend\nfun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"a\")\n    first: String = names[0]\n    inspect(names)\nend",
		"fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"a\")\n    first: String = names[0]\n    names.push(\"b\")\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestListRestrictions(t *testing.T) {
	// RFC 0035: an object member List is an ordinary shallow handle.
	if result := compileSource("type Holder = { values: List<Int32>, }\nfun demo(h: Heap) do\n    holder: Holder = Holder { values = List<Int32>.new(h), }\n    holder.values.push(1)\nend"); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want 0", result.ExitCode, result.Stderr)
	}
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo() do\n    pointer: Ptr<List<Int32>> = nil\nend", "could not construct pointer type"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestListStringElementsAreShallow(t *testing.T) {
	source := "fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    text: String = \"hi\".to_string(h)\n    names.push(text)\n    names.free(h)\n    text.free(h)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if strings.Contains(rootC(t, result), "hex_string_from_bytes") {
		t.Fatalf("List<String> push must not deep-copy:\n%s", rootC(t, result))
	}
}

func TestListFreeReleasesOnlyContainerStorage(t *testing.T) {
	source := "fun demo(h: Heap) do\n    names: List<String> = List<String>.new(h)\n    names.free(h)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_list_free_String") {
		t.Fatalf("missing hex_list_free_String helper:\n%s", rootC(t, result))
	}
	if strings.Contains(rootC(t, result), "hex_string_free") {
		t.Fatalf("List<String> free must not destroy element Strings:\n%s", rootC(t, result))
	}
}
