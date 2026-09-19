//go:build c23

package c23validation

// The fixture data itself. Concurrency fixtures run under the same
// ten-second process bound as every other fixture; see c23_harness_test.go.

import (
	"strings"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

var fixtureCatalog = []fixture{
	// Compile-only: representative programs across the constructs whose
	// generated C has never been read by a compiler before this suite.
	{
		name:       "array-view-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "type Pair is struct mut values: Array<Int32, 2> end\n" +
			"fun sum(values: Slice<Int32>): Int32 do\n    return values[0] + values[1]\nend\n" +
			"fun demo() do\n    let mut pair: Pair = Pair(values = [3, 4])\n    let view: Slice<Int32> = pair.values.slice(0, 2)\n    let total: Int32 = sum(view)\n    let last: Int32 = view[1]\n    pair.values[0] = 9\nend"},
	},
	{
		name:       "list-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values[0] = 9\n    let first: Int32 = values[0]\n    values[1] = 5\n    let last: Int32 = values.pop()\n    values.clear()\n    values.push(7)\n    let view: Slice<Int32> = values.slice(0, 1)\n    let total: Int32 = view[0]\n    let names: List<String> = List<String>(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    let runtime: String = \"bob\".to_string(h)\n    names.push(runtime)\n    let popped: String = names.pop()\n    popped.free(h)\n    let name: String = names[0]\nend"},
	},
	{
		name:       "dict-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    let present: Bool = scores.contains(1)\n    let first: Int32 = scores.get(1)\n    let removed: Int32 = scores.remove(2)\n    let labels: Dict<Strand, Int32> = Dict<Strand, Int32>(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    let score: Int32 = labels.get(\"alice\")\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"bob\")\n    let name: String = people.get(1)\nend"},
	},
	{
		name:       "equality-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "type Point is struct x: Int32, y: Int32 end\ntype Shape is union | Circle as r: Int32 end | Square as a: Int32 end end\nfun demo(h: Heap) do\n    let left: Point = Point(x = 1, y = 2)\n    let right: Point = Point(x = 1, y = 2)\n    let same: Bool = left == right\n    let different: Bool = left != right\n    let i32: Int32 = 1\n    let i64: Int64 = 2\n    let widened: Bool = i32 == i64\n    let text: String = \"abc\"\n    let other: String = \"abd\"\n    let textOrder: Bool = text < other\n    let fixed: Array<Int32, 2> = [1, 2]\n    let twin: Array<Int32, 2> = [1, 2]\n    let arrays: Bool = fixed == twin\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    let lists: Bool = values == values\n    let circle: Shape = Shape.Circle(r = 1)\n    let square: Shape = Shape.Square(a = 1)\n    let shapes: Bool = circle == square\n    let mut value: Int32 = 3\n    let pointer: Ptr<Int32> = @value\n    let twinPointer: Ptr<Int32> = pointer\n    let pointers: Bool = pointer == twinPointer\nend"},
	},
	{
		name:       "string-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun make_text(h: Heap): String do\n    return \"ready\".to_string(h)\nend\nfun demo(h: Heap) do\n    let text: String = make_text(h)\n    defer text.free(h)\n    let loud: String = text.concat(h, \"!\")\n    let raw: Slice<UInt8> = text.bytes()\n    let first: UInt8 = raw[0]\n    let part: Slice<UInt8> = text.slice(0, 2)\n    let second: UInt8 = part[1]\n    loud.free(h)\nend"},
	},
	{
		name:       "error-try-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun cleanup(value: Int32) do\nend\nfun read_count(): Int32 | Error do\n    return Error(ErrorKind.Other(header = \"Read Error\"), \"no count\")\nend\nfun demo(release: Bool): Int32 | Error do\n    errdefer cleanup(1)\n    defer cleanup(2)\n    let mut total: Int32 = 0\n    while true do\n        let count: Int32 = try read_count()\n        total = total + count\n        break\n    end\n    if release then\n        return Error(ErrorKind.Other(header = \"Final Error\"), \"done\")\n    end\n    return total\nend"},
	},
	{
		name:       "bitwise-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo() do\n    let mut flags: UInt32 = 0xFFFF0000\n    let masked: UInt32 = flags & 0x00FF\n    let combined: UInt32 = masked | 0xF0\n    let xor: UInt32 = combined ^ 0x0F0F\n    let complement: UInt8 = ~0x0F\n    let shifted: UInt32 = flags << 4\n    let back: UInt32 = shifted >> 8\n    let mut signed: Int8 = 64\n    let wrapped: Int8 = signed << 1\n    let mut negative: Int8 = -4\n    let halved: Int8 = negative >> 1\n    let floating: Float64 = 1.5\n    let bits: UInt64 = floating.bit_cast<UInt64>()\n    let again: Float64 = bits.bit_cast<Float64>()\n    let value: UInt32 = 0x01020304\n    let little: Array<UInt8, 4> = value.to_le_bytes()\n    let big: Array<UInt8, 4> = value.to_be_bytes()\n    let from_little: UInt32 = UInt32.from_le_bytes(little)\n    let from_big: UInt32 = UInt32.from_be_bytes(big)\n    let mut signed16: Int16 = -2\n    let signed_little: Array<UInt8, 2> = signed16.to_le_bytes()\n    let signed_back: Int16 = Int16.from_le_bytes(signed_little)\nend"},
	},
	{
		name:       "numeric-iteration-compiles",
		entrypoint: "app.hex",
		sources:    map[string]string{"app.hex": "fun demo(h: Heap) do\n    let wide: Int64 = 9_000_000_000\n    let narrowed: Int8 = wide.to<Int8>()\n    let wrapped: UInt8 = (200).to<UInt8>()\n    let whole: Int32 = 3.75.to<Int32>()\n    let mut left: Int32 = 7\n    let mut right: Int32 = 3\n    let quotient: Int32 = left / right\n    let remainder: Int32 = left % right\n    let fixed: Array<Int32, 3> = [10, 20, 30]\n    let mut total: Int32 = 0\n    for value in fixed do\n        total = total + value\n    end\n    for i, value in fixed do\n        total = total + value + i.to<Int32>()\n    end\n    let view: Slice<Int32> = fixed.slice(0, 2)\n    for value in view do\n        total = total + value\n    end\n    let text: String = \"cafe\"\n    let mut runes: Int32 = 0\n    for rune in text do\n        runes = runes + 1\n    end\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    for value in values do\n        total = total + value\n    end\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    for key, value in scores do\n        total = total + key + value\n    end\n    let size: Size = values.length()\nend"},
	},

	// Tier 2: exact runtime output.
	{
		name:        "list-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values[0] = 9\n    let first: Int32 = values[0]\n    values[1] = 5\n    let last: Int32 = values.pop()\n    values.clear()\n    values.push(7)\n    let view: Slice<Int32> = values.slice(0, 1)\n    let total: Int32 = view[0]\n    let names: List<String> = List<String>(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    let runtime: String = \"bob\".to_string(h)\n    names.push(runtime)\n    let popped: String = names.pop()\n    popped.free(h)\n    let name: String = names[0]\n    return (first == 9) and (last == 5) and (total == 7) and (name.length() == 5)\nend\nprint(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "ascii-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Ascii from std.ascii\nend\n" +
			"let digit: Bool = Ascii.is_digit(48)\n" +
			"let non_digit: Bool = !Ascii.is_digit(65)\n" +
			"let alpha: Bool = Ascii.is_alpha(65)\n" +
			"let space: Bool = Ascii.is_space(9)\n" +
			"let lower: Byte = Ascii.to_lower(65)\n" +
			"let upper: Byte = Ascii.to_upper(97)\n" +
			"print(digit and non_digit and alpha and space and (lower == 97) and (upper == 65))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "dict-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    let present: Bool = scores.contains(1)\n    let first: Int32 = scores.get(1)\n    let removed: Int32 = scores.remove(2)\n    scores.insert(3, 30)\n    scores.insert(4, 40)\n    scores.insert(5, 50)\n    let grown: Int32 = scores.get(5)\n    let labels: Dict<Strand, Int32> = Dict<Strand, Int32>(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    let score: Int32 = labels.get(\"alice\")\n    return present and (first == 10) and (removed == 20) and (grown == 50) and (score == 1)\nend\nprint(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		// Cross-module generics: an exported generic function specialized
		// through an import alias, a generic method on that specialization,
		// and an importer-owned nominal argument type (Point, declared only
		// in app.hex) flowing into the defining module's own Box<T>. Proves
		// the cross-module specialization's C definition actually links,
		// not only that the checker accepts it.
		name:       "cross-module-generics-runs",
		entrypoint: "app.hex",
		sources: map[string]string{
			"lib.hex": "type Box<T> is struct\n    item: T,\nend\n" +
				"method Box<T>.get(): T do\n    return self.item\nend\n" +
				"fun new_box<T>(value: T): Box<T> do\n    return Box<T>(item = value)\nend\n" +
				"export\n    Box, Box.get, new_box\nend\n",
			"app.hex": "import\n    Lib from \"./lib\"\nend\n" +
				"type Point is struct\n    x: Int32,\nend\n" +
				"let box: Lib.Box<Point> = Lib.new_box<Point>(Point(x = 7))\n" +
				"print(box.get().x)\n",
		},
		expectation: &processExpectation{zeroExit: true, exactStdout: "7"},
	},
	{
		name:       "program-arguments-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Prog from std.program\nend\n" +
			"let args = Prog.arguments()\n" +
			"if args is Error then\n    return 1\nend\n" +
			"print(args.length())\n" +
			"return 0\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "1"},
	},
	{
		name:       "program-arguments-nonowning-free-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Prog from std.program\nend\n" +
			"let h: Heap = Heap()\n" +
			"let args = Prog.arguments()\n" +
			"if args is Error then\n    return 1\nend\n" +
			"args[0].free(h)\n"},
		expectation: &processExpectation{zeroExit: false, requiredStderrSubstring: "[Runtime Error] cannot free a non-owning String"},
	},
	{
		name:       "program-path-query-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Prog from std.program\nend\n" +
			"fun demo(): Bool | Error do\n" +
			"    let h: Heap = Heap()\n" +
			"    let path: String = try Prog.current_directory(h)\n" +
			"    return path.length() > 0\n" +
			"end\n" +
			"let outcome: Bool | Error = demo()\n" +
			"if outcome is Error then\n    return 1\nend\n" +
			"print(outcome)\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "entropy-fill-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Ent from std.entropy\nend\n" +
			"fun demo(): Bool | Error do\n" +
			"    let h: Heap = Heap()\n" +
			"    let p: Ptr<mut Byte> = h.allocate<Byte>(8)\n" +
			"    unsafe do\n" +
			"        let view: Slice<mut Byte> = Slice<mut Byte>.from_pointer(p, 8)\n" +
			"        try Ent.fill(view)\n" +
			"    end\n" +
			"    return true\n" +
			"end\n" +
			"let outcome: Bool | Error = demo()\n" +
			"if outcome is Error then\n    return 1\nend\n" +
			"print(outcome)\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "dict-removal-preserves-collision-chain-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n" +
			"    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
			"    defer scores.free(h)\n" +
			"    scores.insert(12, 100)\n" +
			"    scores.insert(17, 200)\n" +
			"    scores.insert(35, 300)\n" +
			"    let removedMiddle: Int32 = scores.remove(17)\n" +
			"    let afterMiddle: Bool = scores.contains(12) and scores.contains(35) and !scores.contains(17) and (scores.get(12) == 100) and (scores.get(35) == 300)\n" +
			"    let removedHead: Int32 = scores.remove(12)\n" +
			"    let afterHead: Bool = scores.contains(35) and !scores.contains(12)\n" +
			"    scores.insert(12, 999)\n" +
			"    scores.insert(17, 888)\n" +
			"    let reinsert: Bool = (scores.get(12) == 999) and (scores.get(17) == 888) and (scores.get(35) == 300)\n" +
			"    let removedTail: Int32 = scores.remove(35)\n" +
			"    let afterTail: Bool = !scores.contains(35) and scores.contains(12) and scores.contains(17)\n" +
			"    let lenAfter: Size = scores.length()\n" +
			"    let wrap: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
			"    defer wrap.free(h)\n" +
			"    wrap.insert(0, 1)\n" +
			"    wrap.insert(7, 2)\n" +
			"    wrap.insert(13, 3)\n" +
			"    let removedWrap: Int32 = wrap.remove(7)\n" +
			"    let afterWrap: Bool = wrap.contains(0) and wrap.contains(13) and !wrap.contains(7) and (wrap.get(0) == 1) and (wrap.get(13) == 3)\n" +
			"    let growing: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
			"    defer growing.free(h)\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 20 do\n" +
			"        growing.insert(i, i * 2)\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    let removedGrown: Int32 = growing.remove(10)\n" +
			"    let mut allFound: Bool = true\n" +
			"    let mut j: Int32 = 0\n" +
			"    while j < 20 do\n" +
			"        if j != 10 then\n" +
			"            if growing.get(j) != (j * 2) then\n" +
			"                allFound = false\n" +
			"            end\n" +
			"        end\n" +
			"        j = j + 1\n" +
			"    end\n" +
			"    let afterGrowth: Bool = allFound and !growing.contains(10) and (growing.length() == 19)\n" +
			"    let labels: Dict<Strand, Int32> = Dict<Strand, Int32>(h)\n" +
			"    defer labels.free(h)\n" +
			"    labels.insert(\"k2\", 2)\n" +
			"    labels.insert(\"k11\", 11)\n" +
			"    labels.insert(\"k19\", 19)\n" +
			"    let removedStrand: Int32 = labels.remove(\"k11\")\n" +
			"    let afterStrand: Bool = labels.contains(\"k2\") and labels.contains(\"k19\") and !labels.contains(\"k11\") and (labels.get(\"k2\") == 2) and (labels.get(\"k19\") == 19)\n" +
			"    let mut result: Bool = (removedMiddle == 200) and afterMiddle\n" +
			"    result = result and (removedHead == 100) and afterHead\n" +
			"    result = result and reinsert\n" +
			"    result = result and (removedTail == 300) and afterTail\n" +
			"    result = result and (lenAfter == 2)\n" +
			"    result = result and (removedWrap == 2) and afterWrap\n" +
			"    result = result and (removedGrown == 20) and afterGrowth\n" +
			"    result = result and (removedStrand == 11) and afterStrand\n" +
			"    return result\n" +
			"end\n" +
			"print(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "dict-repeated-removal-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun demo(h: Heap): Int32 do\n" +
			"    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
			"    defer scores.free(h)\n" +
			"    scores.insert(12, 100)\n" +
			"    scores.insert(17, 200)\n" +
			"    let first: Int32 = scores.remove(17)\n" +
			"    let second: Int32 = scores.remove(17)\n" +
			"    return first + second\n" +
			"end\n" +
			"print(demo(Heap()))\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] dictionary key not found"},
	},
	{
		name:        "string-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    let text: String = \"ready\".to_string(h)\n    defer text.free(h)\n    let loud: String = text.concat(h, \"!\")\n    defer loud.free(h)\n    let ok: Bool = loud.length() == 6\n    let part: Slice<UInt8> = text.slice(0, 2)\n    let second: UInt8 = part[1]\n    return ok and (second == 101)\nend\nprint(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "string-owned-cleanup-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    let built: String = String.interpolate(h, \"n={{ 41 }}\")\n    defer built.free(h)\n    let copied: String = built.to_string(h)\n    defer copied.free(h)\n    return built.length() == 4\nend\nprint(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "print-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "type Point is struct\n    x: Int32,\n    y: Int32,\nend\nprint(\"count = \", 42, \"\\n\")\nprint(true, false, nil)\nprint(1.5, -2.5)\nlet point: Point = Point(x = 10, y = 20)\nprint(point)\nprint(\"\\n\")"},
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
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    print(values)\n    let text: String = \"hi\".to_string(h)\n    defer text.free(h)\n    print(text)\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    print(scores)\nend\ndemo(Heap())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "[1, 2]hi{1: 10}"},
	},
	{
		name:        "error-control-flow-success-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun cleanup(label: Int32) do\n    print(label)\nend\nfun ok_read(): Int32 | Error do\n    return 4\nend\nfun succeed(): Int32 | Error do\n    errdefer cleanup(3)\n    defer cleanup(2)\n    defer cleanup(1)\n    let count: Int32 = try ok_read()\n    return count\nend\nfun report(): Bool do\n    let outcome: Int32 | Error = succeed()\n    let result: Bool = match outcome is\n    | Int32 then\n        outcome == 4\n    | Error then\n        false\n    end\n    return result\nend\nprint(report())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "12true"},
	},
	{
		name:        "error-control-flow-failure-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun cleanup(label: Int32) do\n    print(label)\nend\nfun read_count(): Int32 | Error do\n    return Error(ErrorKind.Other(header = \"Read Error\"), \"no count\")\nend\nfun swallow(): Bool do\n    print(\"!\")\n    return false\nend\nfun inner(): Int32 | Error do\n    errdefer cleanup(3)\n    defer cleanup(2)\n    defer cleanup(1)\n    let count: Int32 = try read_count()\n    return count\nend\nfun outer(): Bool do\n    let outcome: Int32 | Error = inner()\n    let result: Bool = match outcome is\n    | Error then\n        swallow()\n    | Int32 then\n        outcome == 0\n    end\n    return result\nend\nprint(outer())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "123!false"},
	},
	{
		name:        "float-to-integer-truncation-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(): Bool do\n    let a: Int32 = 2.5.to<Int32>()\n    let b: Int32 = 3.5.to<Int32>()\n    let c: Int32 = 0.5.to<Int32>()\n    let d: Int32 = 1.5.to<Int32>()\n    let e: Int32 = (-0.5).to<Int32>()\n    let f: Int32 = (-2.5).to<Int32>()\n    return (a == 2) and (b == 3) and (c == 0) and (d == 1) and (e == 0) and (f == -2)\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:        "min-overflow-wraps-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(): Bool do\n    let min: Int32 = -2147483648\n    let quotient: Int32 = min / -1\n    let remainder: Int32 = min % -1\n    return (quotient == -2147483648) and (remainder == 0)\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		// Neither String nor Strand is indexable (docs/reference.md, Text):
		// reaching the nth Rune of UTF-8 walks from the start, so a
		// positional [] would be quadratic behind O(1) syntax. Every access
		// below goes through rune_cursor() instead.
		name:        "text-conformance-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Bool do\n    let byte: UInt8 = b'\\xFF'\n    let byte_ok: Bool = byte == 255\n    let letter: Rune = '\\u{00E9}'\n    let crab: Rune = '\\u{1F980}'\n    let rune_ok: Bool = (letter == 233) and (crab == 129408)\n    let text: String = \"caf\\u{00E9} \\u{1F980}\"\n    let count: Size = text.length()\n    let length_ok: Bool = count == 6\n    let cursor: RuneCursor = text.rune_cursor()\n    let first: Rune = cursor.next()\n    cursor.next()\n    cursor.next()\n    let accented: Rune = cursor.next()\n    let index_ok: Bool = (first == 99) and (accented == 233)\n    let mut seen: Int32 = 4\n    while cursor.has_next() do\n        let value: Rune = cursor.next()\n        seen = seen + 1\n    end\n    let cursor_ok: Bool = seen == 6\n    let label: Strand = \"hexal\"\n    let label_text: String = label.to_string(h)\n    defer label_text.free(h)\n    let label_cursor: RuneCursor = label_text.rune_cursor()\n    let label_first: Rune = label_cursor.next()\n    let strand_ok: Bool = (label.length() == 5) and (label_first == 104)\n    let runes: Array<Rune, 2> = [letter, crab]\n    let view: Slice<Rune> = runes.slice(0, 2)\n    let encoded: String = String.from_runes(h, view)\n    let encoded_cursor: RuneCursor = encoded.rune_cursor()\n    let encoded_first: Rune = encoded_cursor.next()\n    let encoded_ok: Bool = (encoded.length() == 2) and (encoded_first == 233)\n    encoded.free(h)\n    return byte_ok and rune_ok and length_ok and index_ok and cursor_ok and strand_ok and encoded_ok\nend\nprint(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},

	// Tier 3: exact "[Runtime Error] ..." trap text, taken directly from the
	// generator templates that emit it (compiler/generator/packages) rather
	// than guessed.
	{
		name:        "division-by-zero-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let mut left: Int32 = 7\n    let mut right: Int32 = 0\n    let quotient: Int32 = left / right\n    print(quotient)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] numeric operation failed"},
	},
	{
		name:        "remainder-by-zero-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let mut left: Int32 = 7\n    let mut right: Int32 = 0\n    let remainder: Int32 = left % right\n    print(remainder)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] numeric operation failed"},
	},
	{
		name:        "conversion-overflow-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let big: Int64 = 300\n    let small: Int8 = big.to<Int8>()\n    print(small)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] numeric operation failed"},
	},
	{
		name:        "empty-list-pop-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    let last: Int32 = values.pop()\n    print(last)\nend\ndemo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] list index out of bounds"},
	},
	{
		name:        "missing-dict-get-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    let missing: Int32 = scores.get(2)\n    print(missing)\nend\ndemo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] dictionary key not found"},
	},
	{
		name:        "list-index-out-of-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    let first: Int32 = values[4]\n    print(first)\nend\ndemo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] list index out of bounds"},
	},
	{
		// Static bounds are compile errors and constant propagation sees
		// through local bindings, so a parameter supplies the runtime
		// bounds-check path.
		name:        "array-index-out-of-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(index: Int32) do\n    let fixed: Array<Int32, 3> = [10, 20, 30]\n    let out: Int32 = fixed[index]\n    print(out)\nend\ndemo(5)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] array index out of bounds"},
	},
	{
		name:        "array-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(stop: Int32) do\n    let fixed: Array<Int32, 3> = [10, 20, 30]\n    let view: Slice<Int32> = fixed.slice(1, stop)\n    print(view.length())\nend\ndemo(5)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] array slice bounds out of range"},
	},
	{
		name:        "list-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap, stop: Int32) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    let view: Slice<Int32> = values.slice(1, stop)\n    print(view.length())\nend\ndemo(Heap(), 5)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] list slice bounds out of range"},
	},
	{
		// String is not indexable (see the note on text-conformance-runs
		// above), so its only bounds-checked runtime path is slice.
		name:        "string-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(stop: Int32) do\n    let text: String = \"hex\"\n    let view: Slice<UInt8> = text.slice(0, stop)\n    print(view.length())\nend\ndemo(100)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] string slice bounds out of range"},
	},
	{
		name:        "malformed-utf8-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap) do\n    let bytes: Array<UInt8, 2> = [0xC3, 0x28]\n    let view: Slice<UInt8> = bytes.slice(0, 2)\n    let text: String = String.from_bytes(h, view)\n    print(text)\nend\ndemo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] invalid UTF-8 in string"},
	},
	{
		name:        "rune-cursor-exhaustion-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let text: String = \"hex\"\n    let cursor: RuneCursor = text.rune_cursor()\n    cursor.next()\n    cursor.next()\n    cursor.next()\n    let late: Rune = cursor.next()\n    print(late)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] RuneCursor has no next value"},
	},
	{
		name:        "string-literal-free-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun cleanup(h: Heap, text: String) do\n    text.free(h)\nend\ncleanup(Heap(), \"hello\")\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] cannot free a String literal"},
	},
	{
		name:        "slice-index-out-of-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    let view: Slice<Int32> = fixed.slice(0, 2)\n    let out: Int32 = view[5]\n    print(out)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] slice index out of bounds"},
	},
	{
		name:        "slice-slice-bounds-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    let view: Slice<Int32> = fixed.slice(0, 2)\n    let bad: Slice<Int32> = view.slice(0, 5)\n    print(bad.length())\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] slice slice bounds out of range"},
	},
	{
		// Same-variable mutation inside its own for-in loop is a compile-time
		// Type Error ("cannot mutate collection during iteration"), so the
		// runtime version-check trap needs a mutation the checker cannot
		// trace statically: the List lives behind a struct field, reached
		// through a pointer passed to a second function.
		name:       "collection-modified-during-iteration-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "type Holder is struct values: List<Int32> end\n" +
			"fun push_one(h: Ptr<mut Holder>) do\n    h.values.push(3)\nend\n" +
			"fun demo(heap: Heap) do\n    let mut holder: Holder = Holder(values = List<Int32>(heap))\n    defer holder.values.free(heap)\n    holder.values.push(1)\n    holder.values.push(2)\n    for value in holder.values do\n        push_one(@holder)\n    end\nend\ndemo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] collection modified during iteration"},
	},
	{
		name:        "duration-overflow-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "import\n  Time from std.time\nend\nfun demo() do\n    let d: Time.Duration = Time.seconds(18446744073709551615)\n    print(d.as_nanoseconds())\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] duration overflow"},
	},
	{
		name:        "duration-underflow-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "import\n  Time from std.time\nend\nfun demo() do\n    let small: Time.Duration = Time.seconds(1)\n    let large: Time.Duration = Time.seconds(2)\n    let diff: Time.Duration = small - large\n    print(diff.as_seconds())\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] duration underflow"},
	},
	{
		// duration_since is receiver-minus-argument ("later since earlier");
		// calling it with the chronologically earlier reading as the
		// receiver reverses the two and forces the underflow check.
		name:        "instant-subtraction-underflow-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "import\n  Time from std.time\nend\nfun demo() do\n    let first: Time.Instant = Time.now()\n    let mut i: Int32 = 0\n    while i < 1000000 do\n        i = i + 1\n    end\n    let second: Time.Instant = Time.now()\n    let diff: Time.Duration = first.duration_since(second)\n    print(diff.as_nanoseconds())\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] invalid instant subtraction"},
	},
	{
		name:        "sleep-duration-too-large-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "import\n  Time from std.time\nend\nfun demo() do\n    Time.sleep(Time.seconds(10000000000))\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] sleep duration too large"},
	},
	{
		name:        "channel-free-not-closed-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Nil | Error do\n    let ch: Channel<Int32> = try Channel<Int32>(h, 4)\n    ch.free(h)\n    return nil\nend\nlet out: Nil | Error = demo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] channel free requires a closed, empty channel"},
	},
	{
		name:        "mutex-recursive-lock-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Nil | Error do\n    let m: Mutex = try Mutex(h)\n    m.lock()\n    m.lock()\n    return nil\nend\nlet out: Nil | Error = demo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] recursive mutex lock"},
	},
	{
		// A never-unlocked mutex leaves a second Task the non-owner when it
		// calls unlock, forcing the ownership check rather than an
		// unrelated already-unlocked state.
		name:       "mutex-unlock-by-non-owner-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun locker(m: Mutex): Bool do\n    m.lock()\n    return true\nend\n" +
			"fun unlocker(m: Mutex): Bool do\n    m.unlock()\n    return true\nend\n" +
			"fun run(): Bool | Error do\n    let h: Heap = Heap()\n    let m: Mutex = try Mutex(h)\n    let t1: Task<Bool> = try spawn locker(m)\n    t1.join()\n    let t2: Task<Bool> = try spawn unlocker(m)\n    t2.join()\n    return true\nend\nlet out: Bool | Error = run()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] mutex unlock by a non-owner"},
	},
	{
		name:        "mutex-free-while-locked-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(h: Heap): Nil | Error do\n    let m: Mutex = try Mutex(h)\n    m.lock()\n    m.free(h)\n    return nil\nend\nlet out: Nil | Error = demo(Heap())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] mutex free while locked or awaited"},
	},
	{
		// join's generated wrapper releases the target task immediately
		// after a successful join, so joining the same handle twice is a
		// use-after-free, not this trap. The worker blocks on an empty
		// channel that is never sent to, so it cannot reach completion on
		// another scheduler thread before the root's detach; that keeps
		// detach's claim the only terminal claim and makes the join's trap
		// deterministic. A worker that could finish first would be released
		// by detach, and the join would touch freed memory instead -- the
		// race this blocking receive removes from the fixture.
		name:        "join-already-joined-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun worker(ch: Channel<Int32>): Bool do\n    let step: Int32 | EoS = ch.receive()\n    return true\nend\nfun run(): Bool | Error do\n    let h: Heap = Heap()\n    let ch: Channel<Int32> = try Channel<Int32>(h, 1)\n    let t: Task<Bool> = try spawn worker(ch)\n    t.detach()\n    t.join()\n    return true\nend\nlet out: Bool | Error = run()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] Task already joined or detached"},
	},
	{
		name:        "close-borrowed-stream-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "import\n  Io from std.io\nend\nfun demo(): Nil | Error do\n    let stream: Io.IO = try Io.stdout()\n    try stream.close()\n    return nil\nend\nlet out: Nil | Error = demo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] close of a borrowed stream"},
	},
	{
		name:        "pool-exhausted-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo() do\n    let pool: Pool<Int32> = Pool<Int32>(2)\n    let a: Ptr<mut Int32> = pool.allocate(1)\n    let b: Ptr<mut Int32> = pool.allocate(2)\n    let c: Ptr<mut Int32> = pool.allocate(3)\nend\ndemo()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] pool exhausted"},
	},
	{
		// The checker statically rejects destroying a pool while a slot
		// allocated IN THE SAME FUNCTION remains outstanding, so the
		// allocation is pushed behind a second function the destroying
		// function's own analysis cannot see into.
		name:       "pool-destroy-with-live-slots-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun alloc_one(pool: Pool<Int32>): Ptr<mut Int32> do\n    return pool.allocate(1)\nend\n" +
			"fun run() do\n    let mut pool: Pool<Int32> = Pool<Int32>(2)\n    let a: Ptr<mut Int32> = alloc_one(pool)\n    pool.destroy()\nend\nrun()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] pool destroy with live slots"},
	},
	{
		name:       "pool-double-free-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun release_twice(pool: Pool<Int32>, p: Ptr<mut Int32>) do\n    pool.free(p)\nend\n" +
			"fun run() do\n    let pool: Pool<Int32> = Pool<Int32>(2)\n    let a: Ptr<mut Int32> = pool.allocate(1)\n    release_twice(pool, a)\n    release_twice(pool, a)\nend\nrun()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] pool slot is not live"},
	},
	{
		name:       "pool-foreign-pointer-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun free_from_other(other: Pool<Int32>, p: Ptr<mut Int32>) do\n    other.free(p)\nend\n" +
			"fun run() do\n    let first: Pool<Int32> = Pool<Int32>(2)\n    let second: Pool<Int32> = Pool<Int32>(2)\n    let a: Ptr<mut Int32> = first.allocate(1)\n    free_from_other(second, a)\nend\nrun()\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] pointer does not name a slot in this pool"},
	},
	{
		name:        "pool-non-positive-capacity-traps",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun demo(capacity: Size) do\n    let pool: Pool<Int32> = Pool<Int32>(capacity)\nend\ndemo(0)\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] pool capacity must be positive"},
	},
	{
		// Runs on a spawned Task's bounded fiber stack so the guard page is
		// reached (after far fewer than 2_000_000_000 frames) long before
		// the loop condition would ever end it on its own; the conditional
		// base case keeps Clang's infinite-recursion analysis from
		// rejecting the function outright. The yield after the recursive
		// call is load-bearing: it is an opaque side effect whose per-frame
		// ordering a loop cannot reproduce, so neither tail-call
		// elimination nor the accumulator rewrite that would otherwise turn
		// `return recurse(n + 1) + 1` into a constant-stack loop fires at
		// -O2, and both modes reach the guard page.
		name:       "task-stack-overflow-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun recurse(n: Int32): Int32 do\n    if n < 2000000000 then\n        let next: Int32 = recurse(n + 1)\n        Task.yield()\n        return next + 1\n    end\n    return n\nend\n" +
			"fun worker(): Int32 do\n    return recurse(0)\nend\n" +
			"fun run(): Int32 | Error do\n    let t: Task<Int32> = try spawn worker()\n    return t.join()\nend\n" +
			"fun demo(): Int32 do\n    let outcome: Int32 | Error = run()\n    let value: Int32 = match outcome is\n    | Int32 then\n        outcome\n    | Error then\n        -1\n    end\n    return value\nend\nprint(demo())\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] task stack overflow"},
	},

	// Concurrency runs on the M:N scheduler over native platform threads.
	// The root-yield fixtures double as root completion with no spawned
	// child: the root parks and resumes, then completes straight out of
	// generated main.
	{
		name:        "concurrency-root-yield-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun run() do\n    print(1)\n    Task.yield()\n    print(2)\nend\nrun()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "12"},
	},
	{
		name:        "concurrency-root-yield-repeats-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun run() do\n    let mut i: Int32 = 0\n    while i < 5 do\n        Task.yield()\n        i = i + 1\n    end\n    print(i)\nend\nrun()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "5"},
	},
	{
		name:        "concurrency-root-detached-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun worker(): Bool do\n    Task.yield()\n    return true\nend\nfun run(): Nil | Error do\n    let task: Task<Bool> = try spawn worker()\n    task.detach()\n    print(7)\n    return nil\nend\nrun()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "7"},
	},
	{
		name:        "concurrency-spawn-channel-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun worker(count: Int32, ch: Channel<Int32>): Bool do\n    let mut index: Int32 = 0\n    while index < count do\n        ch.send(index)\n        Task.yield()\n        index = index + 1\n    end\n    ch.close()\n    return true\nend\nfun run(): Nil | Error do\n    let h: Heap = Heap()\n    let ch: Channel<Int32> = try Channel<Int32>(h, 8)\n    defer ch.free(h)\n    let worker_task: Task<Bool> = try spawn worker(4, ch)\n    let mut total: Int32 = 0\n    while true do\n        let step: Int32 | EoS = ch.receive()\n        if step is EoS then\n            break\n        end\n        total = total + step\n        Task.yield()\n    end\n    worker_task.join()\n    print(total)\n    return nil\nend\nrun()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "6"},
	},
	{
		name:        "concurrency-task-join-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    let first: Task<Int32> = try spawn square(6)\n    let second: Task<Int32> = try spawn square(7)\n    return first.join() + second.join()\nend\nfun demo(): Int32 do\n    let outcome: Int32 | Error = run()\n    let value: Int32 = match outcome is\n    | Int32 then\n        outcome\n    | Error then\n        0\n    end\n    return value\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "85"},
	},
	{
		name:        "concurrency-mutex-runs",
		entrypoint:  "app.hex",
		sources:     map[string]string{"app.hex": "fun worker(m: Mutex, counter: Ptr<mut Int32>): Int32 do\n    let mut index: Int32 = 0\n    while index < 100 do\n        m.lock()\n        ^counter = ^counter + 1\n        m.unlock()\n        Task.yield()\n        index = index + 1\n    end\n    return index\nend\nfun run(): Int32 | Error do\n    let h: Heap = Heap()\n    let m: Mutex = try Mutex(h)\n    defer m.free(h)\n    let mut count: Int32 = 0\n    let first: Task<Int32> = try spawn worker(m, @count)\n    let second: Task<Int32> = try spawn worker(m, @count)\n    first.join()\n    second.join()\n    return count\nend\nfun demo(): Int32 do\n    let outcome: Int32 | Error = run()\n    let value: Int32 = match outcome is\n    | Int32 then\n        outcome\n    | Error then\n        0\n    end\n    return value\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "200"},
	},
	// Task park/commit/wake stress: each fixture repeats a cheap wait-source
	// transition at least 1,000 times total across concurrently running
	// Tasks. Neither a lost nor a duplicate wake can hide behind averaging:
	// every fixture asserts one exact final total (an Atomic sum, or a
	// Channel's summed received values), so a single dropped or doubled
	// transition anywhere in the run changes the printed number. Real OS
	// thread scheduling gives each run its own timing, so a single process
	// run already exercises both a wake already notified when the dispatcher
	// commits the park and a wake landing after the Task is observably
	// parked, across its many repetitions; the tagged suite additionally
	// runs every fixture under three independent toolchains.
	{
		name:       "concurrency-yield-join-stress-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun worker(): Int32 do\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 250 do\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return i\n" +
			"end\n" +
			"fun run(): Int32 | Error do\n" +
			"    let t1: Task<Int32> = try spawn worker()\n" +
			"    let t2: Task<Int32> = try spawn worker()\n" +
			"    let t3: Task<Int32> = try spawn worker()\n" +
			"    let t4: Task<Int32> = try spawn worker()\n" +
			"    return t1.join() + t2.join() + t3.join() + t4.join()\n" +
			"end\n" +
			"fun demo(): Int32 do\n" +
			"    let outcome: Int32 | Error = run()\n" +
			"    let value: Int32 = match outcome is\n" +
			"    | Int32 then outcome\n" +
			"    | Error then -1\n" +
			"    end\n" +
			"    return value\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "1000"},
	},
	{
		name:       "concurrency-channel-stress-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun producer(ch: Channel<Int32>, count: Int32): Bool do\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < count do\n" +
			"        ch.send(1)\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    ch.close()\n" +
			"    return true\n" +
			"end\n" +
			"fun run(): Int32 | Error do\n" +
			"    let h: Heap = Heap()\n" +
			"    let ch: Channel<Int32> = try Channel<Int32>(h, 4)\n" +
			"    defer ch.free(h)\n" +
			"    let producer_task: Task<Bool> = try spawn producer(ch, 1000)\n" +
			"    let mut total: Int32 = 0\n" +
			"    while true do\n" +
			"        let step: Int32 | EoS = ch.receive()\n" +
			"        if step is EoS then\n" +
			"            break\n" +
			"        end\n" +
			"        total = total + step\n" +
			"    end\n" +
			"    producer_task.join()\n" +
			"    return total\n" +
			"end\n" +
			"fun demo(): Int32 do\n" +
			"    let outcome: Int32 | Error = run()\n" +
			"    let value: Int32 = match outcome is\n" +
			"    | Int32 then outcome\n" +
			"    | Error then -1\n" +
			"    end\n" +
			"    return value\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "1000"},
	},
	{
		name:       "concurrency-mutex-stress-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun worker(m: Mutex, counter: Ptr<mut Int32>): Bool do\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 250 do\n" +
			"        m.lock()\n" +
			"        ^counter = ^counter + 1\n" +
			"        m.unlock()\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return true\n" +
			"end\n" +
			"fun run(): Int32 | Error do\n" +
			"    let h: Heap = Heap()\n" +
			"    let m: Mutex = try Mutex(h)\n" +
			"    defer m.free(h)\n" +
			"    let mut count: Int32 = 0\n" +
			"    let t1: Task<Bool> = try spawn worker(m, @count)\n" +
			"    let t2: Task<Bool> = try spawn worker(m, @count)\n" +
			"    let t3: Task<Bool> = try spawn worker(m, @count)\n" +
			"    let t4: Task<Bool> = try spawn worker(m, @count)\n" +
			"    t1.join()\n" +
			"    t2.join()\n" +
			"    t3.join()\n" +
			"    t4.join()\n" +
			"    return count\n" +
			"end\n" +
			"fun demo(): Int32 do\n" +
			"    let outcome: Int32 | Error = run()\n" +
			"    let value: Int32 = match outcome is\n" +
			"    | Int32 then outcome\n" +
			"    | Error then -1\n" +
			"    end\n" +
			"    return value\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "1000"},
	},
	// The two join orderings the wake protocol must handle identically: a
	// join reached while the target is still certainly running (no yield
	// happens before the join call, so the scheduler has not necessarily
	// even started the child yet) and a join reached long after the target
	// has almost certainly already completed (the parent yields repeatedly
	// first, giving the child every opportunity to finish).
	{
		name:       "concurrency-join-before-completion-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun quick(): Bool do\n" +
			"    return true\n" +
			"end\n" +
			"fun run(): Bool | Error do\n" +
			"    let t: Task<Bool> = try spawn quick()\n" +
			"    return t.join()\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome: Bool | Error = run()\n" +
			"    let value: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then false\n" +
			"    end\n" +
			"    return value\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "concurrency-join-after-completion-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun quick(): Bool do\n" +
			"    return true\n" +
			"end\n" +
			"fun run(): Bool | Error do\n" +
			"    let t: Task<Bool> = try spawn quick()\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 20 do\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return t.join()\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome: Bool | Error = run()\n" +
			"    let value: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then false\n" +
			"    end\n" +
			"    return value\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	// Atomic touches no scheduler state -- no spawn, no Task, no fiber --
	// so it is not subject to the scheduler defect above and runs normally.
	{
		name:       "atomic-operations-run",
		entrypoint: "app.hex",
		// exchange returns the value it replaced (9, set by the preceding
		// store), not the value it just installed (4).
		sources:     map[string]string{"app.hex": "fun demo(): Bool do\n    let counter: Atomic<Int32> = Atomic<Int32>(5)\n    let old: Int32 = counter.fetch_add(3)\n    counter.fetch_sub(2)\n    counter.store(9)\n    let loaded: Int32 = counter.load()\n    let swapped: Int32 = counter.exchange(4)\n    let expected: Bool = counter.compare_exchange(4, 6)\n    let refused: Bool = counter.compare_exchange(4, 6)\n    let final: Int32 = counter.load()\n    return (old == 5) and (loaded == 9) and (swapped == 9) and expected and !refused and (final == 6)\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "network-address-and-dns-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Net from std.net\nend\nfun demo(h: Heap): Nil | Error do\n" +
			"    let v4 = try Net.parse_address(\"127.0.0.1\", 80)\n" +
			"    let v6 = try Net.parse_address(\"::1\", 80)\n" +
			"    let text4 = v4.format(h)\n" +
			"    let text6 = v6.format(h)\n" +
			"    let addresses = try Net.resolve(h, \"localhost\", \"80\")\n" +
			"    return nil\n" +
			"end\n" +
			"let out: Nil | Error = demo(Heap())\n"},
	},
	{
		name:       "network-tcp-loopback-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Net from std.net\nend\nfun serve(listener: Net.TcpListener): Nil | Error do\n" +
			"    let connection = try listener.accept()\n" +
			"    defer connection.close()\n" +
			"    let buffer: List<Byte> = List<Byte>(Heap())\n" +
			"    defer buffer.free(Heap())\n" +
			"    let received = try connection.read(buffer, 64)\n" +
			"    if received is Size then\n" +
			"        let view = buffer.slice(0, received)\n" +
			"        try connection.write(view)\n" +
			"    end\n" +
			"    return nil\n" +
			"end\n" +
			"fun client(address: Net.Address): Bool | Error do\n" +
			"    let connection = try Net.connect(address)\n" +
			"    defer connection.close()\n" +
			"    try connection.write(\"ping\".bytes())\n" +
			"    let buffer: List<Byte> = List<Byte>(Heap())\n" +
			"    defer buffer.free(Heap())\n" +
			"    let received = try connection.read(buffer, 64)\n" +
			"    if received is Size then\n" +
			"        return received == 4\n" +
			"    end\n" +
			"    return false\n" +
			"end\n" +
			"fun run(): Bool | Error do\n" +
			"    let address = try Net.parse_address(\"127.0.0.1\", 18734)\n" +
			"    let listener = try Net.listen(address, 4)\n" +
			"    defer listener.close()\n" +
			"    let task = try spawn serve(listener)\n" +
			"    let ok = try client(address)\n" +
			"    task.join()\n" +
			"    return ok\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome = run()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then false\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	// Repeats a full connect/accept/read/write/close round trip: each
	// iteration parks and wakes the accepting Task via a fresh native TCP
	// event, exercising the wake protocol's native (not just in-memory)
	// wait sources under real repeated host work.
	{
		name:       "network-tcp-loopback-stress-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Net from std.net\nend\nfun serve(listener: Net.TcpListener, count: Int32): Nil | Error do\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < count do\n" +
			"        let connection = try listener.accept()\n" +
			"        let buffer: List<Byte> = List<Byte>(Heap())\n" +
			"        defer buffer.free(Heap())\n" +
			"        let received = try connection.read(buffer, 64)\n" +
			"        if received is Size then\n" +
			"            let view = buffer.slice(0, received)\n" +
			"            try connection.write(view)\n" +
			"        end\n" +
			"        try connection.close()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return nil\n" +
			"end\n" +
			"fun client(address: Net.Address, count: Int32): Int32 | Error do\n" +
			"    let mut ok: Int32 = 0\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < count do\n" +
			"        let connection = try Net.connect(address)\n" +
			"        try connection.write(\"ping\".bytes())\n" +
			"        let buffer: List<Byte> = List<Byte>(Heap())\n" +
			"        defer buffer.free(Heap())\n" +
			"        let received = try connection.read(buffer, 64)\n" +
			"        if received is Size then\n" +
			"            if received == 4 then\n" +
			"                ok = ok + 1\n" +
			"            end\n" +
			"        end\n" +
			"        try connection.close()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return ok\n" +
			"end\n" +
			"fun run(): Int32 | Error do\n" +
			"    let address = try Net.parse_address(\"127.0.0.1\", 18744)\n" +
			"    let listener = try Net.listen(address, 4)\n" +
			"    defer listener.close()\n" +
			"    let task = try spawn serve(listener, 20)\n" +
			"    let ok = try client(address, 20)\n" +
			"    task.join()\n" +
			"    return ok\n" +
			"end\n" +
			"fun demo(): Int32 do\n" +
			"    let outcome = run()\n" +
			"    let result: Int32 = match outcome is\n" +
			"    | Int32 then outcome\n" +
			"    | Error then -1\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "20"},
	},
	{
		name:       "process-options-and-pipe-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Proc from std.process\nend\nfun demo(h: Heap): Nil | Error do\n" +
			"    let arguments = List<String>(h)\n" +
			"    arguments.push(\"--flag\")\n" +
			"    let variables = List<Proc.EnvironmentVariable>(h)\n" +
			"    variables.push(Proc.EnvironmentVariable(name = \"KEY\", value = \"value\"))\n" +
			"    let options = Proc.ProcessOptions(\n" +
			"        program = \"does-not-exist-xyz\",\n" +
			"        arguments = arguments,\n" +
			"        environment = Proc.Environment.Replace(values = variables),\n" +
			"        working_directory = nil,\n" +
			"        input = Proc.ProcessStream.Pipe(),\n" +
			"        output = Proc.ProcessStream.Pipe(),\n" +
			"        error = Proc.ProcessStream.Ignore(),\n" +
			"    )\n" +
			"    let started = try Proc.start(options)\n" +
			"    try started.process.terminate()\n" +
			"    let status = try started.process.wait()\n" +
			"    let exit_code: Int64 = match status is\n" +
			"    | Proc.ExitStatus.Exited then status.code\n" +
			"    | Proc.ExitStatus.Terminated then -1\n" +
			"    end\n" +
			"    let input = started.input\n" +
			"    if input != nil then\n" +
			"        try input.write(\"hi\".bytes())\n" +
			"        try input.shutdown()\n" +
			"        try input.close()\n" +
			"    end\n" +
			"    let output = started.output\n" +
			"    if output != nil then\n" +
			"        let buffer: List<Byte> = List<Byte>(h)\n" +
			"        defer buffer.free(h)\n" +
			"        let received = try output.read(buffer, 64)\n" +
			"        if received is Size then\n" +
			"            try output.write(\"ping\".bytes())\n" +
			"        end\n" +
			"        try output.shutdown()\n" +
			"        try output.close()\n" +
			"    end\n" +
			"    try started.process.close()\n" +
			"    return nil\n" +
			"end\n" +
			"let out: Nil | Error = demo(Heap())\n"},
	},
	{
		name:       "process-spawn-wait-runs",
		entrypoint: "app.hex",
		hosts:      []string{"windows"},
		sources: map[string]string{"app.hex": "import\n  Proc from std.process\nend\nfun run(): Bool | Error do\n" +
			"    let arguments = List<String>(Heap())\n" +
			"    arguments.push(\"/c\")\n" +
			"    arguments.push(\"exit\")\n" +
			"    arguments.push(\"7\")\n" +
			"    let options = Proc.ProcessOptions(\n" +
			"        program = \"cmd.exe\",\n" +
			"        arguments = arguments,\n" +
			"        environment = Proc.Environment.Inherit(),\n" +
			"        working_directory = nil,\n" +
			"        input = Proc.ProcessStream.Ignore(),\n" +
			"        output = Proc.ProcessStream.Ignore(),\n" +
			"        error = Proc.ProcessStream.Ignore(),\n" +
			"    )\n" +
			"    let started = try Proc.start(options)\n" +
			"    let status = try started.process.wait()\n" +
			"    try started.process.close()\n" +
			"    return match status is\n" +
			"    | Proc.ExitStatus.Exited then status.code == 7\n" +
			"    | Proc.ExitStatus.Terminated then false\n" +
			"    end\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome = run()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then false\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "process-pipe-echo-runs",
		entrypoint: "app.hex",
		hosts:      []string{"windows"},
		sources: map[string]string{"app.hex": "import\n  Proc from std.process\nend\nfun run(): Bool | Error do\n" +
			"    let arguments = List<String>(Heap())\n" +
			"    arguments.push(\"/c\")\n" +
			"    arguments.push(\"echo\")\n" +
			"    arguments.push(\"hi\")\n" +
			"    let options = Proc.ProcessOptions(\n" +
			"        program = \"cmd.exe\",\n" +
			"        arguments = arguments,\n" +
			"        environment = Proc.Environment.Inherit(),\n" +
			"        working_directory = nil,\n" +
			"        input = Proc.ProcessStream.Ignore(),\n" +
			"        output = Proc.ProcessStream.Pipe(),\n" +
			"        error = Proc.ProcessStream.Ignore(),\n" +
			"    )\n" +
			"    let started = try Proc.start(options)\n" +
			"    let buffer: List<Byte> = List<Byte>(Heap())\n" +
			"    defer buffer.free(Heap())\n" +
			"    let mut ok: Bool = false\n" +
			"    let output = started.output\n" +
			"    if output != nil then\n" +
			"        while true do\n" +
			"            let received = try output.read(buffer, 64)\n" +
			"            if received is EoS then\n" +
			"                break\n" +
			"            end\n" +
			"            if received == 0 then\n" +
			"                break\n" +
			"            end\n" +
			"        end\n" +
			"        try output.close()\n" +
			"        ok = (buffer.length() >= 2) and (buffer[0] == 104) and (buffer[1] == 105)\n" +
			"    end\n" +
			"    let status = try started.process.wait()\n" +
			"    try started.process.close()\n" +
			"    let exited: Bool = match status is\n" +
			"    | Proc.ExitStatus.Exited then status.code == 0\n" +
			"    | Proc.ExitStatus.Terminated then false\n" +
			"    end\n" +
			"    return ok and exited\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome = run()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then false\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "file-in-collection-positions-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Fs from std.fs\nend\nfun make(h: Heap): Nil | Error do\n" +
			"    let x = try Fs.open(\"a\", Fs.FileMode.Write())\n" +
			"    let files: List<Fs.File> = List<Fs.File>(h)\n" +
			"    defer files.free(h)\n" +
			"    files.push(x)\n" +
			"    try files[0].close()\n" +
			"    let y = try Fs.open(\"b\", Fs.FileMode.Write())\n" +
			"    let fixed: Array<Fs.File, 1> = [y]\n" +
			"    try fixed[0].close()\n" +
			"    let z = try Fs.open(\"c\", Fs.FileMode.Write())\n" +
			"    let byOwner: Dict<Int32, Fs.File> = Dict<Int32, Fs.File>(h)\n" +
			"    defer byOwner.free(h)\n" +
			"    byOwner.insert(1, z)\n" +
			"    try byOwner.get(1).close()\n" +
			"    return nil\n" +
			"end\n" +
			"let out: Nil | Error = make(Heap())\n"},
	},
	{
		name:       "process-in-collection-positions-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Proc from std.process\nend\nfun make(h: Heap): Nil | Error do\n" +
			"    let arguments = List<String>(h)\n" +
			"    let options = Proc.ProcessOptions(\n" +
			"        program = \"does-not-exist-xyz\",\n" +
			"        arguments = arguments,\n" +
			"        environment = Proc.Environment.Inherit(),\n" +
			"        working_directory = nil,\n" +
			"        input = Proc.ProcessStream.Ignore(),\n" +
			"        output = Proc.ProcessStream.Ignore(),\n" +
			"        error = Proc.ProcessStream.Ignore(),\n" +
			"    )\n" +
			"    let started = try Proc.start(options)\n" +
			"    let processes: List<Proc.Process> = List<Proc.Process>(h)\n" +
			"    defer processes.free(h)\n" +
			"    processes.push(started.process)\n" +
			"    try processes[0].close()\n" +
			"    let started2 = try Proc.start(options)\n" +
			"    let fixed: Array<Proc.Process, 1> = [started2.process]\n" +
			"    try fixed[0].close()\n" +
			"    return nil\n" +
			"end\n" +
			"let out: Nil | Error = make(Heap())\n"},
	},
	{
		name:       "signals-subscribe-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Sig from std.signal\nend\nfun wait_for_shutdown(): Sig.Signal | EoS | Error do\n" +
			"    let wanted: Array<Sig.Signal, 2> = [Sig.Signal.Interrupt(), Sig.Signal.Hangup()]\n" +
			"    let signals = try Sig.subscribe(wanted.slice(0, wanted.length()))\n" +
			"    defer signals.close()\n" +
			"    let signal = try signals.next()\n" +
			"    return signal\n" +
			"end\n" +
			"let out: Sig.Signal | EoS | Error = wait_for_shutdown()\n"},
	},
	{
		name:       "signals-close-wakes-waiter-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Sig from std.signal\nend\nfun waiter(s: Sig.Signals): Sig.Signal | EoS | Error do\n" +
			"    return s.next()\n" +
			"end\n" +
			"fun run(): Bool | Error do\n" +
			"    let wanted: Array<Sig.Signal, 1> = [Sig.Signal.Interrupt()]\n" +
			"    let signals = try Sig.subscribe(wanted.slice(0, wanted.length()))\n" +
			"    let task = try spawn waiter(signals)\n" +
			"    try signals.close()\n" +
			"    let outcome = task.join()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Sig.Signal then false\n" +
			"    | EoS then true\n" +
			"    | Error then true\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome = run()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then false\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "terminal-is-attached-and-size-compiles",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Io from std.io,\n  Term from std.terminal\nend\nfun describe(stream: Io.IO): Term.TerminalSize | Error do\n" +
			"    let attached: Bool = try Term.is_attached(stream)\n" +
			"    if attached then\n" +
			"        let size: Term.TerminalSize = try Term.size(stream)\n" +
			"        return size\n" +
			"    end\n" +
			"    return Error(ErrorKind.InvalidInput(), \"not attached\")\n" +
			"end\n" +
			"fun run(): Term.TerminalSize | Error do\n" +
			"    let stream: Io.IO = try Io.stdout()\n" +
			"    return describe(stream)\n" +
			"end\n" +
			"let out: Term.TerminalSize | Error = run()\n"},
	},
	{
		name:       "terminal-redirected-stdout-not-attached-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Io from std.io,\n  Term from std.terminal\nend\nfun check(): Bool | Error do\n" +
			"    let stream: Io.IO = try Io.stdout()\n" +
			"    let attached: Bool = try Term.is_attached(stream)\n" +
			"    return attached\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome = check()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Bool then outcome\n" +
			"    | Error then true\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "false"},
	},
	{
		name:       "terminal-redirected-size-is-invalid-input-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Io from std.io,\n  Term from std.terminal\nend\nfun kind_is_invalid_input(kind: ErrorKind): Bool do\n" +
			"    let result: Bool = match kind is\n" +
			"    | ErrorKind.InvalidInput then true\n" +
			"    | else then false\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"fun check(): Term.TerminalSize | Error do\n" +
			"    let stream: Io.IO = try Io.stdout()\n" +
			"    let size: Term.TerminalSize = try Term.size(stream)\n" +
			"    return size\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let outcome = check()\n" +
			"    let result: Bool = match outcome is\n" +
			"    | Term.TerminalSize then false\n" +
			"    | Error then kind_is_invalid_input(outcome.kind)\n" +
			"    end\n" +
			"    return result\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "true"},
	},
	{
		name:       "unsafe-slice-bridge-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun total(p: Ptr<mut Int32>, count: Size): Int32 do\n" +
			"    let mut sum: Int32 = 0\n" +
			"    unsafe do\n" +
			"        let values: Slice<Int32> = Slice<Int32>.from_pointer(p, count)\n" +
			"        for value in values do\n" +
			"            sum = sum + value\n" +
			"        end\n" +
			"    end\n" +
			"    return sum\n" +
			"end\n" +
			"fun demo(h: Heap): Int32 do\n" +
			"    let p: Ptr<mut Int32> = h.allocate<Int32>(41)\n" +
			"    defer h.free(p)\n" +
			"    return total(p, 1)\n" +
			"end\n" +
			"print(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "41"},
	},
	{
		name:       "fenced-pointer-arithmetic-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun demo(h: Heap): Int32 do\n" +
			"    let block: Ptr<mut Array<Int32, 4>> = h.allocate<Array<Int32, 4>>([10, 20, 30, 40])\n" +
			"    defer h.free(block)\n" +
			"    unsafe do\n" +
			"        let first: Ptr<mut Int32> = block.cast<Int32>()\n" +
			"        let mut total: Int32 = 0\n" +
			"        let mut index: Size = 0\n" +
			"        while index < 4 do\n" +
			"            total = total + first[index]\n" +
			"            index = index + 1\n" +
			"        end\n" +
			"        let third: Ptr<mut Int32> = first.offset(2)\n" +
			"        first[0] = 1\n" +
			"        return total + (^third) + first[0]\n" +
			"    end\n" +
			"end\n" +
			"print(demo(Heap()))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "131"},
	},
	{
		name:       "aligned-allocation-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun demo(h: Heap, dynamic: Size): Int32 do\n" +
			"    let over: Ptr<mut Int32> = h.allocate_aligned<Int32>(13, 64)\n" +
			"    defer h.free(over)\n" +
			"    let natural: Ptr<mut Int32> = h.allocate_aligned<Int32>(29, align_of<Int32>())\n" +
			"    defer h.free(natural)\n" +
			"    let under: Ptr<mut Int32> = h.allocate_aligned<Int32>(7, 1)\n" +
			"    defer h.free(under)\n" +
			"    let moving: Ptr<mut Int32> = h.allocate_aligned<Int32>(3, dynamic)\n" +
			"    defer h.free(moving)\n" +
			"    return (^over) + (^natural) + (^under) + (^moving)\n" +
			"end\n" +
			"print(demo(Heap(), 128))\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "52"},
	},
	{
		name:       "aligned-allocation-zero-alignment-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun demo(h: Heap, alignment: Size): Int32 do\n" +
			"    let p: Ptr<mut Int32> = h.allocate_aligned<Int32>(13, alignment)\n" +
			"    defer h.free(p)\n" +
			"    return ^p\n" +
			"end\n" +
			"print(demo(Heap(), 0))\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] invalid allocation alignment"},
	},
	{
		name:       "aligned-allocation-non-power-of-two-traps",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "fun demo(h: Heap, alignment: Size): Int32 do\n" +
			"    let p: Ptr<mut Int32> = h.allocate_aligned<Int32>(13, alignment)\n" +
			"    defer h.free(p)\n" +
			"    return ^p\n" +
			"end\n" +
			"print(demo(Heap(), 48))\n"},
		expectation: &processExpectation{requiredStderrSubstring: "[Runtime Error] invalid allocation alignment"},
	},
	// One print call is one standard-output transaction. Both concurrency
	// fixtures below have every writer emit the identical byte sequence, so
	// the expected output is independent of which writer wins a race: the
	// only way to fail them is for one call's fragments to be split by
	// another writer, which shows up immediately as interleaved text.
	{
		name:       "print-tasks-never-interleave-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "type Point is struct\n    x: Int32,\n    y: Int32,\nend\n" +
			"fun writer(): Bool do\n" +
			"    let p: Point = Point(x = 1, y = 2)\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 4 do\n" +
			"        print(p)\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return true\n" +
			"end\n" +
			"fun run(): Nil | Error do\n" +
			"    let first: Task<Bool> = try spawn writer()\n" +
			"    let second: Task<Bool> = try spawn writer()\n" +
			"    first.join()\n" +
			"    second.join()\n" +
			"    return nil\n" +
			"end\n" +
			"run()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: strings.Repeat("Point { x = 1, y = 2 }", 8)},
	},
	{
		name:       "print-and-io-write-never-interleave-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "import\n  Io from std.io\nend\ntype Point is struct\n    x: Int32,\n    y: Int32,\nend\n" +
			"fun writer(out: Io.IO): Bool do\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 4 do\n" +
			"        let wrote: Size | Error = out.write(\"Point { x = 1, y = 2 }\".bytes())\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return true\n" +
			"end\n" +
			"fun printer(): Bool do\n" +
			"    let p: Point = Point(x = 1, y = 2)\n" +
			"    let mut i: Int32 = 0\n" +
			"    while i < 4 do\n" +
			"        print(p)\n" +
			"        Task.yield()\n" +
			"        i = i + 1\n" +
			"    end\n" +
			"    return true\n" +
			"end\n" +
			"fun run(): Nil | Error do\n" +
			"    let out: Io.IO = try Io.stdout()\n" +
			"    let first: Task<Bool> = try spawn writer(out)\n" +
			"    let second: Task<Bool> = try spawn printer()\n" +
			"    first.join()\n" +
			"    second.join()\n" +
			"    return nil\n" +
			"end\n" +
			"run()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: strings.Repeat("Point { x = 1, y = 2 }", 8)},
	},
	// The formatter families the print catalog does not otherwise execute:
	// Rune, Strand, Array, Slice, and a union variant with a payload. Each
	// one appends to the same call's builder, so a helper threading the
	// wrong destination shows up as missing output. Error printing is not
	// included: a program that prints an Error emits a Strand length helper
	// the String component only declares alongside interpolation, which is a
	// separate component-selection defect this suite would otherwise trip on.
	{
		name:       "print-remaining-formatters-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "type Shape is union\n" +
			"| Circle as r: Int32 end\n" +
			"| Square as a: Int32 end\n" +
			"end\n" +
			"fun demo() do\n" +
			"    let letter: Rune = (65).to<Rune>()\n" +
			"    let label: Strand = \"tag\"\n" +
			"    let mut fixed: Array<Int32, 2> = [1, 2]\n" +
			"    let view: Slice<Int32> = fixed.slice(0, 2)\n" +
			"    let shape: Shape = Shape.Circle(r = 7)\n" +
			"    print(letter, label, fixed, view, shape)\n" +
			"end\n" +
			"demo()\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "Atag[1, 2][1, 2]Shape.Circle { r = 7 }"},
	},
	// The builder's inline storage is 256 bytes, so these lengths sit either
	// side of its first growth: an empty call, the last inline length, the
	// exact boundary, the first grown length, a call that grows between two
	// arguments, and one far past any doubling step.
	{
		name:       "print-buffer-growth-boundaries-runs",
		entrypoint: "app.hex",
		sources: map[string]string{"app.hex": "print(\"\")\n" +
			"print(\"" + strings.Repeat("a", 255) + "\")\n" +
			"print(\"" + strings.Repeat("b", 256) + "\")\n" +
			"print(\"" + strings.Repeat("c", 257) + "\")\n" +
			"print(\"" + strings.Repeat("d", 200) + "\", \"" + strings.Repeat("e", 100) + "\")\n" +
			"print(\"" + strings.Repeat("f", 5000) + "\")\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: strings.Repeat("a", 255) + strings.Repeat("b", 256) +
			strings.Repeat("c", 257) + strings.Repeat("d", 200) + strings.Repeat("e", 100) + strings.Repeat("f", 5000)},
	},

	// Handwritten C interoperability. Every foreign ABI fact is
	// target-dependent, so each fixture selects the qualified Linux profile
	// (LP64). They use standard headers only: the harness owns no include
	// path or extra C source, and the C library supplies the implementation.
	{
		name:       "foreign-scalars-compiles",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <stdlib.h> do\n" +
			"    fun c_abs as \"abs\"(value: Int32 as \"int\"): Int32 as \"int\"\n" +
			"end\n" +
			"extern c from <string.h> do\n" +
			"    fun c_strlen as \"strlen\"(text: Ptr<Byte> | Nil as \"const char *\"): Size as \"size_t\"\n" +
			"end\n" +
			"fun demo(text: String): Int32 do\n" +
			"    unsafe do\n" +
			"        let magnitude: Int32 = c_abs(-7)\n" +
			"        let length: Size = c_strlen(text.c_pointer())\n" +
			"        return magnitude + length.to<Int32>()\n" +
			"    end\n" +
			"end\n"},
	},
	{
		name:       "foreign-opaque-and-record-compiles",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <stdio.h> do\n" +
			"    type File as \"FILE\" is opaque\n" +
			"end\n" +
			"extern c from <time.h> do\n" +
			"    type Tm as \"struct tm\" is struct\n" +
			"        mut tm_sec: Int32,\n" +
			"        mut tm_min: Int32,\n" +
			"        mut tm_hour: Int32,\n" +
			"        mut tm_mday: Int32,\n" +
			"        mut tm_mon: Int32,\n" +
			"        mut tm_year: Int32,\n" +
			"        mut tm_wday: Int32,\n" +
			"        mut tm_yday: Int32,\n" +
			"        mut tm_isdst: Int32,\n" +
			"    end\n" +
			"    fun c_clock as \"clock\"(): Int64 as \"long long\"\n" +
			"end\n" +
			"fun demo(stream: Ptr<File> | Nil, when: Ptr<mut Tm>) do\n" +
			"    unsafe do\n" +
			"        let ticks: Int64 = c_clock()\n" +
			"        let seconds: Int32 = when.tm_sec\n" +
			"        let opened: Bool = stream != nil\n" +
			"    end\n" +
			"end\n"},
	},
	{
		name:       "foreign-constants-globals-compiles",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <limits.h> do\n" +
			"    constant int_max as \"INT_MAX\": Int32\n" +
			"end\n" +
			"extern c from <stdlib.h> do\n" +
			"    constant exit_success as \"EXIT_SUCCESS\": Int32\n" +
			"end\n" +
			"extern c from <errno.h> do\n" +
			"    global mut errno_value as \"errno\": Int32\n" +
			"end\n" +
			"fun demo(): Int32 do\n" +
			"    let highest: Int32 = int_max\n" +
			"    unsafe do\n" +
			"        errno_value = exit_success\n" +
			"        let current: Int32 = errno_value\n" +
			"    end\n" +
			"    return highest\n" +
			"end\n"},
	},
	{
		name:       "foreign-buffer-bridge-compiles",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <stdio.h> do\n" +
			"    type File as \"FILE\" is opaque\n" +
			"    fun c_fgets as \"fgets\"(buffer: Ptr<mut Byte> | Nil as \"char *\", count: Int32 as \"int\", stream: Ptr<mut File> | Nil as \"FILE *\"): Ptr<mut Byte> | Nil as \"char *\"\n" +
			"end\n" +
			"extern c from <stdlib.h> do\n" +
			"    fun c_malloc as \"malloc\"(size: Size as \"size_t\"): Ptr<mut Unknown> | Nil as \"void *\"\n" +
			"    fun c_free as \"free\"(memory: Ptr<mut Unknown> | Nil as \"void *\")\n" +
			"end\n" +
			"fun demo(buffer: Slice<mut Byte>, stream: Ptr<mut File>) do\n" +
			"    unsafe do\n" +
			"        let line: Ptr<mut Byte> | Nil = c_fgets(buffer.pointer(), 256, stream)\n" +
			"        let region: Ptr<mut Unknown> | Nil = c_malloc(16)\n" +
			"        c_free(region)\n" +
			"    end\n" +
			"end\n"},
	},
	{
		name:        "foreign-call-runs",
		entrypoint:  "app.hex",
		project:     compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources:     map[string]string{"app.hex": "extern c from <stdlib.h> do\n    fun c_abs as \"abs\"(value: Int32 as \"int\"): Int32 as \"int\"\nend\nfun demo(): Int32 do\n    unsafe do\n        return c_abs(-7)\n    end\nend\nprint(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "7"},
	},
	// A header-only library: <stdckdint.h> supplies the checked arithmetic as
	// a type-generic macro, with no separate translation unit or link input.
	{
		name:       "foreign-header-only-compiles",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <stdckdint.h> do\n" +
			"    fun c_check_add as \"ckd_add\"(result: Ptr<mut Int32>, left: Int32 as \"int\", right: Int32 as \"int\"): Bool\n" +
			"end\n" +
			"fun demo(): Bool do\n" +
			"    let mut total: Int32 = 0\n" +
			"    unsafe do\n" +
			"        return c_check_add(@total, 1, 2)\n" +
			"    end\n" +
			"end\n"},
	},
	// A complete record passed and returned by value: `div_t` is returned by
	// value and its fields are read directly.
	{
		name:       "foreign-record-by-value-runs",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <stdlib.h> do\n" +
			"    type DivT as \"div_t\" is struct\n" +
			"        mut quot: Int32,\n" +
			"        mut rem: Int32,\n" +
			"    end\n" +
			"    fun c_div as \"div\"(numer: Int32 as \"int\", denom: Int32 as \"int\"): DivT\n" +
			"end\n" +
			"fun demo(): Int32 do\n" +
			"    unsafe do\n" +
			"        let result: DivT = c_div(7, 2)\n" +
			"        return result.quot\n" +
			"    end\n" +
			"end\n" +
			"print(demo())\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "3"},
	},
	// Typed rest parameters: zero, one, and many elements, a generic rest
	// function, an indirect call through Fun<(T...)>, a deferred rest call, and
	// two spawned rest calls with different rest counts. Prints 23 (the summed
	// value), then 2 (the deferred call's captured element count), then 3 (the
	// second spawn's element count), concatenated with no separators.
	{
		name:       "rest-parameters-run",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "fun sum(rest: Int32...): Int32 do\n    let mut total: Int32 = 0\n    for value in rest do\n        total = total + value\n    end\n    return total\nend\n" +
			"fun first<T>(value: T, rest: T...): T do\n    return value\nend\n" +
			"fun log(rest: Int32...) do\n    print(rest.length())\nend\n" +
			"fun classify(rest: Int32...): Size | Error do\n    return rest.length()\nend\n" +
			"fun demo(): Size | Error do\n    let two: Task<Size | Error> = try spawn classify(1, 2)\n    let firstTwo: Size | Error = two.join()\n    if firstTwo is Error then\n        return firstTwo\n    end\n    let three: Task<Size | Error> = try spawn classify(4, 5, 6)\n    return three.join()\nend\n" +
			"fun run() do\n    defer log(4, 5)\n    let empty: Int32 = sum()\n    let one: Int32 = sum(5)\n    let many: Int32 = sum(1, 2, 3)\n    let generic: Int32 = first(9, 8, 7)\n    let f: Fun<(Int32...): Int32> = sum\n    print(empty + one + many + generic + f(1, 2))\nend\n" +
			"run()\n" +
			"let outcome: Size | Error = demo()\nif outcome is Error then\n    print(\"spawn-error\")\nelse\n    print(outcome)\nend\n"},
		expectation: &processExpectation{zeroExit: true, exactStdout: "2323"},
	},
	// A library-shaped surface without the library: the same shape a real
	// graphics binding has (opaque handle, open and close calls, a buffer
	// call, a constant, and a readable foreign global) over standard headers.
	{
		name:       "foreign-library-shaped-compiles",
		entrypoint: "app.hex",
		project:    compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU},
		sources: map[string]string{"app.hex": "extern c from <stdio.h> do\n" +
			"    type File as \"FILE\" is opaque\n" +
			"    constant eof as \"EOF\": Int32\n" +
			"    global std_out as \"stdout\": Ptr<mut File>\n" +
			"    fun c_fclose as \"fclose\"(stream: Ptr<mut File> | Nil as \"FILE *\"): Int32 as \"int\"\n" +
			"    fun c_fgets as \"fgets\"(buffer: Ptr<mut Byte> | Nil as \"char *\", count: Int32 as \"int\", stream: Ptr<mut File> | Nil as \"FILE *\"): Ptr<mut Byte> | Nil as \"char *\"\n" +
			"end\n" +
			"extern c from <errno.h> do\n" +
			"    global errno_value as \"errno\": Int32\n" +
			"end\n" +
			"fun demo(buffer: Slice<mut Byte>): Bool do\n" +
			"    unsafe do\n" +
			"        let stream: Ptr<mut File> | Nil = std_out\n" +
			"        if stream != nil then\n" +
			"            let line: Ptr<mut Byte> | Nil = c_fgets(buffer.pointer(), 16, stream)\n" +
			"            let stopped: Int32 = c_fclose(stream)\n" +
			"            let sentinel: Int32 = eof\n" +
			"            let failure: Int32 = errno_value\n" +
			"        end\n" +
			"        return true\n" +
			"    end\n" +
			"end\n"},
	},
}
