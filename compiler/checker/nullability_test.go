package checker

import (
	"go/constant"
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

// Standalone Nil is rejected in every written type position: Nil is valid
// only as a union member, and every other written type position rejects it
// with the same diagnostic.
func TestCheckerRejectsStandaloneNilEverywhere(t *testing.T) {
	const want = "Nil is valid only as a member of a union with a non-Nil type"
	for _, source := range []string{
		"type Bad = Nil",
		"bad: Nil := nil",
		"fun bad(argument: Nil) do return end",
		"fun bad(): Nil do return end",
		"type Bad = { marker: Nil, }",
		"type Bad = | One as { marker: Nil } | Two as { value: Int32 }",
		"bad: Array<Nil, 4> := [nil, nil, nil, nil]",
		"bad: View<Nil> := View<Nil>.empty()",
		"bad: List<Nil> := List<Nil>.new(Heap.new())",
		"bad: Dict<Nil, Int32> := Dict<Nil, Int32>.new(Heap.new())",
		"bad: Dict<Int32, Nil> := Dict<Int32, Nil>.new(Heap.new())",
		"bad: Ptr<Nil> := alloc(Nil)",
		"bad: MutPtr<Nil> := alloc(Nil)",
		"fun nothing(value: Int32) do return end bad: Fun<(Nil) : Int32> := nothing",
		"fun nothing(value: Int32) do return end bad: Fun<(Int32) : Nil> := nothing",
		"fun square(value: Int32): Int32 do return value * value end bad: Task<Nil> := try spawn square(6)",
		"h: Heap := Heap.new() bad: Channel<Nil> := Channel<Nil>.new(h, 8)",
		"h: Heap := Heap.new() bad: MutPtr<Nil> := h.allocate<Nil>(0)",
		"value: Int32 | Nil := nil if value is Int32 then noop: Int32 := 0 else bad: Nil := value end",
	} {
		requireDiagnostic(t, source, want)
	}
}

// Nil remains valid as a union member alongside scalar, pointer, handle, and
// aggregate members, and bare nil resolves only under such a contextual union
// or as a print argument.
func TestCheckerAcceptsNilOnlyInContext(t *testing.T) {
	requireAccepted(t, "value: Int32 | Nil := nil")
	requireAccepted(t, "maybe: Ptr<Int32> | Nil := nil")
	requireAccepted(t, "handle: Task<Int32> | Nil := nil")
	requireAccepted(t, "value: Int32 | Bool | Nil := nil")
	requireAccepted(t, "value: Int32 | Bool | Nil := nil flag: Bool := value != nil")
	requireAccepted(t, "print(nil)")
	requireAccepted(t, "value: Int32 | Nil := nil if value != nil then noop: Int32 := value else print(value) end")
}

func TestCheckerResolvesNilUnknownAndNullableAliases(t *testing.T) {
	checked := requireAccepted(t, "type SameMaybe = Ptr<Int32> | Nil type StillMaybe = Ptr<Int32> | Nil type Erased = Unknown type Reader = Ptr<Erased> type Writer = MutPtr<Erased>")

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
	// Standalone Nil has no alias or binding.
	requireDiagnostic(t, "nothing: Nil := nil", "Nil is valid only as a member of a union with a non-Nil type")
}

func TestCheckerAcceptsNullableRecursiveObjectMembers(t *testing.T) {
	checked := requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }")
	member := checked.TypeDeclarations[0].Type.Object.Members[1]
	if !compilerTypes.IsNullable(member.Type) {
		t.Fatalf("next member type = %#v, want nullable", member.Type)
	}
	base, ok := compilerTypes.NullableBase(member.Type)
	if !ok || base.Element == nil || base.Element.Object == nil || base.Element.Object.Name != "Node" {
		t.Fatalf("next nullable base = %#v, want MutPtr<Node>", base)
	}
}

func TestCheckerAcceptsGeneralUnionAndRejectsUnknownValueForms(t *testing.T) {
	requireAccepted(t, "number: Int32 | Nil := nil flag: Bool | Int32 := true")
	requireDiagnostic(t,
		"bad: Unknown | Nil := nil",
		"Unknown | Nil is not a value type; use Ptr<Unknown> | Nil")
	requireDiagnostic(t,
		"value: Unknown := nil",
		"Unknown has no known size or layout; it may only be used behind a pointer")
}

func TestCheckerProtectsNilAndUnknownTypeNames(t *testing.T) {
	for _, name := range []string{"Nil", "Unknown"} {
		requireDiagnostic(t, "type "+name+" = Int32", "built-in type "+name+" cannot be redeclared")
	}
}

func TestCheckerPreservesExistingTypeAfterFailedObjectDeclaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Existing = Int32 type Existing = { link: Existing, } value: Existing := 1"))
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
	requireAccepted(t, "type Holder = { callback: Fun<(Int32) : Int32> | Nil, }")
}

func TestCheckerRoutesNullableAndUnknownAssignabilityThroughAllContexts(t *testing.T) {
	requireAccepted(t, "type Node = { value: Int32, } type Holder = { link: Ptr<Node> | Nil, erased: Ptr<Unknown>, } fun erase(source: MutPtr<Node>): Ptr<Unknown> do return source end fun recover(source: Ptr<Unknown>): Ptr<Node> do return source end fun accept(source: Ptr<Node>): Int32 do return source.value.value end mut node: Node := Node { value = 1, } writer: MutPtr<Node> := ref node mut maybe: Ptr<Node> | Nil := writer maybe = writer mut erased: Ptr<Unknown> := writer mut restored: Ptr<Node> := erased maybe_erased: Ptr<Unknown> | Nil := writer maybe_restored: Ptr<Node> | Nil := maybe_erased holder: Holder := Holder { link = writer, erased = writer, } assigned: Ptr<Node> := recover(erased) erased = writer restored = erased erase_result: Ptr<Unknown> := erase(writer) accepted: Int32 := accept(erased)")
}

func TestCheckerRejectsNullableAndNilRemovalWithExactDiagnostics(t *testing.T) {
	requireDiagnostic(t, "bad: MutPtr<Int32> := nil", "nil requires an expected union containing Nil")
	requireDiagnostic(t, "maybe: Ptr<Int32> | Nil := nil bad: Ptr<Int32> := maybe", "expected Ptr<Int32>; got Ptr<Int32> | Nil")
}

func TestCheckerRejectsUnknownAccessStrengtheningWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 := 1 reader: Ptr<Unknown> := ref value bad: MutPtr<Int32> := reader", "Ptr<Unknown> cannot recover writable access as MutPtr<Int32>")
}

func TestCheckerRejectsNestedUnknownErasureWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 := 1 slot: MutPtr<Int32> := ref value bad: MutPtr<MutPtr<Unknown>> := ref slot", "cannot erase a nested pointer slot as MutPtr<MutPtr<Unknown>>")
}

func TestCheckerRejectsComposedUnknownRecoveryWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "mut value: Int8 := 1 small: MutPtr<Int8> := ref value bad: MutPtr<Int64> := small", "expected MutPtr<Int64>; got MutPtr<Int8>; erasure and recovery do not compose, bind MutPtr<Unknown> first")
}

func TestCheckerRequiresAConcretePointerBeforeUnknownDereference(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 := 1 erased: Ptr<Unknown> := ref value bad: Int32 := erased.value", "Ptr<Unknown> cannot be dereferenced; recover a concrete pointer type first")
}

// Null tests: == and != where one side is Nil and the other is Nil
// or a nullable union. The result is Bool and the checked node is normalized
// so the nullable side is always the node's Operand, making nil == maybe
// share the checked shape of maybe == nil.
func TestCheckerChecksNullTestsAndNormalizesCommutedForms(t *testing.T) {
	checked := requireAccepted(t, "mut maybe: Ptr<Int32> | Nil := nil equal: Bool := maybe == nil notEqual: Bool := maybe != nil commuted: Bool := nil == maybe")
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
	checked := requireAccepted(t, "same: Bool := nil == nil different: Bool := nil != nil")
	for index, want := range []bool{true, false} {
		declaration := checked.Statements[index].(Declaration)
		if declaration.Source.Kind != ConstantOperand || constant.BoolVal(declaration.Source.Constant) != want {
			t.Fatalf("declaration %d source = %#v, want folded %t", index, declaration.Source, want)
		}
	}
}

func TestCheckerRejectsNonNullableNullTestsWithVerdictDiagnostics(t *testing.T) {
	// == nil requires a union containing Nil, so the nil
	// literal gate rejects the comparison before any verdict is computed.
	requireDiagnostic(t, "mut value: Int32 := 1 node: MutPtr<Int32> := ref value bad: Bool := node != nil", "nil requires an expected union containing Nil")
	requireDiagnostic(t, "mut value: Int32 := 1 node: MutPtr<Int32> := ref value bad: Bool := node == nil", "nil requires an expected union containing Nil")
	requireDiagnostic(t, "bad: Bool := 5 == nil", "nil requires an expected union containing Nil")
}

func TestCheckerPointerEqualityAndNullableIdentityRules(t *testing.T) {
	// Identical non-null pointer types compare identity.
	requireAccepted(t, "mut a: Int32 := 1 mut b: Int32 := 2 p: Ptr<Int32> := ref a q: Ptr<Int32> := ref a same: Bool := p == q")
	requireDiagnostic(t, "mut a: Int32 := 1 mut b: Int32 := 2 p: Ptr<Int32> := ref a q: MutPtr<Int32> := ref b bad: Bool := p == q", "pointer equality requires identical pointer types")
	requireDiagnostic(t, "mut a: Int32 := 1 p: Ptr<Int32> := ref a maybe: Ptr<Int32> | Nil := p bad: Bool := maybe == p", "union equality requires identical operand types; got Ptr<Int32> | Nil and Ptr<Int32>")
}

// Every value-producing expression is a valid condition, including a nullable
// value, and not accepts any value-producing operand.
func TestCheckerAcceptsNullableTruthinessAsCondition(t *testing.T) {
	requireAccepted(t, "maybe: Ptr<Int32> | Nil := nil if maybe then end")
	requireAccepted(t, "maybe: Ptr<Int32> | Nil := nil flag: Bool := !maybe")
}

// Flow narrowing: a null test on a local binding narrows the
// binding's effective read type inside the branch where the test holds.
// `!= nil` proves P in the true branch and Nil in the false branch; `== nil`
// reverses those facts. The commuted spelling narrows identically.
func TestCheckerNarrowsNullTestsByBranch(t *testing.T) {
	requireAccepted(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then result: Int32 := maybe.value end")
	requireAccepted(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if nil != maybe then result: Int32 := maybe.value end")
	requireAccepted(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := nil if maybe == nil then noop: Int32 := 0 else result: Int32 := maybe.value end")
	requireAccepted(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe == nil then noop: Int32 := 0 elseif flag then result: Int32 := maybe.value end")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then result: Int32 := maybe.value else bad: Int32 := maybe.value end", "cannot access .value on Nil; expected Ptr<T>")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe == nil then bad: Int32 := maybe.value end", "cannot access .value on Nil; expected Ptr<T>")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil then noop: Int32 := 0 elseif flag then bad: Int32 := maybe.value end", "cannot access .value on Nil; expected Ptr<T>")
}

func TestCheckerNarrowingIsBranchLocal(t *testing.T) {
	requireAccepted(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then result: Int32 := maybe.value end")
	requireAccepted(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then result: Int32 := maybe.value end if maybe != nil then result: Int32 := maybe.value end")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then result: Int32 := maybe.value end bad: Int32 := maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value while maybe != nil do maybe = nil end bad: Int32 := maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerRejectsNullableAccessWithoutNarrowing(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value bad: Int32 := maybe.value", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil and flag then bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if flag then bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerRejectsMemberPathNarrowing(t *testing.T) {
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } mut first: Node := Node { value = 1, next = nil, } node: MutPtr<Node> := ref first if node.next != nil then bad: Int32 := node.next.value end", "only a local binding can be narrowed; bind node.next before testing it")
}

func TestCheckerInvalidatesNarrowingOnAssignmentAndWritableRef(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value if maybe != nil then maybe = nil bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil then if flag then maybe = nil end bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value if maybe != nil then slot: MutPtr<Ptr<Int32> | Nil> := ref maybe bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value slot: MutPtr<Ptr<Int32> | Nil> := ref maybe if maybe != nil then bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil then if flag then slot: MutPtr<Ptr<Int32> | Nil> := ref maybe end bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerPreservesNarrowingAcrossReadOnlyRef(t *testing.T) {
	requireAccepted(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then reader: Ptr<Ptr<Int32> | Nil> := ref maybe result: Int32 := maybe.value end")
}

func TestCheckerReportsRedundantNullTestInsideNarrowedBranch(t *testing.T) {
	// The branch narrows maybe to a plain pointer, which has no Nil member,
	// so the inner null test's nil literal is rejected by the union gate.
	requireDiagnostic(t, "mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then bad: Bool := maybe != nil end", "nil requires an expected union containing Nil")
}

func TestCheckerNarrowingSurvivesWhileLoopBody(t *testing.T) {
	requireAccepted(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value while maybe != nil do result: Int32 := maybe.value maybe = nil end")
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil then while flag do maybe = nil end bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerNarrowsFunctionParameters(t *testing.T) {
	requireAccepted(t, "fun read(maybe: Ptr<Int32> | Nil): Int32 do if maybe != nil then return maybe.value end return 0 end")
}

// Nullable object members, member access, and methods on nullable
// receivers.

func TestCheckerAcceptsRecursiveNullableObjectLiteral(t *testing.T) {
	checked := requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } tail: Node := Node { value = 3, next = nil, }")
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
		"mut first: Node := Node { value = 1, next = nil, }\n"+
		"mut second: Node := Node { value = 2, next = nil, }\n"+
		"first.next = ref second\n"+
		"next: MutPtr<Node> | Nil := first.next\n"+
		"first.next = nil\n")
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"tail: Node := Node { value = 1, next = nil, }\n"+
		"bad: MutPtr<Node> := tail.next\n",
		"expected MutPtr<Node>; got MutPtr<Node> | Nil")
}

func TestCheckerNullableMemberValueRequiresBindThenTest(t *testing.T) {
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"mut first: Node := Node { value = 1, next = nil, }\n"+
		"node: MutPtr<Node> := ref first\n"+
		"bad: Node := node.next.value\n",
		"only a local binding can be narrowed; bind node.next before testing it")
	requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"mut first: Node := Node { value = 1, next = nil, }\n"+
		"node: MutPtr<Node> := ref first\n"+
		"next: MutPtr<Node> | Nil := node.next\n"+
		"if next != nil then\n"+
		"    tail: Node := next.value\n"+
		"end\n")
}

func TestCheckerMethodsOnNullableReceiversRequireNarrowing(t *testing.T) {
	requireAccepted(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"impl Node.read(): Int32 do\n    return self.value\nend\n"+
		"mut first: Node := Node { value = 1, next = nil, }\n"+
		"maybe: MutPtr<Node> | Nil := ref first\n"+
		"if maybe != nil then\n    result: Int32 := maybe.read()\nend\n")
	requireDiagnostic(t, "type Node = { value: Int32, mut next: MutPtr<Node> | Nil, }\n"+
		"impl Node.read(): Int32 do\n    return self.value\nend\n"+
		"mut first: Node := Node { value = 1, next = nil, }\n"+
		"maybe: MutPtr<Node> | Nil := ref first\n"+
		"bad: Int32 := maybe.read()\n",
		"MutPtr<Node> | Nil may be Nil; narrow it before using .value")
}

func TestCheckerKeepsByValueRecursionRejectedWithNullableMembers(t *testing.T) {
	requireDiagnostic(t, "type Bad = { child: Bad, }\n", "object type Bad cannot contain itself by value")
	requireDiagnostic(t, "type Bad = { child: Bad | Nil, }\n", "object type Bad cannot contain itself by value")
}

// Assignment inside an elseif body invalidates the narrowing even when the
// elseif condition itself narrows nothing and the if has no final else: the
// body's effects must still merge into the pre-test flow.
func TestCheckerElseifWithoutElseStillMergesInvalidation(t *testing.T) {
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil then if flag then maybe = nil end bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
	requireDiagnostic(t, "mut value: Int32 := 1 mut maybe: Ptr<Int32> | Nil := ref value flag: Bool := true if maybe != nil then other: Bool := false if other then noop: Int32 := 0 elseif flag then maybe = nil end bad: Int32 := maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}

// Standalone Nil is rejected even through generic substitution and spawn
// arguments, not only in direct spellings.
func TestCheckerRejectsNilThroughGenericSubstitution(t *testing.T) {
	requireDiagnostic(t, "type Box<T> = { value: T } bad: Box<Nil> := Box<Nil> { value = nil }", "Nil is valid only as a member of a union with a non-Nil type")
	requireDiagnostic(t, "type Box<T> = { value: T } fun unwrap(box: Box<Nil>): Int32 do return 0 end", "Nil is valid only as a member of a union with a non-Nil type")
	requireDiagnostic(t, "fun worker(flag: Nil): Bool do return true end fun run(): Int32 | Error do task: Task<Bool> := try spawn worker(nil) return 0 end", "Nil is valid only as a member of a union with a non-Nil type")
}

// A branch-established fact survives only on the sole continuing path when
// every alternative terminates with return, break, or continue.
func TestCheckerSoleContinuingPathNarrowing(t *testing.T) {
	requireAccepted(t, "fun f(): Int32 do mut maybe: Ptr<Int32> | Nil := nil if maybe == nil then return 0 end return maybe.value end")
	requireAccepted(t, "fun f(): Int32 do mut maybe: Ptr<Int32> | Nil := nil while true do if maybe == nil then break end return maybe.value end return 0 end")
	requireAccepted(t, "fun f(): Int32 do mut maybe: Ptr<Int32> | Nil := nil mut total: Int32 := 0 while true do if maybe == nil then continue end total = maybe.value break end return total end")
	requireDiagnostic(t, "fun f(): Int32 do mut maybe: Ptr<Int32> | Nil := nil if maybe != nil then print(maybe.value) end return maybe.value end", "Ptr<Int32> | Nil may be Nil; narrow it before using .value")
}
