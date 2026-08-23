package checker

// Anonymous function literals, local named function declarations, and the
// direct-inferred-literal declaration-sugar boundary between them.

import "testing"

func TestBareLiteralExpressionChecksAsFunctionLiteralExpression(t *testing.T) {
	checked := requireAccepted(t,
		"fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n"+
			"    return callback(value)\n"+
			"end\n"+
			"result: Int32 := apply(fun (value: Int32): Int32 do\n"+
			"    return value * value\n"+
			"end, 5)\n")
	declaration, ok := checked.Statements[1].(Declaration)
	if !ok {
		t.Fatalf("statement 1 = %T, want Declaration", checked.Statements[1])
	}
	if declaration.Source.Node.Kind != CallExpression {
		t.Fatalf("initializer kind = %v, want CallExpression", declaration.Source.Node.Kind)
	}
	if len(declaration.Source.Node.Arguments) != 2 {
		t.Fatalf("argument count = %d, want 2", len(declaration.Source.Node.Arguments))
	}
	argument := declaration.Source.Node.Arguments[0]
	if argument.Node.Kind != FunctionLiteralExpression {
		t.Fatalf("argument 0 kind = %v, want FunctionLiteralExpression", argument.Node.Kind)
	}
	if argument.Node.Function == nil || len(argument.Node.Function.Parameters) != 1 {
		t.Fatalf("argument 0 Function = %#v, want one parameter", argument.Node.Function)
	}
}

func TestTypedLiteralBindingIsRuntimeData(t *testing.T) {
	checked := requireAccepted(t,
		"op: Fun<(Int32) : Int32> := fun (value: Int32): Int32 do\n"+
			"    return value\n"+
			"end\n")
	declaration, ok := checked.Statements[0].(Declaration)
	if !ok {
		t.Fatalf("statement 0 = %T, want Declaration", checked.Statements[0])
	}
	if declaration.Source.Node.Kind != FunctionLiteralExpression {
		t.Fatalf("initializer kind = %v, want FunctionLiteralExpression", declaration.Source.Node.Kind)
	}
	if declaration.Source.Node.Function == nil {
		t.Fatal("checked FunctionLiteralExpression carries no Function")
	}
}

func TestDirectInferredLiteralIsDeclarationSugar(t *testing.T) {
	checked := requireAccepted(t,
		"factorial := fun (value: Int32): Int32 do\n"+
			"    if value == 0 then\n"+
			"        return 1\n"+
			"    end\n"+
			"    return value * factorial(value - 1)\n"+
			"end\n"+
			"x: Int32 := factorial(5)\n")
	declaration, ok := checked.Statements[0].(FunctionDeclaration)
	if !ok {
		t.Fatalf("statement 0 = %T, want FunctionDeclaration (declaration sugar)", checked.Statements[0])
	}
	if declaration.Name != "factorial" {
		t.Fatalf("name = %q, want factorial", declaration.Name)
	}
	if declaration.Exported {
		t.Fatal("declaration sugar must not be exported: export prefixes only the named function form")
	}
}

func TestGroupingOnlyParensPreserveDeclarationSugar(t *testing.T) {
	checked := requireAccepted(t,
		"square := ((fun (value: Int32): Int32 do\n"+
			"    return value * value\n"+
			"end))\n")
	if _, ok := checked.Statements[0].(FunctionDeclaration); !ok {
		t.Fatalf("statement 0 = %T, want FunctionDeclaration", checked.Statements[0])
	}
}

func TestCallSuffixOnInitializerIsRuntimeData(t *testing.T) {
	// A call suffix produces a CallExpression, which does not qualify as a
	// direct literal initializer: x holds the returned value, not a
	// function, so it must not become declaration sugar.
	checked := requireAccepted(t,
		"x := fun (value: Int32): Int32 do\n"+
			"    return value\n"+
			"end(5)\n")
	if _, ok := checked.Statements[0].(FunctionDeclaration); ok {
		t.Fatal("a call-suffixed literal initializer must not become declaration sugar")
	}
	if _, ok := checked.Statements[0].(Declaration); !ok {
		t.Fatalf("statement 0 = %T, want Declaration", checked.Statements[0])
	}
}

func TestMutableLiteralBindingIsNotSelfRecursive(t *testing.T) {
	// A mutable receiving binding is runtime-replaceable and cannot be a
	// stable self identity; referring to it from the literal remains an
	// invalid capture, so it simply does not exist yet at that point.
	requireDiagnostic(t,
		"mut op: Fun<(Int32) : Int32> := fun (value: Int32): Int32 do\n"+
			"    return op(value)\n"+
			"end\n",
		"unknown function op; functions must be declared before use")
}

func TestLocalNamedFunctionChecksAsLocalFunctionDeclaration(t *testing.T) {
	checked := requireAccepted(t,
		"fun outer(): Int32 do\n"+
			"    fun inner(value: Int32): Int32 do\n"+
			"        return value + 1\n"+
			"    end\n"+
			"    return inner(1)\n"+
			"end\n")
	outer, ok := checked.Statements[0].(FunctionDeclaration)
	if !ok {
		t.Fatalf("statement 0 = %T, want FunctionDeclaration", checked.Statements[0])
	}
	local, ok := outer.Body[0].(LocalFunctionDeclaration)
	if !ok {
		t.Fatalf("outer body[0] = %T, want LocalFunctionDeclaration", outer.Body[0])
	}
	if local.Name != "inner" || len(local.Parameters) != 1 {
		t.Fatalf("local = %#v, want inner with one parameter", local)
	}
}

func TestLocalFunctionSelfRecursion(t *testing.T) {
	requireAccepted(t,
		"fun outer(): Int32 do\n"+
			"    fun fact(value: Int32): Int32 do\n"+
			"        if value == 0 then\n"+
			"            return 1\n"+
			"        end\n"+
			"        return value * fact(value - 1)\n"+
			"    end\n"+
			"    return fact(5)\n"+
			"end\n")
}

func TestLocalFunctionAndLiteralRejectEnclosingParameterCapture(t *testing.T) {
	for _, source := range []string{
		// Declaration-sugar literal.
		"fun calculate(factor: Int32): Int32 do\n" +
			"    operation := fun (value: Int32): Int32 do\n" +
			"        return value * factor\n" +
			"    end\n" +
			"    return operation(2)\n" +
			"end\n",
		// Local named function.
		"fun calculate(factor: Int32): Int32 do\n" +
			"    fun operation(value: Int32): Int32 do\n" +
			"        return value * factor\n" +
			"    end\n" +
			"    return operation(2)\n" +
			"end\n",
	} {
		requireDiagnostic(t, source, "unknown variable factor")
	}
}

func TestLiteralInsideMethodRejectsSelf(t *testing.T) {
	requireDiagnostic(t,
		"type T = { n: Int32, }\n"+
			"impl T.method(): Int32 do\n"+
			"    literal := fun (): Int32 do\n"+
			"        return self.n\n"+
			"    end\n"+
			"    return literal()\n"+
			"end\n",
		"self is not bound outside an impl body")
}

func TestLocalFunctionSourceOrderIsAuthoritative(t *testing.T) {
	// A local function may call itself and earlier visible local functions,
	// but a call to a later local function in the same block is rejected.
	requireDiagnostic(t,
		"fun outer(): Int32 do\n"+
			"    fun a(): Int32 do\n"+
			"        return b()\n"+
			"    end\n"+
			"    fun b(): Int32 do\n"+
			"        return 1\n"+
			"    end\n"+
			"    return a()\n"+
			"end\n",
		"unknown function b; functions must be declared before use")
}

func TestLocalFunctionVisibleToLaterLocalFunctionAndLiteral(t *testing.T) {
	for _, source := range []string{
		"fun outer(): Int32 do\n" +
			"    fun helper(): Int32 do\n" +
			"        return 1\n" +
			"    end\n" +
			"    fun caller(): Int32 do\n" +
			"        return helper()\n" +
			"    end\n" +
			"    return caller()\n" +
			"end\n",
		"fun outer(): Int32 do\n" +
			"    fun helper(): Int32 do\n" +
			"        return 1\n" +
			"    end\n" +
			"    inner := fun (): Int32 do\n" +
			"        return helper()\n" +
			"    end\n" +
			"    return inner()\n" +
			"end\n",
	} {
		requireAccepted(t, source)
	}
}

func TestNestedLiteralRejectsEnclosingLocalFunctionParameter(t *testing.T) {
	requireDiagnostic(t,
		"fun outer() do\n"+
			"    fun middle(value: Int32) do\n"+
			"        inner := fun (): Int32 do\n"+
			"            return value\n"+
			"        end\n"+
			"        return\n"+
			"    end\n"+
			"end\n",
		"unknown variable value")
}

func TestLocalFunctionHiddenOutsideItsBlock(t *testing.T) {
	requireDiagnostic(t,
		"fun outer(cond: Bool): Int32 do\n"+
			"    if cond then\n"+
			"        fun helper(): Int32 do\n"+
			"            return 1\n"+
			"        end\n"+
			"        return helper()\n"+
			"    end\n"+
			"    return helper()\n"+
			"end\n",
		"unknown function helper; functions must be declared before use")
}

func TestDuplicateLocalFunctionNameInSameBlockRejected(t *testing.T) {
	requireDiagnostic(t,
		"fun outer(): Int32 do\n"+
			"    fun helper(): Int32 do\n"+
			"        return 1\n"+
			"    end\n"+
			"    fun helper(): Int32 do\n"+
			"        return 2\n"+
			"    end\n"+
			"    return helper()\n"+
			"end\n",
		"helper is already declared")
}

func TestLocalFunctionAndModuleDataShareTheClosedFunctionRule(t *testing.T) {
	requireDiagnostic(t,
		"count: Int32 := 5\n"+
			"fun outer(): Int32 do\n"+
			"    fun inner(): Int32 do\n"+
			"        return count\n"+
			"    end\n"+
			"    return inner()\n"+
			"end\n",
		"function inner cannot access module data binding count; pass it as a parameter")
}

func TestGenericLocalFunctionExplicitAndInferredCalls(t *testing.T) {
	for _, source := range []string{
		"fun outer(): Int32 do\n" +
			"    fun identity<T>(value: T): T do\n" +
			"        return value\n" +
			"    end\n" +
			"    return identity<Int32>(5)\n" +
			"end\n",
		"fun outer(): Int32 do\n" +
			"    fun identity<T>(value: T): T do\n" +
			"        return value\n" +
			"    end\n" +
			"    return identity(5)\n" +
			"end\n",
	} {
		requireAccepted(t, source)
	}
}

func TestGenericAnonymousLiteralContextualSpecialization(t *testing.T) {
	checked := requireAccepted(t,
		"callback: Fun<(Int32) : Int32> := fun<T>(value: T): T do\n"+
			"    return value\n"+
			"end\n")
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != FunctionReferenceExpression {
		t.Fatalf("initializer kind = %v, want FunctionReferenceExpression", declaration.Source.Node.Kind)
	}
}

func TestGenericAnonymousLiteralWithoutExpectedTypeRejected(t *testing.T) {
	requireDiagnostic(t,
		"identity := fun<T>(value: T): T do\n"+
			"    return value\n"+
			"end\n"+
			"alias := identity\n",
		"cannot infer generic parameter for identity")
}

func TestGenericLiteralDirectInvocationInfersFromArguments(t *testing.T) {
	checked := requireAccepted(t,
		"y: Int32 := (fun<T>(value: T): T do\n"+
			"    return value\n"+
			"end)(21)\n")
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != CallExpression {
		t.Fatalf("initializer kind = %v, want CallExpression", declaration.Source.Node.Kind)
	}
	if declaration.Source.Node.Operand == nil || declaration.Source.Node.Operand.Kind != FunctionReferenceExpression {
		t.Fatalf("call operand = %#v, want FunctionReferenceExpression", declaration.Source.Node.Operand)
	}
}

func TestTwoLocalGenericsWithSameNameInDisjointScopesAreIndependent(t *testing.T) {
	checked := requireAccepted(t,
		"fun first(): Int32 do\n"+
			"    fun identity<T>(value: T): T do\n"+
			"        return value\n"+
			"    end\n"+
			"    return identity(1)\n"+
			"end\n"+
			"fun second(): Bool do\n"+
			"    fun identity<T>(value: T): T do\n"+
			"        return value\n"+
			"    end\n"+
			"    return identity(true)\n"+
			"end\n")
	first := checked.Statements[0].(FunctionDeclaration)
	second := checked.Statements[1].(FunctionDeclaration)
	firstCall := first.Body[0].(ReturnStatement).Value.Node
	secondCall := second.Body[0].(ReturnStatement).Value.Node
	if firstCall.Operand.Name == secondCall.Operand.Name {
		t.Fatalf("both local generics specialized to the same C name %q; want distinct identities", firstCall.Operand.Name)
	}
}

func TestNestedGenericLiteralUsesEnclosingTypeParameter(t *testing.T) {
	requireAccepted(t,
		"fun apply<T>(value: T): T do\n"+
			"    operation := fun<U>(input: U): U do\n"+
			"        return input\n"+
			"    end\n"+
			"    holder: Fun<(T) : T> := fun (input: T): T do\n"+
			"        return input\n"+
			"    end\n"+
			"    ignored: Int32 := operation(1)\n"+
			"    return holder(value)\n"+
			"end\n"+
			"x: Int32 := apply<Int32>(5)\n")
}

func TestNestedGenericRejectsEnclosingTypeParameterName(t *testing.T) {
	requireDiagnostic(t,
		"fun outer<T>(value: T): T do\n"+
			"    fun inner<T>(value: T): T do\n"+
			"        return value\n"+
			"    end\n"+
			"    return inner(value)\n"+
			"end\n"+
			"x: Int32 := outer<Int32>(5)\n",
		"generic parameter T is already declared by an enclosing function")
}

func TestGenericLiteralRejectsEnclosingTypeParameterName(t *testing.T) {
	requireDiagnostic(t,
		"fun outer<T>(value: T): T do\n"+
			"    inner: Fun<(Int32) : Int32> := fun<T>(value: T): T do\n"+
			"        return value\n"+
			"    end\n"+
			"    return value\n"+
			"end\n"+
			"x: Int32 := outer<Int32>(5)\n",
		"generic parameter T is already declared by an enclosing function")
}

func TestDirectLiteralInvocation(t *testing.T) {
	// A result-producing literal can be invoked directly only inside an
	// expression; the call suffix routes through the indirect-call path
	// this stage adds, since the callee is neither a name nor a method
	// selection.
	checked := requireAccepted(t,
		"y: Int32 := (fun (value: Int32): Int32 do\n"+
			"    return value * 2\n"+
			"end)(21)\n")
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != CallExpression {
		t.Fatalf("initializer kind = %v, want CallExpression", declaration.Source.Node.Kind)
	}
	if declaration.Source.Node.Operand == nil || declaration.Source.Node.Operand.Kind != FunctionLiteralExpression {
		t.Fatalf("call operand = %#v, want FunctionLiteralExpression", declaration.Source.Node.Operand)
	}
}

func TestCallingANonFunctionExpressionIsStillRejected(t *testing.T) {
	// The indirect-call path must not weaken the existing rejection of a
	// callee that resolves to no Fun<...> type at all.
	requireDiagnostic(t,
		"x: Int32 := (5)(3)\n",
		"a call's callee must be a function name or a method selection")
}

func TestLocalGenericSameArgumentRecursionAllowed(t *testing.T) {
	requireAccepted(t,
		"fun outer(): Int32 do\n"+
			"    fun fact<T>(value: Int32): Int32 do\n"+
			"        if value == 0 then\n"+
			"            return 1\n"+
			"        end\n"+
			"        return value * fact<Int32>(value - 1)\n"+
			"    end\n"+
			"    return fact<Int32>(5)\n"+
			"end\n")
}

func TestLocalGenericArgumentChangingRecursionRejected(t *testing.T) {
	requireDiagnostic(t,
		"fun outer(): Int32 do\n"+
			"    fun bad<T>(value: T): Int32 do\n"+
			"        return bad(1.5)\n"+
			"    end\n"+
			"    return bad(1)\n"+
			"end\n",
		"recursive specialization changes generic arguments")
}

func TestLocalGenericRejectsEnclosingParameterCapture(t *testing.T) {
	requireDiagnostic(t,
		"fun calculate(factor: Int32): Int32 do\n"+
			"    fun scale<T>(value: T): Int32 do\n"+
			"        return factor\n"+
			"    end\n"+
			"    return scale<Int32>(2)\n"+
			"end\n",
		"unknown variable factor")
}
