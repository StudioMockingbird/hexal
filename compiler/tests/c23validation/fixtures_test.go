//go:build c23

package c23validation

// The fixture data itself. The three concurrency fixtures that spawn, join,
// or lock a Task are deliberately kept compile-only rather than run; see the
// comment above them for why.

var fixtureCatalog = []fixture{
	// Compile-only: representative programs across the constructs whose
	// generated C has never been read by a compiler before this suite.
	{
		name:       "array-view-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "type Pair = { mut values: Array<Int32, 2>, }\n" +
			"fun sum(values: View<Int32>): Int32 do\n    return values[0] + values[1]\nend\n" +
			"fun demo() do\n    mut pair: Pair := Pair { values = [3, 4], }\n    view: View<Int32> := pair.values.slice(0, 2)\n    total: Int32 := sum(view)\n    last: Int32 := view[1]\n    pair.values[0] = 9\nend"},
	},
	{
		name:       "list-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values[0] = 9\n    first: Int32 := values[0]\n    values[1] = 5\n    last: Int32 := values.pop()\n    values.clear()\n    values.push(7)\n    view: View<Int32> := values.slice(0, 1)\n    total: Int32 := view[0]\n    names: List<String> := List<String>.new(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    runtime: String := \"bob\".to_string(h)\n    names.push(runtime)\n    popped: String := names.pop()\n    popped.free(h)\n    name: String := names[0]\nend"},
	},
	{
		name:       "dict-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool := scores.contains(1)\n    first: Int32 := scores.get(1)\n    removed: Int32 := scores.remove(2)\n    labels: Dict<Strand, Int32> := Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    score: Int32 := labels.get(\"alice\")\n    people: Dict<Int32, String> := Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"bob\")\n    name: String := people.get(1)\nend"},
	},
	{
		name:       "equality-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "type Point = { x: Int32, y: Int32, }\ntype Shape as | Circle { r: Int32, } | Square { a: Int32, } end\nfun demo(h: Heap) do\n    left: Point := Point { x = 1, y = 2, }\n    right: Point := Point { x = 1, y = 2, }\n    same: Bool := left == right\n    different: Bool := left != right\n    i32: Int32 := 1\n    i64: Int64 := 2\n    widened: Bool := i32 == i64\n    text: String := \"abc\"\n    other: String := \"abd\"\n    textOrder: Bool := text < other\n    fixed: Array<Int32, 2> := [1, 2]\n    twin: Array<Int32, 2> := [1, 2]\n    arrays: Bool := fixed == twin\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    lists: Bool := values == values\n    circle: Shape := Shape.Circle { r = 1, }\n    square: Shape := Shape.Square { a = 1, }\n    shapes: Bool := circle == square\n    mut value: Int32 := 3\n    pointer: Ptr<Int32> := ref value\n    twinPointer: Ptr<Int32> := pointer\n    pointers: Bool := pointer == twinPointer\nend"},
	},
	{
		name:       "string-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun make_text(h: Heap): String do\n    return \"ready\".to_string(h)\nend\nfun demo(h: Heap) do\n    text: String := make_text(h)\n    defer text.free(h)\n    loud: String := text.concat(h, \"!\")\n    raw: View<UInt8> := text.bytes()\n    first: UInt8 := raw[0]\n    part: View<UInt8> := text.slice(0, 2)\n    second: UInt8 := part[1]\n    loud.free(h)\nend"},
	},
	{
		name:       "error-try-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun cleanup(value: Int32) do\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error do\n    errdefer cleanup(1)\n    defer cleanup(2)\n    mut total: Int32 := 0\n    while true do\n        count: Int32 := try read_count()\n        total = total + count\n        break\n    end\n    if release then\n        return Error.new(\"Final Error\", \"done\")\n    end\n    return total\nend"},
	},
	{
		name:       "bitwise-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo() do\n    mut flags: UInt32 := 0xFFFF0000\n    masked: UInt32 := flags & 0x00FF\n    combined: UInt32 := masked | 0xF0\n    xor: UInt32 := combined ^ 0x0F0F\n    complement: UInt8 := ~0x0F\n    shifted: UInt32 := flags << 4\n    back: UInt32 := shifted >> 8\n    mut signed: Int8 := 64\n    wrapped: Int8 := signed << 1\n    mut negative: Int8 := -4\n    halved: Int8 := negative >> 1\n    floating: Float64 := 1.5\n    bits: UInt64 := floating.bit_cast<UInt64>()\n    again: Float64 := bits.bit_cast<Float64>()\n    value: UInt32 := 0x01020304\n    little: Array<UInt8, 4> := value.to_le_bytes()\n    big: Array<UInt8, 4> := value.to_be_bytes()\n    from_little: UInt32 := UInt32.from_le_bytes(little)\n    from_big: UInt32 := UInt32.from_be_bytes(big)\n    mut signed16: Int16 := -2\n    signed_little: Array<UInt8, 2> := signed16.to_le_bytes()\n    signed_back: Int16 := Int16.from_le_bytes(signed_little)\nend"},
	},
	{
		name:       "numeric-iteration-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo(h: Heap) do\n    wide: Int64 := 9_000_000_000\n    narrowed: Int8 := wide.to<Int8>()\n    wrapped: UInt8 := (200).to<UInt8>()\n    whole: Int32 := 3.75.to<Int32>()\n    mut left: Int32 := 7\n    mut right: Int32 := 3\n    quotient: Int32 := left / right\n    remainder: Int32 := left % right\n    fixed: Array<Int32, 3> := [10, 20, 30]\n    mut total: Int32 := 0\n    for value in fixed do\n        total = total + value\n    end\n    for i, value in fixed do\n        total = total + value + i.to<Int32>()\n    end\n    view: View<Int32> := fixed.slice(0, 2)\n    for value in view do\n        total = total + value\n    end\n    text: String := \"cafe\"\n    mut runes: Int32 := 0\n    for rune in text do\n        runes = runes + 1\n    end\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    for value in values do\n        total = total + value\n    end\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    for key, value in scores do\n        total = total + key + value\n    end\n    size: Size := values.length()\nend"},
	},

	// Tier 2: exact runtime output.
	{
		name:        "list-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values[0] = 9\n    first: Int32 := values[0]\n    values[1] = 5\n    last: Int32 := values.pop()\n    values.clear()\n    values.push(7)\n    view: View<Int32> := values.slice(0, 1)\n    total: Int32 := view[0]\n    names: List<String> := List<String>.new(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    runtime: String := \"bob\".to_string(h)\n    names.push(runtime)\n    popped: String := names.pop()\n    popped.free(h)\n    name: String := names[0]\n    return (first == 9) and (last == 5) and (total == 7) and (name.length() == 5)\nend\nprint(demo(Heap.new()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "dict-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool := scores.contains(1)\n    first: Int32 := scores.get(1)\n    removed: Int32 := scores.remove(2)\n    scores.insert(3, 30)\n    scores.insert(4, 40)\n    scores.insert(5, 50)\n    grown: Int32 := scores.get(5)\n    labels: Dict<Strand, Int32> := Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    score: Int32 := labels.get(\"alice\")\n    return present and (first == 10) and (removed == 20) and (grown == 50) and (score == 1)\nend\nprint(demo(Heap.new()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "string-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    text: String := \"ready\".to_string(h)\n    defer text.free(h)\n    loud: String := text.concat(h, \"!\")\n    defer loud.free(h)\n    ok: Bool := loud.length() == 6\n    part: View<UInt8> := text.slice(0, 2)\n    second: UInt8 := part[1]\n    return ok and (second == 101)\nend\nprint(demo(Heap.new()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "print-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "type Point = {\n    x: Int32,\n    y: Int32,\n}\nprint(\"count = \", 42, \"\\n\")\nprint(true, false, nil)\nprint(1.5, -2.5)\npoint: Point := Point { x = 10, y = 20 }\nprint(point)\nprint(\"\\n\")"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "count = 42\ntruefalsenil1.5-2.5Point { x = 10, y = 20 }\n"},
	},
	{
		name:        "print-evaluation-order-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun a(): Int32 do\n    print(\"a\")\n    return 1\nend\nfun b(): Int32 do\n    print(\"b\")\n    return 2\nend\nprint(a(), b(), a())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "aba121"},
	},
	{
		name:        "print-collections-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    print(values)\n    text: String := \"hi\".to_string(h)\n    defer text.free(h)\n    print(text)\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    print(scores)\nend\ndemo(Heap.new())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "[1, 2]hi{1: 10}"},
	},
	{
		name:        "error-control-flow-success-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun cleanup(label: Int32) do\n    print(label)\nend\nfun ok_read(): Int32 | Error do\n    return 4\nend\nfun succeed(): Int32 | Error do\n    errdefer cleanup(3)\n    defer cleanup(2)\n    defer cleanup(1)\n    count: Int32 := try ok_read()\n    return count\nend\nfun report(): Bool do\n    outcome: Int32 | Error := succeed()\n    result: Bool := match outcome is\n    | Int32 then\n        outcome == 4\n    | Error then\n        false\n    end\n    return result\nend\nprint(report())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "12true"},
	},
	{
		name:        "error-control-flow-failure-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun cleanup(label: Int32) do\n    print(label)\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun swallow(): Bool do\n    print(\"!\")\n    return false\nend\nfun inner(): Int32 | Error do\n    errdefer cleanup(3)\n    defer cleanup(2)\n    defer cleanup(1)\n    count: Int32 := try read_count()\n    return count\nend\nfun outer(): Bool do\n    outcome: Int32 | Error := inner()\n    result: Bool := match outcome is\n    | Error then\n        swallow()\n    | Int32 then\n        outcome == 0\n    end\n    return result\nend\nprint(outer())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "123!false"},
	},
	{
		name:        "float-to-integer-truncation-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(): Bool do\n    a: Int32 := 2.5.to<Int32>()\n    b: Int32 := 3.5.to<Int32>()\n    c: Int32 := 0.5.to<Int32>()\n    d: Int32 := 1.5.to<Int32>()\n    e: Int32 := (-0.5).to<Int32>()\n    f: Int32 := (-2.5).to<Int32>()\n    return (a == 2) and (b == 3) and (c == 0) and (d == 1) and (e == 0) and (f == -2)\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "min-overflow-wraps-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(): Bool do\n    min: Int32 := -2147483648\n    quotient: Int32 := min / -1\n    remainder: Int32 := min % -1\n    return (quotient == -2147483648) and (remainder == 0)\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		// Neither String nor Strand is indexable (docs/reference.md, Text):
		// reaching the nth Rune of UTF-8 walks from the start, so a
		// positional [] would be quadratic behind O(1) syntax. Every access
		// below goes through rune_cursor() instead.
		name:        "text-conformance-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    byte: UInt8 := b'\\xFF'\n    byte_ok: Bool := byte == 255\n    letter: Rune := '\\u{00E9}'\n    crab: Rune := '\\u{1F980}'\n    rune_ok: Bool := (letter == 233) and (crab == 129408)\n    text: String := \"caf\\u{00E9} \\u{1F980}\"\n    count: Size := text.length()\n    length_ok: Bool := count == 6\n    cursor: RuneCursor := text.rune_cursor()\n    first: Rune := cursor.next()\n    cursor.next()\n    cursor.next()\n    accented: Rune := cursor.next()\n    index_ok: Bool := (first == 99) and (accented == 233)\n    mut seen: Int32 := 4\n    while cursor.has_next() do\n        value: Rune := cursor.next()\n        seen = seen + 1\n    end\n    cursor_ok: Bool := seen == 6\n    label: Strand := \"hexal\"\n    label_text: String := label.to_string(h)\n    defer label_text.free(h)\n    label_cursor: RuneCursor := label_text.rune_cursor()\n    label_first: Rune := label_cursor.next()\n    strand_ok: Bool := (label.length() == 5) and (label_first == 104)\n    runes: Array<Rune, 2> := [letter, crab]\n    view: View<Rune> := runes.slice(0, 2)\n    encoded: String := String.from_runes(h, view)\n    encoded_cursor: RuneCursor := encoded.rune_cursor()\n    encoded_first: Rune := encoded_cursor.next()\n    encoded_ok: Bool := (encoded.length() == 2) and (encoded_first == 233)\n    encoded.free(h)\n    return byte_ok and rune_ok and length_ok and index_ok and cursor_ok and strand_ok and encoded_ok\nend\nprint(demo(Heap.new()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},

	// Tier 3: exact "[Runtime Error] ..." trap text, taken directly from the
	// generator templates that emit it (compiler/generator/packages) rather
	// than guessed.
	{
		name:        "division-by-zero-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    mut left: Int32 := 7\n    mut right: Int32 := 0\n    quotient: Int32 := left / right\n    print(quotient)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] numeric operation failed"},
	},
	{
		name:        "remainder-by-zero-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    mut left: Int32 := 7\n    mut right: Int32 := 0\n    remainder: Int32 := left % right\n    print(remainder)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] numeric operation failed"},
	},
	{
		name:        "conversion-overflow-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    big: Int64 := 300\n    small: Int8 := big.to<Int8>()\n    print(small)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] numeric operation failed"},
	},
	{
		name:        "empty-list-pop-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    last: Int32 := values.pop()\n    print(last)\nend\ndemo(Heap.new())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] list index out of bounds"},
	},
	{
		name:        "missing-dict-get-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    missing: Int32 := scores.get(2)\n    print(missing)\nend\ndemo(Heap.new())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] dictionary key not found"},
	},
	{
		name:        "list-index-out-of-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    first: Int32 := values[4]\n    print(first)\nend\ndemo(Heap.new())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] list index out of bounds"},
	},
	{
		// Static bounds are compile errors and constant propagation sees
		// through local bindings, so a parameter supplies the runtime
		// bounds-check path.
		name:        "array-index-out-of-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(index: Int32) do\n    fixed: Array<Int32, 3> := [10, 20, 30]\n    out: Int32 := fixed[index]\n    print(out)\nend\ndemo(5)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] array index out of bounds"},
	},
	{
		name:        "array-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(stop: Int32) do\n    fixed: Array<Int32, 3> := [10, 20, 30]\n    view: View<Int32> := fixed.slice(1, stop)\n    print(view.length())\nend\ndemo(5)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] array slice bounds out of range"},
	},
	{
		name:        "list-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap, stop: Int32) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    view: View<Int32> := values.slice(1, stop)\n    print(view.length())\nend\ndemo(Heap.new(), 5)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] list slice bounds out of range"},
	},
	{
		// String is not indexable (see the note on text-conformance-runs
		// above), so its only bounds-checked runtime path is slice.
		name:        "string-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(stop: Int32) do\n    text: String := \"hex\"\n    view: View<UInt8> := text.slice(0, stop)\n    print(view.length())\nend\ndemo(100)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] string slice bounds out of range"},
	},
	{
		name:        "malformed-utf8-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    bytes: Array<UInt8, 2> := [0xC3, 0x28]\n    view: View<UInt8> := bytes.slice(0, 2)\n    text: String := String.from_bytes(h, view)\n    print(text)\nend\ndemo(Heap.new())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] invalid UTF-8 in string"},
	},
	{
		name:        "rune-cursor-exhaustion-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    text: String := \"hex\"\n    cursor: RuneCursor := text.rune_cursor()\n    cursor.next()\n    cursor.next()\n    cursor.next()\n    late: Rune := cursor.next()\n    print(late)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] RuneCursor has no next value"},
	},

	// Concurrency now uses native platform threading primitives rather than
	// <threads.h>, so the historical "Windows lacks <threads.h>" compile
	// blocker no longer applies -- these compile clean on every discovered
	// toolchain. They are deliberately compile-only, not run:
	// docs/status.md's Unowned section records that hex_scheduler_init
	// never returns on any platform, so spawning, joining, or locking would
	// hang every toolchain's process until runProcessTimeout, for a defect
	// owned elsewhere.
	{
		name:       "concurrency-spawn-channel-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun worker(count: Int32, ch: Channel<Int32>): Bool do\n    mut index: Int32 := 0\n    while index < count do\n        ch.send(index)\n        Task.yield()\n        index = index + 1\n    end\n    ch.close()\n    return true\nend\nfun run(): Nil | Error do\n    h: Heap := Heap.new()\n    ch: Channel<Int32> := try Channel<Int32>.new(h, 8)\n    defer ch.free(h)\n    worker_task: Task<Bool> := try spawn worker(4, ch)\n    mut total: Int32 := 0\n    while true do\n        step: Int32 | EoS := ch.receive()\n        if step is EoS then\n            break\n        end\n        total = total + step\n        Task.yield()\n    end\n    worker_task.join()\n    print(total)\n    return nil\nend\nrun()\n"},
	},
	{
		name:       "concurrency-task-join-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    first: Task<Int32> := try spawn square(6)\n    second: Task<Int32> := try spawn square(7)\n    return first.join() + second.join()\nend\nfun demo(): Int32 do\n    outcome: Int32 | Error := run()\n    value: Int32 := match outcome is\n    | Int32 then\n        outcome\n    | Error then\n        0\n    end\n    return value\nend\nprint(demo())\n"},
	},
	{
		name:       "concurrency-mutex-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun worker(m: Mutex, counter: MutPtr<Int32>): Int32 do\n    mut index: Int32 := 0\n    while index < 100 do\n        m.lock()\n        counter.value = counter.value + 1\n        m.unlock()\n        Task.yield()\n        index = index + 1\n    end\n    return index\nend\nfun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    m: Mutex := try Mutex.new(h)\n    defer m.free(h)\n    mut count: Int32 := 0\n    first: Task<Int32> := try spawn worker(m, ref count)\n    second: Task<Int32> := try spawn worker(m, ref count)\n    first.join()\n    second.join()\n    return count\nend\nfun demo(): Int32 do\n    outcome: Int32 | Error := run()\n    value: Int32 := match outcome is\n    | Int32 then\n        outcome\n    | Error then\n        0\n    end\n    return value\nend\nprint(demo())\n"},
	},
	// Atomic touches no scheduler state -- no spawn, no Task, no fiber --
	// so it is not subject to the scheduler defect above and runs normally.
	{
		name:       "atomic-operations-run",
		entrypoint: "app.hex",
		// exchange returns the value it replaced (9, set by the preceding
		// store), not the value it just installed (4).
		sources:     map[string]string{"app.hex": "fun demo(): Bool do\n    counter: Atomic<Int32> := Atomic<Int32>.new(5)\n    old: Int32 := counter.fetch_add(3)\n    counter.fetch_sub(2)\n    counter.store(9)\n    loaded: Int32 := counter.load()\n    swapped: Int32 := counter.exchange(4)\n    expected: Bool := counter.compare_exchange(4, 6)\n    refused: Bool := counter.compare_exchange(4, 6)\n    final: Int32 := counter.load()\n    return (old == 5) and (loaded == 9) and (swapped == 9) and expected and !refused and (final == 6)\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
}
