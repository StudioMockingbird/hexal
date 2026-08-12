package checker

import (
	"go/constant"
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

func TestCheckerResolvesNilUnknownAndNullableAliases(t *testing.T) {
	checked := requireAccepted(t, "type SameMaybe = Ptr<Int32> | Nil type StillMaybe = SameMaybe | Nil type Erased = Unknown type Reader = Ptr<Erased> type Writer = MutPtr<Erased> nothing: Nil = nil")

	if len(checked.TypeDeclarations) != 5 {
		t.Fatalf("type declaration count = %d, want 5", len(checked.TypeDeclarations))
	}
	sameMaybe := checked.TypeDeclarations[0].Type
	stillMaybe := checked.TypeDeclarations[1].Type
	if !compilerTypes.IsNullable(sameMaybe) || !compilerTypes.Equal(sameMaybe, stillMaybe) {
		t.Fatalf("nullable aliases = %#v and %#v, want one canonical type", sameMaybe, stillMaybe)
	}
	if base, ok := compilerTypes.NullableBase(sameMaybe); !ok || base.Name != "Ptr<Int32>" {
		t.Fatalf("nullable base = %#v, %v; want Ptr<Int32>", base, ok)
	}
	if !compilerTypes.IsUnknown(checked.TypeDeclarations[2].Type) {
		t.Fatalf("Erased = %#v, want the Unknown identity", checked.TypeDeclarations[2].Type)
	}
	for _, declaration := range checked.TypeDeclarations[3:] {
		if declaration.Type.Element == nil || !compilerTypes.IsUnknown(*declaration.Type.Element) {
			t.Fatalf("%s = %#v, want a pointer to Unknown", declaration.Name, declaration.Type)
		}
	}
	if got := checked.Statements[0].(Declaration).Type; !compilerTypes.IsNil(got) {
		t.Fatalf("nothing type = %#v, want Nil", got)
	}
}

func TestCheckerAcceptsNullableRecursiveObjectMembers(t *testing.T) {
	checked := requireAccepted(t, "type Node = { marker: Nil, value: Int32, mut next: MutPtr<Node> | Nil, }")
	if !compilerTypes.IsNil(checked.TypeDeclarations[0].Type.Object.Members[0].Type) {
		t.Fatalf("marker member type = %#v, want Nil", checked.TypeDeclarations[0].Type.Object.Members[0].Type)
	}
	member := checked.TypeDeclarations[0].Type.Object.Members[2]
	if !compilerTypes.IsNullable(member.Type) {
		t.Fatalf("next member type = %#v, want nullable", member.Type)
	}
	base, ok := compilerTypes.NullableBase(member.Type)
	if !ok || base.Element == nil || base.Element.Object == nil || base.Element.Object.Name != "Node" {
		t.Fatalf("next nullable base = %#v, want MutPtr<Node>", base)
	}
}

func TestCheckerAcceptsGeneralUnionAndRejectsUnknownValueForms(t *testing.T) {
	requireAccepted(t, "number: Int32 | Nil = nil flag: Bool | Int32 = true")
	requireDiagnostic(t,
		"bad: Unknown | Nil = nil",
		"Unknown | Nil is not a value type; use Ptr<Unknown> | Nil")
	requireDiagnostic(t,
		"value: Unknown = nil",
		"Unknown has no known size or layout; it may only be used behind a pointer")
}

func TestCheckerNormalizesDuplicateUnionMembers(t *testing.T) {
	checked := requireAccepted(t, "type Bad = Ptr<Int32> | Nil | Nil value: Bad = nil")
	if len(checked.TypeDeclarations) != 1 || !compilerTypes.IsNullable(checked.TypeDeclarations[0].Type) {
		t.Fatalf("type declarations = %#v, want one nullable canonical type", checked.TypeDeclarations)
	}
}

func TestCheckerProtectsNilAndUnknownTypeNames(t *testing.T) {
	for _, name := range []string{"Nil", "Unknown"} {
		requireDiagnostic(t, "type "+name+" = Int32", "built-in type "+name+" cannot be redeclared")
	}
}

func TestCheckerPreservesExistingTypeAfterFailedObjectDeclaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Existing = Int32 type Existing = { link: Existing, } value: Existing = 1"))
	if err == nil {
		t.Fatal("Check accepted the duplicate recursive object declaration")
	}
	if strings.Contains(err.Error(), "unknown type Existing") {
		t.Fatalf("failed object declaration removed the existing type: %v", err)
	}
	if len(checked.Statements) != 1 || checked.Statements[0].(Declaration).Type != compilerTypes.Int32 {
		t.Fatalf("value after failed object declaration = %#v, want Existing resolving to Int32", checked.Statements)
	}
}

func TestCheckerPreservesFunctionPositionRestrictions(t *testing.T) {
	requireDiagnostic(t,
		"type Bad = Ptr<Fun<(Int32) : Int32>> | Nil",
		"Ptr<Fun<(Int32) : Int32>> is not supported")
	requireDiagnostic(t,
		"type Holder = { callback: Fun<(Int32) : Int32> | Nil, }",
		"Fun<…> object members are not supported")
}

func TestCheckerRoutesNullableAndUnknownAssignabilityThroughAllContexts(t *testing.T) {
	requireAccepted(t, "type Node = { value: Int32, } type Holder = { link: Ptr<Node> | Nil, erased: Ptr<Unknown>, } fun erase(source: MutPtr<Node>): Ptr<Unknown> return source end fun recover(source: Ptr<Unknown>): Ptr<Node> return source end fun accept(source: Ptr<Node>): Int32 return source.value.value end mut node: Node = Node { value = 1, } writer: MutPtr<Node> = ref node mut maybe: Ptr<Node> | Nil = writer maybe = writer mut erased: Ptr<Unknown> = writer mut restored: Ptr<Node> = erased maybe_erased: Ptr<Unknown> | Nil = writer maybe_restored: Ptr<Node> | Nil = maybe_erased holder: Holder = Holder { link = writer, erased = writer, } assigned: Ptr<Node> = recover(erased) erased = writer restored = erased erase_result: Ptr<Unknown> = erase(writer) accepted: Int32 = accept(erased)")
}

func TestCheckerRejectsNullableAndNilRemovalWithExactDiagnostics(t *testing.T) {
	requireDiagnostic(t, "bad: MutPtr<Int32> = nil", "expected MutPtr<Int32>, got Nil")
	requireDiagnostic(t, "maybe: Ptr<Int32> | Nil = nil bad: Ptr<Int32> = maybe", "expected Ptr<Int32>, got Ptr<Int32> | Nil")
}

func TestCheckerRejectsUnknownAccessStrengtheningWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 reader: Ptr<Unknown> = ref value bad: MutPtr<Int32> = reader", "Ptr<Unknown> cannot recover writable access as MutPtr<Int32>")
}

func TestCheckerRejectsNestedUnknownErasureWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 slot: MutPtr<Int32> = ref value bad: MutPtr<MutPtr<Unknown>> = ref slot", "cannot erase a nested pointer slot as MutPtr<MutPtr<Unknown>>")
}

func TestCheckerRejectsComposedUnknownRecoveryWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "mut value: Int8 = 1 small: MutPtr<Int8> = ref value bad: MutPtr<Int64> = small", "expected MutPtr<Int64>, got MutPtr<Int8>; erasure and recovery do not compose, bind MutPtr<Unknown> first")
}

func TestCheckerRequiresAConcretePointerBeforeUnknownDereference(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 erased: Ptr<Unknown> = ref value bad: Int32 = erased.value", "Ptr<Unknown> cannot be dereferenced; recover a concrete pointer type first")
}

// RFC 0010 null tests: == and != where one side is Nil and the other is Nil
// or a nullable union. The result is Bool and the checked node is normalized
// so the nullable side is always the node's Operand, making nil == maybe
// share the checked shape of maybe == nil.
func TestCheckerChecksNullTestsAndNormalizesCommutedForms(t *testing.T) {
	checked := requireAccepted(t, "mut maybe: Ptr<Int32> | Nil = nil equal: Bool = maybe == nil notEqual: Bool = maybe != nil commuted: Bool = nil == maybe")
	if len(checked.Statements) != 4 {
		t.Fatalf("statements = %d, want 4", len(checked.Statements))
	}
	for index, want := range []Operator{EqualOperator, NotEqualOperator, EqualOperator} {
		declaration := checked.Statements[index+1].(Declaration)
		if declaration.Source.Kind != ExpressionOperand {
			t.Fatalf("declaration %d source kind = %v, want ExpressionOperand", index+1, declaration.Source.Kind)
		}
		node := declaration.Source.Node
		if node.Kind != NullTestExpression || node.Operator != want {
			t.Fatalf("declaration %d node = %#v, want NullTestExpression with %v", index+1, node, want)
		}
		if node.ResultType != compilerTypes.Bool {
			t.Fatalf("declaration %d result type = %s, want Bool", index+1, node.ResultType.Name)
		}
		if node.Operand == nil || node.Operand.Kind != VariableExpression || node.Operand.Name != "maybe" {
			t.Fatalf("declaration %d operand = %#v, want the normalized nullable variable", index+1, node.Operand)
		}
	}
}

func TestCheckerFoldsNilSingletonEquality(t *testing.T) {
	checked := requireAccepted(t, "same: Bool = nil == nil different: Bool = nil != nil")
	for index, want := range []bool{true, false} {
		declaration := checked.Statements[index].(Declaration)
		if declaration.Source.Kind != ConstantOperand || constant.BoolVal(declaration.Source.Constant) != want {
			t.Fatalf("declaration %d source = %#v, want folded %t", index, declaration.Source, want)
		}
	}
}

func TestCheckerRejectsNonNullableNullTestsWithVerdictDiagnostics(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 node: MutPtr<Int32> = ref value bad: Bool = node != nil", "MutPtr<Int32> is never Nil; the test is always true")
	requireDiagnostic(t, "mut value: Int32 = 1 node: MutPtr<Int32> = ref value bad: Bool = node == nil", "MutPtr<Int32> is never Nil; the test is always false")
	requireDiagnostic(t, "bad: Bool = 5 == nil", "Int32 is never Nil; the test is always false")
}

func TestCheckerPointerEqualityAndNullableIdentityRules(t *testing.T) {
	// RFC 0024: identical non-null pointer types compare identity.
	requireAccepted(t, "mut a: Int32 = 1 mut b: Int32 = 2 p: Ptr<Int32> = ref a q: Ptr<Int32> = ref a same: Bool = p == q")
	requireDiagnostic(t, "mut a: Int32 = 1 mut b: Int32 = 2 p: Ptr<Int32> = ref a q: MutPtr<Int32> = ref b bad: Bool = p == q", "pointer equality requires identical pointer types")
	requireDiagnostic(t, "mut a: Int32 = 1 p: Ptr<Int32> = ref a maybe: Ptr<Int32> | Nil = p bad: Bool = maybe == p", "union equality requires identical operand types; got Ptr<Int32> | Nil and Ptr<Int32>")
}

// RFC 0023 supersedes RFC 0015's exact-Bool condition: every value-producing
// expression is a valid condition, including a nullable value, and not
// accepts any value-producing operand.
func TestCheckerAcceptsNullableTruthinessAsCondition(t *testing.T) {
	requireAccepted(t, "maybe: Ptr<Int32> | Nil = nil if maybe end")
	requireAccepted(t, "maybe: Ptr<Int32> | Nil = nil flag: Bool = !maybe")
}

// RFC 0010 flow narrowing: a null test on a local binding narrows the
// binding's effective read type inside the branch where the test holds.
// `!= nil` proves P in the true branch and Nil in the false branch; `== nil`
// reverses those facts. The commuted spelling narrows identically.
func TestCheckerNarrowsNullTestsByBranch(t *testing.T) {
	requireAccepted(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value end")
	requireAccepted(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if nil != maybe result: Int32 = maybe.value end")
	requireAccepted(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = nil if maybe == nil noop: Int32 = 0 else result: Int32 = maybe.value end")
	requireAccepted(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe == nil noop: Int32 = 0 elseif flag result: Int32 = maybe.value end")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value else bad: Int32 = maybe.value end", "cannot access .value on Nil; expected Ptr<T>")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe == nil bad: Int32 = maybe.value end", "cannot access .value on Nil; expected Ptr<T>")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil noop: Int32 = 0 elseif flag bad: Int32 = maybe.value end", "cannot access .value on Nil; expected Ptr<T>")
}

func TestCheckerNarrowingIsBranchLocal(t *testing.T) {
	requireAccepted(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value end")
	requireAccepted(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value end if maybe != nil result: Int32 = maybe.value end")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil result: Int32 = maybe.value end bad: Int32 = maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value while maybe != nil do maybe = nil end bad: Int32 = maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerRejectsNullableAccessWithoutNarrowing(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value bad: Int32 = maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil and flag bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if flag bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerRejectsMemberPathNarrowing(t *testing.T) {
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } mut first: Node = Node { value = 1, next = nil, } node: MutPtr<Node> = ref first if node.next != nil bad: Int32 = node.next.value end", "only a local binding can be narrowed; bind node.next before testing it")
}

func TestCheckerInvalidatesNarrowingOnAssignmentAndWritableRef(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value if maybe != nil maybe = nil bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil if flag maybe = nil end bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value if maybe != nil slot: MutPtr<Ptr<Int32> | Nil> = ref maybe bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value slot: MutPtr<Ptr<Int32> | Nil> = ref maybe if maybe != nil bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil if flag slot: MutPtr<Ptr<Int32> | Nil> = ref maybe end bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerPreservesNarrowingAcrossReadOnlyRef(t *testing.T) {
	requireAccepted(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil reader: Ptr<Ptr<Int32> | Nil> = ref maybe result: Int32 = maybe.value end")
}

func TestCheckerReportsRedundantNullTestInsideNarrowedBranch(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 maybe: Ptr<Int32> | Nil = ref value if maybe != nil bad: Bool = maybe != nil end", "Ptr<Int32> is never Nil; the test is always true")
}

func TestCheckerNarrowingSurvivesWhileLoopBody(t *testing.T) {
	requireAccepted(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value while maybe != nil do result: Int32 = maybe.value maybe = nil end")
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil while flag do maybe = nil end bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerNarrowsFunctionParameters(t *testing.T) {
	requireAccepted(t, "fun read(maybe: Ptr<Int32> | Nil): Int32 if maybe != nil return maybe.value end return 0 end")
}

// Task 7: nullable object members, member access, and methods on nullable
// receivers. Spec 0010.

func TestCheckerAcceptsRecursiveNullableObjectLiteral(t *testing.T) {
	checked := requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } tail: Node = Node { value = 3, next = nil, }")
	literal := checked.Statements[0].(Declaration).Source.Object
	if literal == nil {
		t.Fatalf("tail source = %#v, want an object literal", checked.Statements[0].(Declaration).Source)
	}
	nextInitializer := literal.Initializers[1]
	if nextInitializer.Member == nil || !compilerTypes.IsNullable(nextInitializer.Member.Type) {
		t.Fatalf("next initializer member = %#v, want the nullable member", nextInitializer.Member)
	}
	if !compilerTypes.IsNullable(nextInitializer.Source.Type) || nextInitializer.Source.Node.Kind != UnionInjectionExpression || nextInitializer.Source.Node.Operand == nil || nextInitializer.Source.Node.Operand.Kind != NilExpression {
		t.Fatalf("next initializer = %#v, want a Nil union injection", nextInitializer.Source)
	}
}

func TestCheckerReadsAndAssignsNullableObjectMembers(t *testing.T) {
	requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"mut first: Node = Node { value = 1, next = nil, }\n"+
		"mut second: Node = Node { value = 2, next = nil, }\n"+
		"first.next = ref second\n"+
		"next: MutPtr<Node> | Nil = first.next\n"+
		"first.next = nil\n")
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"tail: Node = Node { value = 1, next = nil, }\n"+
		"bad: MutPtr<Node> = tail.next\n",
		"expected MutPtr<Node>, got MutPtr<Node> | Nil")
}

func TestCheckerNullableMemberValueRequiresBindThenTest(t *testing.T) {
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"mut first: Node = Node { value = 1, next = nil, }\n"+
		"node: MutPtr<Node> = ref first\n"+
		"bad: Node = node.next.value\n",
		"only a local binding can be narrowed; bind node.next before testing it")
	requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"mut first: Node = Node { value = 1, next = nil, }\n"+
		"node: MutPtr<Node> = ref first\n"+
		"next: MutPtr<Node> | Nil = node.next\n"+
		"if next != nil\n"+
		"    tail: Node = next.value\n"+
		"end\n")
}

func TestCheckerMethodsOnNullableReceiversRequireNarrowing(t *testing.T) {
	requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"impl Node.read(): Int32\n    return self.value\nend\n"+
		"mut first: Node = Node { value = 1, next = nil, }\n"+
		"maybe: MutPtr<Node> | Nil = ref first\n"+
		"if maybe != nil\n    result: Int32 = maybe.read()\nend\n")
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"impl Node.read(): Int32\n    return self.value\nend\n"+
		"mut first: Node = Node { value = 1, next = nil, }\n"+
		"maybe: MutPtr<Node> | Nil = ref first\n"+
		"bad: Int32 = maybe.read()\n",
		"MutPtr<Node> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerKeepsByValueRecursionRejectedWithNullableMembers(t *testing.T) {
	requireDiagnostic(t, "type Bad = { child: Bad, }\n", "object type Bad cannot contain itself by value")
	requireDiagnostic(t, "type Bad = { child: Bad | Nil, }\n", "object type Bad cannot contain itself by value")
}

// Assignment inside an elseif body invalidates the narrowing even when the
// elseif condition itself narrows nothing and the if has no final else: the
// body's effects must still merge into the pre-test flow. Regression for a
// lost-invalidation bug where the missing else dropped the merge entirely.
func TestCheckerElseifWithoutElseStillMergesInvalidation(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil if flag maybe = nil end bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 = 1 mut maybe: Ptr<Int32> | Nil = ref value flag: Bool = true if maybe != nil other: Bool = false if other noop: Int32 = 0 elseif flag maybe = nil end bad: Int32 = maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}
