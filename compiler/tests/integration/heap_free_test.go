package integration

// Heap.free discipline: allocator boundary rejection, a single diagnostic
// across terminating return paths, untracked and safe acceptances, and
// cross-allocator release rejection.

import (
	"strings"
	"testing"

	"hexal/compiler"
)

func assertRejectsExactlyOne(t *testing.T, source, want string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("expected rejection, but the source compiled:\n%s", source)
	}
	if len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], want) {
		t.Fatalf("diagnostics = %v, want exactly one containing %q:\n%s", result.Stderr, want, source)
	}
}

func TestHeapFreeRejectsInvalidStorageAndRepeatedRelease(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "direct reference",
			source: `let h: Heap = Heap()
let mut x: Int32 = 1
h.free(@x)
`,
			want: "free does not accept a pointer into this function's local storage",
		},
		{
			name: "reference binding",
			source: `let h: Heap = Heap()
let mut x: Int32 = 1
let p: Ptr<mut Int32> = @x
h.free(p)
`,
			want: "free does not accept a pointer into this function's local storage",
		},
		{
			name: "double free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "double free through alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
h.free(p)
h.free(q)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "double free through alias of alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
let r: Ptr<mut Int32> = q
h.free(p)
h.free(r)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "use after free through alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
h.free(p)
let value: Int32 = ^q
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "use after free through alias of freed alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
h.free(q)
let value: Int32 = ^p
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "alias free inside defer",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
defer h.free(p)
h.free(q)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "alias on every branch",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    let q: Ptr<mut Int32> = p
    h.free(q)
else
    let q: Ptr<mut Int32> = p
    h.free(q)
end
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "member read double free",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
let h: Heap = Heap()
let holder: Holder = Holder(pointer = h.allocate<Int32>(0), )
let q: Ptr<mut Int32> = holder.pointer
h.free(q)
h.free(q)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "union-typed target keeps original tracking",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> | Nil = p
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "use after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
let value: Int32 = ^p
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "deferred free after explicit free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "both branches free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    h.free(p)
else
    h.free(p)
end
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "outer defer after terminating branches",
			source: `fun finish(flag: Bool, h: Heap): Int32 do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    defer h.free(p)
    if flag then
        h.free(p)
        return 1
    else
        h.free(p)
        return 2
    end
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "deferred capture after reallocation",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
p = h.allocate<Int32>(1)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "deferred capture after branch reallocation",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
let flag: Bool = true
if flag then
    p = h.allocate<Int32>(1)
else
    p = h.allocate<Int32>(2)
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "method receiver after free",
			source: `type Point is struct value: Int32, end
method Point.read(): Int32 do
    return self.value
end
let h: Heap = Heap()
let p: Ptr<mut Point> = h.allocate<Point>(Point(value = 1, ))
h.free(p)
let value: Int32 = p.read()
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "volatile read after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut UInt32> = h.allocate<UInt32>(0)
h.free(p)
let value: UInt32 = p.read_volatile()
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "volatile write after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut UInt32> = h.allocate<UInt32>(0)
h.free(p)
p.write_volatile(1)
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "deferred expression after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
defer ^p
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "deferred compound volatile read after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut UInt32> = h.allocate<UInt32>(0)
h.free(p)
defer p.read_volatile() + 1
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "spawn preserves identity",
			source: `fun worker(p: Ptr<mut Int32>): Bool do
    return true
end
fun run(h: Heap): Int32 | Error do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    let task: Task<Bool> = try spawn worker(p)
    task.join()
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "channel send preserves identity",
			source: `fun run(h: Heap): Int32 | Error do
    let channel: Channel<Ptr<mut Int32>> = try Channel<Ptr<mut Int32>>(h, 1)
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    try channel.send(p)
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "return preserves identity",
			source: `fun pick(h: Heap): Ptr<mut Int32> do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    return p
end
fun run(h: Heap): Int32 | Error do
    let p: Ptr<mut Int32> = pick(h)
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "call argument preserves identity",
			source: `fun consume(p: Ptr<mut Int32>) do
end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
consume(p)
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "member store preserves identity",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let holder: Holder = Holder(pointer = p, )
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "collection store preserves identity",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let slots: Array<Ptr<mut Int32>, 1> = [p]
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "join result receives fresh local fact",
			source: `fun make(h: Heap): Ptr<mut Int32> do
    return h.allocate<Int32>(0)
end
fun run(h: Heap): Int32 | Error do
    let task: Task<Ptr<mut Int32>> = try spawn make(h)
    let p: Ptr<mut Int32> = task.join()
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "channel receive receives fresh local fact",
			source: `fun run(h: Heap): Int32 | Error do
    let channel: Channel<Ptr<mut Int32>> = try Channel<Ptr<mut Int32>>(h, 1)
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    try channel.send(p)
    let step: Ptr<mut Int32> | EoS = channel.receive()
    if step is EoS then
        return 0
    end
    let received: Ptr<mut Int32> = step
    h.free(received)
    h.free(received)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}

func TestHeapFreeReportsOneDiagnosticForTerminatingReturnPaths(t *testing.T) {
	assertRejectsExactlyOne(t, `fun finish(flag: Bool, h: Heap): Int32 do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    defer h.free(p)
    if flag then
        h.free(p)
        return 1
    else
        h.free(p)
        return 2
    end
end
`, "free releases storage already released on every path to this point")
}

func TestHeapFreeAcceptsUntrackedAndSafeCases(t *testing.T) {
	testCases := []struct {
		name   string
		source string
	}{
		{
			name: "allocator pointer",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(5)
h.free(p)
`,
		},
		{
			name: "parameter",
			source: `fun release(h: Heap, p: Ptr<mut Int32>) do
    h.free(p)
end
`,
		},
		{
			name: "object member",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
fun release(h: Heap, holder: Holder) do
    h.free(holder.pointer)
end
`,
		},
		{
			name: "collection element",
			source: `fun release(h: Heap, pointers: Array<Ptr<mut Int32>, 1>) do
    h.free(pointers[0])
end
`,
		},
		{
			name: "reallocation after free",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
p = h.allocate<Int32>(1)
h.free(p)
`,
		},
		{
			name: "reassignment of alias relabels",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let mut q: Ptr<mut Int32> = p
q = h.allocate<Int32>(2)
h.free(p)
h.free(q)
`,
		},
		{
			name: "reassignment of original relabels",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
p = h.allocate<Int32>(2)
h.free(q)
h.free(p)
`,
		},
		{
			name: "alias on one branch only",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    let q: Ptr<mut Int32> = p
    h.free(q)
end
h.free(p)
`,
		},
		{
			name: "loop preserves loop-head state",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
let mut i: Int32 = 0
while i < 3 do
    let q: Ptr<mut Int32> = p
    i = i + 1
end
h.free(p)
`,
		},
		{
			name: "member read independent of heap pointer",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let holder: Holder = Holder(pointer = p, )
let q: Ptr<mut Int32> = holder.pointer
h.free(p)
h.free(q)
`,
		},
		{
			name: "copied stash handle forms no alias",
			source: `let st: Stash<Int32> = Stash<Int32>()
let st2: Stash<Int32> = st
st.destroy()
st2.destroy()
`,
		},
		{
			name: "copied pool handle forms no alias",
			source: `let pl: Pool<Int32> = Pool<Int32>(4)
let pl2: Pool<Int32> = pl
pl.destroy()
pl2.destroy()
`,
		},
		{
			name: "reallocation after free",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
p = h.allocate<Int32>(1)
h.free(p)
`,
		},
		{
			name: "one branch free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    h.free(p)
end
`,
		},
		{
			name: "leak",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
`,
		},
		{
			name: "defer-only cleanup",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
`,
		},
		{
			name: "defer timing before action",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
let value: Int32 = ^p
`,
		},
		{
			name: "deferred expression after reallocation",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
defer ^p
p = h.allocate<Int32>(1)
`,
		},
		{
			name: "deferred capture with branch-reassigned pointer",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
let flag: Bool = true
if flag then
    p = h.allocate<Int32>(1)
else
    p = h.allocate<Int32>(2)
end
h.free(p)
`,
		},
		{
			name: "deferred action after unreachable return",
			source: `fun finish(h: Heap): Int32 do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    h.free(p)
    return 1
    defer h.free(p)
end
`,
		},
		{
			name: "passing freed pointer",
			source: `fun consume(p: Ptr<mut Int32>) do
end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
consume(p)
`,
		},
		{
			name: "offset and cast stay fresh identities",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
unsafe do
    let q: Ptr<mut Int32> = p.offset(0)
    let r: Ptr<mut Int32> = p.cast<Int32>()
    h.free(p)
    h.free(q)
    h.free(r)
end
`,
		},
		{
			name: "escaped binding accepts double free",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
let slot: Ptr<mut Ptr<mut Int32>> = @p
h.free(p)
h.free(p)
`,
		},
		{
			name: "heap allocate then heap free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(1)
h.free(p)
`,
		},
		{
			name: "aligned heap allocate then heap free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate_aligned<Int32>(1, 64)
h.free(p)
`,
		},
		{
			name: "same pool allocate then free",
			source: `let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
pl.free(p)
`,
		},
		{
			name: "heap free of parameter",
			source: `fun release(h: Heap, p: Ptr<mut Int32>) do
    h.free(p)
end
`,
		},
		{
			name: "heap free of member read",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
fun release(h: Heap, holder: Holder) do
    h.free(holder.pointer)
end
`,
		},
		{
			name: "heap free of collection element",
			source: `fun release(h: Heap, pointers: Array<Ptr<mut Int32>, 1>) do
    h.free(pointers[0])
end
`,
		},
		{
			name: "heap free of call result",
			source: `fun make(h: Heap): Ptr<mut Int32> do
    return h.allocate<Int32>(0)
end
fun release(h: Heap) do
    let p: Ptr<mut Int32> = make(h)
    h.free(p)
end
`,
		},
		{
			name: "pool free of parameter",
			source: `fun release(pl: Pool<Int32>, p: Ptr<mut Int32>) do
    pl.free(p)
end
`,
		},
		{
			name: "offset and cast release through any allocator",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
unsafe do
    let q: Ptr<mut Int32> = p.offset(0)
    let r: Ptr<mut Int32> = p.cast<Int32>()
    pl.free(q)
    h.free(r)
end
`,
		},
		{
			name: "conditional alias does not carry kind past join",
			source: `let h: Heap = Heap()
let mut q: Ptr<mut Int32> = h.allocate<Int32>(0)
let pl: Pool<Int32> = Pool<Int32>(4)
let flag: Bool = true
if flag then
    q = pl.allocate(1)
end
h.free(q)
`,
		},
		{
			name: "alias of pool pointer released through same pool",
			source: `let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
let q: Ptr<mut Int32> = p
pl.free(q)
`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCompiles(t, testCase.source)
		})
	}
}

// A release through an allocator that provably did not produce the pointer is
// rejected with the real source allocator; unknown kinds stay accepted.
func TestCrossAllocatorReleaseRejects(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "stash allocation through Heap free",
			source: `let h: Heap = Heap()
let st: Stash<Int32> = Stash<Int32>()
let p: Ptr<mut Int32> = st.allocate(1)
h.free(p)
`,
			want: "free does not accept a pointer allocated from a Stash",
		},
		{
			name: "pool allocation through Heap free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
h.free(p)
`,
			want: "free does not accept a pointer allocated from a Pool",
		},
		{
			name: "heap allocation through Pool free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = h.allocate<Int32>(1)
pl.free(p)
`,
			want: "Pool free does not accept a pointer allocated from the Heap",
		},
		{
			name: "aligned heap allocation through Pool free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = h.allocate_aligned<Int32>(1, 64)
pl.free(p)
`,
			want: "Pool free does not accept a pointer allocated from the Heap",
		},
		{
			name: "stash allocation through Pool free",
			source: `let st: Stash<Int32> = Stash<Int32>()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = st.allocate(1)
pl.free(p)
`,
			want: "Pool free does not accept a pointer allocated from a Stash",
		},
		{
			name: "allocation from a different Pool",
			source: `let a: Pool<Int32> = Pool<Int32>(4)
let b: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = a.allocate(1)
b.free(p)
`,
			want: "pointer was allocated from a different Pool",
		},
		{
			name: "aliased pool allocation through Heap free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
let q: Ptr<mut Int32> = p
h.free(q)
`,
			want: "free does not accept a pointer allocated from a Pool",
		},
		{
			name: "deferred Heap free of pool allocation",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
defer h.free(p)
`,
			want: "free does not accept a pointer allocated from a Pool",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejectsExactlyOne(t, testCase.source, testCase.want)
		})
	}
}
