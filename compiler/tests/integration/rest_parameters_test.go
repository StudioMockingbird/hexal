package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

func compileRest(t *testing.T, source string) compiler.CompilationResult {
	t.Helper()
	return compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{})
}

// A rest declaration lowers its final parameter as one read-only Slice and its
// call packs the trailing elements into one Slice value: the canonical empty
// Slice for zero, a compound-literal array otherwise.
func TestRestCallLowering(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		source  string
		markers []string
	}{
		{
			name:   "direct",
			source: "fun sum(rest: Int32...): Size do\n    return rest.length()\nend\nprint(sum(10, 20, 30))\n",
			markers: []string{
				"hex_slice_Int32",
				"(hex_slice_Int32){.data = (int32_t[]){10, 20, 30}, .length = 3}",
			},
		},
		{
			name:    "zero",
			source:  "fun count(rest: Int32...): Size do\n    return rest.length()\nend\nprint(count())\n",
			markers: []string{"(hex_slice_Int32){.data = nullptr, .length = 0}"},
		},
		{
			name:   "fixed-and-rest",
			source: "fun tag(prefix: String, rest: Int32...) do\nend\ntag(\"x\", 1, 2)\n",
			markers: []string{
				"(hex_slice_Int32){.data = (int32_t[]){1, 2}, .length = 2}",
			},
		},
		{
			name:   "method",
			source: "type Worker is struct\n    id: Int32,\nend\nmethod Worker.sum(rest: Int32...): Size do\n    return rest.length()\nend\nprint(Worker(id = 1).sum(1, 2, 3))\n",
			markers: []string{
				"(hex_slice_Int32){.data = (int32_t[]){1, 2, 3}, .length = 3}",
			},
		},
		{
			name:   "function-value",
			source: "fun sum(rest: Int32...): Size do\n    return rest.length()\nend\nf: Fun<(Int32...): Size> := sum\nprint(f(1, 2, 3))\n",
			markers: []string{
				"(hex_slice_Int32){.data = (int32_t[]){1, 2, 3}, .length = 3}",
			},
		},
		{
			name:   "generic",
			source: "fun first<T>(value: T, rest: T...): T do\n    return value\nend\nprint(first(1, 2, 3))\n",
			markers: []string{
				"(hex_slice_Int32){.data = (int32_t[]){2, 3}, .length = 2}",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileRest(t, testCase.source)
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("compile failed: %v", result.Stderr)
			}
			app := result.Files["modules/app.c"]
			for _, marker := range testCase.markers {
				if !strings.Contains(app, marker) {
					t.Fatalf("generated C lacks %q:\n%s", marker, app)
				}
			}
		})
	}
}

// A deferred rest call captures each explicit value at registration and packs
// the backing region at execution.
func TestDeferredRestCallPacksAtExecution(t *testing.T) {
	result := compileRest(t, "fun log(rest: Int32...) do\nend\nfun demo() do\n    defer log(1, 2, 3)\nend\ndemo()\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("compile failed: %v", result.Stderr)
	}
	app := result.Files["modules/app.c"]
	if !strings.Contains(app, "hex_defer_capture_1") {
		t.Fatalf("deferred rest call did not capture its values at registration:\n%s", app)
	}
	if !strings.Contains(app, "(hex_slice_Int32){.data = (int32_t[]){hex_defer_capture_1, hex_defer_capture_2, hex_defer_capture_3}, .length = 3}") {
		t.Fatalf("deferred rest call did not pack at execution:\n%s", app)
	}
}

// Every escape position the RFC names is rejected with the one stable
// diagnostic, while reading, aliasing, iterating, indexing, and copying an
// element remain valid.
func TestRestBackedEscapeDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{name: "return-descriptor", source: "fun leak(rest: Int32...): Slice<Int32> do\n    return rest\nend\nprint(1)\n"},
		{name: "return-derived", source: "fun leak(rest: Int32...): Slice<Int32> do\n    return rest.slice(0, 1)\nend\nprint(1)\n"},
		{name: "pass-argument", source: "fun sink(values: Slice<Int32>) do\nend\nfun use(rest: Int32...) do\n    sink(rest)\nend\nuse(1, 2)\n"},
		{name: "store-field", source: "type Box is struct\n    values: Slice<Int32>,\nend\nfun store(rest: Int32...): Box do\n    return Box(values = rest)\nend\nprint(1)\n"},
		{name: "collection-insert", source: "fun use(heap: Heap, rest: Int32...) do\n    values: List<Slice<Int32>> := List<Slice<Int32>>(heap)\n    defer values.free(heap)\n    values.push(rest)\nend\nprint(1)\n"},
		{name: "defer-capture", source: "fun log(values: Slice<Int32>) do\nend\nfun use(rest: Int32...) do\n    defer log(rest)\nend\nuse(1, 2)\n"},
		{name: "address-of-element", source: "fun use(rest: Int32...) do\n    unsafe do\n        p: Ptr<Int32> := @rest[0]\n    end\nend\nuse(1, 2)\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileRest(t, testCase.source)
			if result.ExitCode == compiler.ExitSuccess {
				t.Fatalf("escape accepted:\n%s", result.Files["modules/app.c"])
			}
			if !strings.Contains(strings.Join(result.Stderr, "\n"), "rest-backed Slice cannot escape its function invocation") {
				t.Fatalf("stderr = %v, want the escape diagnostic", result.Stderr)
			}
		})
	}
}

// A mutable alias is rejected with its own diagnostic, while a fixed alias,
// iteration, indexing, and copying an element out all remain valid.
func TestRestBackedValidUsesAndMutableAlias(t *testing.T) {
	valid := "fun aliasUse(rest: Int32...): Size do\n    alias: Slice<Int32> := rest\n    return alias.length()\nend\n" +
		"fun foldUse(rest: Int32...): Int32 do\n    mut total: Int32 := 0\n    for value in rest do\n        total = total + value\n    end\n    first: Int32 := rest[0]\n    return total + first\nend\n" +
		"print(aliasUse(1, 2))\nprint(foldUse(3, 4))\n"
	if result := compileRest(t, valid); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("valid rest uses rejected: %v", result.Stderr)
	}

	mutable := "fun use(rest: Int32...) do\n    mut alias: Slice<Int32> := rest\nend\nuse(1, 2)\n"
	result := compileRest(t, mutable)
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("a mutable rest alias was accepted")
	}
	if !strings.Contains(strings.Join(result.Stderr, "\n"), "rest-backed Slice requires a fixed local alias") {
		t.Fatalf("stderr = %v, want the fixed-alias diagnostic", result.Stderr)
	}
}

// A rest signature's C declaration takes one final Slice and contains no C
// variadic syntax; Fun<(T...)> is distinct from Fun<(Slice<T>)>.
func TestRestSignatureAndTypeIdentity(t *testing.T) {
	result := compileRest(t, "fun sum(rest: Int32...): Size do\n    return rest.length()\nend\nprint(sum(1, 2))\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("compile failed: %v", result.Stderr)
	}
	app := result.Files["modules/app.c"]
	if !strings.Contains(app, "(hex_slice_Int32") {
		t.Fatalf("rest declaration lacks a Slice parameter:\n%s", app)
	}
	for _, forbidden := range []string{"...", "va_list"} {
		if strings.Contains(app, forbidden) {
			t.Fatalf("rest lowering emitted C variadic syntax %q:\n%s", forbidden, app)
		}
	}

	mismatch := "fun sum(rest: Int32...): Size do\n    return rest.length()\nend\nf: Fun<(Slice<Int32>): Size> := sum\nprint(1)\n"
	if result := compileRest(t, mismatch); result.ExitCode == compiler.ExitSuccess {
		t.Fatal("Fun<(Int32...)> was assignable to Fun<(Slice<Int32>)>")
	}
}

// A rest call with fewer than the fixed arguments is rejected with the
// rest-aware arity diagnostic.
func TestRestArity(t *testing.T) {
	result := compileRest(t, "fun tag(prefix: String, rest: Int32...) do\nend\ntag()\n")
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("a call below the fixed arity was accepted")
	}
	if !strings.Contains(strings.Join(result.Stderr, "\n"), "expects at least 1 arguments; got 0") {
		t.Fatalf("stderr = %v, want the rest arity diagnostic", result.Stderr)
	}
}

// A mismatched rest element reports its written position and the required and
// actual types; an element that is not a valid Slice element or is not
// shallow-copyable reports the rest-element diagnostic.
func TestRestElementDiagnostics(t *testing.T) {
	mismatch := compileRest(t, "fun tag(prefix: String, rest: Int32...) do\nend\ntag(\"x\", \"y\")\n")
	if mismatch.ExitCode == compiler.ExitSuccess {
		t.Fatal("a mismatched rest element was accepted")
	}
	if !strings.Contains(strings.Join(mismatch.Stderr, "\n"), "tag argument 2 requires Int32; got String") {
		t.Fatalf("stderr = %v, want the position-and-type mismatch", mismatch.Stderr)
	}

	for _, testCase := range []struct {
		name    string
		sources map[string]string
		target  compilerTypes.TargetProfileID
	}{
		{name: "atomic-element", sources: map[string]string{"app.hex": "fun f(rest: Atomic<Int32>...) do\nend\nprint(1)\n"}},
		{name: "incomplete-element", target: compilerTypes.TargetX86_64LinuxGNU, sources: map[string]string{"app.hex": "extern c from <stdio.h> do\n    type File as \"FILE\" is opaque\nend\nfun f(rest: File...) do\nend\nprint(1)\n"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compiler.Compile(testCase.sources, "app.hex", compiler.Project{Target: testCase.target})
			if result.ExitCode == compiler.ExitSuccess {
				t.Fatal("an invalid rest element type was accepted")
			}
			if !strings.Contains(strings.Join(result.Stderr, "\n"), "is not a valid rest element type") {
				t.Fatalf("stderr = %v, want the rest-element diagnostic", result.Stderr)
			}
		})
	}
}

// Generic inference reconciles every rest element, reports conflicting
// candidates without building a union, and reports an unresolved parameter
// when zero elements leave T open.
func TestRestGenericInference(t *testing.T) {
	agreeing := "fun first<T>(value: T, rest: T...): T do\n    return value\nend\nprint(first(1, 2, 3))\n"
	if result := compileRest(t, agreeing); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("agreeing rest elements rejected: %v", result.Stderr)
	}

	conflict := "fun second<T>(value: T, rest: T...): T do\n    return value\nend\nprint(second(1, \"x\"))\n"
	result := compileRest(t, conflict)
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("conflicting rest elements were accepted")
	}
	if !strings.Contains(strings.Join(result.Stderr, "\n"), "conflicting inferred types for generic parameter T") {
		t.Fatalf("stderr = %v, want the conflict diagnostic", result.Stderr)
	}

	unresolved := "fun count<T>(rest: T...): Size do\n    return rest.length()\nend\nprint(count())\n"
	result = compileRest(t, unresolved)
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("an unresolved rest parameter was accepted")
	}
	if !strings.Contains(strings.Join(result.Stderr, "\n"), "cannot infer generic parameter T") {
		t.Fatalf("stderr = %v, want the cannot-infer diagnostic", result.Stderr)
	}
}

// An exported rest function called from another module keeps its rest
// signature and packs at the call.
func TestRestCrossModuleExport(t *testing.T) {
	sources := map[string]string{
		"lib.hex":  "fun sum(rest: Int32...): Int32 do\n    mut total: Int32 := 0\n    for value in rest do\n        total = total + value\n    end\n    return total\nend\nexport\n    sum\nend\n",
		"main.hex": "import\n    Lib from \"./lib\"\nend\nprint(Lib.sum(1, 2, 3))\n",
	}
	result := compiler.Compile(sources, "main.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("cross-module rest call rejected: %v", result.Stderr)
	}
	if !strings.Contains(result.Files["modules/lib.c"], "hex_slice_Int32") {
		t.Fatalf("exported rest declaration lacks its Slice:\n%s", result.Files["modules/lib.c"])
	}
	if !strings.Contains(result.Files["modules/main.c"], "(hex_slice_Int32){.data = (int32_t[]){1, 2, 3}, .length = 3}") {
		t.Fatalf("cross-module rest call did not pack:\n%s", result.Files["modules/main.c"])
	}
}

// Effectful rest arguments are hoisted into temporaries in written order and
// the backing array references them; a non-scalar element uses ordinary
// shallow C value initialization.
func TestRestEffectfulAndNonScalarLowering(t *testing.T) {
	effectful := "fun bump(value: Int32): Int32 do\n    print(value)\n    return value\nend\n" +
		"fun total(rest: Int32...): Int32 do\n    mut sum: Int32 := 0\n    for value in rest do\n        sum = sum + value\n    end\n    return sum\nend\n" +
		"print(total(bump(1), bump(2), bump(3)))\n"
	result := compileRest(t, effectful)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("effectful rest call rejected: %v", result.Stderr)
	}
	app := result.Files["modules/app.c"]
	if !strings.Contains(app, "(hex_slice_Int32){.data = (int32_t[]){hex_seq_1, hex_seq_2, hex_seq_3}, .length = 3}") {
		t.Fatalf("effectful rest arguments are not hoisted in order:\n%s", app)
	}

	nonScalar := "type Point is struct\n    x: Int32,\n    y: Int32,\nend\n" +
		"fun total(rest: Point...): Int32 do\n    mut sum: Int32 := 0\n    for p in rest do\n        sum = sum + p.x\n    end\n    return sum\nend\n" +
		"print(total(Point(x = 1, y = 2), Point(x = 3, y = 4)))\n"
	result = compileRest(t, nonScalar)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("non-scalar rest call rejected: %v", result.Stderr)
	}
	app = result.Files["modules/app.c"]
	if !strings.Contains(app, "hex_slice_Point") || !strings.Contains(app, ".length = 2}") {
		t.Fatalf("non-scalar rest lowering is not a shallow Slice of the record:\n%s", app)
	}
}

// Rest lowering emits no C variadic syntax, no VLA, no alloca, and no
// rest-specific runtime helper: the backing region is an ordinary
// compound-literal array and the empty case is the canonical empty Slice.
func TestRestLoweringUsesNoRuntimeFacility(t *testing.T) {
	source := "fun sum(rest: Int32...): Size do\n    return rest.length()\nend\nprint(sum())\nprint(sum(1, 2, 3))\n"
	result := compileRest(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("compile failed: %v", result.Stderr)
	}
	app := result.Files["modules/app.c"]
	for _, forbidden := range []string{"va_list", "va_start", "alloca", "__builtin_alloca"} {
		if strings.Contains(app, forbidden) {
			t.Fatalf("rest lowering emitted forbidden facility %q:\n%s", forbidden, app)
		}
	}
	if !strings.Contains(app, "(hex_slice_Int32){.data = nullptr, .length = 0}") {
		t.Fatalf("zero-element rest call did not use the canonical empty Slice:\n%s", app)
	}
	if !strings.Contains(app, "(hex_slice_Int32){.data = (int32_t[]){1, 2, 3}, .length = 3}") {
		t.Fatalf("rest call did not use a compound-literal array:\n%s", app)
	}
}

// A non-generic and a generic anonymous function literal with a rest
// parameter both specialize against an exact Fun<(T...)> expected type and
// lower their calls.
func TestRestFunctionLiteralLowering(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{
			name:   "non-generic",
			source: "fun demo(): Size do\n    f: Fun<(Int32...): Size> := fun (rest: Int32...): Size do\n        return rest.length()\n    end\n    return f(1, 2, 3)\nend\nprint(demo())\n",
		},
		{
			name:   "generic",
			source: "fun demo(): Size do\n    f: Fun<(Int32...): Size> := fun <T>(values: T...): Size do\n        return values.length()\n    end\n    return f(1, 2, 3)\nend\nprint(demo())\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileRest(t, testCase.source)
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("rest function literal rejected: %v", result.Stderr)
			}
			if !strings.Contains(result.Files["modules/app.c"], "(hex_slice_Int32){.data = (int32_t[]){1, 2, 3}, .length = 3}") {
				t.Fatalf("rest function literal call did not pack:\n%s", result.Files["modules/app.c"])
			}
		})
	}
}

// Nested rest calls each pack their own region, and an outer rest call's
// arguments are the nested calls' hoisted results in source order.
func TestNestedRestCallLowering(t *testing.T) {
	source := "fun sum(rest: Int32...): Int32 do\n    mut total: Int32 := 0\n    for value in rest do\n        total = total + value\n    end\n    return total\nend\n" +
		"print(sum(sum(1), sum(2, 3)))\n"
	result := compileRest(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("nested rest call rejected: %v", result.Stderr)
	}
	app := result.Files["modules/app.c"]
	for _, marker := range []string{
		"(hex_slice_Int32){.data = (int32_t[]){1}, .length = 1}",
		"(hex_slice_Int32){.data = (int32_t[]){2, 3}, .length = 2}",
		".data = (int32_t[]){hex_seq_1, hex_seq_2}, .length = 2",
	} {
		if !strings.Contains(app, marker) {
			t.Fatalf("nested rest lowering lacks %q:\n%s", marker, app)
		}
	}
}

// A spawned rest call stores its elements inline in the task frame and the
// entry adapter builds one Slice over them; the frame identity includes the
// rest count.
func TestSpawnedRestCallLowering(t *testing.T) {
	source := "fun classify(rest: Int32...): Size | Error do\n    return rest.length()\nend\n" +
		"fun demo(): Size | Error do\n    task: Task<Size | Error> := try spawn classify(1, 2)\n    return task.join()\nend\n" +
		"outcome: Size | Error := demo()\nif outcome is Error then\n    print(0)\nelse\n    print(outcome)\nend\n"
	result := compileRest(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("spawned rest call rejected: %v", result.Stderr)
	}
	app := result.Files["modules/app.c"]
	for _, marker := range []string{
		"_r2",
		"(hex_slice_Int32){.data = (int32_t[]){args->a1, args->a2}, .length = 2}",
	} {
		if !strings.Contains(app, marker) {
			t.Fatalf("spawned rest lowering lacks %q:\n%s", marker, app)
		}
	}
}
