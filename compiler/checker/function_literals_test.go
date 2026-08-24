package checker

// Anonymous function literals and the direct-inferred-literal
// declaration-sugar boundary. Named local function declarations no longer
// exist as syntax; only an ordinary Fun<...> value produced by a literal
// remains legal inside a function body.

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

func TestLocalLiteralBindingCannotBeUsedBeforeItsDeclaration(t *testing.T) {
	// Local bindings stay ordinary source-ordered runtime data even though
	// module-level function visibility became order-independent: an earlier
	// statement in the same function body cannot forward-reference a local
	// binding a later statement declares, unlike a module-level function.
	requireDiagnostic(t,
		"fun outer(): Int32 do\n"+
			"    x: Int32 := helper()\n"+
			"    helper: Fun<(): Int32> := fun(): Int32 do\n"+
			"        return 1\n"+
			"    end\n"+
			"    return x\n"+
			"end\n",
		"unknown function helper; functions must be declared before use")
}

func TestFunctionLiteralInsideAFunctionRejectsParameterCapture(t *testing.T) {
	// Declaration-sugar literal: a local binding directly initialized by a
	// literal is ordinary runtime data, not a named declaration, and still
	// closes over nothing from its enclosing function.
	requireDiagnostic(t,
		"fun calculate(factor: Int32): Int32 do\n"+
			"    operation := fun (value: Int32): Int32 do\n"+
			"        return value * factor\n"+
			"    end\n"+
			"    return operation(2)\n"+
			"end\n",
		"unknown variable factor")
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

func TestFunctionLiteralAndModuleDataShareTheClosedFunctionRule(t *testing.T) {
	requireDiagnostic(t,
		"count: Int32 := 5\n"+
			"fun outer(): Int32 do\n"+
			"    inner := fun (): Int32 do\n"+
			"        return count\n"+
			"    end\n"+
			"    return inner()\n"+
			"end\n",
		"function function literal cannot access module data binding count; pass it as a parameter")
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

func TestNestedGenericLiteralUsesEnclosingTypeParameter(t *testing.T) {
	// A literal with an exact contextual Fun<...> type may use the
	// enclosing generic function's own type parameter, exactly like any
	// other type name visible at that point in the body.
	requireAccepted(t,
		"fun apply<T>(value: T): T do\n"+
			"    holder: Fun<(T) : T> := fun (input: T): T do\n"+
			"        return input\n"+
			"    end\n"+
			"    return holder(value)\n"+
			"end\n"+
			"x: Int32 := apply<Int32>(5)\n")
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
