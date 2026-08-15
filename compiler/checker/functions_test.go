package checker

// Function declarations, Fun<...> types, calls, returns, and closed function
// scopes. Spec 0008.

import (
	"testing"

	compilerTypes "hexal/compiler/types"
)

func checkSource(t *testing.T, source string) (Program, error) {
	t.Helper()
	return Check(parseProgram(t, source))
}

func requireDiagnostic(t *testing.T, source, want string) {
	t.Helper()
	_, err := checkSource(t, source)
	if err == nil {
		t.Fatalf("Check accepted %q, want %q", source, want)
	}
	diagnostics, ok := err.(compilerTypes.Diagnostics)
	if !ok {
		t.Fatalf("Check error type = %T, want Diagnostics", err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Message == want {
			return
		}
	}
	t.Fatalf("Check diagnostics = %v, want message %q", diagnostics, want)
}

func requireAccepted(t *testing.T, source string) Program {
	t.Helper()
	checked, err := checkSource(t, source)
	if err != nil {
		t.Fatalf("Check rejected %q: %v", source, err)
	}
	return checked
}

// 1. Checked IR additions.

func TestFunctionDeclarationProducesCheckedIR(t *testing.T) {
	checked := requireAccepted(t, "fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx + dy\nend\n")
	if len(checked.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(checked.Statements))
	}
	declaration, ok := checked.Statements[0].(FunctionDeclaration)
	if !ok {
		t.Fatalf("statement = %T, want FunctionDeclaration", checked.Statements[0])
	}
	if declaration.Name != "adder" || len(declaration.Parameters) != 2 {
		t.Fatalf("declaration = %#v, want adder with two parameters", declaration)
	}
	if declaration.Parameters[0].Name != "dx" || !compilerTypes.Equal(declaration.Parameters[0].Type, compilerTypes.Int32) {
		t.Fatalf("parameter 0 = %#v, want dx: Int32", declaration.Parameters[0])
	}
	if declaration.Result == nil || !compilerTypes.Equal(*declaration.Result, compilerTypes.Int32) {
		t.Fatalf("result = %#v, want Int32", declaration.Result)
	}
	if declaration.Type.Signature == nil || declaration.Type.Name != "Fun<(Int32, Int32) : Int32>" {
		t.Fatalf("declaration type = %#v, want a Fun type", declaration.Type)
	}
	if len(declaration.Body) != 1 {
		t.Fatalf("body = %d statements, want 1", len(declaration.Body))
	}
	returnStatement, ok := declaration.Body[0].(ReturnStatement)
	if !ok {
		t.Fatalf("body statement = %T, want ReturnStatement", declaration.Body[0])
	}
	if returnStatement.Value == nil || returnStatement.Value.Node.Kind != BinaryOperationExpression {
		t.Fatalf("return value = %#v, want a checked binary operation", returnStatement.Value)
	}
}

func TestCheckedCallAndFunctionReferenceNodes(t *testing.T) {
	checked := requireAccepted(t, "fun identity(value: Int32): Int32 do\n    return value\nend\ncallback: Fun<(Int32) : Int32> = identity\ntotal: Int32 = identity(7)\n")
	reference := checked.Statements[1].(Declaration)
	if reference.Source.Node.Kind != FunctionReferenceExpression || reference.Source.Node.Name != "identity" {
		t.Fatalf("callback source = %#v, want a function reference", reference.Source.Node)
	}
	call := checked.Statements[2].(Declaration)
	if call.Source.Node.Kind != CallExpression {
		t.Fatalf("total source = %#v, want a checked call", call.Source.Node)
	}
	if call.Source.Node.Operand == nil || call.Source.Node.Operand.Kind != FunctionReferenceExpression {
		t.Fatalf("call callee = %#v, want a function reference", call.Source.Node.Operand)
	}
	if len(call.Source.Node.Arguments) != 1 || !compilerTypes.Equal(call.Source.Node.ResultType, compilerTypes.Int32) {
		t.Fatalf("call node = %#v, want one argument and an Int32 result", call.Source.Node)
	}
}

func TestNoReturnCallStatementIsChecked(t *testing.T) {
	checked := requireAccepted(t, "fun reset(counter: MutPtr<Int32>) do\n    counter.value = 0\nend\nmut count: Int32 = 1\nreset(ref count)\n")
	statement, ok := checked.Statements[2].(CallStatement)
	if !ok {
		t.Fatalf("statement = %T, want CallStatement", checked.Statements[2])
	}
	if statement.Call.Node.Kind != CallExpression {
		t.Fatalf("call statement = %#v, want a checked call", statement.Call.Node)
	}
}

// 2. Fun<...> type resolution.

func TestFunTypesAreCanonicalAndInterned(t *testing.T) {
	checked := requireAccepted(t, "type Handler = Fun<(Int32) : Int32>\ntype Same = Fun<(Int32) : Int32>\ntype NoReturn = Fun<(Int32)>\n")
	if !compilerTypes.Equal(checked.TypeDeclarations[0].Type, checked.TypeDeclarations[1].Type) {
		t.Fatal("identical Fun types were not interned to one identity")
	}
	if compilerTypes.Equal(checked.TypeDeclarations[0].Type, checked.TypeDeclarations[2].Type) {
		t.Fatal("returning and no-return Fun types shared one identity")
	}
	if checked.TypeDeclarations[2].Type.Signature.Result != nil {
		t.Fatal("Fun<(Int32)> resolved with a result type")
	}
}

// 3. Single-pass source-order declaration order.

func TestSelfRecursionResolves(t *testing.T) {
	requireAccepted(t, "fun factorial(value: Int32): Int32 do\n    return value * factorial(value - 1)\nend\n")
}

func TestEarlierFunctionIsVisible(t *testing.T) {
	requireAccepted(t, "fun double(value: Int32): Int32 do\n    return value + value\nend\nfun quadruple(value: Int32): Int32 do\n    return double(double(value))\nend\n")
}

func TestLaterFunctionIsNotVisible(t *testing.T) {
	requireDiagnostic(t,
		"fun is_even(value: Int32): Int32 do\n    return is_odd(value - 1)\nend\nfun is_odd(value: Int32): Int32 do\n    return value\nend\n",
		"unknown function is_odd; functions must be declared before use")
}

// 4. Function scope.

func TestFunctionCannotReadModuleDataBinding(t *testing.T) {
	requireDiagnostic(t,
		"mut count: Int32 = 0\nfun read_count(): Int32 do\n    return count\nend\n",
		"function read_count cannot access module data binding count; pass it as a parameter")
}

func TestFunctionCannotReadModuleFunBinding(t *testing.T) {
	requireDiagnostic(t,
		"fun identity(value: Int32): Int32 do\n    return value\nend\nhandler: Fun<(Int32) : Int32> = identity\nfun call_handler(value: Int32): Int32 do\n    return handler(value)\nend\n",
		"function call_handler cannot access module data binding handler; pass it as a parameter")
}

func TestParametersAreFixedBindings(t *testing.T) {
	requireDiagnostic(t,
		"fun small(value: Int32): Int32 do\n    value = 1\n    return value\nend\n",
		"cannot assign to parameter value; parameters are fixed bindings")
}

func TestLocalShadowsModuleValueButNotType(t *testing.T) {
	requireAccepted(t, "count: Int32 = 3\nfun scoped(): Int32 do\n    count: Int32 = 1\n    return count\nend\n")
	requireDiagnostic(t,
		"type Counter = Int32\nfun scoped(): Int32 do\n    Counter: Int32 = 1\n    return Counter\nend\n",
		"value Counter is already declared as a type")
}

func TestDuplicateLocalDeclarationRejected(t *testing.T) {
	requireDiagnostic(t,
		"fun scoped(): Int32 do\n    total: Int32 = 1\n    total: Int32 = 2\n    return total\nend\n",
		"variable total is already declared; reassignment must omit the type annotation")
}

func TestLocalsAndMutableLocalsAreAllowed(t *testing.T) {
	requireAccepted(t, "fun scoped(seed: Int32): Int32 do\n    mut total: Int32 = seed\n    total = total + 1\n    return total\nend\n")
}

// 5. Calls.

func TestCallArityIsChecked(t *testing.T) {
	requireDiagnostic(t,
		"fun adder(dx: Int32, dy: Int32): Int32 do\n    return dx + dy\nend\ntotal: Int32 = adder(1, 2, 3)\n",
		"adder expects 2 arguments, got 3")
}

func TestArgumentsUseParameterExpectedTypes(t *testing.T) {
	requireAccepted(t, "fun small(value: UInt8): UInt8 do\n    return value\nend\nok: UInt8 = small(200)\n")
	requireDiagnostic(t,
		"fun small(value: UInt8): UInt8 do\n    return value\nend\nbad: UInt8 = small(300)\n",
		"given value is outside the UInt8 range")
}

func TestArgumentsUsePointerWeakening(t *testing.T) {
	requireAccepted(t, "fun peek(source: Ptr<Int32>): Int32 do\n    return source.value\nend\nmut score: Int32 = 1\ntotal: Int32 = peek(ref score)\n")
}

func TestNoReturnCallCannotInitializeStorage(t *testing.T) {
	requireDiagnostic(t,
		"fun reset(counter: MutPtr<Int32>) do\n    counter.value = 0\nend\nmut count: Int32 = 1\nresult: Int32 = reset(ref count)\n",
		"reset produces no value")
}

func TestFunBindingIsCallableInsideAFunction(t *testing.T) {
	requireAccepted(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n    return callback(value)\nend\nresult: Int32 = apply(square, 5)\n")
}

// 6. Returns.

func TestBareReturnRequiresANoReturnFunction(t *testing.T) {
	requireAccepted(t, "fun log_value(value: Int32) do\n    return\nend\n")
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return\nend\n",
		"return requires a value; adder declares Int32")
}

func TestReturningBodyMustEndWithAReturn(t *testing.T) {
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    total: Int32 = dx\nend\n",
		"returning adder may fall through without returning Int32")
}

func TestReturnValueMustBeAssignable(t *testing.T) {
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return true\nend\n",
		"adder returns Int32; got Bool")
}

// 7. Fun<...> position whitelist.

func TestSupportedFunPositionsAreAccepted(t *testing.T) {
	requireAccepted(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nmodule_level: Fun<(Int32) : Int32> = identity\nfun higher(callback: Fun<(Fun<(Int32) : Int32>)>, value: Int32): Int32 do\n    local: Fun<(Int32) : Int32> = identity\n    return local(value)\nend\n")
}

func TestReturningAFunTypeIsUnsupported(t *testing.T) {
	requireDiagnostic(t,
		"fun maker(): Fun<(Int32) : Int32> do\n    return maker\nend\n",
		"returning Fun<(Int32) : Int32> is not supported")
	requireDiagnostic(t,
		"type Bad = Fun<() : Fun<(Int32) : Int32>>\n",
		"returning Fun<(Int32) : Int32> is not supported")
}

func TestFunObjectMembersAreUnsupported(t *testing.T) {
	requireDiagnostic(t,
		"type Holder = { callback: Fun<(Int32) : Int32>, }\n",
		"Fun<…> object members are not supported")
}

func TestPointersToFunAreUnsupported(t *testing.T) {
	requireDiagnostic(t,
		"type Bad = Ptr<Fun<(Int32) : Int32>>\n",
		"Ptr<Fun<(Int32) : Int32>> is not supported")
	requireDiagnostic(t,
		"type Bad = MutPtr<Fun<(Int32) : Int32>>\n",
		"MutPtr<Fun<(Int32) : Int32>> is not supported")
}

func TestRefOfAFunctionOrFunBindingIsUnsupported(t *testing.T) {
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nbad: Fun<(Int32) : Int32> = ref adder\n",
		"function declarations are not addressable; use adder as a Fun value")
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nhandler: Fun<(Int32) : Int32> = adder\nbad: Ptr<Int32> = ref handler\n",
		"Fun<(Int32) : Int32> bindings are not addressable")
}

// 8. Function names are not storage.

func TestFunctionNameIsAssignableToAnIdenticalFunType(t *testing.T) {
	requireAccepted(t, "fun adder(dx: Int32): Int32 do\n    return dx\nend\nmut handler: Fun<(Int32) : Int32> = adder\nhandler = adder\n")
}

func TestFunTypeMismatchNamesBothSignatures(t *testing.T) {
	requireDiagnostic(t,
		"fun adder(dx: UInt32): UInt32 do\n    return dx\nend\nhandler: Fun<(Int32) : Int32> = adder\n",
		"handler requires Fun<(Int32) : Int32>, got Fun<(UInt32) : UInt32>")
}

func TestFunctionNameIsNotAssignable(t *testing.T) {
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nadder = adder\n",
		"cannot assign to function adder")
}

func TestFunctionNameCollidesWithVisibleNames(t *testing.T) {
	requireDiagnostic(t,
		"type Adder = Int32\nfun Adder(dx: Int32): Int32 do\n    return dx\nend\n",
		"value Adder is already declared as a type")
	requireDiagnostic(t,
		"fun adder(dx: Int32): Int32 do\n    return dx\nend\nfun adder(dx: Int32): Int32 do\n    return dx\nend\n",
		"adder is already declared")
}

// 9. Methods: impl targets, self, and receiver adaptation.

// point is the shared object every method test implements against.
const point = "type Point = { mut x: Int32, mut y: Int32, }\n"

func TestSelfOutsideAnImplBodyIsRejected(t *testing.T) {
	requireDiagnostic(t,
		"fun broken(): Int32 do\n    return self\nend\n",
		"self is not bound outside an impl body")
}

func TestMethodDeclarationProducesCheckedIR(t *testing.T) {
	checked := requireAccepted(t, point+"impl Point.length_squared(): Int32 do\n    return self.x * self.x + self.y * self.y\nend\n")
	declaration, ok := checked.Statements[0].(MethodDeclaration)
	if !ok {
		t.Fatalf("statement = %T, want MethodDeclaration", checked.Statements[0])
	}
	if declaration.Name != "length_squared" || declaration.Object == nil || declaration.Object.Name != "Point" {
		t.Fatalf("declaration = %#v, want length_squared on Point", declaration)
	}
	if !compilerTypes.Equal(declaration.SelfType, checked.TypeDeclarations[0].Type) {
		t.Fatalf("self type = %s, want Point", declaration.SelfType.Name)
	}
	if len(declaration.Parameters) != 0 || declaration.Result == nil {
		t.Fatalf("declaration = %#v, want no parameters and an Int32 result", declaration)
	}
}

func TestMethodControlFlowUsesStructuredScopesAndFlow(t *testing.T) {
	requireAccepted(t, point+"impl Point.classify(value: Int32): Int32 do\n"+
		"    if value > 0 then\n"+
		"        return value\n"+
		"    else\n"+"        return 0\n"+"    end\n"+"end\n")
	requireDiagnostic(t, point+"impl Point.partial(value: Int32): Int32 do\n"+"    if value > 0 then\n"+"        return value\n"+"    end\n"+"end\n",
		"returning partial may fall through without returning Int32")
}

// The three receiver forms all key one method table entry, so each of these
// names must be free on Point and self must carry the written target type.
func TestAllThreeReceiverFormsBindSelf(t *testing.T) {
	checked := requireAccepted(t, point+
		"impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n"+
		"impl Ptr<Point>.is_origin(): Bool do\n    return self.x == 0 and self.y == 0\nend\n"+
		"impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do\n    self.x = self.x + dx\n    self.y = self.y + dy\nend\n")
	want := []string{"Point", "Ptr<Point>", "MutPtr<Point>"}
	for index, name := range want {
		declaration, ok := checked.Statements[index].(MethodDeclaration)
		if !ok {
			t.Fatalf("statement %d = %T, want MethodDeclaration", index, checked.Statements[index])
		}
		if declaration.SelfType.Name != name {
			t.Fatalf("self type %d = %s, want %s", index, declaration.SelfType.Name, name)
		}
		if declaration.Object == nil || declaration.Object.Name != "Point" {
			t.Fatalf("method %d is owned by %#v, want Point", index, declaration.Object)
		}
	}
}

func TestSelfCannotBeAssigned(t *testing.T) {
	requireDiagnostic(t, point+"impl MutPtr<Point>.reset() do\n    self = self\nend\n",
		"cannot assign to self; self is a fixed binding")
	requireDiagnostic(t, point+"impl Point.reset() do\n    self = self\nend\n",
		"cannot assign to self; self is a fixed binding")
}

// A value receiver's self is fixed, so a member write fails; the spec's
// workaround is an explicit mutable copy.
func TestValueReceiverSelfIsFixed(t *testing.T) {
	requireDiagnostic(t, point+"impl Point.moved(dx: Int32): Point do\n    self.x = self.x + dx\n    return self\nend\n",
		"cannot assign to read-only member self.x")
	requireAccepted(t, point+"impl Point.moved(dx: Int32): Point do\n    mut result: Point = self\n    result.x = result.x + dx\n    return result\nend\n")
}

func TestPtrReceiverSelfIsReadOnly(t *testing.T) {
	requireAccepted(t, point+"impl Ptr<Point>.is_origin(): Bool do\n    return self.x == 0\nend\n")
	requireDiagnostic(t, point+"impl Ptr<Point>.reset() do\n    self.x = 0\nend\n",
		"cannot assign to read-only member self.x")
}

func TestDuplicateMethodAcrossReceiverForms(t *testing.T) {
	requireDiagnostic(t, point+
		"impl Point.translate() do\n    return\nend\n"+
		"impl MutPtr<Point>.translate() do\n    return\nend\n",
		"Point already has a method named translate")
}

// Aliases are not new nominal types, so a method on the alias collides with a
// method on the object it names.
func TestDuplicateMethodThroughAnAlias(t *testing.T) {
	requireDiagnostic(t, point+"type Coordinate = Point\n"+
		"impl Point.translate() do\n    return\nend\n"+
		"impl Coordinate.translate() do\n    return\nend\n",
		"Point already has a method named translate")
}

func TestMethodNameCannotEqualAMemberName(t *testing.T) {
	requireDiagnostic(t, point+"impl Point.x(): Int32 do\n    return 0\nend\n",
		"Point already has a member named x")
}

func TestMethodSelfRecursionResolves(t *testing.T) {
	requireAccepted(t, point+"impl Point.countdown(value: Int32): Int32 do\n    return self.countdown(value - 1)\nend\n")
}

func TestLaterMethodIsNotVisible(t *testing.T) {
	requireDiagnostic(t, point+
		"impl Point.magnitude(): Int32 do\n    return self.length_squared()\nend\n"+
		"impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n",
		"Point has no method named length_squared")
}

// The four ordered receiver adaptations from the spec's Calling methods.
func TestReceiverAdaptationRules(t *testing.T) {
	// exact target type
	requireAccepted(t, point+"impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n"+
		"origin: Point = Point { x = 0, y = 0, }\ntotal: Int32 = origin.length_squared()\n")
	// MutPtr<T> weakens to a Ptr<T> target
	requireAccepted(t, point+"impl Ptr<Point>.is_origin(): Bool do\n    return self.x == 0\nend\n"+
		"mut here: Point = Point { x = 0, y = 0, }\nwriter: MutPtr<Point> = ref here\nflag: Bool = writer.is_origin()\n")
	// a pointer dereferences to a copied T target
	requireAccepted(t, point+"impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n"+
		"origin: Point = Point { x = 0, y = 0, }\nreader: Ptr<Point> = ref origin\ntotal: Int32 = reader.length_squared()\n")
	// an addressable T takes ref for a MutPtr<T> target
	requireAccepted(t, point+"impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do\n    self.x = self.x + dx\nend\n"+
		"mut here: Point = Point { x = 0, y = 0, }\nhere.translate(5, 5)\n")
}

func TestFixedReceiverCannotReachAMutPtrMethod(t *testing.T) {
	requireDiagnostic(t, point+"impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do\n    self.x = self.x + dx\nend\n"+
		"origin: Point = Point { x = 0, y = 0, }\norigin.translate(5, 5)\n",
		"translate needs MutPtr<Point>; ref origin is Ptr<Point>")
}

func TestMissingMethodIsRejected(t *testing.T) {
	requireDiagnostic(t, point+"origin: Point = Point { x = 0, y = 0, }\ntotal: Int32 = origin.rotate()\n",
		"Point has no method named rotate")
}

func TestMethodsAreNotValues(t *testing.T) {
	requireDiagnostic(t, point+"impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n"+
		"origin: Point = Point { x = 0, y = 0, }\ncallback: Fun<() : Int32> = origin.length_squared\n",
		"length_squared is a method on Point; methods are not values")
}

func TestImplTargetMustBeANominalObject(t *testing.T) {
	requireDiagnostic(t, "impl Int32.doubled(): Int32 do\n    return 0\nend\n",
		"Int32 is not a nominal object type; impl requires an object")
	requireDiagnostic(t, "impl Ptr<Int32>.doubled(): Int32 do\n    return 0\nend\n",
		"Ptr<Int32> is not a nominal object type; impl requires an object")
	requireDiagnostic(t, "impl Fun<(Int32) : Int32>.doubled(): Int32 do\n    return 0\nend\n",
		"Fun<(Int32) : Int32> is not a nominal object type; impl requires an object")
}

// 10. RFC 0010 nullable integration: signatures, returns, arguments, and
// nullable function pointers. Spec 0010.

func TestNullableFunctionSignaturesAndReturns(t *testing.T) {
	requireAccepted(t, "fun find_none(): Ptr<Int32> | Nil do\n    return nil\nend\n")
	requireAccepted(t, "fun pass_through(maybe: Ptr<Int32> | Nil): Ptr<Int32> | Nil do\n    return maybe\nend\n")
	// RFC 0007 weakening then RFC 0010 injection: MutPtr<Int32> -> Ptr<Int32> | Nil
	requireAccepted(t, "mut value: Int32 = 1\nwriter: MutPtr<Int32> = ref value\nfun lift(source: MutPtr<Int32>): Ptr<Int32> | Nil do\n    return source\nend\nok: Ptr<Int32> | Nil = lift(writer)\n")
	// RFC 0049 item 8.1: a function returning no value has no result type.
	requireAccepted(t, "fun absent() do\n    return\nend\nabsent()\n")
}

func TestNonNullableReturnRejectsNullableValues(t *testing.T) {
	requireDiagnostic(t,
		"fun bad(): Ptr<Int32> do\n    mut value: Int32 = 1\n    maybe: Ptr<Int32> | Nil = ref value\n    return maybe\nend\n",
		"bad returns Ptr<Int32>; got Ptr<Int32> | Nil")
	requireDiagnostic(t,
		"fun bad(): Ptr<Int32> do\n    return nil\nend\n",
		"nil requires an expected union containing Nil")
}

func TestNullableArgumentsToNullableParameters(t *testing.T) {
	requireAccepted(t, "fun probe(maybe: Ptr<Int32> | Nil): Ptr<Int32> | Nil do\n    return maybe\nend\n"+
		"mut value: Int32 = 1\nwriter: MutPtr<Int32> = ref value\n"+
		"ok: Ptr<Int32> | Nil = probe(writer)\nnone: Ptr<Int32> | Nil = probe(nil)\n")
	requireDiagnostic(t,
		"fun peek(source: Ptr<Int32>): Int32 do\n    return source.value\nend\n"+
			"mut value: Int32 = 1\nmaybe: Ptr<Int32> | Nil = ref value\nbad: Int32 = peek(maybe)\n",
		"peek argument 1 requires Ptr<Int32>, got Ptr<Int32> | Nil")
	requireDiagnostic(t,
		"fun peek(source: Ptr<Int32>): Int32 do\n    return source.value\nend\nbad: Int32 = peek(nil)\n",
		"nil requires an expected union containing Nil")
}

func TestResetRemainsNoResultForNilBindings(t *testing.T) {
	requireAccepted(t, "fun reset() do\n    return\nend\nreset()\n")
	requireDiagnostic(t, "fun reset() do\n    return\nend\nresult: Nil = reset()\n", "reset produces no value")
}

func TestMethodSignaturesAcceptNullableParameterAndReturnTypes(t *testing.T) {
	requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"impl Node.set_next(next: MutPtr<Node> | Nil): MutPtr<Node> | Nil do\n    return next\nend\n")
}

// A nullable Fun<...> union must be narrowed before it is called. The call
// path consults the branch-local narrowing, so the same binding that cannot
// be called bare can be called inside a != nil branch.
func TestNullableFunctionPointerRequiresNarrowingBeforeCall(t *testing.T) {
	requireAccepted(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nhandler: Fun<(Int32) : Int32> | Nil = identity\n")
	requireDiagnostic(t,
		"fun identity(value: Int32): Int32 do\n    return value\nend\nhandler: Fun<(Int32) : Int32> | Nil = identity\nresult: Int32 = handler(5)\n",
		"Fun<(Int32) : Int32> | Nil may be Nil; narrow it before calling it")
	requireAccepted(t, "fun identity(value: Int32): Int32 do\n    return value\nend\nhandler: Fun<(Int32) : Int32> | Nil = identity\nif handler != nil then\n    result: Int32 = handler(5)\nend\n")
	requireAccepted(t, "fun identity(value: Int32): Int32 do\n    return value\nend\n"+
		"fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do\n    return callback(value)\nend\n"+
		"handler: Fun<(Int32) : Int32> | Nil = identity\nif handler != nil then\n    result: Int32 = apply(handler, 5)\nend\n")
	requireAccepted(t, "fun identity(value: Int32): Int32 do\n    return value\nend\n"+
		"fun invoke(callback: Fun<(Int32) : Int32> | Nil, value: Int32): Int32 do\n"+
		"    if callback != nil then\n        return callback(value)\n    end\n    return 0\nend\n")
}

// Method rule 1 admits T, Ptr<T>, and MutPtr<T> targets. A nullable union is
// not a receiver form: self would be nullable and every use would fail
// closed, so the target itself is rejected.
func TestImplTargetCannotBeNullable(t *testing.T) {
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"impl MutPtr<Node> | Nil.read(): Int32 do\n    return 0\nend\n",
		"impl requires T, Ptr<T>, or MutPtr<T>; got MutPtr<Node> | Nil")
}

// hex_f_ name encoding is not injective, so the checker owns the clash.
func TestFreeFunctionCollidesWithAMethodCName(t *testing.T) {
	requireDiagnostic(t, point+
		"impl Point.translate() do\n    return\nend\n"+
		"fun Point_translate() do\n    return\nend\n",
		"free function Point_translate collides with impl Point.translate")
	requireDiagnostic(t, point+
		"fun Point_translate() do\n    return\nend\n"+
		"impl Point.translate() do\n    return\nend\n",
		"free function Point_translate collides with impl Point.translate")
}

func TestMethodCallProducesCheckedIR(t *testing.T) {
	checked := requireAccepted(t, point+"impl MutPtr<Point>.translate(dx: Int32) do\n    self.x = self.x + dx\nend\n"+
		"mut here: Point = Point { x = 0, y = 0, }\nhere.translate(5)\n")
	statement, ok := checked.Statements[2].(CallStatement)
	if !ok {
		t.Fatalf("statement = %T, want CallStatement", checked.Statements[2])
	}
	node := statement.Call.Node
	if node.Kind != MethodCallExpression || node.Name != "translate" {
		t.Fatalf("call node = %#v, want a translate method call", node)
	}
	if node.Owner == nil || node.Owner.Name != "Point" {
		t.Fatalf("call owner = %#v, want Point", node.Owner)
	}
	// The receiver arrives already adapted: `here` needed its address taken.
	if node.Operand == nil || node.Operand.Kind != AddressOfExpression {
		t.Fatalf("receiver = %#v, want an address-of expression", node.Operand)
	}
	if node.OperandType.Name != "MutPtr<Point>" || len(node.Arguments) != 1 {
		t.Fatalf("call node = %#v, want a MutPtr<Point> receiver and one argument", node)
	}
}
