package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestParseGenericTypeDeclarationParameters(t *testing.T) {
	program := parseOneItem(t, "type Box<T> = { value: T }").(TypeDeclaration)
	if len(program.Parameters) != 1 || program.Parameters[0].Lexeme != "T" {
		t.Fatalf("parameters = %#v, want [T]", program.Parameters)
	}
	if _, ok := program.Target.(ObjectTypeExpression); !ok {
		t.Fatalf("target = %#v, want object type", program.Target)
	}
}

func TestParseGenericAliasParameters(t *testing.T) {
	program := parseOneItem(t, "type Pointer<T> = Ptr<T>").(TypeDeclaration)
	if len(program.Parameters) != 1 || program.Parameters[0].Lexeme != "T" {
		t.Fatalf("parameters = %#v, want [T]", program.Parameters)
	}
}

func TestParseGenericTypeExpression(t *testing.T) {
	typeExpression := parseAnnotation(t, "box: Box<Int32> = value")
	generic, ok := typeExpression.(GenericTypeExpression)
	if !ok || generic.Name.Lexeme != "Box" || len(generic.Arguments) != 1 {
		t.Fatalf("type expression = %#v, want Box<Int32>", typeExpression)
	}
	if named, ok := generic.Arguments[0].(NamedTypeExpression); !ok || named.Name.Lexeme != "Int32" {
		t.Fatalf("argument = %#v, want Int32", generic.Arguments[0])
	}
}

func TestParseNestedGenericTypeExpression(t *testing.T) {
	typeExpression := parseAnnotation(t, "box: Ptr<Box<Pair<Int32, Bool>>> = value")
	pointer, ok := typeExpression.(PtrTypeExpression)
	if !ok {
		t.Fatalf("type expression = %#v, want Ptr", typeExpression)
	}
	if _, ok := pointer.Element.(GenericTypeExpression); !ok {
		t.Fatalf("element = %#v, want generic type", pointer.Element)
	}
}

func TestParseGenericTypeExpressionRequiresArgument(t *testing.T) {
	tokens, err := lexer.Lex("box: Box<> = value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tokens); err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("Parse error = %v, want empty-argument diagnostic", err)
	}
}

func TestParseGenericFunctionDeclaration(t *testing.T) {
	program := parseOneItem(t, "fun identity<T>(value: T): T\nreturn value\nend").(FunctionDeclaration)
	if len(program.TypeParameters) != 1 || program.TypeParameters[0].Lexeme != "T" {
		t.Fatalf("type parameters = %#v, want [T]", program.TypeParameters)
	}
}

func TestParseGenericMethodDeclaration(t *testing.T) {
	program := parseOneItem(t, "impl Box<T>.same<U>(other: U): Bool\nreturn false\nend").(ImplDeclaration)
	if len(program.TypeParameters) != 1 || program.TypeParameters[0].Lexeme != "U" {
		t.Fatalf("method type parameters = %#v, want [U]", program.TypeParameters)
	}
	if _, ok := program.SelfType.(GenericTypeExpression); !ok {
		t.Fatalf("self type = %#v, want generic receiver", program.SelfType)
	}
}

func TestParseGenericCallSuffix(t *testing.T) {
	tokens, err := lexer.Lex("result: Int64 = identity<Int64>(42)")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := program.Statements[0].(Declaration).Initializer.(CallExpression)
	if !ok || len(call.TypeArguments) != 1 {
		t.Fatalf("initializer = %#v, want call with one type argument", program.Statements[0])
	}
}

func TestParseGenericMethodCallSuffix(t *testing.T) {
	tokens, err := lexer.Lex("box.same<Bool>(other)")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := program.Statements[0].(CallExpression)
	if !ok || len(call.TypeArguments) != 1 {
		t.Fatalf("statement = %#v, want call with one type argument", program.Statements[0])
	}
}

func TestParseRelationalLessIsNotGenericSuffix(t *testing.T) {
	tokens, err := lexer.Lex("flag: Bool = left < right")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := program.Statements[0].(Declaration).Initializer.(BinaryExpression); !ok {
		t.Fatalf("initializer = %#v, want relational binary expression", program.Statements[0])
	}
}

func TestParseGenericObjectLiteral(t *testing.T) {
	tokens, err := lexer.Lex("box: Box<Int32> = Box<Int32> { value = 42 }")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	literal, ok := program.Statements[0].(Declaration).Initializer.(ObjectLiteral)
	if !ok || len(literal.TypeArguments) != 1 {
		t.Fatalf("initializer = %#v, want object literal with one type argument", program.Statements[0])
	}
}
