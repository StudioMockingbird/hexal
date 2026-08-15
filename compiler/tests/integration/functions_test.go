package integration

// Functions: declarations, Fun<...> function-pointer types, calls, returns,
// closed function scopes, and the deferred Fun<...> positions. Spec 0008.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func requireChecked(t *testing.T, source string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile rejected %q: %#v", source, result.Stderr)
	}
}

func requireRejected(t *testing.T, source, want string) {
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
	requireChecked(t, "fun adder(dx: Int32, dy: Int32): Int32\n    return dx + dy\nend\ntotal: Int32 = adder(2, 3)\n")
}

func TestNoReturnFunctionIsACallStatement(t *testing.T) {
	requireChecked(t, "fun reset(counter: MutPtr<Int32>)\n    counter.value = 0\nend\nmut count: Int32 = 1\nreset(ref count)\n")
	requireRejected(t,
		"fun reset(counter: MutPtr<Int32>)\n    counter.value = 0\nend\nmut count: Int32 = 1\nresult: Int32 = reset(ref count)\n",
		"reset produces no value")
}

func TestFunTypeBindingsAndCallbacks(t *testing.T) {
	requireChecked(t, "fun square(value: Int32): Int32\n    return value * value\nend\nfun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32\n    return callback(value)\nend\nresult: Int32 = apply(square, 5)\n")
	requireChecked(t, "fun identity(value: Int32): Int32\n    return value\nend\nmut selected: Fun<(Int32) : Int32> = identity\nselected = identity\n")
}

func TestFunTypeMismatchIsReported(t *testing.T) {
	requireRejected(t,
		"fun adder(dx: UInt32): UInt32\n    return dx\nend\nhandler: Fun<(Int32) : Int32> = adder\n",
		"handler requires Fun<(Int32) : Int32>, got Fun<(UInt32) : UInt32>")
}

func TestDeclarationOrderIsSourceOrder(t *testing.T) {
	requireChecked(t, "fun factorial(value: Int32): Int32\n    return value * factorial(value - 1)\nend\n")
	requireRejected(t,
		"fun is_even(value: Int32): Int32\n    return is_odd(value - 1)\nend\nfun is_odd(value: Int32): Int32\n    return value\nend\n",
		"unknown function is_odd; functions must be declared before use")
}

func TestFunctionScopeIsClosed(t *testing.T) {
	requireChecked(t, "count: Int32 = 3\nfun scoped(seed: Int32): Int32\n    mut count: Int32 = seed\n    count = count + 1\n    return count\nend\n")
	requireRejected(t,
		"mut count: Int32 = 0\nfun read_count(): Int32\n    return count\nend\n",
		"function read_count cannot access module data binding count; pass it as a parameter")
}

func TestParametersAreFixed(t *testing.T) {
	requireRejected(t,
		"fun small(value: Int32): Int32\n    value = 1\n    return value\nend\n",
		"cannot assign to parameter value; parameters are fixed bindings")
}

func TestCallChecksArityAndArgumentTypes(t *testing.T) {
	requireRejected(t,
		"fun adder(dx: Int32, dy: Int32): Int32\n    return dx + dy\nend\ntotal: Int32 = adder(1, 2, 3)\n",
		"adder expects 2 arguments, got 3")
	requireChecked(t, "fun small(value: UInt8): UInt8\n    return value\nend\nok: UInt8 = small(200)\n")
	requireRejected(t,
		"fun small(value: UInt8): UInt8\n    return value\nend\nbad: UInt8 = small(300)\n",
		"given value is outside the UInt8 range")
	requireChecked(t, "fun peek(source: Ptr<Int32>): Int32\n    return source.value\nend\nmut score: Int32 = 1\ntotal: Int32 = peek(ref score)\n")
}

func TestReturnFormsMatchTheDeclaration(t *testing.T) {
	requireChecked(t, "fun log_value(value: Int32)\n    return\nend\n")
	requireRejected(t,
		"fun adder(dx: Int32): Int32\n    return\nend\n",
		"return requires a value; adder declares Int32")
	requireRejected(t,
		"fun adder(dx: Int32): Int32\n    total: Int32 = dx\nend\n",
		"returning adder may fall through without returning Int32")
}

func TestUnsupportedFunPositions(t *testing.T) {
	requireRejected(t,
		"fun maker(): Fun<(Int32) : Int32>\n    return maker\nend\n",
		"returning Fun<(Int32) : Int32> is not supported")
	requireRejected(t,
		"type Holder = { callback: Fun<(Int32) : Int32>, }\n",
		"Fun<…> object members are not supported")
	requireRejected(t,
		"type Bad = Ptr<Fun<(Int32) : Int32>>\n",
		"Ptr<Fun<(Int32) : Int32>> is not supported")
	requireRejected(t,
		"fun adder(dx: Int32): Int32\n    return dx\nend\nbad: Fun<(Int32) : Int32> = ref adder\n",
		"function declarations are not addressable; use adder as a Fun value")
}

func TestFunctionNamesAreNotStorage(t *testing.T) {
	requireRejected(t,
		"fun adder(dx: Int32): Int32\n    return dx\nend\nadder = adder\n",
		"cannot assign to function adder")
	requireRejected(t,
		"fun adder(dx: Int32): Int32\n    return dx\nend\nmut adder: Int32 = 1\n",
		"adder is already declared")
}

// Methods. `pointType` is the object every method case implements against.
const pointType = "type Point = { mut x: Int32, mut y: Int32, }\n"

func TestMethodDeclarationsAndCalls(t *testing.T) {
	requireChecked(t, pointType+
		"impl Point.length_squared(): Int32\n    return self.x * self.x + self.y * self.y\nend\n"+
		"impl Ptr<Point>.is_origin(): Bool\n    return self.x == 0 and self.y == 0\nend\n"+
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32)\n    self.x = self.x + dx\n    self.y = self.y + dy\nend\n"+
		"mut here: Point = Point { x = 0, y = 0, }\n"+
		"here.translate(5, 5)\n"+
		"total: Int32 = here.length_squared()\n"+
		"flag: Bool = here.is_origin()\n")
}

func TestSelfIsAFixedBinding(t *testing.T) {
	requireRejected(t, pointType+"impl MutPtr<Point>.reset()\n    self = self\nend\n",
		"cannot assign to self; self is a fixed binding")
	requireRejected(t, pointType+"impl Point.moved(dx: Int32): Point\n    self.x = self.x + dx\n    return self\nend\n",
		"cannot assign to read-only member self.x")
	requireChecked(t, pointType+"impl Point.moved(dx: Int32): Point\n    mut result: Point = self\n    result.x = result.x + dx\n    return result\nend\n")
}

func TestMethodRulesAreEnforced(t *testing.T) {
	requireRejected(t, pointType+"impl Point.translate()\n    return\nend\nimpl MutPtr<Point>.translate()\n    return\nend\n",
		"Point already has a method named translate")
	requireRejected(t, pointType+"impl Point.x(): Int32\n    return 0\nend\n",
		"Point already has a member named x")
	requireRejected(t, "impl Int32.doubled(): Int32\n    return 0\nend\n",
		"Int32 is not a nominal object type; impl requires an object")
	requireRejected(t, pointType+"origin: Point = Point { x = 0, y = 0, }\ntotal: Int32 = origin.rotate()\n",
		"Point has no method named rotate")
}

func TestMethodDeclarationOrderIsSourceOrder(t *testing.T) {
	requireChecked(t, pointType+"impl Point.countdown(value: Int32): Int32\n    return self.countdown(value - 1)\nend\n")
	requireRejected(t, pointType+
		"impl Point.magnitude(): Int32\n    return self.length_squared()\nend\n"+
		"impl Point.length_squared(): Int32\n    return self.x * self.x\nend\n",
		"Point has no method named length_squared")
}

func TestFixedReceiverCannotReachAMutPtrMethod(t *testing.T) {
	requireRejected(t, pointType+
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32)\n    self.x = self.x + dx\nend\n"+
		"origin: Point = Point { x = 0, y = 0, }\norigin.translate(5, 5)\n",
		"translate needs MutPtr<Point>; ref origin is Ptr<Point>")
}

func TestFreeFunctionCollidesWithAMethodCName(t *testing.T) {
	requireRejected(t, pointType+
		"impl Point.translate()\n    return\nend\nfun Point_translate()\n    return\nend\n",
		"free function Point_translate collides with impl Point.translate")
	requireRejected(t,
		"type A_B = { value: Int32, }\n"+
			"type A = { value: Int32, }\n"+
			"impl A_B.c()\n    return\nend\n"+
			"impl A.B_c()\n    return\nend\n",
		"impl A.B_c collides with impl A_B.c")
}

// Generated C23 for function lowering. Spec 0008, section "C23 lowering".

func requireGeneratedC(t *testing.T, source, want string) {
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
		"impl Point.length_squared(): Int32\n" +
		"    return self.x * self.x + self.y * self.y\n" +
		"end\n" +
		"impl Ptr<Point>.is_origin(): Bool\n" +
		"    return self.x == 0 and self.y == 0\n" +
		"end\n" +
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32)\n" + "    self.x = self.x + dx\n" + "    self.y = self.y + dy\n" + "end\n" +
		"mut here: Point = Point { x = 0, y = 0, }\n" +
		"here.translate(5, 5)\n" + "total: Int32 = here.length_squared()\n" + "flag: Bool = here.is_origin()\n"
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
	requireGeneratedC(t,
		"fun identity(value: Int32): Int32\n    return value\nend\n",
		"#include \"modules/app.h\"\n\nstatic int32_t hex_f_m3_app_identity(const int32_t hex_v_value) {\n    return hex_v_value;\n}\n\nint main(void) {\n")
}

func TestGeneratedNoReturnFunctionIsVoid(t *testing.T) {
	requireGeneratedC(t,
		"fun reset(counter: MutPtr<Int32>)\n    counter.value = 0\nend\nmut count: Int32 = 1\nreset(ref count)\n",
		"static void hex_f_m3_app_reset(int32_t *const hex_v_counter) {\n    *hex_v_counter = 0;\n}\n")
}

func TestGeneratedZeroParameterFunctionTakesVoid(t *testing.T) {
	requireGeneratedC(t,
		"fun seed(): Int32\n    return 7\nend\n",
		"static int32_t hex_f_m3_app_seed(void) {\n    return 7;\n}\n")
}

// The stored pointer type carries unqualified parameters even though the
// definition binds const int32_t. C ignores top-level parameter qualifiers
// when comparing function types, and spec 0008 forbids "correcting" this.
func TestGeneratedFunctionPointerObjectsKeepUnqualifiedParameters(t *testing.T) {
	source := "fun identity(value: Int32): Int32\n    return value\nend\n" +
		"callback: Fun<(Int32) : Int32> = identity\nmut selected: Fun<(Int32) : Int32> = identity\n"
	requireGeneratedC(t, source, "    int32_t (*const hex_v_callback)(int32_t) = hex_f_m3_app_identity;\n")
	requireGeneratedC(t, source, "    int32_t (*hex_v_selected)(int32_t) = hex_f_m3_app_identity;\n")
	if got := rootC(t, compileSource(source)); strings.Contains(got, ")(const int32_t)") {
		t.Fatalf("modules/app.c = %q, function-pointer parameters must stay unqualified", got)
	}
}

func TestGeneratedFunctionPointerParameterAndCall(t *testing.T) {
	requireGeneratedC(t,
		"fun square(value: Int32): Int32\n    return value * value\nend\n"+
			"fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32\n    return callback(value)\nend\n"+
			"result: Int32 = apply(square, 5)\n",
		"static int32_t hex_f_m3_app_apply(int32_t (*const hex_v_callback)(int32_t), const int32_t hex_v_value) {\n"+
			"    return hex_v_callback(hex_v_value);\n}\n")
}

func TestGeneratedCallExpressionAndCallStatement(t *testing.T) {
	requireGeneratedC(t,
		"fun adder(dx: Int32, dy: Int32): Int32\n    return dx\nend\ntotal: Int32 = adder(2, 3)\n",
		"    const int32_t hex_v_total = hex_f_m3_app_adder(2, 3);\n")
	requireGeneratedC(t,
		"fun reset(counter: MutPtr<Int32>)\n    counter.value = 0\nend\nmut count: Int32 = 1\nreset(ref count)\n",
		"    hex_f_m3_app_reset(&hex_v_count);\n")
}

func TestGeneratedSelfRecursionNeedsNoPrototype(t *testing.T) {
	source := "fun countdown(value: Int32): Int32\n    return countdown(value)\nend\n"
	requireGeneratedC(t, source,
		"static int32_t hex_f_m3_app_countdown(const int32_t hex_v_value) {\n    return hex_f_m3_app_countdown(hex_v_value);\n}\n")
	if got := rootC(t, compileSource(source)); strings.Contains(got, "hex_f_m3_app_countdown(const int32_t hex_v_value);") {
		t.Fatalf("modules/app.c = %q, want no forward prototype region", got)
	}
}

// Object typedefs live in the module header, so the module C holds the
// definitions in source order and then main. Module storage stays inside
// main.
func TestGeneratedDefinitionsAreOrderedBeforeMain(t *testing.T) {
	result := compileSource(pointType +
		"fun first(value: Int32): Int32\n    return value\nend\n" +
		"fun second(value: Int32): Int32\n    return value\nend\n" +
		"origin: Point = Point { x = 0, y = 0, }\n")
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
	result := compileSource("fun identity(value: Int32): Int32\n    return value\nend\n")
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
	result := compileSource("fun greet(text: String)\nend\ngreet(\"hi\")\n")
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
	result := compileSource("fun greet(text: String)\nend\nfun wrap<T>(v: T): T\n    greet(\"hi\")\n    return v\nend\nx: Int32 = wrap(1)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "&hex_lit_0") {
		t.Fatalf("generated C = %q, want the literal registry entry in the specialized body", rootC(t, result))
	}
}
