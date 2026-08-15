package parser

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestParseNamedTypeExpression(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 = 13")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	declaration := program.Statements[0].(Declaration)
	typeName, ok := declaration.Type.(NamedTypeExpression)
	if !ok || typeName.Name.Lexeme != "Int32" {
		t.Fatalf("type = %#v, want named Int32", declaration.Type)
	}
}

// Standalone `nothing: Nil = nil` appears here as syntax only; the checker
// rejects the written standalone Nil type.
func TestParseBuiltinNilAndUnknownTypeExpressions(t *testing.T) {
	if got := fmt.Sprintf("%T", parseAnnotation(t, "nothing: Nil = nil")); got != "parser.NilTypeExpression" {
		t.Fatalf("Nil annotation type = %q, want parser.NilTypeExpression", got)
	}
	if got := fmt.Sprintf("%T", parseAnnotation(t, "opaque: Unknown = value")); got != "parser.UnknownTypeExpression" {
		t.Fatalf("Unknown annotation type = %q, want parser.UnknownTypeExpression", got)
	}

	pointer, ok := parseAnnotation(t, "erased: Ptr<Unknown> = value").(PtrTypeExpression)
	if !ok {
		t.Fatalf("Ptr<Unknown> annotation = %#v, want pointer type", pointer)
	}
	if got := fmt.Sprintf("%T", pointer.Element); got != "parser.UnknownTypeExpression" {
		t.Fatalf("Ptr<Unknown> element type = %q, want parser.UnknownTypeExpression", got)
	}
}

func TestParseNullableTypeExpressionPreservesNestedAndRepeatedSuffixes(t *testing.T) {
	typeExpression := parseAnnotation(t, "nested: MutPtr<MutPtr<Node> | Nil> | Nil | Nil = nil")
	outer, ok := typeExpression.(UnionTypeExpression)
	if !ok || len(outer.Pipes) != 2 || len(outer.Members) != 3 {
		t.Fatalf("outer union = %#v, want 3 members and 2 pipes", typeExpression)
	}
	outerPointer, ok := outer.Members[0].(PtrTypeExpression)
	if !ok || !outerPointer.Writable {
		t.Fatalf("outer member = %#v, want writable pointer type", outer.Members[0])
	}
	inner, ok := outerPointer.Element.(UnionTypeExpression)
	if !ok || len(inner.Pipes) != 1 || len(inner.Members) != 2 {
		t.Fatalf("inner union = %#v, want 2 members and 1 pipe", outerPointer.Element)
	}
	innerPointer, ok := inner.Members[0].(PtrTypeExpression)
	if !ok || !innerPointer.Writable {
		t.Fatalf("inner member = %#v, want writable pointer type", inner.Members[0])
	}
	if named, ok := innerPointer.Element.(NamedTypeExpression); !ok || named.Name.Lexeme != "Node" {
		t.Fatalf("inner pointer element = %#v, want named Node", innerPointer.Element)
	}

	if got := fmt.Sprintf("%T", parseInitializer(t, "nothing: Nil = nil")); got != "parser.NilLiteral" {
		t.Fatalf("nil initializer type = %q, want parser.NilLiteral", got)
	}
}

func TestParseGeneralUnionTypeExpression(t *testing.T) {
	typeExpression := parseAnnotation(t, "value: (Int32 | Float64) | Nil = nil")
	if got := fmt.Sprintf("%T", typeExpression); got != "parser.UnionTypeExpression" {
		t.Fatalf("type expression = %q, want parser.UnionTypeExpression", got)
	}
	value := reflect.ValueOf(typeExpression)
	if got := value.FieldByName("Members").Len(); got != 2 {
		t.Fatalf("union member count = %d, want 2", got)
	}
	if got := fmt.Sprintf("%T", value.FieldByName("Members").Index(0).Interface()); got != "parser.GroupedTypeExpression" {
		t.Fatalf("first member = %q, want parser.GroupedTypeExpression", got)
	}
}

func TestParseTypeTestExpression(t *testing.T) {
	typeExpression := parseInitializer(t, "tested: Bool = value is Int32")
	if got := fmt.Sprintf("%T", typeExpression); got != "parser.TypeTestExpression" {
		t.Fatalf("initializer = %q, want parser.TypeTestExpression", got)
	}
}

func TestParseTypeTestPrecedence(t *testing.T) {
	initializer := parseInitializer(t, "tested: Bool = value is Int32 == expected")
	if got := fmt.Sprintf("%T", initializer); got != "parser.BinaryExpression" {
		t.Fatalf("initializer = %q, want parser.BinaryExpression", got)
	}
	value := reflect.ValueOf(initializer)
	if got := value.FieldByName("Left").Interface(); fmt.Sprintf("%T", got) != "parser.TypeTestExpression" {
		t.Fatalf("left operand = %T, want parser.TypeTestExpression", got)
	}
}

func TestParseRejectsChainedTypeTests(t *testing.T) {
	message := parseError(t, "tested: Bool = value is Int32 is Bool")
	if !strings.Contains(message, "is tests cannot be chained") {
		t.Fatalf("Parse error = %q, want chained-is diagnostic", message)
	}
}

func TestParseGeneralUnionAcceptsAnyMemberOrder(t *testing.T) {
	for _, source := range []string{
		"value: Nil | Ptr<Int32> = nil",
		"value: Ptr<Int32> | MutPtr<Int32> = nil",
	} {
		if _, err := Parse(mustLex(t, source)); err != nil {
			t.Errorf("Parse(%q) returned an error: %v", source, err)
		}
	}
}

func mustLex(t *testing.T, source string) []lexer.Token {
	t.Helper()
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("Lex(%q) returned an error: %v", source, err)
	}
	return tokens
}

func TestParsePtrTypeExpressionIsRecursive(t *testing.T) {
	tokens, err := lexer.Lex("x: Ptr<Ptr<Int32>> = y")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	outer, ok := program.Statements[0].(Declaration).Type.(PtrTypeExpression)
	if !ok {
		t.Fatalf("type = %#v, want outer pointer type", program.Statements[0])
	}
	if _, ok := outer.Element.(PtrTypeExpression); !ok {
		t.Fatalf("pointer element = %#v, want nested pointer type", outer.Element)
	}
}

func TestParseObjectTypeExpressionOnlyAfterTypeDeclaration(t *testing.T) {
	tokens, err := lexer.Lex("type Point = { mut x: Int32, y: Ptr<Int32>, }")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration, ok := program.Items[0].(TypeDeclaration)
	if !ok {
		t.Fatalf("item = %#v, want type declaration", program.Items[0])
	}
	object, ok := declaration.Target.(ObjectTypeExpression)
	if !ok || len(object.Members) != 2 {
		t.Fatalf("target = %#v, want object type with two members", declaration.Target)
	}
	if !object.Members[0].Mutable || object.Members[0].Name.Lexeme != "x" {
		t.Fatalf("member 0 = %#v, want mutable x", object.Members[0])
	}
	if object.Members[1].Mutable || object.Members[1].Name.Lexeme != "y" {
		t.Fatalf("member 1 = %#v, want read-only y", object.Members[1])
	}
	if _, ok := object.Members[1].Type.(PtrTypeExpression); !ok {
		t.Fatalf("member 1 type = %#v, want pointer type", object.Members[1].Type)
	}
}

func TestParseRejectsObjectTypeOutsideTypeDeclaration(t *testing.T) {
	for _, source := range []string{
		"point: { x: Int32 } = value",
		"point: { x: Int32 } | Nil = value",
		"type Box = Ptr<{ x: Int32 }>",
		"type Box = Ptr<{ x: Int32 } | Nil>",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Fatalf("Parse accepted object type outside direct type declaration in %q", source)
		}
	}
}

func TestParseRejectsEmptyObjectType(t *testing.T) {
	tokens, err := lexer.Lex("type Empty = {}")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	_, err = Parse(tokens)
	if err == nil || err.Error() != "[Syntax Error] an object type must declare at least one member at 1:15" {
		t.Fatalf("Parse error = %v, want empty-object-type diagnostic", err)
	}
}

func TestParseRejectsMalformedPtrType(t *testing.T) {
	for _, source := range []string{
		"x: Ptr<Int32 = y",
		"x: Ptr<> = y",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Fatalf("Parse accepted malformed pointer type %q", source)
		}
	}
}

func TestParseTypeDeclarationIsTopLevelItem(t *testing.T) {
	tokens, err := lexer.Lex("type Coordinate = Ptr<Ptr<Int32>>")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if len(program.Items) != 1 || len(program.Statements) != 0 {
		t.Fatalf("program items/statements = %d/%d, want 1/0", len(program.Items), len(program.Statements))
	}
	declaration, ok := program.Items[0].(TypeDeclaration)
	if !ok || declaration.Name.Lexeme != "Coordinate" {
		t.Fatalf("item = %#v, want Coordinate type declaration", program.Items[0])
	}
	outer, ok := declaration.Target.(PtrTypeExpression)
	if !ok {
		t.Fatalf("target = %#v, want pointer type", declaration.Target)
	}
	if _, ok := outer.Element.(PtrTypeExpression); !ok {
		t.Fatalf("target element = %#v, want nested pointer type", outer.Element)
	}
}

func TestParseMixedTopLevelItemsPreservesOrder(t *testing.T) {
	tokens, err := lexer.Lex("type Coordinate = Int32 x: Coordinate = 1")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if len(program.Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(program.Items))
	}
	if _, ok := program.Items[0].(TypeDeclaration); !ok {
		t.Fatalf("item 0 = %T, want type declaration", program.Items[0])
	}
	if _, ok := program.Items[1].(Declaration); !ok {
		t.Fatalf("item 1 = %T, want value declaration", program.Items[1])
	}
}

func TestParseMutPtrTypeExpression(t *testing.T) {
	for _, testCase := range []struct {
		source   string
		writable bool
	}{
		{"x: MutPtr<Int32> = y", true},
		{"x: MutPtr<MutPtr<Int32>> = y", true},
		{"x: Ptr<MutPtr<Int32>> = y", false},
		{"x: MutPtr<Ptr<Int32>> = y", true},
	} {
		tokens, err := lexer.Lex(testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		program, err := Parse(tokens)
		if err != nil {
			t.Fatalf("Parse(%q) returned an error: %v", testCase.source, err)
		}
		outer, ok := program.Statements[0].(Declaration).Type.(PtrTypeExpression)
		if !ok {
			t.Fatalf("type for %q = %#v, want pointer type", testCase.source, program.Statements[0])
		}
		if outer.Writable != testCase.writable {
			t.Fatalf("writable for %q = %v, want %v", testCase.source, outer.Writable, testCase.writable)
		}
	}
}

func TestParseRejectsMutInsidePtr(t *testing.T) {
	tokens, err := lexer.Lex("x: Ptr<mut Int32> = y")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	_, err = Parse(tokens)
	if err == nil || err.Error() != "[Syntax Error] mut is not allowed inside Ptr<...>; use MutPtr<...> at 1:8" {
		t.Fatalf("Parse error = %v, want focused mut-inside-Ptr diagnostic", err)
	}
}

// Fun<...> type expressions: `Fun` is an ordinary identifier lexeme, not a
// keyword, so it is matched the same way Ptr and MutPtr are.

func parseAnnotation(t *testing.T, source string) TypeExpression {
	t.Helper()
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("Lex(%q) returned an error: %v", source, err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse(%q) returned an error: %v", source, err)
	}
	return program.Statements[0].(Declaration).Type
}

func TestParseFunctionTypeExpression(t *testing.T) {
	function, ok := parseAnnotation(t, "handler: Fun<(Int32, Bool) : Int32> = f").(FunctionTypeExpression)
	if !ok {
		t.Fatal("annotation is not a FunctionTypeExpression")
	}
	if len(function.Parameters) != 2 {
		t.Fatalf("parameter count = %d, want 2", len(function.Parameters))
	}
	if named, ok := function.Parameters[1].(NamedTypeExpression); !ok || named.Name.Lexeme != "Bool" {
		t.Fatalf("second parameter = %#v, want Bool", function.Parameters[1])
	}
	if named, ok := function.Return.(NamedTypeExpression); !ok || named.Name.Lexeme != "Int32" {
		t.Fatalf("return = %#v, want Int32", function.Return)
	}
}

func TestParseFunctionTypeWithoutReturn(t *testing.T) {
	function := parseAnnotation(t, "handler: Fun<(Int32)> = f").(FunctionTypeExpression)
	if function.Return != nil {
		t.Fatalf("return = %#v, want nil", function.Return)
	}
	if len(function.Parameters) != 1 {
		t.Fatalf("parameter count = %d, want 1", len(function.Parameters))
	}
}

func TestParseFunctionTypeWithoutParameters(t *testing.T) {
	function := parseAnnotation(t, "handler: Fun<()> = f").(FunctionTypeExpression)
	if len(function.Parameters) != 0 {
		t.Fatalf("parameter count = %d, want 0", len(function.Parameters))
	}
	if function.Return != nil {
		t.Fatalf("return = %#v, want nil", function.Return)
	}
}

func TestParseNestedFunctionType(t *testing.T) {
	function := parseAnnotation(t, "handler: Fun<(Fun<(Int32) : Int32>) : Int32> = f").(FunctionTypeExpression)
	inner, ok := function.Parameters[0].(FunctionTypeExpression)
	if !ok {
		t.Fatalf("first parameter = %#v, want a nested function type", function.Parameters[0])
	}
	if named, ok := inner.Return.(NamedTypeExpression); !ok || named.Name.Lexeme != "Int32" {
		t.Fatalf("nested return = %#v, want Int32", inner.Return)
	}
}

func TestParseRejectsMalformedFunctionTypes(t *testing.T) {
	for _, source := range []string{
		"handler: Fun<Int32> = f",
		"handler: Fun<(Int32) : Int32 = f",
		"handler: Fun(Int32) = f",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Errorf("Parse(%q) accepted a malformed Fun type", source)
		}
	}
}
