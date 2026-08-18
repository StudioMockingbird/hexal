//go:build c23

package c23validation

import "testing"

// Smoke-check that a representative array/view program generates C that gcc
// accepts with -std=c23. Not part of the default suite gate (needs gcc).
func c23GeneratedArrayViewCCompiles(t *testing.T) {
	source := "type Pair = { mut values: Array<Int32, 2>, }\nfun sum(values: View<Int32>): Int32 do\n    return values[0] + values[1]\nend\nfun demo() do\n    mut pair: Pair = Pair { values = [3, 4], }\n    view: View<Int32> = pair.values.slice(0, 2)\n    total: Int32 = sum(view)\n    last: Int32 = view[1]\n    pair.values[0] = 9\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// Smoke-check that an owning List program generates C that gcc accepts with
// -std=c23: growth, bounds traps, and shallow String handle copies.
func c23GeneratedListCCompiles(t *testing.T) {
	source := "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values.set(0, 9)\n    first: Int32 = values[0]\n    values[1] = 5\n    last: Int32 = values.pop()\n    values.clear()\n    values.push(7)\n    view: View<Int32> = values.slice(0, 1)\n    total: Int32 = view[0]\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    runtime: String = \"bob\".to_string(h)\n    names.push(runtime)\n    popped: String = names.pop()\n    popped.free(h)\n    name: String = names[0]\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// Smoke-check that an owning Dict program generates C that gcc accepts with
// -std=c23: hashing, probing, growth, and shallow String value copies.
func c23GeneratedDictCCompiles(t *testing.T) {
	source := "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool = scores.contains(1)\n    first: Int32 = scores.get(1)\n    removed: Int32 = scores.remove(2)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    score: Int32 = labels.get(\"alice\")\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"bob\")\n    name: String = people.get(1)\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// Smoke-check that a comparison program generates C that gcc accepts with
// -std=c23: lossless widening, deep object/sequence equality, pointer
// identity, and String ordering.
func c23GeneratedEqualityCCompiles(t *testing.T) {
	source := "type Point = { x: Int32, y: Int32, }\ntype Shape = | Circle as { r: Int32, } | Square as { a: Int32, }\nfun demo(h: Heap) do\n    left: Point = Point { x = 1, y = 2, }\n    right: Point = Point { x = 1, y = 2, }\n    same: Bool = left == right\n    different: Bool = left != right\n    i32: Int32 = 1\n    i64: Int64 = 2\n    widened: Bool = i32 == i64\n    text: String = \"abc\"\n    other: String = \"abd\"\n    textOrder: Bool = text < other\n    fixed: Array<Int32, 2> = [1, 2]\n    twin: Array<Int32, 2> = [1, 2]\n    arrays: Bool = fixed == twin\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    lists: Bool = values == values\n    circle: Shape = Shape.Circle { r = 1, }\n    square: Shape = Shape.Square { a = 1, }\n    shapes: Bool = circle == square\n    mut value: Int32 = 3\n    pointer: Ptr<Int32> = ref value\n    twinPointer: Ptr<Int32> = pointer\n    pointers: Bool = pointer == twinPointer\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// Smoke-check that an owning String program generates C that gcc accepts
// with -std=c23: literals, shallow handle copy, concat, and byte views.
func c23GeneratedStringCCompiles(t *testing.T) {
	source := "fun make_text(h: Heap): String do\n    return \"ready\".to_string(h)\nend\nfun demo(h: Heap) do\n    text: String = make_text(h)\n    defer text.free(h)\n    loud: String = text.concat(h, \"!\")\n    raw: View<UInt8> = text.bytes()\n    first: UInt8 = raw[0]\n    part: View<UInt8> = text.slice(0, 2)\n    second: UInt8 = part[1]\n    loud.free(h)\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// Runtime conformance: an owning List computes length, at, set, pop, clear,
// slice, and String element round-trips through the checked heap machinery.
func c23GeneratedListRuns(t *testing.T) {
	source := "fun demo(h: Heap): Bool do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values.set(0, 9)\n    first: Int32 = values[0]\n    values[1] = 5\n    last: Int32 = values.pop()\n    values.clear()\n    values.push(7)\n    view: View<Int32> = values.slice(0, 1)\n    total: Int32 = view[0]\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    runtime: String = \"bob\".to_string(h)\n    names.push(runtime)\n    popped: String = names.pop()\n    popped.free(h)\n    name: String = names[0]\n    return first == 9 and last == 5 and total == 7 and name.length() == 5\nend\nprint(demo(Heap.new()))\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
}

// Runtime conformance: an owning Dict computes insert, contains, get, remove,
// growth, and Strand-key lookup through the checked heap machinery.
func c23GeneratedDictRuns(t *testing.T) {
	source := "fun demo(h: Heap): Bool do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool = scores.contains(1)\n    first: Int32 = scores.get(1)\n    removed: Int32 = scores.remove(2)\n    scores.insert(3, 30)\n    scores.insert(4, 40)\n    scores.insert(5, 50)\n    grown: Int32 = scores.get(5)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    score: Int32 = labels.get(\"alice\")\n    return present and first == 10 and removed == 20 and grown == 50 and score == 1\nend\nprint(demo(Heap.new()))\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
}

// Runtime conformance: to_string, concat, bytes, slice, and free compute
// byte-accurate results through the checked heap machinery.
func c23GeneratedStringRuns(t *testing.T) {
	source := "fun demo(h: Heap): Bool do\n    text: String = \"ready\".to_string(h)\n    defer text.free(h)\n    loud: String = text.concat(h, \"!\")\n    defer loud.free(h)\n    ok: Bool = loud.length() == 6\n    part: View<UInt8> = text.slice(0, 2)\n    second: UInt8 = part[1]\n    return ok and second == 101\nend\nprint(demo(Heap.new()))\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
}
