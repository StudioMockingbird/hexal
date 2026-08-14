//go:build c23

package tests

import "testing"

// Smoke-check that RFC 0031 stream programs generate C that gcc accepts
// with -std=c23: ops tables, produce node, list source, adapters, and free.
func c23GeneratedStreamCCompiles(t *testing.T) {
	source := "type Counter = {\n    mut current: Int32,\n    limit: Int32,\n}\nfun counter_next(state: MutPtr<Counter>): Int32 | EoS\n    if state.current >= state.limit\n        return eos\n    end\n    result: Int32 = state.current\n    state.current = state.current + 1\n    return result\nend\nfun is_even(value: Int32): Bool\n    return value % 2 == 0\nend\nfun double(value: Int32): Int32\n    return value * 2\nend\nfun demo(h: Heap)\n    initial: Counter = Counter { current = 0, limit = 4 }\n    numbers: Stream<Int32> = Stream<Int32>.produce(h, initial, counter_next)\n    even: Stream<Int32> = numbers.filter(h, is_even)\n    doubled: Stream<Int32> = even.map(h, double)\n    limited: Stream<Int32> = doubled.take(h, 2)\n    defer limited.free(h)\n    mut total: Int32 = 0\n    for value in limited do\n        total = total + value\n    end\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    source: Stream<Int32> = values.stream(h)\n    defer source.free(h)\n    step: Int32 | EoS = source.next()\n    empty: Stream<Int32> = Stream<Int32>.new()\n    empty.free(h)\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// RFC 0031 runtime conformance: produce construction is lazy (no callback
// runs until a pull), one next pulls at most once, and filter/take compose
// to the exact values.
func c23GeneratedStreamRuntimeRuns(t *testing.T) {
	lazy := "type Counter = {\n    mut current: Int32,\n    limit: Int32,\n}\nfun counter_next(state: MutPtr<Counter>): Int32 | EoS\n    print(\"p\")\n    if state.current >= state.limit\n        return eos\n    end\n    result: Int32 = state.current\n    state.current = state.current + 1\n    return result\nend\nfun demo(h: Heap)\n    initial: Counter = Counter { current = 0, limit = 2 }\n    numbers: Stream<Int32> = Stream<Int32>.produce(h, initial, counter_next)\n    print(\"constructed\")\n    step: Int32 | EoS = numbers.next()\n    numbers.free(h)\nend\ndemo(Heap.new())\n"
	if got := runGeneratedC(t, assertCompiles(t, lazy)); got != "constructedp" {
		t.Fatalf("program output = %q, want %q", got, "constructedp")
	}
	compose := "type Counter = {\n    mut current: Int32,\n    limit: Int32,\n}\nfun counter_next(state: MutPtr<Counter>): Int32 | EoS\n    if state.current >= state.limit\n        return eos\n    end\n    result: Int32 = state.current\n    state.current = state.current + 1\n    return result\nend\nfun is_even(value: Int32): Bool\n    return value % 2 == 0\nend\nfun double(value: Int32): Int32\n    return value * 2\nend\nfun demo(h: Heap): Bool\n    initial: Counter = Counter { current = 0, limit = 4 }\n    numbers: Stream<Int32> = Stream<Int32>.produce(h, initial, counter_next)\n    even: Stream<Int32> = numbers.filter(h, is_even)\n    doubled: Stream<Int32> = even.map(h, double)\n    limited: Stream<Int32> = doubled.take(h, 2)\n    defer limited.free(h)\n    mut total: Int32 = 0\n    for value in limited do\n        total = total + value\n    end\n    return total == 4\nend\nprint(demo(Heap.new()))\n"
	if got := runGeneratedC(t, assertCompiles(t, compose)); got != "true" {
		t.Fatalf("program output = %q, want %q", got, "true")
	}
}
