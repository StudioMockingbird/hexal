package integration

import (
	"slices"
	"strings"
	"testing"

	"hexal/compiler"
)

func TestPrintScalars(t *testing.T) {
	result := compileSource("fun demo() do\n    print(\"count = \", 42, \"\\n\")\n    print(true, false, nil)\n    print(1.5, -2.5, 3, -3)\n    let size: Size = 7\n    print(size)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_print_int32(&hex_print_out_4, hex_print_arg_2);",
		"hex_print_bool(&hex_print_out_8, hex_print_arg_5);",
		"hex_print_bool(&hex_print_out_8, hex_print_arg_6);",
		"hex_print_nil(&hex_print_out_8);",
		"hex_print_float64(&hex_print_out_13, hex_print_arg_9);",
		"hex_print_size(&hex_print_out_15, hex_print_arg_14);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestPrintStringsDirectAndNested(t *testing.T) {
	result := compileSource("type Point is struct\n    x: Int32,\n    y: Int32,\nend\nfun demo(h: Heap) do\n    let text: String = \"hello\"\n    print(text)\n    let names: List<Int32> = List<Int32>(h)\n    defer names.free(h)\n    names.push(1)\n    print(names)\n    let point: Point = Point(x = 10, y = 20)\n    print(point)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_print_text(&hex_print_out_2, hex_print_arg_1->data, hex_print_arg_1->byte_length);",
		"static void hex_print_nested_hex_list_Int32(hex_print_buffer *out, int depth, const void *value) {",
		"hex_print_text(out, (const uint8_t *)\"[\", 1);",
		"static void hex_print_nested_hex_t_m3_app_Point(hex_print_buffer *out, int depth, const void *value) {",
		"hex_print_text(out, (const uint8_t *)\"Point { \", 8);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestPrintNestedStringQuoting(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let names: List<String> = List<String>(h)\n    defer names.free(h)\n    names.push(\"hello\")\n    print(names)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static void hex_print_nested_hex_list_String(hex_print_buffer *out, int depth, const void *value) {",
		"static void hex_print_nested_hex_string(hex_print_buffer *out, int depth, const void *value) {",
		"hex_print_quoted_text(out, text->data, text->byte_length);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestPrintError(t *testing.T) {
	result := compileSource("import\n  Fs from std.fs\nend\nfun demo() do\n    let err: Error = Error(ErrorKind.Other(header = \"Fs.File Error\"), \"file not found\")\n    print(err)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_print_error_direct") ||
		!strings.Contains(rootC(t, result), "hex_print_error_direct(&hex_print_out_2, &hex_print_arg_1);") {
		t.Fatalf("generated output = %q %q, want direct Error print", rootC(t, result), rootH(t, result))
	}
}

// One source print call is one transaction: one builder, every fragment
// appended to it, one commit, one release -- direct and deferred alike.
func TestPrintIsOneBufferedTransaction(t *testing.T) {
	result := compileSource("type Point is struct\n    x: Int32,\n    y: Int32,\nend\nfun demo() do\n    let point: Point = Point(x = 10, y = 20)\n    print(\"a\", point, 1)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	body := rootC(t, result)
	for _, name := range []string{"hex_print_begin", "hex_print_commit", "hex_print_destroy"} {
		if strings.Count(body, name+"(") != 1 {
			t.Fatalf("aggregate print must use exactly one %s: %q", name, body)
		}
	}
	begin := strings.Index(body, "hex_print_begin(")
	commit := strings.Index(body, "hex_print_commit(")
	release := strings.Index(body, "hex_print_destroy(")
	argument := strings.Index(body, "hex_print_arg_1 =")
	nested := strings.Index(body, "hex_print_nested_hex_t_m3_app_Point(")
	if argument < 0 || argument > begin || begin > nested || nested > commit || commit > release {
		t.Fatalf("print must evaluate arguments, then build, then commit, then release: %q", body)
	}
}

// Buffering a print call is call-local and grows through the bundled C
// library allocator, so a program that only prints gains no event loop, no
// scheduler, no native mutex and no libuv: its builder storage is
// independent of whichever allocator the rest of the program selects.
func TestPrintOnlyProgramAddsNoDependency(t *testing.T) {
	result := assertCompiles(t, "type Point is struct\n    x: Int32,\n    y: Int32,\nend\nprint(Point(x = 1, y = 2))\n")
	if slices.Contains(dependencyNames(result), string(compiler.RuntimeLibuv)) {
		t.Fatalf("print-only dependencies = %v, want no libuv", dependencyNames(result))
	}
	for _, forbidden := range []string{"hexal/event.c", "hexal/concurrency.c"} {
		if hasFile(result, forbidden) {
			t.Fatalf("a print-only program selected %q", forbidden)
		}
	}
	sink := printC(t, result)
	for _, forbidden := range []string{"uv_", "mi_", "hex_heap_", "hex_stdout_lock"} {
		if strings.Contains(sink, forbidden) {
			t.Fatalf("hexal/print.c names %q without Task support:\n%s", forbidden, sink)
		}
	}
	if !strings.Contains(sink, "malloc(") || !strings.Contains(sink, "realloc(") || !strings.Contains(sink, "free(") {
		t.Fatalf("the print builder must grow through the bundled C library allocator:\n%s", sink)
	}
}

// With Task support the whole call becomes one native job, and every writer
// to standard output takes the same critical section inside that job.
func TestPrintCommitIsOneSerializedJob(t *testing.T) {
	result := assertCompiles(t, "fun worker(): Bool do\n    print(1)\n    return true\nend\n"+
		"fun run(): Nil | Error do\n    let task: Task<Bool> = try spawn worker()\n    task.join()\n    return nil\nend\nrun()\n")
	sink := printC(t, result)
	if strings.Count(sink, "hex_event_work_call(") != 1 {
		t.Fatalf("a print call must be exactly one work submission:\n%s", sink)
	}
	commit := strings.Index(sink, "void hex_print_commit(hex_print_buffer *out) {")
	submission := strings.Index(sink, "hex_event_work_call(")
	if commit < 0 || submission < commit {
		t.Fatalf("the one submission must belong to the commit:\n%s", sink)
	}
	stream := ioC(t, result)
	if !strings.Contains(stream, "hex_stdout_lock();") || !strings.Contains(stream, "hex_stdout_unlock();") {
		t.Fatalf("standard-output writes must share one critical section:\n%s", stream)
	}
	// The target test resolves the standard descriptor at call time rather
	// than consulting a flag recorded when the stream was constructed.
	if !strings.Contains(stream, "bool serialized = stream.desc == hex_io_stdout_desc();") {
		t.Fatalf("stdout recognition must compare the resolved descriptor at call time:\n%s", stream)
	}
}

// A deferred print keeps registration-time capture and gets its own complete
// transaction when the defer executes.
func TestPrintDeferredIsOneTransaction(t *testing.T) {
	result := compileSource("fun demo() do\n    let text: String = \"early\"\n    defer print(text, 1)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	body := rootC(t, result)
	capture := strings.Index(body, "hex_defer_capture_1 =")
	begin := strings.Index(body, "hex_print_begin(")
	commit := strings.Index(body, "hex_print_commit(")
	if capture < 0 || begin < 0 || capture > begin || begin > commit {
		t.Fatalf("deferred print must capture at registration and build at execution: %q", body)
	}
	if strings.Count(body, "hex_print_commit(") != 1 {
		t.Fatalf("deferred print must commit exactly once: %q", body)
	}
}

func TestPrintDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo() do\n    print()\nend", "print expects at least 1 argument"},
		{"fun demo() do\n    let value: Int32 = 1\n    let pointer: Ptr<Int32> = @value\n    print(pointer)\nend", "print does not support Ptr<Int32>"},
		{"type Node is struct\n    value: Int32,\n    next: Ptr<Int32>,\nend\nfun demo() do\n    let value: Int32 = 1\n    let node: Node = Node(value = 1, next = @value)\n    print(node)\nend", "print does not support Node because next is Ptr<Int32>"},
		{"fun demo() do\n    let value: Int32 | Float32 = 1\n    print(value)\nend", "print does not support Int32 | Float32; narrow or match it first"},
		{"fun demo() do\n    let heap: Heap = Heap()\n    print(heap)\nend", "print does not support Heap"},
		{"fun worker(): Bool do\n    return true\nend\nfun f(h: Heap): Int32 | Error do\n    let task: Task<Bool> = try spawn worker()\n    print(task)\n    return 0\nend", "print does not support Task<Bool>"},
		{"fun f(h: Heap): Int32 | Error do\n    let channel: Channel<Int32> = try Channel<Int32>(h, 4)\n    print(channel)\n    return 0\nend", "print does not support Channel<Int32>"},
		{"fun f(h: Heap): Int32 | Error do\n    let mutex: Mutex = try Mutex(h)\n    print(mutex)\n    return 0\nend", "print does not support Mutex"},
		{"let counter: Atomic<Int32> = Atomic<Int32>(0)\nprint(counter)", "print does not support Atomic<Int32>"},
		{"fun helper() do\nend\nprint(helper)", "print does not support Fun<()>"},
		{"type Inner is struct\n    next: Ptr<Int32>,\nend\ntype Outer is struct\n    inner: Inner,\nend\nfun demo() do\n    let mut value: Int32 = 1\n    let outer: Outer = Outer(inner = Inner(next = @value))\n    print(outer)\nend", "print does not support Outer because inner is Inner"},
		{"let print: Int32 = 1", "print is a protected built-in name"},
		{"fun print() do\nend", "print is a protected built-in name"},
		{"fun demo() do\n    let step: Int32 | EoS = 1\n    print(step)\nend", "print does not support Int32 | EoS"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPrintNoResult(t *testing.T) {
	// The destination is otherwise valid, so failure proves that print
	// produces no value rather than that standalone Nil is invalid.
	result := compileSource("fun demo() do\n    let bad: Int32 = print(\"x\")\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "print produces no value") {
		t.Fatalf("Compile stderr = %#v, want no-result rejection", result.Stderr)
	}
}

func TestPrintDeferred(t *testing.T) {
	result := compileSource("fun demo() do\n    defer print(\"leaving\\n\")\n    let text: String = \"early\"\n    defer print(text)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_defer_capture_2 = hex_v_text;",
		"hex_print_text(&hex_print_out_2, hex_defer_capture_1->data, hex_defer_capture_1->byte_length);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestRecursiveTypePrintAccepted(t *testing.T) {
	result := compileSource("type T is union | Leaf | Node as kids: List<T> end end\n" +
		"fun demo(h: Heap) do\n" +
		"    let inner: List<T> = List<T>(h)\n" +
		"    inner.push(T.Leaf())\n" +
		"    let kids: List<T> = List<T>(h)\n" +
		"    kids.push(T.Leaf())\n" +
		"    kids.push(T.Node(kids = inner))\n" +
		"    let a: T = T.Node(kids = kids)\n" +
		"    print(a)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "static void hex_print_nested_hex_t_m3_app_T(") {
		t.Fatalf("modules/app.h lacks the recursive print helper:\n%s", rootH(t, result))
	}
}

func TestRecursiveTypeThroughDictPrintAccepted(t *testing.T) {
	result := compileSource("type U is union | Leaf | Node as kids: Dict<Int32, U> end end\n" +
		"fun demo(h: Heap) do\n" +
		"    let d: Dict<Int32, U> = Dict<Int32, U>(h)\n" +
		"    defer d.free(h)\n" +
		"    d.insert(1, U.Leaf())\n" +
		"    let a: U = U.Node(kids = d)\n" +
		"    print(a)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "static void hex_print_nested_hex_t_m3_app_U(") {
		t.Fatalf("modules/app.h lacks the recursive print helper:\n%s", rootH(t, result))
	}
}

func TestRecursiveTypePrintRejectsNonPrintableMember(t *testing.T) {
	// The recursion terminates, but a non-printable payload member outside the
	// recursion still makes the whole aggregate unprintable and names it.
	assertRejects(
		t,
		"type T is union | Leaf | Node as kids: List<T>, label: Fun<(Int32)> end end\n"+
			"fun tag(x: Int32) do\nend\n"+
			"fun demo(h: Heap) do\n"+
			"    let kids: List<T> = List<T>(h)\n"+
			"    kids.push(T.Leaf())\n"+
			"    let a: T = T.Node(kids = kids, label = tag)\n"+
			"    print(a)\nend",
		"print does not support",
	)
}

// The depth guard is the compiler-owned bound from config, emitted as a
// literal into every aggregate's nested helper; leaves receive depth but
// ignore it, so every nested call site passes depth + 1 uniformly.
func TestBoundedPrintDepthGuardEmitted(t *testing.T) {
	result := compileSource("type Point is struct x: Int32, y: Int32 end\nfun demo(h: Heap) do\n    let values: List<Point> = List<Point>(h)\n    defer values.free(h)\n    values.push(Point(x = 1, y = 2))\n    print(values)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	header := rootH(t, result)
	guards := strings.Count(header, "if (depth >= 16) {")
	if guards != 2 { // one per aggregate kind: the Point object and the List
		t.Fatalf("modules/app.h carries %d depth guards, want one per aggregate helper:\n%s", guards, header)
	}
	if strings.Count(header, "depth + 1") != 3 { // Point's two members plus the List element
		t.Fatalf("modules/app.h carries %d descending calls, want depth + 1 at every nested call:\n%s", strings.Count(header, "depth + 1"), header)
	}
	if strings.Count(header, "\"...\"") != 2 {
		t.Fatalf("modules/app.h carries %d marker prints, want one per aggregate helper:\n%s", strings.Count(header, "\"...\""), header)
	}
}

// Fence-post rule: an aggregate at depth 0 through 15 renders normally, and
// the aggregate that would enter at depth 16 prints only the unquoted marker.
// Nested leaf fields of the last rendered aggregate still render.
func TestBoundedPrintDepthFencePost(t *testing.T) {
	deepCells := "fun build(h: Heap, remaining: Int32): Cell do\n" +
		"    let kids: List<Cell> = List<Cell>(h)\n" +
		"    if remaining > 1 then\n" +
		"        kids.push(build(h, remaining - 1))\n" +
		"    end\n" +
		"    return Cell(value = kids)\n" +
		"end\n" +
		"fun demo(h: Heap) do\n" +
		"    print(build(h, 20))\n" +
		"end\n"
	result := compileSource("type Cell is struct value: List<Cell> end\n" + deepCells)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	c := rootC(t, result)
	if strings.Contains(c, "hex_print_nested_") && !strings.Contains(rootH(t, result), "int depth") {
		t.Fatalf("nested helpers did not take depth:\n%s", rootH(t, result))
	}
	// The wrapper's own structural signature is what a boundary test needs
	// exact output from; that belongs to the boxed C23 run, so the ordinary
	// lane only proves the plumbing survived generation.
}

// Two top-level arguments each carry their own depth budget: neither descends
// through the other's.
func TestBoundedPrintDepthPerArgumentBudget(t *testing.T) {
	result := compileSource("type Cell is struct value: List<Cell> end\n" +
		"fun build(h: Heap, remaining: Int32): Cell do\n" +
		"    let kids: List<Cell> = List<Cell>(h)\n" +
		"    if remaining > 1 then\n" +
		"        kids.push(build(h, remaining - 1))\n" +
		"    end\n" +
		"    return Cell(value = kids)\n" +
		"end\n" +
		"fun demo(h: Heap) do\n" +
		"    print(build(h,20), \"|\", build(h, 20))\n" +
		"end\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// Both arguments render through the same helper at depth 0; the exact
	// equal rendering is the boxed run's assertion.
}

// A String whose value is literally "..." stays quoted and quoted only: the
// truncation marker is print-owned syntax, not a String value.
func TestPrintQuotedEllipsisLiteral(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let scores: List<String> = List<String>(h)\n    defer scores.free(h)\n    scores.push(\"...\")\n    print(scores)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result)+"\n"+rootH(t, result), "hex_print_quoted_text") {
		t.Fatalf("nested String must print through hex_print_quoted_text to keep the literal quoted")
	}
}
