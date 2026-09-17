package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
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
