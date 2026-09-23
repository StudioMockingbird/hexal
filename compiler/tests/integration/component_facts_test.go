package integration

// Generated-C text assertions for the migrated component facts. A demanded
// component's recorded standard headers and its own include must appear in the
// generated artifacts, and a native dependency must be one a selected component
// records; a scalar program must show none of them.

import (
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/compiler/specdata"
)

// Each program demands one component, and the generated hexal.h must contain
// exactly one include of every standard header that component's record owns.
func TestComponentHeaderRecordsAppearInHexalHeader(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		component specdata.ComponentID
		source    string
	}{
		{"heap", specdata.ComponentHeap, "let h: Heap = Heap()\n"},
		{"slice", specdata.ComponentSlice, "fun demo() do\n    let view: Slice<Int32> = Slice<Int32>.empty()\n    let count: Size = view.length()\nend"},
		{"array", specdata.ComponentArray, "fun demo() do\n    let fixed: Array<Int32, 3> = [1, 2, 3]\n    let first: Int32 = fixed[0]\nend"},
		{"string", specdata.ComponentString, "let greeting: String = \"hello\"\n"},
		{"list", specdata.ComponentList, "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\nend"},
		{"dict", specdata.ComponentDict, "fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\nend"},
		{"print", specdata.ComponentPrint, "print(42)\n"},
		{"stash", specdata.ComponentStash, "type Node is struct value: Int32 end\nfun demo() do\n    let stash = Stash<Node>()\n    defer stash.destroy()\n    let node: Ptr<mut Node> = stash.allocate(Node(value = 1))\nend"},
		{"pool", specdata.ComponentPool, "type Node is struct value: Int32 end\nfun demo() do\n    let pool = Pool<Node>(4)\n    defer pool.destroy()\n    let node: Ptr<mut Node> = pool.allocate(Node(value = 1))\n    pool.free(node)\nend"},
		{"concurrency", specdata.ComponentConcurrency, "fun work(): Int32 do\n    return 1\nend\nfun run(): Int32 | Error do\n    let task: Task<Int32> = try spawn work()\n    return task.join()\nend"},
		{"time", specdata.ComponentTime, "import\n  Time from std.time\nend\nfun f(): Nil | Error do\n    let w: Time.WallTime = try Time.wall_time()\n    return nil\nend\nlet r: Nil | Error = f()\n"},
		{"numeric", specdata.ComponentNumeric, "fun demo() do\n    let value: Float64 = 3.75\n    let whole: Int32 = value.to<Int32>()\nend"},
		{"wrap", specdata.ComponentWrap, "let mut signed8: Int8 = 127 let wrapped8: Int8 = signed8 + 1"},
		{"equality", specdata.ComponentEquality, "type Point is struct x: Int32, end\nfun demo() do\n    let left: Point | Bool = Point(x = 1, )\n    let right: Point | Bool = Point(x = 1, )\n    let same: Bool = left == right\nend"},
		{"io", specdata.ComponentIO, streamFacetSource()},
		{"file", specdata.ComponentFile, fileFacetSource()},
		{"network", specdata.ComponentNetwork, "import\n  Net from std.net\nend\nfun describe(h: Heap): Nil | Error do\n    let v4: Net.Address = try Net.parse_address(\"127.0.0.1\", 8080)\n    let host: String = v4.format(h)\n    return nil\nend\nlet described: Nil | Error = describe(Heap())"},
		{"program", specdata.ComponentProgram, programImport + "let count: Size = Prog.available_parallelism()\n"},
		{"entropy", specdata.ComponentEntropy, entropyImport +
			"let h: Heap = Heap()\n" +
			"let p: Ptr<mut Byte> = h.allocate<Byte>(8)\n" +
			"unsafe do\n" +
			"    let view: Slice<mut Byte> = Slice<mut Byte>.from_pointer(p, 8)\n" +
			"    let result = Ent.fill(view)\n" +
			"end\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := assertCompiles(t, testCase.source)
			component, ok := specdata.Component(testCase.component)
			if !ok {
				t.Fatalf("component %s is not registered", testCase.component)
			}
			header := hexalH(t, result)
			for _, want := range component.RequiredCHeaders {
				include := "#include <" + want + ">"
				if strings.Count(header, include) != 1 {
					t.Errorf("hexal.h lacks exactly one %q for demanded component %s:\n%s", include, component.ID, header)
				}
			}
		})
	}
}

// The conditional header groups are registry data whose activation stays in Go:
// the header appears exactly when its condition holds, and never otherwise.
func TestConditionalComponentHeaderRecordsFollowDemand(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		source    string
		present   string
		forbidden string
	}{
		{
			name:      "equality-aborting",
			source:    "type Shape is union | Circle as r: Int32, end | Square as a: Int32, end end\nfun demo() do\n    let left: Shape = Shape.Circle(r = 1, )\n    let right: Shape = Shape.Circle(r = 1, )\n    let same: Bool = left == right\nend",
			present:   "#include <stdlib.h>",
			forbidden: "#include <stdio.h>",
		},
		{
			name:      "equality-non-aborting",
			source:    "type Point is struct x: Int32, end\nfun demo() do\n    let a: Point = Point(x = 1, )\n    let b: Point = Point(x = 1, )\n    let same: Bool = a == b\nend",
			present:   "",
			forbidden: "#include <stdlib.h>",
		},
		{
			name:      "concurrency-atomic",
			source:    "let counter: Atomic<Int32> = Atomic<Int32>(5)\nlet value: Int32 = counter.load()\n",
			present:   "#include <stdatomic.h>",
			forbidden: "#include <stdckdint.h>",
		},
		{
			name:      "conversion-float",
			source:    "fun demo() do\n    let value: Float64 = 3.75\n    let whole: Int32 = value.to<Int32>()\nend",
			present:   "#include <math.h>",
			forbidden: "",
		},
		{
			name:      "conversion-integer",
			source:    "fun demo() do\n    let value: Int64 = 9_000_000_000\n    let narrow: Int8 = value.to<Int8>()\nend",
			present:   "",
			forbidden: "#include <math.h>",
		},
		{
			name:      "bit-cast",
			source:    "fun demo() do\n    let bits: UInt32 = 1\n    let floating: Float32 = bits.bit_cast<Float32>()\nend",
			present:   "#include <string.h>",
			forbidden: "",
		},
		{
			name:    "corelib-paths",
			source:  programImport + "let count: Size = Prog.available_parallelism()\n",
			present: "#include <stdckdint.h>",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := assertCompiles(t, testCase.source)
			header := hexalH(t, result)
			if testCase.present != "" && !strings.Contains(header, testCase.present) {
				t.Errorf("hexal.h = %q, want %q", header, testCase.present)
			}
			if testCase.forbidden != "" && strings.Contains(header, testCase.forbidden) {
				t.Errorf("hexal.h = %q, must not contain %q", header, testCase.forbidden)
			}
		})
	}
}

// A demanded native dependency is one a selected component records, and its
// adapter include appears in the generated C exactly when it is demanded.
func TestNativeDependencyDemandFollowsComponentRecords(t *testing.T) {
	literal := assertCompiles(t, "let greeting: String = \"hello\"\n")
	if hasDependency(literal, string(specdata.DependencyUtf8proc)) {
		t.Fatalf("literal-only String must not select %s: %v", specdata.DependencyUtf8proc, dependencyNames(literal))
	}
	if source := moduleFile(t, literal, "hexal/string.c"); strings.Contains(source, "#include <utf8proc.h>") {
		t.Fatalf("literal-only hexal/string.c emitted the utf8proc adapter:\n%s", source)
	}

	validator := assertCompiles(t, "fun demo(h: Heap): String | Error do\n    let s: String = try String.from_bytes(h, \"hi\".bytes())\n    return s\nend\n")
	if !hasDependency(validator, string(specdata.DependencyUtf8proc)) {
		t.Fatalf("runtime construction must select %s: %v", specdata.DependencyUtf8proc, dependencyNames(validator))
	}
	if source := moduleFile(t, validator, "hexal/string.c"); !strings.Contains(source, "#include <utf8proc.h>") {
		t.Fatalf("validator-demanding hexal/string.c lacks the utf8proc adapter:\n%s", source)
	}

	heap := assertCompiles(t, "let h: Heap = Heap()\n")
	if !hasDependency(heap, string(specdata.DependencyMimalloc)) {
		t.Fatalf("Heap must select %s: %v", specdata.DependencyMimalloc, dependencyNames(heap))
	}
	if runtime := moduleFile(t, heap, "hexal/runtime.c"); strings.Contains(runtime, "#include <uv.h>") || strings.Contains(runtime, "uv_replace_allocator") {
		t.Fatalf("Heap alone must not emit the native bootstrap:\n%s", runtime)
	}

	instant := assertCompiles(t, "import\n  Time from std.time\nend\nlet a: Time.Instant = Time.now()\nlet d: Time.Duration = a.elapsed()\n")
	if !hasDependency(instant, string(specdata.DependencyLibuv)) {
		t.Fatalf("Instant must select %s: %v", specdata.DependencyLibuv, dependencyNames(instant))
	}
	runtime := moduleFile(t, instant, "hexal/runtime.c")
	if !strings.Contains(runtime, "#include <uv.h>") || !strings.Contains(runtime, "uv_replace_allocator") {
		t.Fatalf("native hexal/runtime.c lacks the libuv bootstrap:\n%s", runtime)
	}
}

func hasDependency(result compiler.CompilationResult, name string) bool {
	for _, dependency := range result.Dependencies {
		if string(dependency) == name {
			return true
		}
	}
	return false
}

// The handle registry self-includes its own <string.h>, so demanding it for a
// bare type-only Process or Signal must not add a standard header to hexal.h.
func TestHandleComponentContributesNoProgramHeader(t *testing.T) {
	for _, source := range []string{
		"import\n  Proc from std.process\nend\nfun f(p: Proc.Process) do\nend\n",
		"import\n  Sig from std.signal\nend\nfun f(s: Sig.Signal) do\nend\n",
	} {
		result := assertCompiles(t, source)
		if header := hexalH(t, result); strings.Contains(header, "#include <string.h>") {
			t.Errorf("type-only handle demand added <string.h>:\n%s", header)
		}
	}
}

// A scalar program demands no component, so its module header includes none;
// a demanding program's module header includes the demanded component.
func TestDemandedComponentIncludeAppearsInModuleHeader(t *testing.T) {
	scalar := assertCompiles(t, "fun demo() do\n    let value: Int32 = 1\nend")
	if strings.Contains(rootH(t, scalar), "#include \"hexal/") {
		t.Fatalf("scalar module header must include no component:\n%s", rootH(t, scalar))
	}
	for _, testCase := range []struct {
		name      string
		source    string
		component string
	}{
		{"heap", "let h: Heap = Heap()\n", "hexal/heap.h"},
		{"list", "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\nend", "hexal/list.h"},
		{"dict", "fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\nend", "hexal/dict.h"},
		{"array", "fun demo() do\n    let fixed: Array<Int32, 3> = [1, 2, 3]\n    let first: Int32 = fixed[0]\nend", "hexal/array.h"},
		{"equality", "fun demo(h: Heap): Bool do\n    let left: List<Int32> = List<Int32>(h)\n    let right: List<Int32> = List<Int32>(h)\n    return left == right\nend", "hexal/equality.h"},
		{"program", programImport + "let count: Size = Prog.available_parallelism()\n", "hexal/program.h"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := assertCompiles(t, testCase.source)
			include := "#include \"" + testCase.component + "\""
			if !strings.Contains(rootH(t, result), include) {
				t.Fatalf("modules/app.h lacks %q for the demanded component:\n%s", include, rootH(t, result))
			}
		})
	}
}
