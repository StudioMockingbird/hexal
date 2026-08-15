//go:build c23

package c23validation

import "testing"

// Smoke-check that conversions, defined arithmetic, Size, for-in, and text
// iteration (RFC 0016/0017/0036/0028) generate C that gcc accepts.
func c23GeneratedNumericAndIterationCCompiles(t *testing.T) {
	source := "fun demo(h: Heap) do\n    wide: Int64 = 9_000_000_000\n    narrowed: Int8 = wide.to<Int8>()\n    wrapped: UInt8 = (200).to<UInt8>()\n    whole: Int32 = 3.75.to<Int32>()\n    mut left: Int32 = 7\n    mut right: Int32 = 3\n    quotient: Int32 = left / right\n    remainder: Int32 = left % right\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    mut total: Int32 = 0\n    for value in fixed do\n        total = total + value\n    end\n    for i, value in fixed do\n        total = total + value + i.to<Int32>()\n    end\n    view: View<Int32> = fixed.slice(0, 2)\n    for value in view do\n        total = total + value\n    end\n    text: String = \"cafe\"\n    mut runes: Int32 = 0\n    for rune in text do\n        runes = runes + 1\n    end\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    for value in values do\n        total = total + value\n    end\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    for key, value in scores do\n        total = total + key + value\n    end\n    size: Size = values.length()\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// RFC 0016 runtime conformance: float-to-integer conversion truncates
// toward zero, then checks the destination range.
func c23GeneratedFloatToIntegerTruncationRuns(t *testing.T) {
	source := "fun demo(): Bool do\n    a: Int32 = 2.5.to<Int32>()\n    b: Int32 = 3.5.to<Int32>()\n    c: Int32 = 0.5.to<Int32>()\n    d: Int32 = 1.5.to<Int32>()\n    e: Int32 = (-0.5).to<Int32>()\n    f: Int32 = (-2.5).to<Int32>()\n    return a == 2 and b == 3 and c == 0 and d == 1 and e == 0 and f == -2\nend\nprint(demo())\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
}

// RFC 0017 runtime conformance: a signed type's MIN / -1 yields MIN and
// MIN % -1 yields zero, and dynamic division by zero traps.
func c23GeneratedMinOverflowAndDivisionTraps(t *testing.T) {
	values := "fun demo(): Bool do\n    min: Int32 = -2147483648\n    quotient: Int32 = min / -1\n    remainder: Int32 = min % -1\n    return quotient == -2147483648 and remainder == 0\nend\nprint(demo())\n"
	if got := runGeneratedC(t, assertCompiles(t, values)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
	divisionByZero := "fun demo() do\n    mut left: Int32 = 7\n    mut right: Int32 = 0\n    quotient: Int32 = left / right\n    print(quotient)\nend\ndemo()\n"
	trapGeneratedC(t, assertCompiles(t, divisionByZero))
	remainderByZero := "fun demo() do\n    mut left: Int32 = 7\n    mut right: Int32 = 0\n    remainder: Int32 = left % right\n    print(remainder)\nend\ndemo()\n"
	trapGeneratedC(t, assertCompiles(t, remainderByZero))
}

// RFC 0016/0046/0047 runtime traps: dynamic conversion overflow, empty List
// pop, missing Dict get, and Array/View slice bounds all terminate with a
// runtime diagnostic.
func c23GeneratedBoundsAndConversionTraps(t *testing.T) {
	cases := map[string]string{
		"conversion overflow":      "fun demo() do\n    big: Int64 = 300\n    small: Int8 = big.to<Int8>()\n    print(small)\nend\ndemo()\n",
		"empty list pop":           "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    last: Int32 = values.pop()\n    print(last)\nend\ndemo(Heap.new())\n",
		"missing dict get":         "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    missing: Int32 = scores.get(2)\n    print(missing)\nend\ndemo(Heap.new())\n",
		"list index out of bounds": "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    first: Int32 = values.at(4)\n    print(first)\nend\ndemo(Heap.new())\n",
		// Static bounds are compile errors and constant propagation sees
		// through local bindings, so a parameter supplies the runtime
		// bounds-check path.
		"array index out of bounds": "fun demo(index: Int32) do\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    out: Int32 = fixed[index]\n    print(out)\nend\ndemo(5)\n",
		"array slice bounds":        "fun demo(stop: Int32) do\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    view: View<Int32> = fixed.slice(1, stop)\n    print(view.length())\nend\ndemo(5)\n",
		"list slice bounds":         "fun demo(h: Heap, stop: Int32) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    view: View<Int32> = values.slice(1, stop)\n    print(view.length())\nend\ndemo(Heap.new(), 5)\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			trapGeneratedC(t, assertCompiles(t, source))
		})
	}
}
