// Package benchmarks is the compiler's measurement harness: the committed
// benchmark suite (RFC 0075) and the complexity report over the compiler's own
// Go source (RFC 0080).
//
// Every file here is a _test.go file, deliberately. This package sits under
// compiler/, so `go build ./compiler/...` matches it; with test files only the
// package is empty to `go build` and the third-party complexity libraries are
// never compiled on the ordinary build path.
//
// It imports hexal/compiler and hexal/workbench/snippets. That is not the
// layering inversion RFC 0075 guarded against: 0075's constraint was that
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
// comparable (RFC 0080 Part 1).
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
    mut total: Int32 := seed
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
result: Int32 := sum_sequence(21)`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "generics-heavy",
		sources: map[string]string{
			"app.hex": `fun identity<T>(value: T): T do
    return value
end
type Box<T> = { value: T, }
impl Box<T>.get(): T do
    return self.value
end
i32: Int32 := 1
i64: Int64 := 2
u32: UInt32 := 3
u16: UInt16 := 4
u8: UInt8 := 5
f64: Float64 := 6.5
f32: Float32 := 7.5
strand: Strand := "a"
flag: Bool := true
i16: Int16 := 10
a1: Int32 := identity(i32)
a2: Int64 := identity(i64)
a3: UInt32 := identity(u32)
a4: UInt16 := identity(u16)
a5: UInt8 := identity(u8)
a6: Float64 := identity(f64)
a7: Float32 := identity(f32)
a8: Strand := identity(strand)
a9: Bool := identity(flag)
a10: Int16 := identity(i16)
box1: Box<Int32> := Box { value = i32 }
b1: Int32 := box1.get()
box2: Box<Int64> := Box { value = i64 }
b2: Int64 := box2.get()
box3: Box<UInt32> := Box { value = u32 }
b3: UInt32 := box3.get()
box4: Box<Float64> := Box { value = f64 }
b4: Float64 := box4.get()
box5: Box<Strand> := Box { value = strand }
b5: Strand := box5.get()`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "multi-module",
		sources: map[string]string{
			"app.hex": `module A = import "./a"
module B = import "./b"
module D = import "./extra/d"
module E = import "./extra/e"
module F = import "./extra/f"
module G = import "./extra/g"
answer: Int32 := A.run() + B.run() + D.run() + E.run() + F.run() + G.run()`,
			"a.hex": `module C = import "./util/c"
export fun run(): Int32 do
    origin: C.Point := C.origin()
    return origin.width() + 1
end`,
			"b.hex": `module C = import "./util/c"
export fun run(): Int32 do
    return C.scale(2)
end`,
			"util/c.hex": `export type Point = { x: Int32, y: Int32 }
export fun origin(): Point do
    return Point { x = 10, y = 20 }
end
export impl Point.width(): Int32 do
    return self.x
end
export fun scale(multiplier: Int32): Int32 do
    return origin().width() * multiplier
end`,
			"extra/d.hex": `module C = import "../util/c"
export fun run(): Int32 do
    return C.scale(3)
end`,
			"extra/e.hex": `export fun run(): Int32 do
    return 5
end`,
			"extra/f.hex": `export fun run(): Int32 do
    return 6
end`,
			"extra/g.hex": `export fun run(): Int32 do
    return 7
end`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "collections",
		sources: map[string]string{
			"app.hex": `fun demo(h: Heap): Int32 do
    values: List<Int32> := List<Int32>.new(h)
    defer values.free(h)
    values.push(3)
    values.push(6)
    totals: Dict<Int32, Int64> := Dict<Int32, Int64>.new(h)
    defer totals.free(h)
    totals.insert(1, 90)
    totals.insert(2, 75)
    fixed: Array<Float64, 4> := [1.5, 2.5, 3.5, 4.5]
    view: View<Float64> := fixed.slice(0, 4)
    names: List<Strand> := List<Strand>.new(h)
    defer names.free(h)
    names.push("alpha")
    names.push("beta")
    mut total: Int32 := values[0] + totals.get(1).to<Int32>() + view[0].to<Int32>()
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
    cursor: RuneCursor := text.rune_cursor()
    mut letters: Int32 := 0
    while cursor.has_next() do
        value: Rune := cursor.next()
        if value == ' ' then
            continue
        end
        letters = letters + 1
    end
    return letters
end
fun demo(h: Heap): Int32 do
    text: String := "caf\u{00E9} finale \u{03BB}"
    raw: View<Byte> := text.bytes()
    label: Strand := "hexal"
    runtime: String := label.to_string(h)
    mut total: Int32 := count_letters(text) + raw[1].to<Int32>() + runtime.length().to<Int32>()
    runtime.free(h)
    return total
end`,
		},
		entrypoint: "app.hex",
	},
	{
		name: "concurrency",
		sources: map[string]string{
			"app.hex": `type Shared = { count: Atomic<Int32>, }
fun square(value: Int32): Int32 do
    return value * value
end
fun run(h: Heap): Int32 | Error do
    task: Task<Int32> := try spawn square(6)
    channel: Channel<Int32> := try Channel<Int32>.new(h, 4)
    defer channel.free(h)
    channel.send(task.join())
    channel.close()
    step: Int32 | EoS := channel.receive()
    mut total: Int32 := 0
    if step is Int32 then
        total = total + step
    end
    mutex: Mutex := try Mutex.new(h)
    defer mutex.free(h)
    mutex.lock()
    mut shared: Shared := Shared { count = Atomic<Int32>.new(0) }
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
    return Error.new("Level Three", "three")
end
fun level2(): Int32 | Error do
    errdefer rollback()
    value: Int32 := try level3()
    return value + 1
end
fun level1(): Int32 | Error do
    defer spill(1)
    value: Int32 := try level2()
    return value + 1
end
result: Int32 | Error := level1()`,
		},
		entrypoint: "app.hex",
	},
}
