package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0030: the print builtin.

func TestPrintScalars(t *testing.T) {
	result := compileSource("fun demo()\n    print(\"count = \", 42, \"\\n\")\n    print(true, false, nil)\n    print(1.5, -2.5, 3, -3)\n    letter: Rune = (65).to<Rune>()\n    print(letter)\n    size: Size = 7\n    print(size)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static void hex_print_bytes(const uint8_t *data, size_t length) {",
		"hex_print_int32(hex_print_arg_2);",
		"hex_print_bool(hex_print_arg_4);",
		"hex_print_bool(hex_print_arg_5);",
		"hex_print_nil();",
		"hex_print_float64(hex_print_arg_7);",
		"hex_print_rune(hex_print_arg_11);",
		"hex_print_size(hex_print_arg_12);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestPrintStringsDirectAndNested(t *testing.T) {
	result := compileSource("type Point = {\n    x: Int32,\n    y: Int32,\n}\nfun demo(h: Heap)\n    text: String = \"hello\"\n    print(text)\n    names: List<Int32> = List<Int32>.new(h)\n    defer names.free(h)\n    names.push(1)\n    print(names)\n    point: Point = Point { x = 10, y = 20 }\n    print(point)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_print_text(hex_print_arg_1->data, hex_print_arg_1->byte_length);",
		"static void hex_print_nested_hex_list_Int32(const void *value) {",
		"hex_print_text((const uint8_t *)\"[\", 1);",
		"static void hex_print_nested_hex_t_3_app_Point(const void *value) {",
		"hex_print_text((const uint8_t *)\"Point { \", 8);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestPrintNestedStringQuoting(t *testing.T) {
	result := compileSource("fun demo(h: Heap)\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"hello\")\n    print(names)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static void hex_print_nested_hex_list_String(const void *value) {",
		"static void hex_print_nested_hex_string(const void *value) {",
		"hex_print_quoted_text(text->data, text->byte_length);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestPrintError(t *testing.T) {
	result := compileSource("fun demo()\n    err: Error = Error.new(\"File Error\", \"file not found\")\n    print(err)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_print_error_direct") || !strings.Contains(rootC(t, result), "hex_print_error_direct(hex_print_arg_1);") {
		t.Fatalf("generated output = %q %q, want direct Error print", rootC(t, result), rootH(t, result))
	}
}

func TestPrintDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    print()\nend", "print expects at least 1 argument"},
		{"fun demo()\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    print(pointer)\nend", "print does not support Ptr<Int32>"},
		{"type Node = {\n    value: Int32,\n    next: Ptr<Int32>,\n}\nfun demo()\n    value: Int32 = 1\n    node: Node = Node { value = 1, next = ref value }\n    print(node)\nend", "print does not support Node because next is Ptr<Int32>"},
		{"fun demo()\n    value: Int32 | Float32 = 1\n    print(value)\nend", "print does not support Int32 | Float32; narrow or match it first"},
		{"fun demo()\n    stream: Stream<Int32> = Stream<Int32>.new()\n    print(stream)\nend", "print does not support Stream<Int32>"},
		{"fun demo()\n    heap: Heap = Heap.new()\n    print(heap)\nend", "print does not support Heap"},
		{"fun f(): Int32 | Error\n    file: File = try File.open(\"x\", FileMode.Read)\n    print(file)\n    return 0\nend", "print does not support File"},
		{"fun worker(): Bool\n    return true\nend\nfun f(h: Heap): Int32 | Error\n    task: Task<Bool> = try spawn worker()\n    print(task)\n    return 0\nend", "print does not support Task<Bool>"},
		{"fun f(h: Heap): Int32 | Error\n    channel: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    print(channel)\n    return 0\nend", "print does not support Channel<Int32>"},
		{"fun f(h: Heap): Int32 | Error\n    mutex: Mutex = try Mutex.new(h)\n    print(mutex)\n    return 0\nend", "print does not support Mutex"},
		{"counter: Atomic<Int32> = Atomic<Int32>.new(0)\nprint(counter)", "print does not support Atomic<Int32>"},
		{"fun helper()\nend\nprint(helper)", "print does not support Fun<()>"},
		{"type Inner = {\n    next: Ptr<Int32>,\n}\ntype Outer = {\n    inner: Inner,\n}\nfun demo()\n    mut value: Int32 = 1\n    outer: Outer = Outer { inner = Inner { next = ref value } }\n    print(outer)\nend", "print does not support Outer because inner is Inner"},
		{"print: Int32 = 1", "print is a protected built-in name"},
		{"fun print()\nend", "print is a protected built-in name"},
		{"fun demo()\n    step: Int32 | EoS = 1\n    print(step)\nend", "print does not support Int32 | EoS"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPrintNoResult(t *testing.T) {
	// RFC 0048: the destination is otherwise valid, so failure proves that
	// print produces no value rather than that standalone Nil is invalid.
	result := compileSource("fun demo()\n    bad: Int32 = print(\"x\")\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "print produces no value") {
		t.Fatalf("Compile stderr = %#v, want no-result rejection", result.Stderr)
	}
}

func TestPrintDeferred(t *testing.T) {
	result := compileSource("fun demo()\n    defer print(\"leaving\\n\")\n    text: String = \"early\"\n    defer print(text)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_defer_capture_2 = hex_v_text;",
		"hex_print_text(hex_defer_capture_1->data, hex_defer_capture_1->byte_length);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("main.c = %q, want %q", rootC(t, result), want)
		}
	}
}
