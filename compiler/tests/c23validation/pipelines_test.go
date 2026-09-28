//go:build c23

package c23validation

import "testing"

func TestLazyTraversalPipelinesRun(t *testing.T) {
	const source = `fun keep(value: Int32): Bool do
    return value > 1
end
fun twice(value: Int32): Int32 do
    return value * 2
end
fun add(total: Int32, value: Int32): Int32 do
    return total + value
end
fun entry_value(entry: DictEntry<Int32, Int32>): Int32 do
    return entry.value
end
fun make_fixed(): List<Int32, 2> do
    return [4, 5]
end
fun run(heap: Heap): Int32 do
    let values: List<Int32> = List<Int32>(heap)
    defer values.free(heap)
    values.push(1)
    values.push(2)
    values.push(3)
    let transformed: List<Int32> = values.filter(keep).map(twice).to_list(heap)
    defer transformed.free(heap)
    let reduced: Int32 = values.filter(keep).map(twice).reduce(0, add)
    let mut traversed: Int32 = 0
    for value in values.map(twice) do
        traversed = traversed + value
    end

    let mut fixed: List<Int32, 3> = [6, 7, 8]
    let inline_copy: List<Int32> = fixed.filter(keep).to_list(heap)
    defer inline_copy.free(heap)
    let temporary_copy: List<Int32> = make_fixed().map(twice).to_list(heap)
    defer temporary_copy.free(heap)
    let view: Slice<Int32> = fixed.slice(0, 3)
    let slice_copy: List<Int32> = view.to_list(heap)
    defer slice_copy.free(heap)
    let mut mutable_view: Slice<mut Int32> = fixed.mut_slice(0, 3)
    let mutable_copy: List<Int32> = mutable_view.filter(keep).to_list(heap)
    defer mutable_copy.free(heap)

    let dict: Dict<Int32, Int32> = Dict<Int32, Int32>(heap)
    defer dict.free(heap)
    dict.insert(1, 10)
    dict.insert(2, 20)
    dict.insert(3, 30)
    let removed: Int32 = dict.remove(2)
    let mixed: Dict<Bool, Int32> = Dict<Bool, Int32>(heap)
    defer mixed.free(heap)
    mixed.insert(true, 9)
    let mixed_flat: List<Bool | Int32> = mixed.to_list(heap)
    defer mixed_flat.free(heap)
    let entry_copy: List<Int32> = dict.entries().map(entry_value).to_list(heap)
    defer entry_copy.free(heap)
    let flattened: List<Int32> = dict.to_list(heap)
    defer flattened.free(heap)
    let mut projected: Int32 = 0
    for key in dict.keys() do
        projected = projected + key
    end
    for value in dict.values().filter(keep) do
        projected = projected + value
    end
    for entry in dict.entries() do
        projected = projected + entry.key + entry.value
    end
    let empty: List<Int32> = values.filter(always_false).to_list(heap)
    defer empty.free(heap)
    return reduced + traversed + inline_copy.length().to<Int32>() + temporary_copy.length().to<Int32>() +
        slice_copy.length().to<Int32>() + mutable_copy.length().to<Int32>() + entry_copy.length().to<Int32>() +
        flattened.length().to<Int32>() + mixed_flat.length().to<Int32>() + projected + removed + empty.length().to<Int32>()
end
fun always_false(value: Int32): Bool do
    return false
end
print(run(Heap()))
`
	result := assertCompiles(t, source)
	got := runGeneratedC(t, result, t.TempDir())
	if got != "149" {
		t.Fatalf("pipeline program output = %q, want %q", got, "149")
	}
}

func TestLazyTraversalPipelineEffectOrderRuns(t *testing.T) {
	const source = `fun keep(value: Int32): Bool do
    print(value)
    return value > 1
end
fun report(value: Int32): Int32 do
    print(value + 10)
    return value
end
fun add(total: Int32, value: Int32): Int32 do
    return total + value
end
let values: List<Int32, 2> = [1, 2]
let total: Int32 = values.filter(keep).map(report).reduce(0, add)
print(total)
`
	result := assertCompiles(t, source)
	got := runGeneratedC(t, result, t.TempDir())
	if got != "12122" {
		t.Fatalf("pipeline callback output = %q, want %q", got, "12122")
	}
}

func TestPipelineReturnRunsErrorDefers(t *testing.T) {
	const source = `fun seed(): Error do
    return Error(ErrorKind.Other(header = "seed"), "seed")
end
fun combine(accumulator: Error, value: Int32): Error do
    return accumulator
end
fun twice(value: Int32): Int32 do
    return value * 2
end
fun cleanup() do
    print("errdefer")
end
fun run(heap: Heap): Error do
    errdefer cleanup()
    let values: List<Int32> = List<Int32>(heap)
    defer values.free(heap)
    values.push(1)
    return values.map(twice).reduce(seed(), combine)
end
let result: Error = run(Heap())
print("done")
`
	result := assertCompiles(t, source)
	got := runGeneratedC(t, result, t.TempDir())
	if got != "errdeferdone" {
		t.Fatalf("pipeline Error return output = %q, want %q", got, "errdeferdone")
	}
}

func TestPipelineEvaluatesSourceCallbacksAndTerminalArgumentsOnce(t *testing.T) {
	const source = `fun twice(value: Int32): Int32 do
    return value * 2
end
fun make_values(): List<Int32> do
    print("source")
    let values: List<Int32> = List<Int32>(Heap())
    values.push(1)
    return values
end
fun callback(): Fun<(Int32) : Int32> do
    print("callback")
    return twice
end
fun heap(): Heap do
    print("heap")
    return Heap()
end
let values: List<Int32> = make_values().map(callback()).to_list(heap())
`
	result := assertCompiles(t, source)
	got := runGeneratedC(t, result, t.TempDir())
	if got != "sourcecallbackheap" {
		t.Fatalf("pipeline evaluation trace = %q, want %q", got, "sourcecallbackheap")
	}
}

func TestPipelineForPreservesBreakContinueAndDeferredCleanup(t *testing.T) {
	const source = `fun identity(value: Int32): Int32 do
    return value
end
fun mark(value: Int32) do
    print(value)
end
let values: List<Int32, 3> = [1, 2, 3]
let mut total: Int32 = 0
for value in values.map(identity) do
    defer mark(value)
    if value == 1 then
        continue
    end
    if value == 3 then
        break
    end
    total = total + value
end
print(total)
`
	result := assertCompiles(t, source)
	got := runGeneratedC(t, result, t.TempDir())
	if got != "1232" {
		t.Fatalf("pipeline loop output = %q, want %q", got, "1232")
	}
}
