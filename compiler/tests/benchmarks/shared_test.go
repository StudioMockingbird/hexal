// Package benchmarks is the compiler's measurement harness: the committed
// benchmark suite and the complexity report over the compiler's own
// Go source.
//
// Every file here is a _test.go file, deliberately. This package sits under
// compiler/, so `go build ./compiler/...` matches it; with test files only the
// package is empty to `go build` and the third-party complexity libraries are
// never compiled on the ordinary build path.
//
// It imports hexal/compiler and hexal/workbench/snippets. That is not a
// layering inversion: the original constraint was that
// `package compiler`'s own test binary must not import the workbench, and
// `go test ./compiler` does not build this package. Directory nesting carries
// no dependency meaning in Go.
package benchmarks

import (
	"hexal/compiler"
	"testing"
)

// benchmarkProgram is one fixed in-memory compilation. Sources are exact Hexal
// literals kept in this file so a benchmark is stable when the workbench
// catalog changes for unrelated reasons.
type benchmarkProgram struct {
	name       string
	sources    map[string]string
	entrypoint string
}

// sourceBytes totals the program's source length, which each benchmark hands
// to b.SetBytes so Go reports MB/s and differently sized shapes become
// comparable.
func (program benchmarkProgram) sourceBytes() int64 {
	total := 0
	for _, source := range program.sources {
		total += len(source)
	}
	return int64(total)
}

// benchmarkCompileSink receives every benchmark result so the Compile call is
// not eliminated.
var benchmarkCompileSink compiler.CompilationResult

// runBenchmarkProgram compiles one program b.N times. The source map is built
// once at package initialization, so nothing but Compile is inside the loop.
func runBenchmarkProgram(b *testing.B, program benchmarkProgram) {
	b.ReportAllocs()
	b.SetBytes(program.sourceBytes())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkCompileSink = compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
	}
}

var benchmarkPrograms = []benchmarkProgram{
	{
		name: "scalar",
		sources: map[string]string{
			"app.hex": `fun sum_sequence(seed: Int32): Int32 do
    let mut total: Int32 = seed
    if seed > 0 then
        total = total + 10
    else
        total = total - 10
    end
    total = total * 2
    total = total - 3
    if total >= 100 then
        return total
    end
    return total + 1
end
let result: Int32 = sum_sequence(21)`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "generics-heavy",
		sources: map[string]string{
			"app.hex": `fun identity<T>(value: T): T do
    return value
end
type Box<T> is struct value: T end
method Box<T>.get(): T do
    return self.value
end
let i32: Int32 = 1
let i64: Int64 = 2
let u32: UInt32 = 3
let u16: UInt16 = 4
let u8: UInt8 = 5
let f64: Float64 = 6.5
let f32: Float32 = 7.5
let inline: String<8> = "a"
let flag: Bool = true
let i16: Int16 = 10
let a1: Int32 = identity(i32)
let a2: Int64 = identity(i64)
let a3: UInt32 = identity(u32)
let a4: UInt16 = identity(u16)
let a5: UInt8 = identity(u8)
let a6: Float64 = identity(f64)
let a7: Float32 = identity(f32)
let a8: String<8> = identity(inline)
let a9: Bool = identity(flag)
let a10: Int16 = identity(i16)
let box1: Box<Int32> = Box(value = i32)
let b1: Int32 = box1.get()
let box2: Box<Int64> = Box(value = i64)
let b2: Int64 = box2.get()
let box3: Box<UInt32> = Box(value = u32)
let b3: UInt32 = box3.get()
let box4: Box<Float64> = Box(value = f64)
let b4: Float64 = box4.get()
let box5: Box<String<8>> = Box(value = inline)
let b5: String<8> = box5.get()`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "multi-module",
		sources: map[string]string{
			"app.hex": `import
    A from "./a",
    B from "./b",
    D from "./extra/d",
    E from "./extra/e",
    F from "./extra/f",
    G from "./extra/g"
end
let answer: Int32 = A.run() + B.run() + D.run() + E.run() + F.run() + G.run()`,
			"a.hex": `import
    C from "./util/c"
end
fun run(): Int32 do
    let origin: C.Point = C.origin()
    return origin.width() + 1
end
export
    run
end`,
			"b.hex": `import
    C from "./util/c"
end
fun run(): Int32 do
    return C.scale(2)
end
export
    run
end`,
			"util/c.hex": `type Point is struct x: Int32, y: Int32 end
fun origin(): Point do
    return Point(x = 10, y = 20)
end
method Point.width(): Int32 do
    return self.x
end
fun scale(multiplier: Int32): Int32 do
    return origin().width() * multiplier
end
export
    Point,
    origin,
    Point.width,
    scale
end`,
			"extra/d.hex": `import
    C from "../util/c"
end
fun run(): Int32 do
    return C.scale(3)
end
export
    run
end`,
			"extra/e.hex": `fun run(): Int32 do
    return 5
end
export
    run
end`,
			"extra/f.hex": `fun run(): Int32 do
    return 6
end
export
    run
end`,
			"extra/g.hex": `fun run(): Int32 do
    return 7
end
export
    run
end`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "collections",
		sources: map[string]string{
			"app.hex": `fun demo(h: Heap): Int32 do
    let values: List<Int32> = List<Int32>(h)
    defer values.free(h)
    values.push(3)
    values.push(6)
    let totals: Dict<Int32, Int64> = Dict<Int32, Int64>(h)
    defer totals.free(h)
    totals.insert(1, 90)
    totals.insert(2, 75)
    let fixed: Array<Float64, 4> = [1.5, 2.5, 3.5, 4.5]
    let view: Slice<Float64> = fixed.slice(0, 4)
    let names: List<String<8>> = List<String<8>>(h)
    defer names.free(h)
    names.push("alpha")
    names.push("beta")
    let mut total: Int32 = values[0] + totals.get(1).to<Int32>() + view[0].to<Int32>()
    for name in names do
        total = total + name.length().to<Int32>()
    end
    return total
end`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "text",
		sources: map[string]string{
			"app.hex": `fun count_letters(text: String): Int32 do
    let mut letters: Int32 = 0
    for value: Byte in text do
        if value == b' ' then
            continue
        end
        letters = letters + 1
    end
    return letters
end
fun demo(h: Heap): Int32 do
    let text: String = "caf\u{00E9} finale \u{03BB}"
    let raw: Slice<Byte> = text.bytes()
    let label: String<8> = "hexal"
    let runtime: String = label.copy(h)
    let mut total: Int32 = count_letters(text) + raw[1].to<Int32>() + runtime.length().to<Int32>()
    runtime.free(h)
    return total
end`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "concurrency",
		sources: map[string]string{
			"app.hex": `type Shared is struct count: Atomic<Int32> end
fun square(value: Int32): Int32 do
    return value * value
end
fun run(h: Heap): Int32 | Error do
    let task: Task<Int32> = try spawn square(6)
    let channel: Channel<Int32> = try Channel<Int32>(h, 4)
    defer channel.free(h)
    channel.send(task.join())
    channel.close()
    let step: Int32 | EoS = channel.receive()
    let mut total: Int32 = 0
    if step is Int32 then
        total = total + step
    end
    let mutex: Mutex = try Mutex(h)
    defer mutex.free(h)
    mutex.lock()
    let mut shared: Shared = Shared(count = Atomic<Int32>(0))
    shared.count.fetch_add(1)
    mutex.unlock()
    total = total + shared.count.load()
    return total
end`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "error-paths",
		sources: map[string]string{
			"app.hex": `fun spill(value: Int32) do
end
fun rollback() do
end
fun level3(): Int32 | Error do
    return Error(ErrorKind.Other(header = "Level Three"), "three")
end
fun level2(): Int32 | Error do
    errdefer rollback()
    let value: Int32 = try level3()
    return value + 1
end
fun level1(): Int32 | Error do
    defer spill(1)
    let value: Int32 = try level2()
    return value + 1
end
let result: Int32 | Error = level1()`,
		},
		entrypoint: "app.hex",
	},
}
