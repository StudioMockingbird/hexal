package checker

import (
	"go/constant"
	"testing"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func parseProgram(t *testing.T, source string) parser.Program {
	t.Helper()
	tokens, err := lexer.Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	return program
}

func TestCheckResolvesInt32Declaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "let x: Int32 = 13"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Name != "x" {
		t.Fatalf("name = %q, want %q", declaration.Name, "x")
	}
	if declaration.Type != compilerTypes.Int32 {
		t.Fatalf("type = %#v, want %#v", declaration.Type, compilerTypes.Int32)
	}
	if got := constantInt64(declaration.Source); got != 13 || declaration.Source.Kind != ConstantOperand {
		t.Fatalf("source = %#v, want constant 13", declaration.Source)
	}
}

func TestCheckRejectsUnknownType(t *testing.T) {
	_, err := Check(parseProgram(t, "let x: yyy = 13"))
	if err == nil {
		t.Fatal("Check accepted an unknown type")
	}
	if got, want := err.Error(), "[Type Error type.unknown-type] unknown type yyy at app.hex:1:8"; got != want {
		t.Fatalf("Check error = %q, want %q", got, want)
	}
}

func TestCheckRejectsOutOfRangeInt32(t *testing.T) {
	_, err := Check(parseProgram(t, "let x: Int32 = 2147483648"))
	if err == nil {
		t.Fatal("Check accepted an out-of-range Int32 literal")
	}
}

func TestCheckReportsIndependentErrors(t *testing.T) {
	_, err := Check(parseProgram(t, "let x: Bogus = 2147483648"))
	if err == nil {
		t.Fatal("Check accepted invalid type and value")
	}
	want := "[Type Error type.unknown-type] unknown type Bogus at app.hex:1:8\n[Type Error type.numeric-literal-out-of-range] given value is outside the Int32 range at app.hex:1:16"
	if err.Error() != want {
		t.Fatalf("Check errors = %q, want %q", err, want)
	}
}

func TestCheckResolvesBoolDeclaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "let enabled: Bool = true"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Type != compilerTypes.Bool {
		t.Fatalf("type = %#v, want %#v", declaration.Type, compilerTypes.Bool)
	}
	if got, want := declaration.Source.Literal, "true"; got != want {
		t.Fatalf("literal = %q, want %q", got, want)
	}
}

func TestCheckResolvesHexadecimalInt32Declaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "let mask: Int32 = 0xFF"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	declaration := checked.Statements[0].(Declaration)
	if got, _ := constant.Int64Val(declaration.Source.Constant); got != 255 {
		t.Fatalf("value = %d, want 255", got)
	}
	if got, want := declaration.Source.Literal, "0xFF"; got != want {
		t.Fatalf("literal = %q, want %q", got, want)
	}
}

func TestCheckRejectsOutOfRangeHex(t *testing.T) {
	_, err := Check(parseProgram(t, "let mask: Int32 = 0x80000000"))
	if err == nil {
		t.Fatal("Check accepted an out-of-range hexadecimal Int32 literal")
	}
	if got, want := err.Error(), "[Type Error type.numeric-literal-out-of-range] given value is outside the Int32 range at app.hex:1:19"; got != want {
		t.Fatalf("Check error = %q, want %q", got, want)
	}
}

func TestCheckTracksAssignmentsInOrder(t *testing.T) {
	checked, err := Check(parseProgram(t, "let mut x: Int32 = 13 x = 14"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	if got, want := len(checked.Statements), 2; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
	assignment, ok := checked.Statements[1].(Assignment)
	if !ok || assignment.Name != "x" || constantInt64(assignment.Source) != 14 {
		t.Fatalf("assignment = %#v, want x = 14", checked.Statements[1])
	}
}

func TestCheckKeepsEnvironmentAfterFailedAssignment(t *testing.T) {
	checked, err := Check(parseProgram(t, "let mut x: Int32 = 1 let flag: Bool = true x = flag x = 2"))
	if err == nil {
		t.Fatal("Check accepted a mismatched assignment")
	}
	if got, want := err.Error(), "[Type Error type.initializer-type-mismatch] expected Int32 initializer; got Bool at app.hex:1:48"; got != want {
		t.Fatalf("Check error = %q, want %q", got, want)
	}
	if got, want := len(checked.Statements), 3; got != want {
		t.Fatalf("checked statement count = %d, want %d", got, want)
	}
	assignment := checked.Statements[2].(Assignment)
	if constantInt64(assignment.Source) != 2 {
		t.Fatalf("final assignment value = %d, want 2", constantInt64(assignment.Source))
	}
}

func constantInt64(source Operand) int64 {
	value, _ := constant.Int64Val(source.Constant)
	return value
}

func isAddressOfVariable(expression Expression, name string) bool {
	return expression.Kind == AddressOfExpression && expression.Operand != nil &&
		expression.Operand.Kind == VariableExpression && expression.Operand.Name == name
}

func isDereferenceVariable(expression Expression, name string) bool {
	return expression.Kind == DereferenceExpression && expression.Operand != nil &&
		expression.Operand.Kind == VariableExpression && expression.Operand.Name == name
}

func isNestedDereference(expression Expression, name string) bool {
	return expression.Kind == DereferenceExpression && expression.Operand != nil &&
		expression.Operand.Kind == DereferenceExpression && expression.Operand.Operand != nil &&
		expression.Operand.Operand.Kind == VariableExpression && expression.Operand.Operand.Name == name
}

func TestCheckResolvesPointerExpressions(t *testing.T) {
	checked, err := Check(parseProgram(t, "let mut x: Int32 = 13 let writer: Ptr<mut Int32> = @x let alias: Ptr<Int32> = writer let y: Int32 = ^writer"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	if got, want := len(checked.Statements), 4; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
	pointer := checked.Statements[1].(Declaration)
	if pointer.Type.Name != "Ptr<mut Int32>" || !isAddressOfVariable(pointer.Source.Node, "x") || !pointer.Type.PointeeWritable {
		t.Fatalf("pointer declaration = %#v, want writable Ptr<mut Int32> from &x", pointer)
	}
	read := checked.Statements[3].(Declaration)
	if !isDereferenceVariable(read.Source.Node, "writer") || read.Source.Type != compilerTypes.Int32 {
		t.Fatalf("pointer read = %#v, want *writer Int32", read.Source)
	}
}

func TestCheckResolvesNestedPointers(t *testing.T) {
	checked, err := Check(parseProgram(t, "let mut x: Int32 = 13 let writer: Ptr<mut Int32> = @x let writer_pointer: Ptr<Ptr<mut Int32>> = @writer let z: Int32 = ^(^writer_pointer)"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	declaration := checked.Statements[2].(Declaration)
	if declaration.Type.Name != "Ptr<Ptr<mut Int32>>" || !isAddressOfVariable(declaration.Source.Node, "writer") {
		t.Fatalf("nested pointer = %#v, want Ptr<Ptr<mut Int32>> from &writer", declaration)
	}
	value := checked.Statements[3].(Declaration)
	if !isNestedDereference(value.Source.Node, "writer_pointer") || value.Source.Type != compilerTypes.Int32 {
		t.Fatalf("nested dereference = %#v, want *(*writer_pointer) Int32", value.Source)
	}
}

func TestCheckPointerDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let x: Int32 = 13 let p: Ptr<Int32> = 13", "[Type Error type.initializer-type-mismatch] expected Ptr<Int32> initializer; got Int32 at app.hex:1:39"},
		{"let x: Int32 = 13 let p: Ptr<Int32> = ^x", "[Type Error type.dereference-requires-pointer] cannot dereference Int32; ^ requires Ptr<T> at app.hex:1:39"},
		{"let mut x: Int32 = 13 let p: Ptr<Int32> = @x let q: Ptr<Bool> = p", "[Type Error type.initializer-type-mismatch] expected Ptr<Bool> initializer; got Ptr<Int32> at app.hex:1:65"},
		{"let mut x: Int32 = 13 let look: Ptr<Int32> = @x ^look = 42", "[Type Error type.cannot-write-through-read-only-pointer] cannot write through a read-only pointer ^look at app.hex:1:49"},
		{"let x: Int32 = 13 let promoted: Ptr<mut Int32> = @x", "[Type Error type.initializer-type-mismatch] expected Ptr<mut Int32> initializer; got Ptr<Int32> at app.hex:1:50"},
	} {
		_, err := Check(parseProgram(t, testCase.source))
		if err == nil || err.Error() != testCase.want {
			t.Fatalf("Check(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}
