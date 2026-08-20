package checker

import (
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

func TestResolveTypeUseKeepsWrittenOrderThroughAliases(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Number = Int64 | Int32 value: Number := 1"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if len(declaration.TypeUse.Candidates) != 2 || declaration.TypeUse.Candidates[0].Type != compilerTypes.Int64 {
		t.Fatalf("type use = %#v, want Int64 then Int32", declaration.TypeUse)
	}
}

func TestResolveTypeUseFlattensAliasCandidatesInOuterUnion(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Number = Int64 | Int32 value: Number | Nil := 1"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if len(declaration.TypeUse.Candidates) != 3 || declaration.TypeUse.Candidates[0].Type != compilerTypes.Int64 || declaration.TypeUse.Candidates[1].Type != compilerTypes.Int32 || !compilerTypes.IsNil(declaration.TypeUse.Candidates[2].Type) {
		t.Fatalf("type use = %#v, want Int64, Int32, Nil candidates", declaration.TypeUse)
	}
	if declaration.Source.Node.Kind != UnionInjectionExpression || declaration.Source.Node.OperandType != compilerTypes.Int64 {
		t.Fatalf("source = %#v, want Int64 contextual injection", declaration.Source)
	}
}

func TestResolveTypeUseKeepsNestedPointerCandidates(t *testing.T) {
	checked, err := Check(parseProgram(t, "value: MutPtr<UInt16 | UInt8> := nil"))
	if err == nil {
		t.Fatal("Check accepted a Nil initializer for a non-null pointer")
	}
	if len(checked.Statements) != 0 {
		t.Fatalf("checked statements = %d, want no invalid declaration", len(checked.Statements))
	}
}

func TestCheckUnionContextUsesWrittenOrder(t *testing.T) {
	checked, err := Check(parseProgram(t, "a: UInt8 | UInt16 := 7 b: Int64 | Int32 := 7"))
	if err != nil {
		t.Fatal(err)
	}
	a := checked.Statements[0].(Declaration).Source
	if a.Node.Kind != UnionInjectionExpression || a.Node.OperandType != compilerTypes.UInt8 {
		t.Fatalf("a source = %#v, want UInt8 union injection", a)
	}
	b := checked.Statements[1].(Declaration).Source
	if b.Node.Kind != UnionInjectionExpression || b.Node.OperandType != compilerTypes.Int64 {
		t.Fatalf("b source = %#v, want Int64 union injection", b)
	}
}

func TestCheckUnionContextRetriesAfterRangeFailure(t *testing.T) {
	checked, err := Check(parseProgram(t, "value: UInt8 | UInt16 := 256"))
	if err != nil {
		t.Fatal(err)
	}
	source := checked.Statements[0].(Declaration).Source
	if source.Node.Kind != UnionInjectionExpression || source.Node.OperandType != compilerTypes.UInt16 {
		t.Fatalf("source = %#v, want UInt16 union injection", source)
	}
}

func TestCheckUnionContextRetainsUnknownDiagnostic(t *testing.T) {
	_, err := Check(parseProgram(t, "value: UInt8 | UInt16 := missing + 1"))
	if err == nil || !strings.Contains(err.Error(), "unknown variable missing") {
		t.Fatalf("error = %v, want earliest unknown-variable diagnostic", err)
	}
}

func TestCheckUnionInjectsAlreadyTypedMember(t *testing.T) {
	checked, err := Check(parseProgram(t, "value: Int32 | Bool := true"))
	if err != nil {
		t.Fatal(err)
	}
	source := checked.Statements[0].(Declaration).Source
	if source.Node.Kind != UnionInjectionExpression || source.Node.OperandType != compilerTypes.Bool {
		t.Fatalf("source = %#v, want Bool union injection", source)
	}
}

func TestCheckUnionWidensSourceUnion(t *testing.T) {
	checked, err := Check(parseProgram(t, "small: Int32 | Bool := true wide: Int32 | Bool | Nil := small"))
	if err != nil {
		t.Fatal(err)
	}
	source := checked.Statements[1].(Declaration).Source
	if source.Node.Kind != UnionWidenExpression || len(source.Node.MemberMap) != 2 {
		t.Fatalf("source = %#v, want two-member union widening", source)
	}
}

func TestCheckUnionIsTest(t *testing.T) {
	checked, err := Check(parseProgram(t, "value: Int32 | Float64 := 1 active: Bool := value is Int32"))
	if err != nil {
		t.Fatal(err)
	}
	active := checked.Statements[1].(Declaration).Source
	if active.Node.Kind != UnionTestExpression || active.Node.TestType != compilerTypes.Int32 || active.Type != compilerTypes.Bool {
		t.Fatalf("active test = %#v, want Int32 UnionTestExpression", active)
	}
}

func TestCheckUnionRejectsDuplicateMembers(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"x: Int32 | Nil | Int32 := 0", "union member Int32 appears more than once"},
		{"y: Int32 | Nil | Nil := nil", "union member Nil appears more than once"},
		{"type Bad = Ptr<Int32> | Nil | Nil value: Bad := nil", "union member Nil appears more than once"},
		{"x: Int32 | Float64 | Int32 | Nil := 1", "union member Int32 appears more than once"},
		{"type Score = Int32 x: Int32 | Nil | Score := 0", "union member Int32 appears more than once"},
		{"type MaybeScore = Int32 | Nil x: Bool | MaybeScore | Int32 := true", "union member Int32 appears more than once"},
		{"type M<T> = T | Int32 x: M<Int32> := 0", "union member Int32 appears more than once"},
	} {
		_, err := Check(parseProgram(t, testCase.source))
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("Check(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}

func TestCheckUnionDuplicateDoesNotAlsoReportMemberCount(t *testing.T) {
	_, err := Check(parseProgram(t, "x: Int32 | Int32 := 0"))
	if err == nil {
		t.Fatal("Check accepted a union with a repeated member")
	}
	if !strings.Contains(err.Error(), "union member Int32 appears more than once") {
		t.Fatalf("error = %v, want duplicate-member diagnostic", err)
	}
	if strings.Contains(err.Error(), "at least two distinct members") {
		t.Fatalf("error = %v, must not also report the member-count diagnostic", err)
	}
}

func TestCheckUnionAcceptsDistinctEquivalentWritings(t *testing.T) {
	_, err := Check(parseProgram(t, "x: Int32 | Nil := 0 y: Int32 | Nil := nil z: Nil | Int32 := 0"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckUnionRejectsInvalidIsQueries(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"value: Int32 | Float64 := 1 bad: Bool := value is Bool", "Bool is not a member"},
		{"value: Int32 | Float64 := 1 bad: Bool := value is Int32 | Float64", "one exact member"},
		{"maybe: Int32 | Nil := 1 bad: Bool := maybe is Int32", "redundant"},
	} {
		_, err := Check(parseProgram(t, testCase.source))
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("Check(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}

func TestCheckUnionNullTestAndEquality(t *testing.T) {
	checked, err := Check(parseProgram(t, "value: Int32 | Float64 | Nil := nil present: Bool := value != nil left: Int32 | Bool := true right: Bool | Int32 := false same: Bool := left == right"))
	if err != nil {
		t.Fatal(err)
	}
	if checked.Statements[1].(Declaration).Source.Node.Kind != NullTestExpression {
		t.Fatalf("null test = %#v, want NullTestExpression", checked.Statements[1])
	}
	if checked.Statements[4].(Declaration).Source.Node.Kind != UnionEqualityExpression {
		t.Fatalf("union equality = %#v, want UnionEqualityExpression", checked.Statements[4])
	}
}

func TestCheckUnionRejectsOrdering(t *testing.T) {
	_, err := Check(parseProgram(t, "left: Int32 | Bool := true right: Int32 | Bool := false ordered: Bool := left < right"))
	if err == nil || !strings.Contains(err.Error(), "ordered") {
		t.Fatalf("error = %v, want union ordering diagnostic", err)
	}
}

func TestCheckUnionIsNarrowsIfElseAndElseIf(t *testing.T) {
	// The else arm narrows value to Nil, which is
	// printable but cannot initialize a standalone Nil binding.
	_, err := Check(parseProgram(t, "value: Int32 | Float64 | Nil := 1 if value is Int32 then integer: Int32 := value elseif value != nil then floating: Float64 := value else print(value) end"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckUnionIsNarrowsWhileBody(t *testing.T) {
	_, err := Check(parseProgram(t, "mut value: Int32 | Float64 := 1 while value is Int32 do integer: Int32 := value value = 2 end"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckUnionWritableEscapePreventsNarrowing(t *testing.T) {
	_, err := Check(parseProgram(t, "mut value: Int32 | Bool := true writer: MutPtr<Int32 | Bool> := ref value if value is Int32 then bad: Int32 := value end"))
	if err == nil || !strings.Contains(err.Error(), "cannot be narrowed") {
		t.Fatalf("error = %v, want writable-escape narrowing diagnostic", err)
	}
}

func TestCheckUnionNarrowedReadUsesPayloadNode(t *testing.T) {
	checked, err := Check(parseProgram(t, "value: Int32 | Float64 := 1 if value is Int32 then result: Int32 := value end"))
	if err != nil {
		t.Fatal(err)
	}
	branch := checked.Statements[1].(IfStatement)
	declaration := branch.Then[0].(Declaration)
	if declaration.Source.Node.Kind != UnionPayloadExpression || declaration.Source.Node.OperandType.Union == nil || declaration.Source.Node.ResultType != compilerTypes.Int32 {
		t.Fatalf("narrowed source = %#v, want UnionPayloadExpression for Int32", declaration.Source)
	}
}
