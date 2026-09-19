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
		"type Bad is Nil",
		"let bad: Nil = nil",
		"fun bad(argument: Nil) do return end",
		"fun bad(): Nil do return end",
		"type Bad is struct marker: Nil, end",
		"type Bad is union | One as marker: Nil end | Two as value: Int32 end end",
		"let bad: Array<Nil, 4> = [nil, nil, nil, nil]",
		"let bad: Slice<Nil> = Slice<Nil>.empty()",
		"let bad: List<Nil> = List<Nil>(Heap())",
		"let bad: Dict<Nil, Int32> = Dict<Nil, Int32>(Heap())",
		"let bad: Dict<Int32, Nil> = Dict<Int32, Nil>(Heap())",
		"let bad: Ptr<Nil> = alloc(Nil)",
		"let bad: Ptr<mut Nil> = alloc(Nil)",
		"fun nothing(value: Int32) do return end let bad: Fun<(Nil) : Int32> = nothing",
		"fun nothing(value: Int32) do return end let bad: Fun<(Int32) : Nil> = nothing",
		"fun square(value: Int32): Int32 do return value * value end let bad: Task<Nil> = try spawn square(6)",
		"let h: Heap = Heap() let bad: Channel<Nil> = Channel<Nil>(h, 8)",
		"let h: Heap = Heap() let bad: Ptr<mut Nil> = h.allocate<Nil>(0)",
		"let value: Int32 | Nil = nil if value is Int32 then let noop: Int32 = 0 else let bad: Nil = value end",
	} {
		requireDiagnostic(t, source, want)
	}
}

// Nil remains valid as a union member alongside scalar, pointer, handle, and
// aggregate members, and bare nil resolves only under such a contextual union
// or as a print argument.
func TestCheckerAcceptsNilOnlyInContext(t *testing.T) {
	requireAccepted(t, "let value: Int32 | Nil = nil")
	requireAccepted(t, "let maybe: Ptr<Int32> | Nil = nil")
	requireAccepted(t, "let handle: Task<Int32> | Nil = nil")
	requireAccepted(t, "let value: Int32 | Bool | Nil = nil")
	requireAccepted(t, "let value: Int32 | Bool | Nil = nil let flag: Bool = value != nil")
	requireAccepted(t, "print(nil)")
	requireAccepted(t, "let value: Int32 | Nil = nil if value != nil then let noop: Int32 = value else print(value) end")
}

func TestCheckerResolvesNilUnknownAndNullableAliases(t *testing.T) {
	checked := requireAccepted(t, "type SameMaybe is union Ptr<Int32> | Nil end type StillMaybe is union Ptr<Int32> | Nil end type Erased is Unknown type Reader is Ptr<Erased> type Writer is Ptr<mut Erased>")

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
	requireDiagnostic(t, "let nothing: Nil = nil", "Nil is valid only as a member of a union with a non-Nil type")
}

func TestCheckerAcceptsNullableRecursiveObjectMembers(t *testing.T) {
	checked := requireAccepted(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end")
	member := checked.TypeDeclarations[0].Type.Object.Members[1]
	if !compilerTypes.IsNullable(member.Type) {
		t.Fatalf("next member type = %#v, want nullable", member.Type)
	}
	base, ok := compilerTypes.NullableBase(member.Type)
	if !ok || base.Element == nil || base.Element.Object == nil || base.Element.Object.Name != "Node" {
		t.Fatalf("next nullable base = %#v, want Ptr<mut Node>", base)
	}
}

func TestCheckerAcceptsGeneralUnionAndRejectsUnknownValueForms(t *testing.T) {
	requireAccepted(t, "let number: Int32 | Nil = nil let flag: Bool | Int32 = true")
	requireDiagnostic(t,
		"let bad: Unknown | Nil = nil",
		"Unknown | Nil is not a value type; use Ptr<Unknown> | Nil")
	requireDiagnostic(t,
		"let value: Unknown = nil",
		"Unknown has no known size or layout; it may only be used behind a pointer")
}

func TestCheckerProtectsNilAndUnknownTypeNames(t *testing.T) {
	for _, name := range []string{"Nil", "Unknown"} {
		requireDiagnostic(t, "type "+name+" is Int32", "built-in type "+name+" cannot be redeclared")
	}
}

func TestCheckerPreservesExistingTypeAfterFailedObjectDeclaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Existing is Int32 type Existing is struct link: Existing, end let value: Existing = 1"))
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
		"type Bad is union Ptr<Fun<(Int32) : Int32>> | Nil end",
		"Ptr<Fun<(Int32) : Int32>> is not supported")
	requireAccepted(t, "type Holder is struct callback: Fun<(Int32) : Int32> | Nil, end")
}

func TestCheckerRoutesNullableAndUnknownAssignabilityThroughAllContexts(t *testing.T) {
	requireAccepted(t, "type Node is struct value: Int32, end type Holder is struct link: Ptr<Node> | Nil, erased: Ptr<Unknown>, end fun erase(source: Ptr<mut Node>): Ptr<Unknown> do return source end fun recover(source: Ptr<Unknown>): Ptr<Node> do return source end fun accept(source: Ptr<Node>): Int32 do return (^source).value end let mut node: Node = Node(value = 1,) let writer: Ptr<mut Node> = @node let mut maybe: Ptr<Node> | Nil = writer maybe = writer let mut erased: Ptr<Unknown> = writer let mut restored: Ptr<Node> = erased let maybe_erased: Ptr<Unknown> | Nil = writer let maybe_restored: Ptr<Node> | Nil = maybe_erased let holder: Holder = Holder(link = writer, erased = writer,) let assigned: Ptr<Node> = recover(erased) erased = writer restored = erased let erase_result: Ptr<Unknown> = erase(writer) let accepted: Int32 = accept(erased)")
}

func TestCheckerRejectsNullableAndNilRemovalWithExactDiagnostics(t *testing.T) {
	requireDiagnostic(t, "let bad: Ptr<mut Int32> = nil", "nil requires an expected union containing Nil")
	requireDiagnostic(t, "let maybe: Ptr<Int32> | Nil = nil let bad: Ptr<Int32> = maybe", "expected Ptr<Int32>; got Ptr<Int32> | Nil")
}

func TestCheckerRejectsUnknownAccessStrengtheningWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int32 = 1 let reader: Ptr<Unknown> = @value let bad: Ptr<mut Int32> = reader", "Ptr<Unknown> cannot recover writable access as Ptr<mut Int32>")
}

func TestCheckerRejectsNestedUnknownErasureWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int32 = 1 let slot: Ptr<mut Int32> = @value let bad: Ptr<mut Ptr<mut Unknown>> = @slot", "cannot erase a nested pointer slot as Ptr<mut Ptr<mut Unknown>>")
}

func TestCheckerRejectsComposedUnknownRecoveryWithExactDiagnostic(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int8 = 1 let small: Ptr<mut Int8> = @value let bad: Ptr<mut Int64> = small", "expected Ptr<mut Int64>; got Ptr<mut Int8>; erasure and recovery do not compose, bind Ptr<mut Unknown> first")
}

func TestCheckerRequiresAConcretePointerBeforeUnknownDereference(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int32 = 1 let erased: Ptr<Unknown> = @value let bad: Int32 = ^erased", "Ptr<Unknown> cannot be dereferenced; recover a concrete pointer type first")
}

// Null tests: == and != where one side is Nil and the other is Nil
// or a nullable union. The result is Bool and the checked node is normalized
// so the nullable side is always the node's Operand, making nil == maybe
// share the checked shape of maybe == nil.
func TestCheckerChecksNullTestsAndNormalizesCommutedForms(t *testing.T) {
	checked := requireAccepted(t, "let mut maybe: Ptr<Int32> | Nil = nil let equal: Bool = maybe == nil let notEqual: Bool = maybe != nil let commuted: Bool = nil == maybe")
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
	checked := requireAccepted(t, "let same: Bool = nil == nil let different: Bool = nil != nil")
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
	requireDiagnostic(t, "let mut value: Int32 = 1 let node: Ptr<mut Int32> = @value let bad: Bool = node != nil", "nil requires an expected union containing Nil")
	requireDiagnostic(t, "let mut value: Int32 = 1 let node: Ptr<mut Int32> = @value let bad: Bool = node == nil", "nil requires an expected union containing Nil")
	requireDiagnostic(t, "let bad: Bool = 5 == nil", "nil requires an expected union containing Nil")
}

func TestCheckerPointerEqualityAndNullableIdentityRules(t *testing.T) {
	// Identical non-null pointer types compare identity.
	requireAccepted(t, "let mut a: Int32 = 1 let mut b: Int32 = 2 let p: Ptr<Int32> = @a let q: Ptr<Int32> = @a let same: Bool = p == q")
	requireDiagnostic(t, "let mut a: Int32 = 1 let mut b: Int32 = 2 let p: Ptr<Int32> = @a let q: Ptr<mut Int32> = @b let bad: Bool = p == q", "pointer equality requires identical pointer types")
	requireDiagnostic(t, "let mut a: Int32 = 1 let p: Ptr<Int32> = @a let maybe: Ptr<Int32> | Nil = p let bad: Bool = maybe == p", "union equality requires identical operand types; got Ptr<Int32> | Nil and Ptr<Int32>")
}

// Every value-producing expression is a valid condition, including a nullable
// value, and not accepts any value-producing operand.
func TestCheckerAcceptsNullableTruthinessAsCondition(t *testing.T) {
	requireAccepted(t, "let maybe: Ptr<Int32> | Nil = nil if maybe then end")
	requireAccepted(t, "let maybe: Ptr<Int32> | Nil = nil let flag: Bool = !maybe")
}

// Flow narrowing: a null test on a local binding narrows the
// binding's effective read type inside the branch where the test holds.
// `!= nil` proves P in the true branch and Nil in the false branch; `== nil`
// reverses those facts. The commuted spelling narrows identically.
func TestCheckerNarrowsNullTestsByBranch(t *testing.T) {
	requireAccepted(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let result: Int32 = ^maybe end")
	requireAccepted(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if nil != maybe then let result: Int32 = ^maybe end")
	requireAccepted(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = nil if maybe == nil then let noop: Int32 = 0 else let result: Int32 = ^maybe end")
	requireAccepted(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe == nil then let noop: Int32 = 0 elseif flag then let result: Int32 = ^maybe end")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let result: Int32 = ^maybe else let bad: Int32 = ^maybe end", "cannot dereference Nil; ^ requires Ptr<T>")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe == nil then let bad: Int32 = ^maybe end", "cannot dereference Nil; ^ requires Ptr<T>")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe != nil then let noop: Int32 = 0 elseif flag then let bad: Int32 = ^maybe end", "cannot dereference Nil; ^ requires Ptr<T>")
}

func TestCheckerNarrowingIsBranchLocal(t *testing.T) {
	requireAccepted(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let result: Int32 = ^maybe end")
	requireAccepted(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let result: Int32 = ^maybe end if maybe != nil then let result: Int32 = ^maybe end")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let result: Int32 = ^maybe end let bad: Int32 = ^maybe", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value while maybe != nil do maybe = nil end let bad: Int32 = ^maybe", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
}

func TestCheckerRejectsNullableAccessWithoutNarrowing(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value let bad: Int32 = ^maybe", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if (maybe != nil) and flag then let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if flag then let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
}

func TestCheckerRejectsMemberPathNarrowing(t *testing.T) {
	requireDiagnostic(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end let mut first: Node = Node(value = 1, next = nil,) let node: Ptr<mut Node> = @first if node.next != nil then let bad: Int32 = node.next.value end", "only a local binding can be narrowed; bind node.next before testing it")
}

func TestCheckerInvalidatesNarrowingOnAssignmentAndWritableRef(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value if maybe != nil then maybe = nil let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe != nil then if flag then maybe = nil end let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value if maybe != nil then let slot: Ptr<mut Ptr<Int32> | Nil> = @maybe let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value let slot: Ptr<mut Ptr<Int32> | Nil> = @maybe if maybe != nil then let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe != nil then if flag then let slot: Ptr<mut Ptr<Int32> | Nil> = @maybe end let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
}

func TestCheckerPreservesNarrowingAcrossReadOnlyRef(t *testing.T) {
	requireAccepted(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let reader: Ptr<Ptr<Int32> | Nil> = @maybe let result: Int32 = ^maybe end")
}

func TestCheckerReportsRedundantNullTestInsideNarrowedBranch(t *testing.T) {
	// The branch narrows maybe to a plain pointer, which has no Nil member,
	// so the inner null test's nil literal is rejected by the union gate.
	requireDiagnostic(t, "let mut value: Int32 = 1 let maybe: Ptr<Int32> | Nil = @value if maybe != nil then let bad: Bool = maybe != nil end", "nil requires an expected union containing Nil")
}

func TestCheckerNarrowingSurvivesWhileLoopBody(t *testing.T) {
	requireAccepted(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value while maybe != nil do let result: Int32 = ^maybe maybe = nil end")
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe != nil then while flag do maybe = nil end let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
}

func TestCheckerNarrowsFunctionParameters(t *testing.T) {
	requireAccepted(t, "fun read(maybe: Ptr<Int32> | Nil): Int32 do if maybe != nil then return ^maybe end return 0 end")
}

// Nullable object members, member access, and methods on nullable
// receivers.

func TestCheckerAcceptsRecursiveNullableObjectLiteral(t *testing.T) {
	checked := requireAccepted(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end let tail: Node = Node(value = 3, next = nil,)")
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
	requireAccepted(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end\n"+
		"let mut first: Node = Node(value = 1, next = nil,)\n"+
		"let mut second: Node = Node(value = 2, next = nil,)\n"+
		"first.next = @second\n"+
		"let next: Ptr<mut Node> | Nil = first.next\n"+
		"first.next = nil\n")
	requireDiagnostic(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end\n"+
		"let tail: Node = Node(value = 1, next = nil,)\n"+
		"let bad: Ptr<mut Node> = tail.next\n",
		"expected Ptr<mut Node>; got Ptr<mut Node> | Nil")
}

func TestCheckerNullableMemberValueRequiresBindThenTest(t *testing.T) {
	requireDiagnostic(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end\n"+
		"let mut first: Node = Node(value = 1, next = nil,)\n"+
		"let node: Ptr<mut Node> = @first\n"+
		"let bad: Node = node.next.value\n",
		"only a local binding can be narrowed; bind node.next before testing it")
	requireAccepted(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end\n"+
		"let mut first: Node = Node(value = 1, next = nil,)\n"+
		"let node: Ptr<mut Node> = @first\n"+
		"let next: Ptr<mut Node> | Nil = node.next\n"+
		"if next != nil then\n"+
		"    let tail: Node = ^next\n"+
		"end\n")
}

func TestCheckerMethodsOnNullableReceiversRequireNarrowing(t *testing.T) {
	requireAccepted(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end\n"+
		"method Node.read(): Int32 do\n    return self.value\nend\n"+
		"let mut first: Node = Node(value = 1, next = nil,)\n"+
		"let maybe: Ptr<mut Node> | Nil = @first\n"+
		"if maybe != nil then\n    let result: Int32 = maybe.read()\nend\n")
	requireDiagnostic(t, "type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end\n"+
		"method Node.read(): Int32 do\n    return self.value\nend\n"+
		"let mut first: Node = Node(value = 1, next = nil,)\n"+
		"let maybe: Ptr<mut Node> | Nil = @first\n"+
		"let bad: Int32 = maybe.read()\n",
		"Ptr<mut Node> | Nil may be Nil; narrow it before dereferencing")
}

func TestCheckerKeepsByValueRecursionRejectedWithNullableMembers(t *testing.T) {
	requireDiagnostic(t, "type Bad is struct child: Bad, end\n", "object type Bad cannot contain itself by value")
	requireDiagnostic(t, "type Bad is struct child: Bad | Nil, end\n", "object type Bad cannot contain itself by value")
}

// Assignment inside an elseif body invalidates the narrowing even when the
// elseif condition itself narrows nothing and the if has no final else: the
// body's effects must still merge into the pre-test flow.
func TestCheckerElseifWithoutElseStillMergesInvalidation(t *testing.T) {
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe != nil then if flag then maybe = nil end let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
	requireDiagnostic(t, "let mut value: Int32 = 1 let mut maybe: Ptr<Int32> | Nil = @value let flag: Bool = true if maybe != nil then let other: Bool = false if other then let noop: Int32 = 0 elseif flag then maybe = nil end let bad: Int32 = ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
}

// Standalone Nil is rejected even through generic substitution and spawn
// arguments, not only in direct spellings.
func TestCheckerRejectsNilThroughGenericSubstitution(t *testing.T) {
	requireDiagnostic(t, "type Box<T> is struct value: T end let bad: Box<Nil> = Box<Nil>(value = nil)", "Nil is valid only as a member of a union with a non-Nil type")
	requireDiagnostic(t, "type Box<T> is struct value: T end fun unwrap(box: Box<Nil>): Int32 do return 0 end", "Nil is valid only as a member of a union with a non-Nil type")
	requireDiagnostic(t, "fun worker(flag: Nil): Bool do return true end fun run(): Int32 | Error do let task: Task<Bool> = try spawn worker(nil) return 0 end", "Nil is valid only as a member of a union with a non-Nil type")
}

// A branch-established fact survives only on the sole continuing path when
// every alternative terminates with return, break, or continue.
func TestCheckerSoleContinuingPathNarrowing(t *testing.T) {
	requireAccepted(t, "fun f(): Int32 do let mut maybe: Ptr<Int32> | Nil = nil if maybe == nil then return 0 end return ^maybe end")
	requireAccepted(t, "fun f(): Int32 do let mut maybe: Ptr<Int32> | Nil = nil while true do if maybe == nil then break end return ^maybe end return 0 end")
	requireAccepted(t, "fun f(): Int32 do let mut maybe: Ptr<Int32> | Nil = nil let mut total: Int32 = 0 while true do if maybe == nil then continue end total = ^maybe break end return total end")
	requireDiagnostic(t, "fun f(): Int32 do let mut maybe: Ptr<Int32> | Nil = nil if maybe != nil then print(^maybe) end return ^maybe end", "Ptr<Int32> | Nil may be Nil; narrow it before dereferencing")
}
