package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// assertChecked requires the source to compile with no diagnostic at all. It
// is stricter than assertCompiles, which only requires a successful exit.
func assertChecked(t *testing.T, source string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile rejected %q: %#v", source, result.Stderr)
	}
}

// assertRejectsAnyDiagnostic requires the source to fail with want in ANY
// diagnostic. assertRejects is the stricter form, requiring it in the first;
// the two are deliberately separate names because merging them would weaken
// one set of tests or break the other.
func assertRejectsAnyDiagnostic(t *testing.T, source, want string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile accepted %q, want %q", source, want)
	}
	for _, message := range result.Stderr {
		if strings.Contains(message, want) {
			return
		}
	}
	t.Fatalf("Compile diagnostics = %#v, want %q", result.Stderr, want)
}

func TestFunctionDeclarationAndCall(t *testing.T) {
	assertChecked(t, "fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx + dy\nend\ntotal: Int32 := adder(2, 3)\n")
}

func TestNoReturnFunctionIsACallStatement(t *testing.T) {
	assertChecked(t, "fun reset(counter: MutPtr<Int32>) do\n    counter.value = 0\nend\nmut count: Int32 := 1\nreset(ref count)\n")
	assertRejectsAnyDiagnostic(t,
		"fun reset(counter: MutPtr<Int32>) do\n    counter.value = 0\nend\nmut count: Int32 := 1\nresult: Int32 := reset(ref count)\n",
		"reset produces no value")
}

func TestFunTypeBindingsAndCallbacks(t *testing.T) {
	assertChecked(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n    return callback(value)\nend\nresult: Int32 := apply(square, 5)\n")
	assertChecked(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nmut selected: Fun<(Int32) : Int32> := identity\nselected = identity\n")
}

func TestFunTypeMismatchIsReported(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: UInt32): UInt32 do\n    return dx\nend\nhandler: Fun<(Int32) : Int32> := adder\n",
		"handler requires Fun<(Int32) : Int32>; got Fun<(UInt32) : UInt32>")
}

func TestSelfRecursionAndForwardCallsResolve(t *testing.T) {
	assertChecked(t, "fun factorial(value: Int32): Int32 do\n    return value * factorial(value - 1)\nend\n")
	assertChecked(t,
		"fun is_even(value: Int32): Int32 do\n    return is_odd(value - 1)\nend\nfun is_odd(value: Int32): Int32 do\n    return value\nend\n")
}

func TestFunctionScopeIsClosed(t *testing.T) {
	assertChecked(t, "count: Int32 := 3\nfun scoped(seed: Int32): Int32 do\n    mut count: Int32 := seed\n    count = count + 1\n    return count\nend\n")
	assertRejectsAnyDiagnostic(t,
		"mut count: Int32 := 0\nfun read_count(): Int32 do\n    return count\nend\n",
		"function read_count cannot access module data binding count; pass it as a parameter")
}

func TestParametersAreFixed(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun small(value: Int32): Int32 do\n    value = 1\n    return value\nend\n",
		"cannot assign to parameter value; parameters are fixed bindings")
}

func TestCallChecksArityAndArgumentTypes(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx + dy\nend\ntotal: Int32 := adder(1, 2, 3)\n",
		"adder expects 2 arguments; got 3")
	assertChecked(t, "fun small(value: UInt8): UInt8 do\n    return value\nend\nok: UInt8 := small(200)\n")
	assertRejectsAnyDiagnostic(t,
		"fun small(value: UInt8): UInt8 do\n    return value\nend\nbad: UInt8 := small(300)\n",
		"given value is outside the UInt8 range")
	assertChecked(t, "fun peek(source: Ptr<Int32>): Int32 do\n    return source.value\nend\nmut score: Int32 := 1\ntotal: Int32 := peek(ref score)\n")
}

func TestReturnFormsMatchTheDeclaration(t *testing.T) {
	assertChecked(t, "fun log_value(value: Int32) do\n    return\nend\n")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return\nend\n",
		"return requires a value; adder declares Int32")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    total: Int32 := dx\nend\n",
		"returning adder may fall through without returning Int32")
}

func TestUnsupportedFunPositions(t *testing.T) {
	assertChecked(t, "fun helper(x: Int32): Int32 do\n    return x\nend\nfun maker(): Fun<(Int32) : Int32> do\n    return helper\nend\n")
	assertChecked(t, "type Holder = { callback: Fun<(Int32) : Int32>, }\n")
	assertRejectsAnyDiagnostic(t,
		"type Bad = Ptr<Fun<(Int32) : Int32>>\n",
		"Ptr<Fun<(Int32) : Int32>> is not supported")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nbad: Fun<(Int32) : Int32> := ref adder\n",
		"function declarations are not addressable; use adder as a Fun value")
}

func TestDispatchTableMemberCalls(t *testing.T) {
	assertChecked(t,
		"type Ops = { callback: Fun<(Int32) : Int32>, }\n"+
			"fun handler(value: Int32): Int32 do\n    return value\nend\n"+
			"table: Ops := Ops { callback = handler, }\n"+
			"result: Int32 := table.callback(5)\n")
	assertChecked(t,
		"type ReaderOps<S> = { read: Fun<(MutPtr<S>, MutPtr<Byte>, Size)>, }\n"+
			"type FileState = { position: Size, }\n"+
			"fun read_file(state: MutPtr<FileState>, dest: MutPtr<Byte>, count: Size) do\n    return\nend\n"+
			"mut state: FileState := FileState { position = 0, }\n"+
			"ops: ReaderOps<FileState> := ReaderOps<FileState> { read = read_file, }\n"+
			"mut buf: Byte := b'a'\n"+
			"ops.read(ref state, ref buf, 1)\n")
	assertChecked(t,
		"type Inner = { callback: Fun<(Int32) : Int32>, }\n"+
			"type Outer = { inner: Inner, }\n"+
			"fun handler(value: Int32): Int32 do\n    return value\nend\n"+
			"table: Outer := Outer { inner = Inner { callback = handler, }, }\n"+
			"result: Int32 := table.inner.callback(5)\n")
	assertChecked(t,
		"type Ops<T> = { callback: Fun<(T) : T>, }\n"+
			"fun identity<T>(value: T): T do\n    return value\nend\n"+
			"fun use<T>(table: Ops<T>, value: T): T do\n    return table.callback(value)\nend\n"+
			"fun make<T>(callback: Fun<(T) : T>): Ops<T> do\n    return Ops<T> { callback = callback, }\nend\n"+
			"table: Ops<Int32> := Ops<Int32> { callback = identity, }\n"+
			"result: Int32 := use<Int32>(table, 5)\n"+
			"returned: Ops<Int32> := make<Int32>(identity)\n"+
			"again: Int32 := returned.callback(6)\n")
}

// A non-capturing anonymous function literal can initialize a dispatch-table
// member directly, exactly like a named function or an existing function
// binding.
func TestDispatchTableMemberFromAnonymousLiteral(t *testing.T) {
	assertChecked(t,
		"type Ops = { callback: Fun<(Int32) : Int32>, }\n"+
			"table: Ops := Ops { callback = fun (value: Int32): Int32 do\n"+
			"    return value * 2\n"+
			"end, }\n"+
			"result: Int32 := table.callback(5)\n")
}

// A mutable dispatch-table member can be reassigned to a compatible function
// value.
func TestDispatchTableMutableMemberReassignment(t *testing.T) {
	assertChecked(t,
		"type Ops = { mut callback: Fun<(Int32) : Int32>, }\n"+
			"fun double(v: Int32): Int32 do\n    return v * 2\nend\n"+
			"fun triple(v: Int32): Int32 do\n    return v * 3\nend\n"+
			"mut table: Ops := Ops { callback = double, }\n"+
			"table.callback = triple\n"+
			"result: Int32 := table.callback(5)\n")
}

func TestDispatchTableMemberDiagnosticsAndCShape(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Ops = { value: Int32, }\n"+
			"table: Ops := Ops { value = 1, }\n"+
			"table.value()\n",
		"member value is not callable; its type is Int32")

	source := "type Ops = { callback: Fun<(Int32) : Int32>, }\n" +
		"fun handler(value: Int32): Int32 do\n    return value\nend\n" +
		"table: Ops := Ops { callback = handler, }\n" +
		"result: Int32 := table.callback(5)\n"
	result := assertCompiles(t, source)
	if !strings.Contains(rootH(t, result), "int32_t (*hex_m_callback)(int32_t);") {
		t.Fatalf("modules/app.h = %q, want the concrete function-pointer field", rootH(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_v_table.hex_m_callback(5)") {
		t.Fatalf("modules/app.c = %q, want an indirect member call", rootC(t, result))
	}
}

func TestDispatchTableMemberAcrossModule(t *testing.T) {
	result := compiler.Compile(map[string]string{
		"app.hex": "module Lib = import \"./lib\"\n" +
			"table: Lib.Ops := Lib.make()\n" +
			"result: Int32 := table.callback(5)\n",
		"lib.hex": "export type Ops = { callback: Fun<(Int32) : Int32>, }\n" +
			"fun handler(value: Int32): Int32 do\n    return value\nend\n" +
			"export fun make(): Ops do\n    return Ops { callback = handler, }\nend\n",
	}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("cross-module dispatch table rejected: %#v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_v_table.hex_m_callback(5)") {
		t.Fatalf("modules/app.c = %q, want an indirect imported-member call", rootC(t, result))
	}
}

func TestFunctionNamesAreNotStorage(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nadder = adder\n",
		"cannot assign to function adder")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nmut adder: Int32 := 1\n",
		"adder is already declared")
}

// Methods. `pointType` is the object every method case implements against.
const pointType = "type Point = { mut x: Int32, mut y: Int32, }\n"

func TestMethodDeclarationsAndCalls(t *testing.T) {
	assertChecked(t, pointType+
		"impl Point.length_squared(): Int32 do\n    return self.x * self.x + self.y * self.y\nend\n"+
		"impl Ptr<Point>.is_origin(): Bool do\n    return self.x == 0 and self.y == 0\nend\n"+
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do\n    self.x = self.x + dx\n    self.y = self.y + dy\nend\n"+
		"mut here: Point := Point { x = 0, y = 0, }\n"+
		"here.translate(5, 5)\n"+
		"total: Int32 := here.length_squared()\n"+
		"flag: Bool := here.is_origin()\n")
}

func TestSelfIsAFixedBinding(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+"impl MutPtr<Point>.reset() do\n    self = self\nend\n",
		"cannot assign to self; self is a fixed binding")
	assertRejectsAnyDiagnostic(t, pointType+"impl Point.moved(dx: Int32): Point do\n    self.x = self.x + dx\n    return self\nend\n",
		"cannot assign to read-only member self.x")
	assertChecked(t, pointType+"impl Point.moved(dx: Int32): Point do\n    mut result: Point := self\n    result.x = result.x + dx\n    return result\nend\n")
}

func TestMethodRulesAreEnforced(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+"impl Point.translate() do\n    return\nend\nimpl MutPtr<Point>.translate() do\n    return\nend\n",
		"Point already has a method named translate")
	assertRejectsAnyDiagnostic(t, pointType+"impl Point.x(): Int32 do\n    return 0\nend\n",
		"Point already has a member named x")
	assertRejectsAnyDiagnostic(t, "impl Int32.doubled(): Int32 do\n    return 0\nend\n",
		"Int32 is not a nominal object type; impl requires an object")
	assertRejectsAnyDiagnostic(t, pointType+"origin: Point := Point { x = 0, y = 0, }\ntotal: Int32 := origin.rotate()\n",
		"Point has no method named rotate")
}

func TestMethodSelfRecursionAndForwardCallsResolve(t *testing.T) {
	assertChecked(t, pointType+"impl Point.countdown(value: Int32): Int32 do\n    return self.countdown(value - 1)\nend\n")
	assertChecked(t, pointType+
		"impl Point.magnitude(): Int32 do\n    return self.length_squared()\nend\n"+
		"impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n")
}

func TestFixedReceiverCannotReachAMutPtrMethod(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do\n    self.x = self.x + dx\nend\n"+
		"origin: Point := Point { x = 0, y = 0, }\norigin.translate(5, 5)\n",
		"translate needs MutPtr<Point>; ref origin is Ptr<Point>")
}

func TestFreeFunctionCollidesWithAMethodCName(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+
		"impl Point.translate() do\n    return\nend\nfun Point_translate() do\n    return\nend\n",
		"free function Point_translate collides with impl Point.translate")
	assertRejectsAnyDiagnostic(t,
		"type A_B = { value: Int32, }\n"+
			"type A = { value: Int32, }\n"+
			"impl A_B.c() do\n    return\nend\n"+
			"impl A.B_c() do\n    return\nend\n",
		"impl A.B_c collides with impl A_B.c")
}

func assertGeneratedC(t *testing.T, source, want string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected %q: %#v", source, result.Stderr)
	}
	if got := withoutLineDirectives(rootC(t, result)); !strings.Contains(got, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", got, want)
	}
}

func TestGeneratedMethodDefinitionsAndCalls(t *testing.T) {
	source := pointType +
		"impl Point.length_squared(): Int32 do\n" +
		"    return self.x * self.x + self.y * self.y\n" +
		"end\n" +
		"impl Ptr<Point>.is_origin(): Bool do\n" +
		"    return self.x == 0 and self.y == 0\n" +
		"end\n" +
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do\n" + "    self.x = self.x + dx\n" + "    self.y = self.y + dy\n" + "end\n" +
		"mut here: Point := Point { x = 0, y = 0, }\n" +
		"here.translate(5, 5)\n" + "total: Int32 := here.length_squared()\n" + "flag: Bool := here.is_origin()\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("method generation failed: %#v", result)
	}
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Point_length_squared(const hex_t_m3_app_Point hex_v_self) {",
		"static bool hex_f_m3_app_Point_is_origin(const hex_t_m3_app_Point *const hex_v_self) {",
		"static void hex_f_m3_app_Point_translate(hex_t_m3_app_Point *const hex_v_self, const int32_t hex_v_dx, const int32_t hex_v_dy) {",
		"hex_f_m3_app_Point_translate(&hex_v_here, 5, 5);",
		"hex_f_m3_app_Point_length_squared(hex_v_here)",
		"hex_f_m3_app_Point_is_origin(&hex_v_here)",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
}

func TestGeneratedFunctionDefinitionIsStaticAtFileScope(t *testing.T) {
	assertGeneratedC(t,
		"fun identity(value: Int32): Int32 do\n    return value\nend\n",
		"#include \"modules/app.h\"\n\nstatic int32_t hex_f_m3_app_identity(int32_t);\n\nstatic int32_t hex_f_m3_app_identity(const int32_t hex_v_value) {\n    return hex_v_value;\n}\n\nint main(void) {\n")
}

func TestGeneratedNoReturnFunctionIsVoid(t *testing.T) {
	assertGeneratedC(t,
		"fun reset(counter: MutPtr<Int32>) do\n    counter.value = 0\nend\nmut count: Int32 := 1\nreset(ref count)\n",
		"static void hex_f_m3_app_reset(int32_t *const hex_v_counter) {\n    *hex_v_counter = 0;\n}\n")
}

func TestGeneratedZeroParameterFunctionTakesVoid(t *testing.T) {
	assertGeneratedC(t,
		"fun seed(): Int32 do\n    return 7\nend\n",
		"static int32_t hex_f_m3_app_seed(void) {\n    return 7;\n}\n")
}

// The stored pointer type carries unqualified parameters even though the
// definition binds const int32_t. C ignores top-level parameter qualifiers
// when comparing function types, so the parameters must stay unqualified.
func TestGeneratedFunctionPointerObjectsKeepUnqualifiedParameters(t *testing.T) {
	source := "fun identity(value: Int32): Int32 do\n    return value\nend\n" +
		"callback: Fun<(Int32) : Int32> := identity\nmut selected: Fun<(Int32) : Int32> := identity\n"
	assertGeneratedC(t, source, "    int32_t (*const hex_v_callback)(int32_t) = hex_f_m3_app_identity;\n")
	assertGeneratedC(t, source, "    int32_t (*hex_v_selected)(int32_t) = hex_f_m3_app_identity;\n")
	if got := rootC(t, compileSource(source)); strings.Contains(got, ")(const int32_t)") {
		t.Fatalf("modules/app.c = %q, function-pointer parameters must stay unqualified", got)
	}
}

func TestGeneratedFunctionPointerParameterAndCall(t *testing.T) {
	assertGeneratedC(t,
		"fun square(value: Int32): Int32 do\n    return value * value\nend\n"+
			"fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n    return callback(value)\nend\n"+
			"result: Int32 := apply(square, 5)\n",
		"static int32_t hex_f_m3_app_apply(int32_t (*const hex_v_callback)(int32_t), const int32_t hex_v_value) {\n"+
			"    return hex_v_callback(hex_v_value);\n}\n")
}

func TestGeneratedCallExpressionAndCallStatement(t *testing.T) {
	assertGeneratedC(t,
		"fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx\nend\ntotal: Int32 := adder(2, 3)\n",
		"    const int32_t hex_v_total = hex_f_m3_app_adder(2, 3);\n")
	assertGeneratedC(t,
		"fun reset(counter: MutPtr<Int32>) do\n    counter.value = 0\nend\nmut count: Int32 := 1\nreset(ref count)\n",
		"    hex_f_m3_app_reset(&hex_v_count);\n")
}

// Every private module-level function gets a static prototype ahead of its
// definition, self-recursive or not: module-level visibility is
// order-independent, so the generator cannot special-case self-recursion as
// the one case needing no forward declaration.
func TestGeneratedPrivateFunctionGetsAPrototype(t *testing.T) {
	source := "fun countdown(value: Int32): Int32 do\n    return countdown(value)\nend\n"
	body := withoutLineDirectives(rootC(t, compileSource(source)))
	prototype := "static int32_t hex_f_m3_app_countdown(int32_t);"
	definition := "static int32_t hex_f_m3_app_countdown(const int32_t hex_v_value) {\n    return hex_f_m3_app_countdown(hex_v_value);\n}\n"
	prototypeIndex := strings.Index(body, prototype)
	definitionIndex := strings.Index(body, definition)
	if prototypeIndex < 0 || definitionIndex < 0 {
		t.Fatalf("modules/app.c = %q, want both %q and %q", body, prototype, definition)
	}
	if prototypeIndex >= definitionIndex {
		t.Fatalf("prototype at %d must precede definition at %d", prototypeIndex, definitionIndex)
	}
}

// A function calling a later private function compiles: the earlier
// definition's own prototype region gives the later function's symbol a
// declaration before it is used.
func TestGeneratedForwardCallCompiles(t *testing.T) {
	body := withoutLineDirectives(rootC(t, compileSource(
		"fun is_even(value: Int32): Int32 do\n    return is_odd(value - 1)\nend\nfun is_odd(value: Int32): Int32 do\n    return value\nend\n")))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_is_even(int32_t);",
		"static int32_t hex_f_m3_app_is_odd(int32_t);",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("modules/app.c = %q, want %q", body, want)
		}
	}
	if strings.Index(body, "static int32_t hex_f_m3_app_is_even(int32_t);") >= strings.Index(body, "static int32_t hex_f_m3_app_is_even(const int32_t hex_v_value) {") {
		t.Fatalf("modules/app.c = %q, want is_even's own prototype before its own definition", body)
	}
}

// Two mutually recursive private functions each get exactly one prototype
// and one definition, with both prototypes ahead of both definitions.
func TestGeneratedMutualRecursionCompiles(t *testing.T) {
	body := withoutLineDirectives(rootC(t, compileSource(
		"fun is_even(value: Int32): Bool do\n    if value == 0 then\n        return true\n    end\n    return is_odd(value - 1)\nend\n"+
			"fun is_odd(value: Int32): Bool do\n    if value == 0 then\n        return false\n    end\n    return is_even(value - 1)\nend\n")))
	firstDefinition := strings.Index(body, "static bool hex_f_m3_app_is_even(const int32_t hex_v_value) {")
	secondDefinition := strings.Index(body, "static bool hex_f_m3_app_is_odd(const int32_t hex_v_value) {")
	for _, symbol := range []string{"hex_f_m3_app_is_even", "hex_f_m3_app_is_odd"} {
		if strings.Count(body, symbol) != 3 {
			// One prototype, one definition, one call from the other
			// function's own body.
			t.Fatalf("generated C = %q, want exactly 3 occurrences of %q", body, symbol)
		}
	}
	prototype := "static bool hex_f_m3_app_is_even(int32_t);"
	if index := strings.Index(body, prototype); index < 0 || index >= firstDefinition || index >= secondDefinition {
		t.Fatalf("generated C = %q, want %q before both definitions", body, prototype)
	}
}

// Repeated compilations of mutually recursive functions produce identical
// generated C, including prototype ordering: collectedFunctions is keyed by
// item index and prototypes are emitted by a single source-order walk, never
// by iterating a name-keyed map, so nothing here can vary between runs.
func TestGeneratedMutualRecursionIsDeterministic(t *testing.T) {
	source := "fun is_even(value: Int32): Bool do\n    if value == 0 then\n        return true\n    end\n    return is_odd(value - 1)\nend\n" +
		"fun is_odd(value: Int32): Bool do\n    if value == 0 then\n        return false\n    end\n    return is_even(value - 1)\nend\n"
	first := rootC(t, compileSource(source))
	for attempt := range 8 {
		next := rootC(t, compileSource(source))
		if next != first {
			t.Fatalf("compile %d changed modules/app.c:\nfirst:\n%s\nlater:\n%s", attempt+2, first, next)
		}
	}
}

// An exported function's prototype comes from the module header only; the
// module C file never duplicates it as a static prototype.
func TestGeneratedExportedFunctionPrototypeIsNotDuplicated(t *testing.T) {
	result := assertCompiles(t, "export fun square(value: Int32): Int32 do\n    return value * value\nend\n")
	body := rootC(t, result)
	if strings.Contains(body, "static int32_t hex_f_m3_app_square") {
		t.Fatalf("modules/app.c = %q, want no static prototype or definition for an exported function", body)
	}
	if !strings.Contains(rootH(t, result), "int32_t hex_f_m3_app_square(int32_t);") {
		t.Fatalf("modules/app.h = %q, want the exported prototype", rootH(t, result))
	}
}

// Object typedefs live in the module header, so the module C holds the
// definitions in source order and then main. Module storage stays inside
// main.
func TestGeneratedDefinitionsAreOrderedBeforeMain(t *testing.T) {
	result := compileSource(pointType +
		"fun first(value: Int32): Int32 do\n    return value\nend\n" +
		"fun second(value: Int32): Int32 do\n    return value\nend\n" +
		"origin: Point := Point { x = 0, y = 0, }\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	first := strings.Index(rootC(t, result), "hex_f_m3_app_first")
	second := strings.Index(rootC(t, result), "hex_f_m3_app_second")
	main := strings.Index(rootC(t, result), "int main(void)")
	origin := strings.Index(rootC(t, result), "hex_v_origin")
	if first < 0 || second < first || main < second || origin < main {
		t.Fatalf("modules/app.c = %q, want first, second, main, then module storage", rootC(t, result))
	}
	if !strings.Contains(rootH(t, result), "struct hex_t_m3_app_Point {") {
		t.Fatalf("modules/app.h = %q, want the object definition region", rootH(t, result))
	}
}

func TestGeneratedFunctionBodiesKeepLineDirectives(t *testing.T) {
	result := compileSource("fun identity(value: Int32): Int32 do\n    return value\nend\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "#line 2 \"app.hex\"\n    return hex_v_value;") {
		t.Fatalf("modules/app.c = %q, want a line directive inside the function body", rootC(t, result))
	}
}

// A root call statement with a string-literal argument compiles: the
// preflight pass renders call statements to prove renderability and must
// resolve literals against the module's registry (snippet-audit regression).
func TestRootCallStatementWithStringLiteralArgument(t *testing.T) {
	result := compileSource("fun greet(text: String) do\nend\ngreet(\"hi\")\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_greet(&hex_lit_0)") {
		t.Fatalf("generated C = %q, want the literal-backed call", rootC(t, result))
	}
}

// The preflight also validates specialized generic bodies, so a string
// literal argument there resolves against the registry too.
func TestSpecializedBodyCallStatementWithStringLiteralArgument(t *testing.T) {
	result := compileSource("fun greet(text: String) do\nend\nfun wrap<T>(v: T): T do\n    greet(\"hi\")\n    return v\nend\nx: Int32 := wrap(1)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "&hex_lit_0") {
		t.Fatalf("generated C = %q, want the literal registry entry in the specialized body", rootC(t, result))
	}
}

// A bare anonymous literal passed directly as a call argument lowers to a
// direct function pointer reference with no wrapper, dispatcher, or
// allocation.
func TestBareLiteralArgumentLowersToDirectFunctionPointer(t *testing.T) {
	result := assertCompiles(t,
		"fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n"+
			"    return callback(value)\n"+
			"end\n"+
			"result: Int32 := apply(fun (value: Int32): Int32 do\n"+
			"    return value * value\n"+
			"end, 5)\n")
	body := rootC(t, result)
	if !strings.Contains(body, "static int32_t hex_fun_") {
		t.Fatalf("generated C = %q, want a static hex_fun_<ordinal> helper", body)
	}
	if !strings.Contains(body, "hex_f_m3_app_apply(hex_fun_") {
		t.Fatalf("generated C = %q, want apply called with the literal's function pointer directly", body)
	}
}

// Two sibling anonymous literals at the same nesting level receive distinct
// ordinals in source order, and two identical compilations produce
// byte-identical output.
func TestSiblingLiteralsGetDistinctDeterministicOrdinals(t *testing.T) {
	source := "fun apply2(a: Fun<(Int32) : Int32>, b: Fun<(Int32) : Int32>, v: Int32): Int32 do\n" +
		"    return a(v) + b(v)\n" +
		"end\n" +
		"x: Int32 := apply2(fun (n: Int32): Int32 do\n" +
		"    return n + 1\n" +
		"end, fun (n: Int32): Int32 do\n" +
		"    return n * 2\n" +
		"end, 5)\n"
	first := assertCompiles(t, source)
	second := assertCompiles(t, source)
	firstBody := rootC(t, first)
	if firstBody != rootC(t, second) {
		t.Fatal("two identical compilations produced different generated C")
	}
	firstOrdinal := strings.Index(firstBody, "static int32_t hex_fun_")
	secondOrdinal := strings.LastIndex(firstBody, "static int32_t hex_fun_")
	if firstOrdinal < 0 || firstOrdinal == secondOrdinal {
		t.Fatalf("generated C = %q, want two distinct hex_fun_<ordinal> definitions", firstBody)
	}
}

// A named function declaration is rejected wherever it is nested: directly
// inside a function body, inside module-level control flow, and inside a
// loop, each with the exact module-scope-only diagnostic.
func TestNamedFunctionDeclarationRejectedWhenNested(t *testing.T) {
	for _, source := range []string{
		"fun apply(value: Int32): Int32 do\n    fun identity(input: Int32): Int32 do\n        return input\n    end\n    return identity(value)\nend\n",
		"cond: Bool := true\nif cond then\n    fun helper(): Int32 do\n        return 1\n    end\n    x: Int32 := helper()\nend\n",
		"mut i: Int32 := 0\nwhile i < 1 do\n    fun helper(): Int32 do\n        return 1\n    end\n    i = i + helper()\nend\n",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "named function declarations are only valid at module scope") {
			t.Fatalf("Compile(%q) = %#v, want the module-scope-only diagnostic", source, result.Stderr)
		}
	}
}
