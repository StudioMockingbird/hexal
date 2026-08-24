package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestParseADTDeclaration(t *testing.T) {
	program := parseOneItem(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end").(TypeDeclaration)
	adt, ok := program.Target.(AdtDefinitionExpression)
	if !ok || len(adt.Variants) != 2 {
		t.Fatalf("target = %#v, want two-variant ADT", program.Target)
	}
	if adt.Variants[0].Name.Lexeme != "Circle" || adt.Variants[0].Payload == nil {
		t.Fatalf("variant 0 = %#v, want Circle with payload", adt.Variants[0])
	}
	if adt.Variants[1].Name.Lexeme != "Square" || adt.Variants[1].Payload == nil {
		t.Fatalf("variant 1 = %#v, want Square with payload", adt.Variants[1])
	}
}

func TestParseADTUnitVariants(t *testing.T) {
	program := parseOneItem(t, "type Direction as | East | West end").(TypeDeclaration)
	adt := program.Target.(AdtDefinitionExpression)
	if adt.Variants[0].Payload != nil || adt.Variants[1].Payload != nil {
		t.Fatalf("variants = %#v, want unit variants", adt.Variants)
	}
}

func TestParseADTRejectsMutablePayloadField(t *testing.T) {
	tokens, err := lexer.Lex("type Shape as | Circle { mut r: Int32 } | Square { a: Int32 } end")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tokens); err == nil || !strings.Contains(err.Error(), "mut") {
		t.Fatalf("Parse error = %v, want mut-rejection diagnostic", err)
	}
}

func TestParseADTRequiresVariantAfterPipe(t *testing.T) {
	tokens, err := lexer.Lex("type Shape as | | Square { a: Int32 } end")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tokens); err == nil {
		t.Fatal("Parse accepted an empty variant")
	}
}

// The obsolete `type Name = | ...` header is rejected with the exact
// migration diagnostic naming the new form.
func TestParseADTObsoleteHeaderIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape = | Circle { r: Int32 } | Square { a: Int32 }")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "ADT declarations use 'type Name as ... end'") {
		t.Fatalf("Parse error = %v, want the obsolete-header diagnostic", err)
	}
}

// The obsolete per-variant `as` payload introducer is rejected with the
// exact migration diagnostic, even though the block itself opens correctly.
func TestParseADTObsoletePerVariantAsIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape as | Circle as { r: Int32 } end")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "ADT payload follows the variant name directly; remove 'as'") {
		t.Fatalf("Parse error = %v, want the obsolete-payload diagnostic", err)
	}
}

// A missing 'end' is rejected with the exact unterminated-block diagnostic.
func TestParseADTMissingEndIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape as | Circle { r: Int32 }")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "expected 'end' after ADT declaration") {
		t.Fatalf("Parse error = %v, want the unterminated-block diagnostic", err)
	}
}

// Object declarations and transparent aliases keep using '=' unaffected by
// the ADT block syntax.
func TestParseObjectAndAliasStillUseEquals(t *testing.T) {
	program := parseOneItem(t, "type Point = { x: Int32, y: Int32 }").(TypeDeclaration)
	if _, ok := program.Target.(ObjectTypeExpression); !ok {
		t.Fatalf("target = %#v, want ObjectTypeExpression", program.Target)
	}
	program = parseOneItem(t, "type Count = Int32").(TypeDeclaration)
	if _, ok := program.Target.(NamedTypeExpression); !ok {
		t.Fatalf("target = %#v, want NamedTypeExpression", program.Target)
	}
}

func TestParseQualifiedVariantConstructor(t *testing.T) {
	tokens, err := lexer.Lex("shape: Shape := Shape.Circle { r = 10 }")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	constructor, ok := program.Statements[0].(Declaration).Initializer.(QualifiedVariantExpression)
	if !ok || constructor.Owner.Lexeme != "Shape" || constructor.Variant.Lexeme != "Circle" || constructor.Payload == nil || len(*constructor.Payload) != 1 {
		t.Fatalf("initializer = %#v, want Shape.Circle constructor", program.Statements[0])
	}
}

func TestParseQualifiedUnitVariantValue(t *testing.T) {
	tokens, err := lexer.Lex("heading: Direction := Direction.North")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	property, ok := program.Statements[0].(Declaration).Initializer.(PropertyExpression)
	if !ok || property.Property.Lexeme != "North" {
		t.Fatalf("initializer = %#v, want qualified unit variant chain", program.Statements[0])
	}
}

func TestParseValueModeMatch(t *testing.T) {
	tokens, err := lexer.Lex("label: Int32 := match ready\n| true then 1\n| false then 0\nend")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	match, ok := program.Statements[0].(Declaration).Initializer.(MatchExpression)
	if !ok || match.TypeMode || len(match.Arms) != 2 {
		t.Fatalf("initializer = %#v, want two-arm value match", program.Statements[0])
	}
	if _, ok := match.Arms[0].Pattern.(BoolPattern); !ok {
		t.Fatalf("arm 0 pattern = %#v, want BoolPattern", match.Arms[0].Pattern)
	}
}

func TestParseTypeModeMatch(t *testing.T) {
	tokens, err := lexer.Lex("area: Int32 := match shape is\n| Shape.Circle then 1\n| Shape.Square then 2\nend")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	match, ok := program.Statements[0].(Declaration).Initializer.(MatchExpression)
	if !ok || !match.TypeMode || len(match.Arms) != 2 {
		t.Fatalf("initializer = %#v, want two-arm type match", program.Statements[0])
	}
	if _, ok := match.Arms[0].Pattern.(VariantPattern); !ok {
		t.Fatalf("arm 0 pattern = %#v, want VariantPattern", match.Arms[0].Pattern)
	}
}

func TestParseMatchScrutineeWithIsRequiresParens(t *testing.T) {
	tokens, err := lexer.Lex("label: Int32 := match (value is Int32)\n| true then 1\n| false then 0\nend")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tokens); err != nil {
		t.Fatalf("Parse error = %v, want parenthesized scrutinee accepted", err)
	}
}

func TestParseMatchParenthesizedPipeIsBitwiseOr(t *testing.T) {
	tokens, err := lexer.Lex("r: Int32 := match (mask | flag)\n| true then 1\n| false then 0\nend")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	match, ok := program.Statements[0].(Declaration).Initializer.(MatchExpression)
	if !ok || match.TypeMode || len(match.Arms) != 2 {
		t.Fatalf("initializer = %#v, want two-arm value match", program.Statements[0])
	}
	binary, ok := match.Scrutinee.(BinaryExpression)
	if !ok || binary.Operator.Kind != lexer.Pipe {
		t.Fatalf("scrutinee = %#v, want parenthesized bitwise-or", match.Scrutinee)
	}
}

func TestParseMatchScrutineeAndOrExpressions(t *testing.T) {
	tokens, err := lexer.Lex("r: Int32 := match ready and enabled\n| true then 1\n| false then 0\nend")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	match, ok := program.Statements[0].(Declaration).Initializer.(MatchExpression)
	if !ok {
		t.Fatalf("initializer = %#v, want match expression", program.Statements[0])
	}
	if _, ok := match.Scrutinee.(BinaryExpression); !ok {
		t.Fatalf("scrutinee = %#v, want and expression", match.Scrutinee)
	}
}

func TestParseNestedMatchOwnsItsBoundary(t *testing.T) {
	source := "r: Int32 := match x is\n| Int32 then match y\n    | true then 1\n    | false then 0\n    end\n| else then 0\nend\n"
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	outer, ok := program.Statements[0].(Declaration).Initializer.(MatchExpression)
	if !ok || len(outer.Arms) != 2 {
		t.Fatalf("initializer = %#v, want two-arm outer match", program.Statements[0])
	}
	if _, ok := outer.Arms[0].Expression.(MatchExpression); !ok {
		t.Fatalf("arm 0 expression = %#v, want nested match", outer.Arms[0].Expression)
	}
}
