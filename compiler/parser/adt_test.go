package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestParseADTDeclaration(t *testing.T) {
	program := parseOneItem(t, "type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end").(TypeDeclaration)
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
	program := parseOneItem(t, "type Direction is union | East | West end").(TypeDeclaration)
	adt := program.Target.(AdtDefinitionExpression)
	if adt.Variants[0].Payload != nil || adt.Variants[1].Payload != nil {
		t.Fatalf("variants = %#v, want unit variants", adt.Variants)
	}
}

func TestParseAllUnitADTShorthand(t *testing.T) {
	program := parseOneItem(t, "type Direction is East | West end").(TypeDeclaration)
	adt, ok := program.Target.(AdtDefinitionExpression)
	if !ok || len(adt.Variants) != 2 {
		t.Fatalf("target = %#v, want a two-variant ADT", program.Target)
	}
	if adt.Variants[0].Name.Lexeme != "East" || adt.Variants[0].Payload != nil {
		t.Fatalf("variant 0 = %#v, want unit variant East", adt.Variants[0])
	}
	if adt.Variants[1].Name.Lexeme != "West" || adt.Variants[1].Payload != nil {
		t.Fatalf("variant 1 = %#v, want unit variant West", adt.Variants[1])
	}
}

func TestParseADTRejectsMutablePayloadField(t *testing.T) {
	tokens, err := lexer.Lex("type Shape is union | Circle as mut r: Int32 end | Square as a: Int32 end end")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tokens); err == nil || !strings.Contains(err.Error(), "mut") {
		t.Fatalf("Parse error = %v, want mut-rejection diagnostic", err)
	}
}

func TestParseADTRequiresVariantAfterPipe(t *testing.T) {
	tokens, err := lexer.Lex("type Shape is union | | Square as a: Int32 end end")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(tokens); err == nil {
		t.Fatal("Parse accepted an empty variant")
	}
}

// The obsolete `type Name = ...` header is rejected with the exact migration
// diagnostic naming the new form.
func TestParseTypeObsoleteEqualsHeaderIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape = Int32")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "type declarations use 'is', not '='") {
		t.Fatalf("Parse error = %v, want the obsolete-header diagnostic", err)
	}
}

// The obsolete `type Name as ... end` header is rejected with the exact
// migration diagnostic naming the new form.
func TestParseADTObsoleteAsHeaderIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape as | Circle { r: Int32 } | Square { a: Int32 } end")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "ADT declarations use 'type Name is union ... end'") {
		t.Fatalf("Parse error = %v, want the obsolete-header diagnostic", err)
	}
}

// Legacy brace payload syntax after an ADT variant is rejected with the exact
// migration diagnostic.
func TestParseADTObsoleteBracePayloadIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape is union | Circle { r: Int32 } end")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "ADT payloads use 'as ... end', not braces") {
		t.Fatalf("Parse error = %v, want the obsolete-payload diagnostic", err)
	}
}

// A missing 'end' is rejected with the exact unterminated-block diagnostic.
func TestParseADTMissingEndIsRejected(t *testing.T) {
	tokens, err := lexer.Lex("type Shape is union | Circle as r: Int32 end")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "expected 'end' after ADT declaration") {
		t.Fatalf("Parse error = %v, want the unterminated-block diagnostic", err)
	}
}

// Struct declarations and transparent aliases use 'is', not '='.
func TestParseStructAndAliasUseIs(t *testing.T) {
	program := parseOneItem(t, "type Point is struct x: Int32, y: Int32 end").(TypeDeclaration)
	if _, ok := program.Target.(ObjectTypeExpression); !ok {
		t.Fatalf("target = %#v, want ObjectTypeExpression", program.Target)
	}
	program = parseOneItem(t, "type Count is Int32").(TypeDeclaration)
	if _, ok := program.Target.(NamedTypeExpression); !ok {
		t.Fatalf("target = %#v, want NamedTypeExpression", program.Target)
	}
}

func TestParseEmptyStruct(t *testing.T) {
	program := parseOneItem(t, "type Marker is struct end").(TypeDeclaration)
	object, ok := program.Target.(ObjectTypeExpression)
	if !ok || len(object.Members) != 0 {
		t.Fatalf("target = %#v, want an empty ObjectTypeExpression", program.Target)
	}
}

func TestParseQualifiedVariantConstructor(t *testing.T) {
	tokens, err := lexer.Lex("shape: Shape := Shape.Circle(r = 10)")
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	constructor, ok := program.Statements[0].(Declaration).Initializer.(CallExpression)
	if !ok {
		t.Fatalf("initializer = %#v, want Shape.Circle constructor", program.Statements[0])
	}
	property, ok := constructor.Callee.(PropertyExpression)
	if !ok || property.Property.Lexeme != "Circle" || len(constructor.Arguments) != 1 {
		t.Fatalf("callee = %#v, want Shape.Circle(r = 10)", constructor.Callee)
	}
	if len(constructor.ArgumentLabels) != 1 || constructor.ArgumentLabels[0] == nil || constructor.ArgumentLabels[0].Lexeme != "r" {
		t.Fatalf("argument labels = %#v, want [r]", constructor.ArgumentLabels)
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
	dotted, ok := match.Arms[0].Pattern.(DottedPattern)
	if !ok {
		t.Fatalf("arm 0 pattern = %#v, want neutral DottedPattern", match.Arms[0].Pattern)
	}
	if dotted.Owner.Lexeme != "Shape" || dotted.Name.Lexeme != "Circle" {
		t.Fatalf("arm 0 pattern = %#v, want Shape.Circle preserved verbatim", match.Arms[0].Pattern)
	}
}

func TestParseGenericVariantMatchStaysClassified(t *testing.T) {
	tokens, err := lexer.Lex("area: Int32 := match result is\n| Result<Int32, Bool>.Ok then 1\n| Result<Int32, Bool>.Err then 0\nend")
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
	variant, ok := match.Arms[0].Pattern.(VariantPattern)
	if !ok || len(variant.OwnerArguments) != 2 {
		t.Fatalf("arm 0 pattern = %#v, want explicit generic VariantPattern", match.Arms[0].Pattern)
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
