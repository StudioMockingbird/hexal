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
	assertChecked(t, "fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx + dy\nend\nlet total: Int32 = adder(2, 3)\n")
}

func TestNoReturnFunctionIsACallStatement(t *testing.T) {
	assertChecked(t, "fun reset(counter: Ptr<mut Int32>) do\n    ^counter = 0\nend\nlet mut count: Int32 = 1\nreset(@count)\n")
	assertRejectsAnyDiagnostic(t,
		"fun reset(counter: Ptr<mut Int32>) do\n    ^counter = 0\nend\nlet mut count: Int32 = 1\nlet result: Int32 = reset(@count)\n",
		"reset produces no value")
}

func TestFunTypeBindingsAndCallbacks(t *testing.T) {
	assertChecked(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n    return callback(value)\nend\nlet result: Int32 = apply(square, 5)\n")
	assertChecked(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nlet mut selected: Fun<(Int32) : Int32> = identity\nselected = identity\n")
}

func TestFunTypeMismatchIsReported(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: UInt32): UInt32 do\n    return dx\nend\nlet handler: Fun<(Int32) : Int32> = adder\n",
		"handler requires Fun<(Int32) : Int32>; got Fun<(UInt32) : UInt32>")
}

func TestSelfRecursionAndForwardCallsResolve(t *testing.T) {
	assertChecked(t, "fun factorial(value: Int32): Int32 do\n    return value * factorial(value - 1)\nend\n")
	assertChecked(t,
		"fun is_even(value: Int32): Int32 do\n    return is_odd(value - 1)\nend\nfun is_odd(value: Int32): Int32 do\n    return value\nend\n")
}

func TestFunctionScopeIsClosed(t *testing.T) {
	assertChecked(t, "let count: Int32 = 3\nfun scoped(seed: Int32): Int32 do\n    let mut count: Int32 = seed\n    count = count + 1\n    return count\nend\n")
	assertChecked(t,
		"let mut count: Int32 = 0\nfun read_count(): Int32 do\n    return count\nend\n")
}

func TestParametersAreFixed(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun small(value: Int32): Int32 do\n    value = 1\n    return value\nend\n",
		"cannot assign to parameter value; parameters are fixed bindings")
}

func TestCallChecksArityAndArgumentTypes(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx + dy\nend\nlet total: Int32 = adder(1, 2, 3)\n",
		"adder expects 2 arguments; got 3")
	assertChecked(t, "fun small(value: UInt8): UInt8 do\n    return value\nend\nlet ok: UInt8 = small(200)\n")
	assertRejectsAnyDiagnostic(t,
		"fun small(value: UInt8): UInt8 do\n    return value\nend\nlet bad: UInt8 = small(300)\n",
		"given value is outside the UInt8 range")
	assertChecked(t, "fun peek(source: Ptr<Int32>): Int32 do\n    return ^source\nend\nlet mut score: Int32 = 1\nlet total: Int32 = peek(@score)\n")
}

func TestReturnFormsMatchTheDeclaration(t *testing.T) {
	assertChecked(t, "fun log_value(value: Int32) do\n    return\nend\n")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return\nend\n",
		"return requires a value; adder declares Int32")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    let total: Int32 = dx\nend\n",
		"returning adder may fall through without returning Int32")
}

func TestUnsupportedFunPositions(t *testing.T) {
	assertChecked(t, "fun helper(x: Int32): Int32 do\n    return x\nend\nfun maker(): Fun<(Int32) : Int32> do\n    return helper\nend\n")
	assertChecked(t, "type Holder is struct callback: Fun<(Int32) : Int32>, end\n")
	assertRejectsAnyDiagnostic(t,
		"type Bad is Ptr<Fun<(Int32) : Int32>>\n",
		"Ptr<Fun<(Int32) : Int32>> is not supported")
	assertRejectsAnyDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nlet bad: Fun<(Int32) : Int32> = @adder\n",
		"function declarations are not addressable; use adder as a Fun value")
}

func TestDispatchTableMemberCalls(t *testing.T) {
	assertChecked(t,
		"type Ops is struct callback: Fun<(Int32) : Int32>, end\n"+
			"fun handler(value: Int32): Int32 do\n    return value\nend\n"+
			"let table: Ops = Ops(callback = handler, )\n"+
			"let result: Int32 = table.callback(5)\n")
	assertChecked(t,
		"type ReaderOps<S> is struct read: Fun<(Ptr<mut S>, Ptr<mut Byte>, Size)>, end\n"+
			"type FileState is struct position: Size, end\n"+
			"fun read_file(state: Ptr<mut FileState>, dest: Ptr<mut Byte>, count: Size) do\n    return\nend\n"+
			"let mut state: FileState = FileState(position = 0, )\n"+
			"let ops: ReaderOps<FileState> = ReaderOps<FileState>(read = read_file, )\n"+
			"let mut buf: Byte = b'a'\n"+
			"ops.read(@state, @buf, 1)\n")
	assertChecked(t,
		"type Inner is struct callback: Fun<(Int32) : Int32>, end\n"+
			"type Outer is struct inner: Inner, end\n"+
			"fun handler(value: Int32): Int32 do\n    return value\nend\n"+
			"let table: Outer = Outer(inner = Inner(callback = handler, ), )\n"+
			"let result: Int32 = table.inner.callback(5)\n")
	assertChecked(t,
		"type Ops<T> is struct callback: Fun<(T) : T>, end\n"+
			"fun identity<T>(value: T): T do\n    return value\nend\n"+
			"fun use<T>(table: Ops<T>, value: T): T do\n    return table.callback(value)\nend\n"+
			"fun make<T>(callback: Fun<(T) : T>): Ops<T> do\n    return Ops<T>(callback = callback, )\nend\n"+
			"let table: Ops<Int32> = Ops<Int32>(callback = identity, )\n"+
			"let result: Int32 = use<Int32>(table, 5)\n"+
			"let returned: Ops<Int32> = make<Int32>(identity)\n"+
			"let again: Int32 = returned.callback(6)\n")
}

// A non-capturing anonymous function literal can initialize a dispatch-table
// member directly, exactly like a named function or an existing function
// binding.
func TestDispatchTableMemberFromAnonymousLiteral(t *testing.T) {
	assertChecked(t,
		"type Ops is struct callback: Fun<(Int32) : Int32>, end\n"+
			"let table: Ops = Ops(callback = fun (value: Int32): Int32 do\n"+
			"    return value * 2\n"+
			"end, )\n"+
			"let result: Int32 = table.callback(5)\n")
}

// A mutable dispatch-table member can be reassigned to a compatible function
// value.
func TestDispatchTableMutableMemberReassignment(t *testing.T) {
	assertChecked(t,
		"type Ops is struct mut callback: Fun<(Int32) : Int32>, end\n"+
			"fun double(v: Int32): Int32 do\n    return v * 2\nend\n"+
			"fun triple(v: Int32): Int32 do\n    return v * 3\nend\n"+
			"let mut table: Ops = Ops(callback = double, )\n"+
			"table.callback = triple\n"+
			"let result: Int32 = table.callback(5)\n")
}

func TestDispatchTableMemberDiagnosticsAndCShape(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Ops is struct value: Int32, end\n"+
			"let table: Ops = Ops(value = 1, )\n"+
			"table.value()\n",
		"member value is not callable; its type is Int32")

	source := "type Ops is struct callback: Fun<(Int32) : Int32>, end\n" +
		"fun handler(value: Int32): Int32 do\n    return value\nend\n" +
		"let table: Ops = Ops(callback = handler, )\n" +
		"let result: Int32 = table.callback(5)\n"
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
		"app.hex": "import\n    Lib from \"./lib\"\nend\n" +
			"let table: Lib.Ops = Lib.make()\n" +
			"let result: Int32 = table.callback(5)\n",
		"lib.hex": "type Ops is struct callback: Fun<(Int32) : Int32>, end\n" +
			"fun handler(value: Int32): Int32 do\n    return value\nend\n" +
			"fun make(): Ops do\n    return Ops(callback = handler, )\nend\nexport\n    Ops,\n    make\nend\n",
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
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nlet mut adder: Int32 = 1\n",
		"adder is already declared")
}

// Methods. `pointType` is the object every method case implements against.
const pointType = "type Point is struct mut x: Int32, mut y: Int32, end\n"

func TestMethodDeclarationsAndCalls(t *testing.T) {
	assertChecked(t, pointType+
		"method Point.length_squared(): Int32 do\n    return (self.x * self.x) + (self.y * self.y)\nend\n"+
		"method Point.is_origin(): Bool do\n    return (self.x == 0) and (self.y == 0)\nend\n"+
		"fun translate(target: Ptr<mut Point>, dx: Int32, dy: Int32) do\n    target.x = target.x + dx\n    target.y = target.y + dy\nend\n"+
		"let mut here: Point = Point(x = 0, y = 0, )\n"+
		"translate(@here, 5, 5)\n"+
		"let total: Int32 = here.length_squared()\n"+
		"let flag: Bool = here.is_origin()\n")
}

func TestMethodCallsThroughPointersReachThePointee(t *testing.T) {
	assertChecked(t, pointType+
		"method Point.is_origin(): Bool do\n    return (self.x == 0) and (self.y == 0)\nend\n"+
		"let mut here: Point = Point(x = 0, y = 0, )\n"+
		"let reader: Ptr<Point> = @here\n"+
		"let writer: Ptr<mut Point> = @here\n"+
		"let first: Bool = reader.is_origin()\n"+
		"let second: Bool = writer.is_origin()\n")
}

func TestSelfIsAFixedBinding(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+"method Point.reset() do\n    self = self\nend\n",
		"cannot assign to self; self is a fixed binding")
	assertRejectsAnyDiagnostic(t, pointType+"method Point.moved(dx: Int32): Point do\n    self.x = self.x + dx\n    return self\nend\n",
		"method Point.moved is not declared mut but assigns to self.x")
	assertChecked(t, pointType+"method Point.moved(dx: Int32): Point do\n    let mut result: Point = self\n    result.x = result.x + dx\n    return result\nend\n")
}

func TestMethodRulesAreEnforced(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+"method Point.translate() do\n    return\nend\nmethod Point.translate() do\n    return\nend\n",
		"Point already has a method named translate")
	assertRejectsAnyDiagnostic(t, pointType+"method Point.x(): Int32 do\n    return 0\nend\n",
		"Point already has a member named x")
	assertRejectsAnyDiagnostic(t, "method Int32.doubled(): Int32 do\n    return 0\nend\n",
		"method receiver must be a struct or union type; got Int32")
	assertRejectsAnyDiagnostic(t, "method Ptr<Point>.doubled(): Int32 do\n    return 0\nend\n"+
		"type Point is struct mut x: Int32, mut y: Int32, end\n",
		"method receiver must be a struct or union type; got Ptr<Point>")
	assertRejectsAnyDiagnostic(t, pointType+"let origin: Point = Point(x = 0, y = 0, )\nlet total: Int32 = origin.rotate()\n",
		"Point has no method named rotate")
}

func TestMethodSelfRecursionAndForwardCallsResolve(t *testing.T) {
	assertChecked(t, pointType+"method Point.countdown(value: Int32): Int32 do\n    return self.countdown(value - 1)\nend\n")
	assertChecked(t, pointType+
		"method Point.magnitude(): Int32 do\n    return self.length_squared()\nend\n"+
		"method Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n")
}

func TestPointerReceiversAreRejected(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+
		"method Ptr<Point>.is_origin(): Bool do\n    return true\nend\n",
		"method receiver must be a struct or union type; got Ptr<Point>")
	assertRejectsAnyDiagnostic(t, pointType+
		"method Ptr<mut Point>.translate(dx: Int32, dy: Int32) do\n    return\nend\n",
		"method receiver must be a struct or union type; got Ptr<mut Point>")
}

func TestFreeFunctionCollidesWithAMethodCName(t *testing.T) {
	assertRejectsAnyDiagnostic(t, pointType+
		"method Point.translate() do\n    return\nend\nfun Point_translate() do\n    return\nend\n",
		"free function Point_translate collides with method Point.translate")
	assertRejectsAnyDiagnostic(t,
		"type A_B is struct value: Int32, end\n"+
			"type A is struct value: Int32, end\n"+
			"method A_B.c() do\n    return\nend\n"+
			"method A.B_c() do\n    return\nend\n",
		"method A.B_c collides with method A_B.c")
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
		"method Point.length_squared(): Int32 do\n" +
		"    return (self.x * self.x) + (self.y * self.y)\n" +
		"end\n" +
		"method Point.is_origin(): Bool do\n" +
		"    return (self.x == 0) and (self.y == 0)\n" +
		"end\n" +
		"fun translate(target: Ptr<mut Point>, dx: Int32, dy: Int32) do\n" + "    target.x = target.x + dx\n" + "    target.y = target.y + dy\n" + "end\n" +
		"let mut here: Point = Point(x = 0, y = 0, )\n" +
		"translate(@here, 5, 5)\n" + "let total: Int32 = here.length_squared()\n" + "let flag: Bool = here.is_origin()\n" +
		"let reader: Ptr<Point> = @here\n" + "let copied: Bool = reader.is_origin()\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("method generation failed: %#v", result)
	}
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Point_length_squared(const hex_t_m3_app_Point *const hex_v_self) {",
		"static bool hex_f_m3_app_Point_is_origin(const hex_t_m3_app_Point *const hex_v_self) {",
		"static void hex_f_m3_app_translate(hex_t_m3_app_Point *const hex_v_target",
		"hex_f_m3_app_translate(&hex_v_here, 5, 5);",
		"hex_f_m3_app_Point_length_squared(&hex_v_here)",
		"hex_f_m3_app_Point_is_origin(&hex_v_here)",
		"hex_f_m3_app_Point_is_origin(hex_v_reader)",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
	if strings.Contains(generated, "const hex_t_m3_app_Point hex_v_self") {
		t.Fatalf("modules/app.c = %q, want no value-receiver definition", generated)
	}
}

const shapeType = "type Shape is union | Circle as radius: Int32 end | Rect as width: Int32, height: Int32 end end\n"

const shapeArea = "method Shape.area(): Int32 do\n    return match self is\n    | Shape.Circle then self.radius * self.radius\n    | Shape.Rect then self.width * self.height\n    end\nend\n"

// A nominal union owns methods like a struct: self narrows by variant inside
// match, and the method lowers to a pointer-receiver C function.
func TestUnionMethodNarrowsSelf(t *testing.T) {
	result := assertCompiles(t, shapeType+shapeArea+"let a: Int32 = Shape.Circle(radius = 2).area()\nlet b: Int32 = Shape.Rect(width = 3, height = 4).area()\nprint(a + b)\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Shape_area(const hex_t_m3_app_Shape *const hex_v_self) {",
		"hex_f_m3_app_Shape_area((const hex_t_m3_app_Shape[1]){",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
}

func TestGenericUnionMethodSpecializesPerArgument(t *testing.T) {
	result := assertCompiles(t, "type Maybe<T> is union | Some as value: T end | None end\n"+
		"method Maybe<T>.is_some(): Bool do\n    return match self is\n    | Maybe.Some then true\n    | Maybe.None then false\n    end\nend\n"+
		"let a: Maybe<Int32> = Maybe.Some(value = 1)\nlet b: Maybe<Bool> = Maybe.None()\nprint(a.is_some())\nprint(b.is_some())\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{"Maybe_Int32__is_some", "Maybe_Bool__is_some"} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want a specialization containing %q", generated, want)
		}
	}
}

func TestUnionMethodReceiverRules(t *testing.T) {
	assertRejects(t, "method Int32.f() do\nend\n", "method receiver must be a struct or union type; got Int32")
	assertRejects(t, "method Bool | Int32.f() do\nend\n", "method receiver must be a struct or union type; got Bool | Int32")
	assertRejects(t, shapeType+"method Shape.radius(): Int32 do\n    return 1\nend\n", "Shape already has a member named radius")
	assertCompiles(t, shapeType+"method Shape.is_circle(): Bool do\n    return match self is\n    | Shape.Circle then true\n    | Shape.Rect then false\n    end\nend\n"+
		"fun check(p: Ptr<Shape>): Bool do\n    return p.is_circle()\nend\nlet s: Shape = Shape.Circle(radius = 1)\nprint(check(@s))\n")
	imports := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\nmethod Lib.Shape.area(): Int32 do\n    return 1\nend\n",
		"lib.hex": shapeType + "export\n    Shape\nend\n",
	}
	if result := compiler.Compile(imports, "app.hex", compiler.Project{}); result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "cannot declare methods for imported type Lib.Shape") {
		t.Fatalf("a method on an imported union must be rejected; stderr = %v", result.Stderr)
	}
}

func TestExportedUnionMethodIsCallableFromImporter(t *testing.T) {
	exported := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\nlet s: Lib.Shape = Lib.Shape.Circle(radius = 3)\nprint(s.area())\n",
		"lib.hex": shapeType + shapeArea + "export\n    Shape,\n    Shape.area\nend\n",
	}
	result := compiler.Compile(exported, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile stderr = %v, want success", result.Stderr)
	}
	if !strings.Contains(result.Files["modules/app.h"], "Shape_area(const hex_t_m3_lib_Shape *)") {
		t.Fatalf("modules/app.h = %q, want the exported union method prototype", result.Files["modules/app.h"])
	}
}

const accountType = "type Account is struct mut balance: Int64 end\n"

// A method mut writes the caller's storage through a pointer receiver; a
// method without mut reads it through a const one.
func TestMutMethodWritesCallerStorage(t *testing.T) {
	result := assertCompiles(t, accountType+
		"method mut Account.deposit(n: Int64) do\n    self.balance = self.balance + n\nend\n"+
		"method Account.balance_of(): Int64 do\n    return self.balance\nend\n"+
		"let mut acct: Account = Account(balance = 0)\nacct.deposit(50)\nacct.deposit(50)\nprint(acct.balance_of())\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static void hex_f_m3_app_Account_deposit(hex_t_m3_app_Account *const hex_v_self, const int64_t hex_v_n) {",
		"hex_v_self->hex_m_balance = hex_wrap_add_int64_t(hex_v_self->hex_m_balance, hex_v_n);",
		"static int64_t hex_f_m3_app_Account_balance_of(const hex_t_m3_app_Account *const hex_v_self) {",
		"hex_f_m3_app_Account_deposit(&hex_v_acct, INT64_C(50));",
		"hex_f_m3_app_Account_balance_of(&hex_v_acct)",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
}

func TestSelfWriteRules(t *testing.T) {
	assertRejects(t, "type Fixed is struct balance: Int64 end\nmethod mut Fixed.set() do\n    self.balance = 1\nend\n", "cannot assign to read-only member self.balance")
	assertRejects(t, accountType+"method mut Account.reset(other: Account) do\n    self = other\nend\n", "cannot assign to self; self is a fixed binding")
}

// The mut declaration is the contract, resolved by method identity, so a
// readonly method may not call a later-declared mut method on self, and a
// cycle of mut methods compiles.
func TestReadonlyMethodContractIsDeclared(t *testing.T) {
	const want = "method Account.a is not declared mut but calls mut method Account.b on self"
	assertRejects(t, accountType+"method Account.a() do\n    self.b()\nend\nmethod mut Account.b() do\n    self.balance = 1\nend\n", want)
	assertCompiles(t, accountType+"method mut Account.a() do\n    self.b()\nend\nmethod mut Account.b() do\n    self.balance = 1\nend\n")
	assertCompiles(t, accountType+"method mut Account.ping(n: Int64) do\n    if n > 0 then\n        self.pong(n - 1)\n    end\nend\nmethod mut Account.pong(n: Int64) do\n    self.balance = n\n    self.ping(n)\nend\n")
	assertRejects(t, accountType+"method Account.ping(n: Int64) do\n    self.pong(n)\nend\nmethod mut Account.pong(n: Int64) do\n    self.balance = n\n    self.ping(n)\nend\n", "method Account.ping is not declared mut but calls mut method Account.pong on self")
}

// A writable address rooted at self can carry the write anywhere, so only a
// mut method takes one.
func TestSelfAddressRequiresMutMethod(t *testing.T) {
	poke := accountType + "fun poke(p: Ptr<mut Account>) do\n    p.balance = 1\nend\n"
	assertCompiles(t, poke+"method mut Account.go() do\n    poke(@self)\nend\n")
	assertRejects(t, poke+"method Account.go() do\n    poke(@self)\nend\n", "method Account.go is not declared mut but takes a writable address of self")
	assertCompiles(t, accountType+"fun poke(p: Ptr<mut Int64>) do\n    ^p = 1\nend\nmethod mut Account.go() do\n    poke(@self.balance)\nend\n")
	assertRejects(t, accountType+"fun poke(p: Ptr<mut Int64>) do\n    ^p = 1\nend\nmethod Account.go() do\n    poke(@self.balance)\nend\n", "takes a writable address of self.balance")
}

// Writing through an indirection below self changes separately owned storage,
// not the receiver's bytes; an inline List is the receiver's own.
func TestStorageBelowSelfAndIndirectionBoundary(t *testing.T) {
	const bag = "type Bag is struct items: List<Int32> end\n"
	assertCompiles(t, bag+"method Bag.add(x: Int32) do\n    self.items.push(x)\n    self.items[0] = x\nend\nlet h = Heap()\nlet fixed: Bag = Bag(items = List<Int32>(h))\nfixed.add(1)\n")
	assertCompiles(t, "type Holder is struct p: Ptr<mut Int32>, s: Slice<mut Int32> end\nmethod Holder.set() do\n    ^self.p = 1\n    self.s[0] = 2\nend\n")
	assertCompiles(t, bag+"method Bag.count(): Size do\n    return self.items.length()\nend\n")
	const cell = "type Cell is struct mut a: List<Int32, 2> end\n"
	assertRejects(t, cell+"method Cell.set() do\n    self.a[0] = 1\nend\n", "method Cell.set is not declared mut but assigns to self.a[...]")
	assertCompiles(t, cell+"method mut Cell.set() do\n    self.a[0] = 1\nend\n")
}

// A mut call is valid exactly when its receiver could take a writable
// address; the rejected kinds are the ones where the write would land on a
// copy or a read-only view.
func TestMutCallNeedsWritableReceiver(t *testing.T) {
	const dep = accountType + "method mut Account.dep() do\n    self.balance = 1\nend\n"
	const want = "mut method Account.dep requires a writable receiver, but "
	for _, testCase := range []struct{ name, body, reason string }{
		{"fixed binding", "let a: Account = Account(balance = 0)\na.dep()\n", "a is not writable"},
		{"parameter", "fun f(a: Account) do\n    a.dep()\nend\n", "a is not writable"},
		{"for binder", "let h = Heap()\nlet mut xs: List<Account> = List<Account>(h)\nxs.push(Account(balance = 0))\nfor x in xs do\n    x.dep()\nend\n", "x is not writable; write through the collection instead: for i, x in xs do xs[i].dep(...) end"},
		{"read-only pointer", "fun f(a: Ptr<Account>) do\n    a.dep()\nend\n", "a is not writable"},
		{"call result", "fun make(): Account do\n    return Account(balance = 0)\nend\nmake().dep()\n", "a temporary is not writable"},
		{"non-mut member", "type Wrap is struct inner: Account end\nlet mut w: Wrap = Wrap(inner = Account(balance = 0))\nw.inner.dep()\n", "w.inner is not writable"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, dep+testCase.body, want+testCase.reason)
		})
	}
	assertCompiles(t, dep+"let mut a: Account = Account(balance = 0)\na.dep()\n")
	assertCompiles(t, dep+"fun f(a: Ptr<mut Account>) do\n    a.dep()\nend\n")
}

// A readonly call on a temporary materializes it for the call.
func TestReadonlyMethodOnTemporary(t *testing.T) {
	result := assertCompiles(t, accountType+
		"method Account.balance_of(): Int64 do\n    return self.balance\nend\n"+
		"fun make(): Account do\n    return Account(balance = 3)\nend\n"+
		"print(make().balance_of())\n")
	generated := withoutLineDirectives(rootC(t, result))
	if !strings.Contains(generated, "(const hex_t_m3_app_Account[1]){ hex_f_m3_app_make() }") {
		t.Fatalf("modules/app.c = %q, want the temporary materialized before its address is passed", generated)
	}
}

const counterLib = "type Counter is struct mut n: Int32 end\n" +
	"method mut Counter.bump() do\n    self.n = self.n + 1\nend\n" +
	"method Counter.read(): Int32 do\n    return self.n\nend\n" +
	"fun make(): Counter do\n    return Counter(n = 0)\nend\n" +
	"export\n    Counter,\n    Counter.bump,\n    Counter.read,\n    make\nend\n"

// An imported mut method keeps its declared contract in the importer, and
// both artifacts declare its receiver with the same pointer spelling.
func TestImportedMethodKeepsDeclaredContract(t *testing.T) {
	const imports = "import\n    Lib from \"./lib\"\nend\n"
	rejected := compiler.Compile(map[string]string{"app.hex": imports + "let c = Lib.make()\nc.bump()\n", "lib.hex": counterLib}, "app.hex", compiler.Project{})
	if rejected.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(rejected.Stderr, "\n"), "mut method Counter.bump requires a writable receiver, but c is not writable") {
		t.Fatalf("a fixed binding must not take an imported mut call; stderr = %v", rejected.Stderr)
	}
	accepted := compiler.Compile(map[string]string{
		"app.hex": imports + "fun f(c: Ptr<mut Lib.Counter>) do\n    c.bump()\n    print(c.read())\nend\nlet mut c = Lib.make()\nf(@c)\n",
		"lib.hex": counterLib,
	}, "app.hex", compiler.Project{})
	if accepted.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile stderr = %v, want success", accepted.Stderr)
	}
	for file, wants := range map[string][]string{
		"modules/lib.h": {"Counter_bump(hex_t_m3_lib_Counter *)", "Counter_read(const hex_t_m3_lib_Counter *)"},
		"modules/app.h": {"Counter_bump(hex_t_m3_lib_Counter *)", "Counter_read(const hex_t_m3_lib_Counter *)"},
		"modules/lib.c": {"Counter_bump(hex_t_m3_lib_Counter *const hex_v_self)", "Counter_read(const hex_t_m3_lib_Counter *const hex_v_self)"},
	} {
		for _, want := range wants {
			if !strings.Contains(accepted.Files[file], want) {
				t.Fatalf("%s = %q, want %q", file, accepted.Files[file], want)
			}
		}
	}
}

// A generic method keeps its declared contract for every specialization, and
// a call that only resolves per specialization is verified against the
// declaration.
func TestGenericMethodsKeepDeclaredContract(t *testing.T) {
	const box = "type Box<T> is struct mut value: T end\n"
	assertCompiles(t, box+"method mut Box<T>.set(v: T) do\n    self.value = v\nend\nlet mut a: Box<Int32> = Box<Int32>(value = 1)\na.set(2)\nlet mut b: Box<Bool> = Box<Bool>(value = true)\nb.set(false)\n")
	assertRejects(t, box+"method mut Box<T>.set(v: T) do\n    self.value = v\nend\nlet a: Box<Int32> = Box<Int32>(value = 1)\na.set(2)\n", "mut method Box<Int32>.set requires a writable receiver")
	assertRejects(t, box+"method mut Box<T>.set(v: T) do\n    self.value = v\nend\nmethod Box<T>.touch(v: T) do\n    self.set(v)\nend\nlet a: Box<Int32> = Box<Int32>(value = 1)\na.touch(2)\n", "method Box<Int32>.touch is not declared mut but calls mut method Box<Int32>.set on self")
	assertRejects(t, accountType+"method mut Account.dep() do\n    self.balance = 1\nend\n"+
		"type Wrapper<T> is struct mut inner: T end\nmethod Wrapper<T>.poke() do\n    self.inner.dep()\nend\n"+
		"let w: Wrapper<Account> = Wrapper<Account>(inner = Account(balance = 0))\nw.poke()\n", "method Wrapper<Account>.poke is not declared mut but calls mut method Account.dep on self.inner")
}

// Calling a mut method takes the receiver's address implicitly, so a union
// narrowing on the receiver binding no longer holds; a readonly call keeps it.
func TestMutCallClearsReceiverNarrowing(t *testing.T) {
	const dep = accountType + "method mut Account.dep() do\n    self.balance = 1\nend\nmethod Account.get(): Int64 do\n    return self.balance\nend\n"
	assertRejects(t, dep+"fun f(): Int64 do\n    let mut a: Account | Int32 = Account(balance = 1)\n    if a is Account then\n        a.dep()\n        return a.balance\n    end\n    return 0\nend\n", "cannot access .balance on Int32 | Account")
	assertCompiles(t, dep+"fun f(): Int64 do\n    let mut a: Account | Int32 = Account(balance = 1)\n    if a is Account then\n        let x: Int64 = a.get()\n        return a.balance\n    end\n    return 0\nend\n")
}

// An element receiver is a view of its collection, so passing the collection
// in the same call is rejected when the call can change it.
func TestElementReceiverWithCollectionArgument(t *testing.T) {
	const prelude = accountType + "method mut Account.absorb(other: List<Account>) do\n    other.push(Account(balance = 0))\nend\nlet h = Heap()\nlet mut xs: List<Account> = List<Account>(h)\nxs.push(Account(balance = 1))\n"
	assertRejects(t, prelude+"xs[0].absorb(xs)\n", "call receives a view of xs and can also change xs")
	assertCompiles(t, accountType+"method mut Account.dep(n: Int64) do\n    self.balance = self.balance + n\nend\nlet h = Heap()\nlet mut accounts: List<Account> = List<Account>(h)\naccounts.push(Account(balance = 1))\nfor i, a in accounts do\n    accounts[i].dep(5)\nend\n")
}

// A result reached through self borrows the receiver: valid on an addressable
// receiver, rejected on a temporary that would die while the result is used.
func TestResultBorrowingSelfNeedsAPlaceReceiver(t *testing.T) {
	const fixed = "type Fixed is struct balance: Int64 end\nmethod Fixed.balance_ref(): Ptr<Int64> do\n    return @self.balance\nend\nfun make(): Fixed do\n    return Fixed(balance = 1)\nend\n"
	assertCompiles(t, fixed+"let f: Fixed = make()\nlet p = f.balance_ref()\nprint(^p)\n")
	assertRejects(t, fixed+"let p = make().balance_ref()\n", "method Fixed.balance_ref returns a view of its receiver, which here is a temporary")
	const bag = "type Bag is struct items: List<Int32> end\nmethod Bag.head(): Slice<Int32> do\n    return self.items.slice(0, 1)\nend\nfun make_bag(h: Heap): Bag do\n    return Bag(items = List<Int32>(h))\nend\nlet h = Heap()\n"
	assertCompiles(t, bag+"let bag: Bag = make_bag(h)\nbag.items.push(1)\nlet s = bag.head()\nprint(s[0])\n")
	assertRejects(t, bag+"let s = make_bag(h).head()\n", "method Bag.head returns a view of its receiver, which here is a temporary")
}

// Two receiver types may share a method name and keep independent contracts.
func TestSameMethodNameKeepsEachReceiversContract(t *testing.T) {
	const types = "type A is struct mut n: Int32 end\ntype B is struct mut n: Int32 end\nmethod mut A.touch() do\n    self.n = 1\nend\nmethod B.touch() do\n    print(self.n)\nend\n"
	assertCompiles(t, types+"let mut a: A = A(n = 0)\nlet b: B = B(n = 0)\na.touch()\nb.touch()\n")
	assertRejects(t, types+"let a: A = A(n = 0)\na.touch()\n", "mut method A.touch requires a writable receiver")
}

// A deferred mut call acts on the receiver place captured at registration,
// and an address derived from self cannot reach a spawned Task.
func TestDeferredMutCallAndSelfAddressInSpawn(t *testing.T) {
	result := assertCompiles(t, accountType+
		"method mut Account.dep() do\n    self.balance = 7\nend\n"+
		"fun demo(): Int64 do\n    let mut a: Account = Account(balance = 0)\n    defer a.dep()\n    return a.balance\nend\n"+
		"print(demo())\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"hex_t_m3_app_Account *const hex_defer_capture_1 = &hex_v_a;",
		"hex_f_m3_app_Account_dep(hex_defer_capture_1);",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
	assertRejects(t, accountType+"fun work(p: Ptr<mut Account>): Int32 do\n    return 1\nend\nmethod mut Account.go() do\n    let t = spawn work(@self)\nend\n", "an address derived from self cannot be passed to spawn")
}

func TestGeneratedFunctionDefinitionIsStaticAtFileScope(t *testing.T) {
	assertGeneratedC(t,
		"fun identity(value: Int32): Int32 do\n    return value\nend\n",
		"#include \"modules/app.h\"\n\nstatic int32_t hex_f_m3_app_identity(int32_t);\n\nstatic int32_t hex_f_m3_app_identity(const int32_t hex_v_value) {\n    return hex_v_value;\n}\n\nint main(void) {\n")
}

func TestGeneratedNoReturnFunctionIsVoid(t *testing.T) {
	assertGeneratedC(t,
		"fun reset(counter: Ptr<mut Int32>) do\n    ^counter = 0\nend\nlet mut count: Int32 = 1\nreset(@count)\n",
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
		"let callback: Fun<(Int32) : Int32> = identity\nlet mut selected: Fun<(Int32) : Int32> = identity\n"
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
			"let result: Int32 = apply(square, 5)\n",
		"static int32_t hex_f_m3_app_apply(int32_t (*const hex_v_callback)(int32_t), const int32_t hex_v_value) {\n"+
			"    return hex_v_callback(hex_v_value);\n}\n")
}

func TestGeneratedCallExpressionAndCallStatement(t *testing.T) {
	assertGeneratedC(t,
		"fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx\nend\nlet total: Int32 = adder(2, 3)\n",
		"    const int32_t hex_v_total = hex_f_m3_app_adder(2, 3);\n")
	assertGeneratedC(t,
		"fun reset(counter: Ptr<mut Int32>) do\n    ^counter = 0\nend\nlet mut count: Int32 = 1\nreset(@count)\n",
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
	result := assertCompiles(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nexport\n    square\nend\n")
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
		"let origin: Point = Point(x = 0, y = 0, )\n")
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
	result := compileSource("fun greet(text: String) do\nend\nfun wrap<T>(v: T): T do\n    greet(\"hi\")\n    return v\nend\nlet x: Int32 = wrap(1)\n")
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
			"let result: Int32 = apply(fun (value: Int32): Int32 do\n"+
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
		"let x: Int32 = apply2(fun (n: Int32): Int32 do\n" +
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
		"let cond: Bool = true\nif cond then\n    fun helper(): Int32 do\n        return 1\n    end\n    let x: Int32 = helper()\nend\n",
		"let mut i: Int32 = 0\nwhile i < 1 do\n    fun helper(): Int32 do\n        return 1\n    end\n    i = i + helper()\nend\n",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "named function declarations are only valid at module scope") {
			t.Fatalf("Compile(%q) = %#v, want the module-scope-only diagnostic", source, result.Stderr)
		}
	}
}

func TestMethodCallOnLiteralIsRejected(t *testing.T) {
	prelude := "fun demo(h: Heap) do\n"
	for _, call := range []string{
		"let text: String = \"carol\".copy(h)",
		"let text: String = r\"carol\".copy(h)",
		"let wide: Int64 = (3).to<Int64>()",
		"let wide: Float64 = (3.5).to<Float64>()",
		"let flag: Int32 = true.to<Int32>()",
		"let count: Size = [1, 2, 3].length()",
		"let wide: Int64 = (-3).to<Int64>()",
	} {
		t.Run(call, func(t *testing.T) {
			assertRejectsAnyDiagnostic(t, prelude+"    "+call+"\nend\n", "syntax.method-call-on-literal")
		})
	}
	assertRejectsAnyDiagnostic(t, "fun demo(h: Heap) do\n    let text: String = \"carol\".copy(h)\nend\n", "a method cannot be called on a literal; bind it with let first")
}

func TestMethodCallOnNonLiteralReceiversStaysValid(t *testing.T) {
	assertCompiles(t, "fun keep(value: Int32): Bool do\n    return value > 1\nend\n"+
		"fun double(value: Int32): Int32 do\n    return value * 2\nend\n"+
		"fun make(h: Heap): String do\n    let name: String = \"carol\"\n    return name.copy(h)\nend\n"+
		"fun demo(h: Heap) do\n"+
		"    let name: String = \"carol\"\n"+
		"    let copy: String = name.copy(h)\n"+
		"    let a: Int32 = 1\n"+
		"    let wide: Int64 = (a + 1).to<Int64>()\n"+
		"    let again: String = make(h).copy(h)\n"+
		"    let xs: List<Int32, 3> = [1, 2, 3]\n"+
		"    let kept = xs.filter(keep).map(double).to_list(h)\n"+
		"end\n")
}
