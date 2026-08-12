package compiler

import (
	"strings"
	"testing"
)

// RFC 0031: lazy single-pass pull Stream<T> with EoS, produce, List sources,
// filter/map/take adapters, for iteration, and explicit free.

func TestStreamEmptyNew(t *testing.T) {
	result := Compile("fun demo()\n    empty: Stream<Int32> = Stream<Int32>.new()\n    step: Int32 | EoS = empty.next()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct sw_stream_ops_Int32 {",
		"sw_stream_empty_ops_Int32",
		"(&sw_stream_empty_Int32)",
		"static inline sw_internal_union_1 sw_stream_next_Int32(sw_stream_Int32 *stream) {",
		"step = (sw_internal_union_1){ .tag = sw_internal_union_1_tag_member_1 }",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestStreamEmptyFreeIsNoOp(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    empty: Stream<Int32> = Stream<Int32>.new()\n    empty.free(h)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "if (stream == NULL || stream == &sw_stream_empty_Int32) {") {
		t.Fatalf("main.h = %q, want empty-free no-op guard", result.MainH)
	}
}

func TestStreamProduce(t *testing.T) {
	result := Compile("type Counter = {\n    mut current: Int32,\n    limit: Int32,\n}\nfun counter_next(state: MutPtr<Counter>): Int32 | EoS\n    if state.current >= state.limit\n        return eos\n    end\n    result: Int32 = state.current\n    state.current = state.current + 1\n    return result\nend\nfun demo(h: Heap)\n    initial: Counter = Counter { current = 0, limit = 3 }\n    numbers: Stream<Int32> = Stream<Int32>.produce(h, initial, counter_next)\n    defer numbers.free(h)\n    mut total: Int32 = 0\n    while true do\n        step: Int32 | EoS = numbers.next()\n        if step is EoS\n            break\n        end\n        total = total + step\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct sw_stream_produce_t_Counter_Int32 {",
		"sw_f_counter_next(&(node->state))",
		"node->state = initial;",
		"sw_v_numbers = sw_stream_produce_t_Counter_Int32_new(sw_v_h, sw_v_initial);",
		"sw_stream_next_Int32(sw_v_numbers)",
		"sw_v_total = ((uint64_t)(uint32_t)((uint64_t)sw_v_total + (uint64_t)sw_v_step.payload.member_0)",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestStreamListSource(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    source: Stream<Int32> = values.stream(h)\n    defer source.free(h)\n    step: Int32 | EoS = source.next()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct sw_stream_list_Int32 {",
		"node->length = list->length;",
		"sw_v_source = sw_stream_list_Int32_new(sw_v_h, sw_v_values);",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestStreamAdapters(t *testing.T) {
	result := Compile("fun is_even(value: Int32): Bool\n    return value % 2 == 0\nend\nfun double(value: Int32): Int32\n    return value * 2\nend\nfun demo(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    source: Stream<Int32> = values.stream(h)\n    even: Stream<Int32> = source.filter(h, is_even)\n    doubled: Stream<Int32> = even.map(h, double)\n    limited: Stream<Int32> = doubled.take(h, 1)\n    defer limited.free(h)\n    step: Int32 | EoS = limited.next()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct sw_stream_filter_Int32 {",
		"sw_f_is_even",
		"typedef struct sw_stream_map_Int32_Int32 {",
		"sw_f_double",
		"typedef struct sw_stream_take_Int32 {",
		"node->remaining = remaining;",
		"sw_stream_free_Int32((sw_heap){ node->allocator }, node->upstream)",
		"sw_v_limited = sw_stream_take_Int32_new(sw_v_h, sw_v_doubled, 1);",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestStreamForIteration(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values.push(3)\n    source: Stream<Int32> = values.stream(h)\n    defer source.free(h)\n    mut total: Int32 = 0\n    for value in source do\n        total = total + value\n    end\n    for i, value in source do\n        total = total + value + i.to<Int32>()\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"sw_stream_Int32 *const sw_for_1 = sw_v_source;",
		"while (sw_for_1->ops->next((void *)sw_for_1, &sw_for_1_value)) {",
		"sw_for_2_ordinal++",
		"const size_t sw_v_i = sw_for_2_ordinal;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestStreamDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"element is EoS", "fun demo()\n    bad: Stream<EoS> = Stream<EoS>.new()\nend", "Stream element type cannot be EoS or include EoS as a top-level union member"},
		{"element union contains EoS", "fun demo()\n    bad: Stream<Int32 | EoS> = Stream<Int32 | EoS>.new()\nend", "Stream element type cannot be EoS or include EoS as a top-level union member"},
		{"produce arity", "fun demo(h: Heap)\n    Stream<Int32>.produce(h)\nend", "produce expects 3 arguments"},
		{"produce callback shape", "fun not_callback()\nend\nfun demo(h: Heap)\n    state: Int32 = 0\n    Stream<Int32>.produce(h, state, not_callback)\nend", "Stream producer callback must accept MutPtr<State>"},
		{"no length", "fun demo(h: Heap)\n    source: Stream<Int32> = Stream<Int32>.new()\n    count: Size = source.length()\nend", "Stream has no method length"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := Compile(testCase.source)
			if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestEosSingletonSemantics(t *testing.T) {
	result := Compile("fun demo()\n    first: EoS = eos\n    second: EoS = eos\n    same: Bool = first == second\n    different: Bool = first != second\n    marker: Int32 | EoS = eos\n    if marker is EoS\n        text: Bool = true\n    end\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"typedef uint8_t sw_eos;",
		"const bool sw_v_same = true;",
		"const bool sw_v_different = false;",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}
