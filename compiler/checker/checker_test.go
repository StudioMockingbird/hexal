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
	tokens, err := lexer.Lex(source)
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
	checked, err := Check(parseProgram(t, "x: Int32 := 13"))
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
	_, err := Check(parseProgram(t, "x: yyy := 13"))
	if err == nil {
		t.Fatal("Check accepted an unknown type")
	}
	if got, want := err.Error(), "[Type Error] unknown type yyy at app.hex:1:4"; got != want {
		t.Fatalf("Check error = %q, want %q", got, want)
	}
}

func TestCheckRejectsOutOfRangeInt32(t *testing.T) {
	_, err := Check(parseProgram(t, "x: Int32 := 2147483648"))
	if err == nil {
		t.Fatal("Check accepted an out-of-range Int32 literal")
	}
}

func TestCheckReportsIndependentErrors(t *testing.T) {
	_, err := Check(parseProgram(t, "x: Bogus := 2147483648"))
	if err == nil {
		t.Fatal("Check accepted invalid type and value")
	}
	want := "[Type Error] unknown type Bogus at app.hex:1:4\n[Type Error] given value is outside the Int32 range at app.hex:1:13"
	if err.Error() != want {
		t.Fatalf("Check errors = %q, want %q", err, want)
	}
}

func TestCheckResolvesBoolDeclaration(t *testing.T) {
	checked, err := Check(parseProgram(t, "enabled: Bool := true"))
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
	checked, err := Check(parseProgram(t, "mask: Int32 := 0xFF"))
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
	_, err := Check(parseProgram(t, "mask: Int32 := 0x80000000"))
	if err == nil {
		t.Fatal("Check accepted an out-of-range hexadecimal Int32 literal")
	}
	if got, want := err.Error(), "[Type Error] given value is outside the Int32 range at app.hex:1:16"; got != want {
		t.Fatalf("Check error = %q, want %q", got, want)
	}
}

func TestCheckTracksAssignmentsInOrder(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut x: Int32 := 13 x = 14"))
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
	checked, err := Check(parseProgram(t, "mut x: Int32 := 1 flag: Bool := true x = flag x = 2"))
	if err == nil {
		t.Fatal("Check accepted a mismatched assignment")
	}
	if got, want := err.Error(), "[Type Error] expected Int32 initializer; got Bool at app.hex:1:42"; got != want {
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
	checked, err := Check(parseProgram(t, "mut x: Int32 := 13 writer: MutPtr<Int32> := ref x alias: Ptr<Int32> := writer y: Int32 := writer.value"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	if got, want := len(checked.Statements), 4; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
	pointer := checked.Statements[1].(Declaration)
	if pointer.Type.Name != "MutPtr<Int32>" || !isAddressOfVariable(pointer.Source.Node, "x") || !pointer.Type.PointeeWritable {
		t.Fatalf("pointer declaration = %#v, want writable MutPtr<Int32> from &x", pointer)
	}
	read := checked.Statements[3].(Declaration)
	if !isDereferenceVariable(read.Source.Node, "writer") || read.Source.Type != compilerTypes.Int32 {
		t.Fatalf("pointer read = %#v, want *writer Int32", read.Source)
	}
}

func TestCheckResolvesNestedPointers(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut x: Int32 := 13 writer: MutPtr<Int32> := ref x writer_pointer: Ptr<MutPtr<Int32>> := ref writer z: Int32 := writer_pointer.value.value"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	declaration := checked.Statements[2].(Declaration)
	if declaration.Type.Name != "Ptr<MutPtr<Int32>>" || !isAddressOfVariable(declaration.Source.Node, "writer") {
		t.Fatalf("nested pointer = %#v, want Ptr<MutPtr<Int32>> from &writer", declaration)
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
		{"x: Int32 := 13 p: Ptr<Int32> := 13", "[Type Error] expected Ptr<Int32> initializer; got Int32 at app.hex:1:33"},
		{"x: Int32 := 13 p: Ptr<Int32> := x.value", "[Type Error] cannot access .value on Int32; expected Ptr<T> at app.hex:1:35"},
		{"mut x: Int32 := 13 p: Ptr<Int32> := ref x q: Ptr<Bool> := p", "[Type Error] expected Ptr<Bool> initializer; got Ptr<Int32> at app.hex:1:59"},
		{"mut x: Int32 := 13 look: Ptr<Int32> := ref x look.value = 42", "[Type Error] cannot write through a read-only pointer look.value at app.hex:1:46"},
		{"x: Int32 := 13 promoted: MutPtr<Int32> := ref x", "[Type Error] expected MutPtr<Int32> initializer; got Ptr<Int32> at app.hex:1:43"},
	} {
		_, err := Check(parseProgram(t, testCase.source))
		if err == nil || err.Error() != testCase.want {
			t.Fatalf("Check(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}
