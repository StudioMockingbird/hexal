package parser

import (
	"fmt"
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestParseDeclaration(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = 13")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("statement count = %d, want 1", len(program.Statements))
	}
	declaration, ok := program.Statements[0].(Declaration)
	if !ok {
		t.Fatalf("statement = %#v, want declaration", program.Statements[0])
	}
	if declaration.Name.Lexeme != "x" {
		t.Fatalf("name = %q, want %q", declaration.Name.Lexeme, "x")
	}
	typeName, ok := declaration.Type.(NamedTypeExpression)
	if !ok {
		t.Fatalf("type = %#v, want named type expression", declaration.Type)
	}
	if typeName.Name.Lexeme != "Int32" {
		t.Fatalf("type name = %q, want %q", typeName.Name.Lexeme, "Int32")
	}
	if literal, ok := declaration.Initializer.(IntegerLiteral); !ok || literal.Token.Lexeme != "13" {
		t.Fatalf("initializer = %#v, want integer literal 13", declaration.Initializer)
	}
}

func TestParseRejectsTrailingTokens(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = 13 y")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	_, err = Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted trailing tokens")
	}
}

func TestParseBooleanLiteral(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let enabled: Bool = true")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration := program.Statements[0].(Declaration)
	literal, ok := declaration.Initializer.(BooleanLiteral)
	if !ok || literal.Token.Lexeme != "true" {
		t.Fatalf("initializer = %#v, want boolean literal true", declaration.Initializer)
	}
}

func TestParseHexadecimalIntegerLiteral(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let mask: Int32 = 0xFF")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration := program.Statements[0].(Declaration)
	literal, ok := declaration.Initializer.(IntegerLiteral)
	if !ok || literal.Token.Kind != lexer.HexInteger {
		t.Fatalf("initializer = %#v, want hexadecimal integer literal", declaration.Initializer)
	}
}

func TestParsePointerExpressions(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let mut x: Int32 = 13 let p: Ptr<Int32> = @x let y: Int32 = ^p")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration := program.Statements[1].(Declaration)
	address, ok := declaration.Initializer.(AddressExpression)
	if !ok || address.Operator.Kind != lexer.At {
		t.Fatalf("initializer = %#v, want @expression", declaration.Initializer)
	}
	if variable, ok := address.Place.(VariableExpression); !ok || variable.Name.Lexeme != "x" {
		t.Fatalf("@place = %#v, want x", address.Place)
	}
	dereference := program.Statements[2].(Declaration).Initializer.(DereferenceExpression)
	if dereference.Operator.Kind != lexer.Caret {
		t.Fatalf("dereference operator = %#v, want ^", dereference.Operator)
	}
	if variable, ok := dereference.Operand.(VariableExpression); !ok || variable.Name.Lexeme != "p" {
		t.Fatalf("dereference operand = %#v, want p", dereference.Operand)
	}
}

func TestParseConstructorCallPreservesArgumentOrder(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "type Point is struct x: Int32, y: Int32, end let point: Point = Point(y = 2, x = 1,).x")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if len(program.Items) != 2 || len(program.Statements) != 1 {
		t.Fatalf("program items/statements = %d/%d, want 2/1", len(program.Items), len(program.Statements))
	}
	declaration := program.Statements[0].(Declaration)
	selection, ok := declaration.Initializer.(PropertyExpression)
	if !ok || selection.Property.Lexeme != "x" {
		t.Fatalf("initializer = %#v, want .x selection", declaration.Initializer)
	}
	call, ok := selection.Receiver.(CallExpression)
	if !ok {
		t.Fatalf("selection receiver = %#v, want a constructor call", selection.Receiver)
	}
	variable, ok := call.Callee.(VariableExpression)
	if !ok || variable.Name.Lexeme != "Point" || len(call.Arguments) != 2 {
		t.Fatalf("call = %#v, want Point with two arguments", call)
	}
	if len(call.ArgumentLabels) != 2 || call.ArgumentLabels[0] == nil || call.ArgumentLabels[1] == nil {
		t.Fatalf("argument labels = %#v, want both labeled", call.ArgumentLabels)
	}
	if got, want := call.ArgumentLabels[0].Lexeme, "y"; got != want {
		t.Fatalf("first argument label = %q, want %q", got, want)
	}
	if got, want := call.ArgumentLabels[1].Lexeme, "x"; got != want {
		t.Fatalf("second argument label = %q, want %q", got, want)
	}
}

func TestParseDeclarationStoresKeyword(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let typed: Int32 = 1 let inferred = typed")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if len(program.Statements) != 2 {
		t.Fatalf("statement count = %d, want 2", len(program.Statements))
	}
	typed := program.Statements[0].(Declaration)
	if typed.Keyword.Kind != lexer.Let || typed.Type == nil {
		t.Fatalf("typed declaration = %#v, want let and a type", typed)
	}
	inferred := program.Statements[1].(Declaration)
	if inferred.Keyword.Kind != lexer.Let || inferred.Type != nil {
		t.Fatalf("inferred declaration = %#v, want let and no type", inferred)
	}
}

func TestParseDeclarationDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"x := 5", "[Syntax Error syntax.deprecated-colon-equals] ':=' is not a declaration operator; use 'let name = value' at 1:3"},
		{"x : = 5", "[Syntax Error syntax.deprecated-colon-equals] ':=' is not a declaration operator; use 'let name = value' at 1:3"},
		{"x: Int32 := 5", "[Syntax Error syntax.deprecated-colon-equals] ':=' is not a declaration operator; use 'let name = value' at 1:10"},
		{"let x := 5", "[Syntax Error syntax.type-after-colon] expected a type after ':' in a 'let' declaration at 1:7"},
		{"let x : = 5", "[Syntax Error syntax.type-after-colon] expected a type after ':' in a 'let' declaration at 1:7"},
		{"let x mut = 1", "[Syntax Error syntax.mut-after-let] 'mut' appears only immediately after 'let' in a declaration at 1:7"},
		{"let x: Int32", "[Syntax Error syntax.expected-token] expected '=' in a 'let' declaration at 1:13"},
		{"x: Int32 = 5", "[Syntax Error syntax.declaration-needs-let] declarations require 'let' at 1:1"},
	} {
		tokens, err := lexer.Lex("test.hex", testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		_, err = Parse(tokens)
		if err == nil || err.Error() != testCase.want {
			t.Errorf("Parse(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}

func TestParseRejectsMutInConstructorCall(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let point: Point = Point(mut x = 1)")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if _, err := Parse(tokens); err == nil {
		t.Fatal("Parse accepted mut inside a constructor call")
	}
}

func TestParseGeneralDottedMemberNames(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = point.foo.bar point.addr = x")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	initializer := program.Statements[0].(Declaration).Initializer
	outer, ok := initializer.(PropertyExpression)
	if !ok || outer.Property.Lexeme != "bar" {
		t.Fatalf("initializer = %#v, want .foo.bar", initializer)
	}
	inner, ok := outer.Receiver.(PropertyExpression)
	if !ok || inner.Property.Lexeme != "foo" {
		t.Fatalf("receiver = %#v, want .foo", outer.Receiver)
	}
	assignment, ok := program.Statements[1].(Assignment)
	if !ok {
		t.Fatalf("statement 1 = %#v, want assignment", program.Statements[1])
	}
	property, ok := assignment.Target.(PropertyExpression)
	if !ok || property.Property.Lexeme != "addr" {
		t.Fatalf("assignment target = %#v, want .addr", assignment.Target)
	}
}

func TestParseRejectsExpressionSideMut(t *testing.T) {
	for _, testCase := range []struct {
		source string
		column int
	}{
		{"let x: Int32 = mut @y", 16},
		{"let x: Int32 = mut y", 16},
		{"let p: Ptr<Int32> = mut x", 21},
	} {
		tokens, err := lexer.Lex("test.hex", testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		_, err = Parse(tokens)
		want := fmt.Sprintf("[Syntax Error syntax.mut-right-hand-side] mut is not valid on the right-hand side; use @value at 1:%d", testCase.column)
		if err == nil || err.Error() != want {
			t.Fatalf("Parse(%q) error = %v, want %q", testCase.source, err, want)
		}
	}
}

func TestParsePointerExpressionDoesNotRemoveBuiltInMismatchHandling(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = true")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if _, err = Parse(tokens); err != nil {
		t.Fatalf("Parse rejected a literal whose type belongs to checking: %v", err)
	}
}

func TestParseAcceptsGeneralDottedProperty(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = 13 let y: Int32 = x.foo")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	property, ok := program.Statements[1].(Declaration).Initializer.(PropertyExpression)
	if !ok || property.Property.Lexeme != "foo" {
		t.Fatalf("initializer = %#v, want .foo property", program.Statements[1])
	}
}

func TestParseRejectsMissingDottedMemberName(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = point.")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	_, err = Parse(tokens)
	if err == nil || err.Error() != "[Syntax Error syntax.expected-token] expected an identifier after '.' at 1:22" {
		t.Fatalf("Parse error = %v, want missing member-name diagnostic", err)
	}
}

func TestParseReportsExpectedDeclaredTypeValue(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = ")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	_, err = Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted a missing initializer")
	}
	if got, want := err.Error(), "[Syntax Error syntax.expected-value] expected a value at 1:16"; got != want {
		t.Fatalf("Parse error = %q, want %q", got, want)
	}
}

func TestParseRejectsLiteralForWrongDeclaredType(t *testing.T) {
	for _, source := range []string{"let x: Int32 = true", "let flag: Bool = 1"} {
		tokens, err := lexer.Lex("test.hex", source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err = Parse(tokens); err != nil {
			t.Fatalf("Parse rejected %q; checker owns typed compatibility: %v", source, err)
		}
	}
}

func TestParseMultipleStatements(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = 13 x = 14 let flag: Bool = true flag = false")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if got, want := len(program.Statements), 4; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}
	if _, ok := program.Statements[0].(Declaration); !ok {
		t.Fatalf("statement 0 = %T, want declaration", program.Statements[0])
	}
	if assignment, ok := program.Statements[1].(Assignment); !ok || assignment.Name.Lexeme != "x" {
		t.Fatalf("statement 1 = %#v, want assignment to x", program.Statements[1])
	}
	if _, ok := program.Statements[2].(Declaration); !ok {
		t.Fatalf("statement 2 = %T, want declaration", program.Statements[2])
	}
	if assignment, ok := program.Statements[3].(Assignment); !ok || assignment.Name.Lexeme != "flag" {
		t.Fatalf("statement 3 = %#v, want assignment to flag", program.Statements[3])
	}
}

func parseInitializer(t *testing.T, source string) Expression {
	t.Helper()
	tokens, err := lexer.Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex(%q) returned an error: %v", source, err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse(%q) returned an error: %v", source, err)
	}
	if len(program.Statements) != 1 {
		t.Fatalf("Parse(%q) returned %d statements, want 1", source, len(program.Statements))
	}
	return program.Statements[0].(Declaration).Initializer
}

func parseOneItem(t *testing.T, source string) TopLevelItem {
	t.Helper()
	tokens, err := lexer.Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex(%q) returned an error: %v", source, err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse(%q) returned an error: %v", source, err)
	}
	if len(program.Items) != 1 {
		t.Fatalf("Parse(%q) returned %d items, want 1", source, len(program.Items))
	}
	return program.Items[0]
}

func parseError(t *testing.T, source string) string {
	t.Helper()
	tokens, err := lexer.Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex(%q) returned an error: %v", source, err)
	}
	if _, err := Parse(tokens); err != nil {
		return err.Error()
	}
	t.Fatalf("Parse(%q) accepted invalid source", source)
	return ""
}

func TestParseFunctionDeclaration(t *testing.T) {
	item := parseOneItem(t, "fun adder(left: Int32, right: Int32) : Int32 do\nreturn left\nend")
	function, ok := item.(FunctionDeclaration)
	if !ok {
		t.Fatalf("item = %#v, want FunctionDeclaration", item)
	}
	if function.Name.Lexeme != "adder" {
		t.Fatalf("name = %q, want adder", function.Name.Lexeme)
	}
	if len(function.Parameters) != 2 {
		t.Fatalf("parameter count = %d, want 2", len(function.Parameters))
	}
	if function.Parameters[0].Name.Lexeme != "left" {
		t.Fatalf("first parameter = %q, want left", function.Parameters[0].Name.Lexeme)
	}
	if named, ok := function.Parameters[1].Type.(NamedTypeExpression); !ok || named.Name.Lexeme != "Int32" {
		t.Fatalf("second parameter type = %#v, want Int32", function.Parameters[1].Type)
	}
	if named, ok := function.Return.(NamedTypeExpression); !ok || named.Name.Lexeme != "Int32" {
		t.Fatalf("return type = %#v, want Int32", function.Return)
	}
	if len(function.Body) != 1 {
		t.Fatalf("body length = %d, want 1", len(function.Body))
	}
	if function.End.Kind != lexer.End {
		t.Fatalf("end token = %#v, want end", function.End)
	}
}

func TestParseFunctionDeclarationIsNotAStatement(t *testing.T) {
	item := parseOneItem(t, "fun reset() do\nend")
	if _, ok := item.(Statement); ok {
		t.Fatal("FunctionDeclaration implements Statement; it is module-level only")
	}
	function := item.(FunctionDeclaration)
	if function.Return != nil {
		t.Fatalf("return type = %#v, want nil", function.Return)
	}
	if len(function.Parameters) != 0 {
		t.Fatalf("parameter count = %d, want 0", len(function.Parameters))
	}
}

func TestParseFunctionOneParameter(t *testing.T) {
	function := parseOneItem(t, "fun twice(value: Int32) : Int32 do\nreturn value\nend").(FunctionDeclaration)
	if len(function.Parameters) != 1 {
		t.Fatalf("parameter count = %d, want 1", len(function.Parameters))
	}
}

func TestParseAnonymousFunctionLiteral(t *testing.T) {
	expression := parseInitializer(t, "let result = fun (value: Int32) : Int32 do\nreturn value\nend")
	literal, ok := expression.(AnonymousFunctionLiteral)
	if !ok {
		t.Fatalf("initializer = %#v, want AnonymousFunctionLiteral", expression)
	}
	if len(literal.Parameters) != 1 || literal.Parameters[0].Name.Lexeme != "value" {
		t.Fatalf("parameters = %#v, want one parameter named value", literal.Parameters)
	}
	if named, ok := literal.Return.(NamedTypeExpression); !ok || named.Name.Lexeme != "Int32" {
		t.Fatalf("return type = %#v, want Int32", literal.Return)
	}
	if len(literal.Body) != 1 {
		t.Fatalf("body length = %d, want 1", len(literal.Body))
	}
	if len(literal.TypeParameters) != 0 {
		t.Fatalf("type parameters = %#v, want none", literal.TypeParameters)
	}
}

func TestParseAnonymousFunctionLiteralNoResult(t *testing.T) {
	expression := parseInitializer(t, "let callback = fun (value: Int32) do\nend")
	literal := expression.(AnonymousFunctionLiteral)
	if literal.Return != nil {
		t.Fatalf("return type = %#v, want nil", literal.Return)
	}
}

func TestParseAnonymousFunctionLiteralIsNotAStatement(t *testing.T) {
	var expression Expression = AnonymousFunctionLiteral{}
	if _, ok := expression.(Statement); ok {
		t.Fatal("AnonymousFunctionLiteral implements Statement; it must be bound before it can be invoked as a statement")
	}
}

func TestParseGenericAnonymousFunctionLiteral(t *testing.T) {
	// `<` immediately after `fun` is unambiguously the generic-parameter
	// delimiter: no value expression can end with the `fun` token, so this
	// cannot be misread as a relational comparison.
	expression := parseInitializer(t, "let identity = fun<T>(value: T) : T do\nreturn value\nend")
	literal := expression.(AnonymousFunctionLiteral)
	if len(literal.TypeParameters) != 1 || literal.TypeParameters[0].Lexeme != "T" {
		t.Fatalf("type parameters = %#v, want [T]", literal.TypeParameters)
	}
}

func TestParseAnonymousFunctionLiteralDirectCall(t *testing.T) {
	// A call suffix on a literal is a postfix base; it is valid only where an
	// expression is expected, never as a call statement (see
	// TestParseFunctionDiagnostics for the rejected statement form).
	expression := parseInitializer(t, "let result = fun (value: Int32) : Int32 do\nreturn value\nend(5)")
	call, ok := expression.(CallExpression)
	if !ok {
		t.Fatalf("initializer = %#v, want CallExpression", expression)
	}
	if _, ok := call.Callee.(AnonymousFunctionLiteral); !ok {
		t.Fatalf("callee = %#v, want AnonymousFunctionLiteral", call.Callee)
	}
	if len(call.Arguments) != 1 {
		t.Fatalf("argument count = %d, want 1", len(call.Arguments))
	}
}

func TestParseNestedAnonymousFunctionLiteral(t *testing.T) {
	expression := parseInitializer(t,
		"let outer = fun (value: Int32) : Fun<(Int32) : Int32> do\nreturn fun (delta: Int32) : Int32 do\nreturn delta\nend\nend")
	outer := expression.(AnonymousFunctionLiteral)
	returned, ok := outer.Body[0].(ReturnStatement)
	if !ok {
		t.Fatalf("outer body[0] = %#v, want ReturnStatement", outer.Body[0])
	}
	if _, ok := returned.Value.(AnonymousFunctionLiteral); !ok {
		t.Fatalf("returned value = %#v, want a nested AnonymousFunctionLiteral", returned.Value)
	}
}

func TestParseAnonymousFunctionLiteralDiagnostics(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"let result = fun do\nend", "anonymous function requires '(' or '<' after 'fun'"},
		{"let result = fun (value: Int32) : Int32\nreturn value\nend", "expected 'do' after function signature"},
		{"let result = fun (value) : Int32 do\nreturn value\nend", "function parameters require type annotations"},
	} {
		if message := parseError(t, testCase.source); !strings.Contains(message, testCase.want) {
			t.Errorf("parseError(%q) = %q, want to contain %q", testCase.source, message, testCase.want)
		}
	}
}

// A named function declaration is rejected at statement position, inside a
// function body, with the exact diagnostic naming module scope. A bare
// `return` on its own line must not confuse this rejection with an
// attempted, wrongly-placed return value: `fun` is value-only exactly when
// it begins an anonymous literal, which a next-line `fun identifier` never
// does, so both forms report the same diagnostic.
func TestParseLocalFunctionDeclarationIsRejected(t *testing.T) {
	for _, source := range []string{
		"fun outer() do\nfun inner(value: Int32) : Int32 do\nreturn value\nend\nreturn inner(1)\nend",
		"fun outer() do\nfun identity<T>(value: T) : T do\nreturn value\nend\nend",
		"fun outer() do\nreturn\nfun inner() do\nend\nend",
	} {
		message := parseError(t, source)
		if !strings.Contains(message, "named function declarations are only valid at module scope") {
			t.Fatalf("parseError(%q) = %q, want the module-scope-only diagnostic", source, message)
		}
	}
}

func TestParseFunctionDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun adder(left: Int32) do\nreturn left", "expected end to close function adder"},
		{"fun adder(left)\nend", "function parameters require type annotations"},
		{"mut fun adder()\nend", "'mut' appears only immediately after 'let' in a declaration"},
		{"fun inner()\nend", "expected 'do' after function signature"},
		{"fun outer() do\nmethod Point.m()\nend\nend", "method declarations are module-level only"},
	} {
		message := parseError(t, testCase.source)
		if !strings.Contains(message, testCase.want) {
			t.Errorf("error for %q = %q, want %q", testCase.source, message, testCase.want)
		}
	}
}

// A module-scope return is a legal top-level item: only the checker can know
// whether the module is the entrypoint, so the parser represents it and
// defers the entry-versus-import decision.
func TestParseModuleLevelReturn(t *testing.T) {
	for _, source := range []string{"return", "return 1"} {
		tokens, err := lexer.Lex("test.hex", source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		program, err := Parse(tokens)
		if err != nil {
			t.Fatalf("Parse(%q) returned an error: %v", source, err)
		}
		if len(program.Statements) != 1 {
			t.Fatalf("Parse(%q) returned %d statements, want 1", source, len(program.Statements))
		}
		if _, ok := program.Statements[0].(ReturnStatement); !ok {
			t.Fatalf("Parse(%q) statement = %#v, want ReturnStatement", source, program.Statements[0])
		}
	}
}

func TestParseSelfReceiverExpression(t *testing.T) {
	method := parseOneItem(t, "method Point.grow() do\nself.x = 1\nend").(MethodDeclaration)
	assignment, ok := method.Body[0].(Assignment)
	if !ok {
		t.Fatalf("body[0] = %#v, want an assignment", method.Body[0])
	}
	property, ok := assignment.Target.(PropertyExpression)
	if !ok {
		t.Fatalf("target = %#v, want a property selection", assignment.Target)
	}
	receiver, ok := property.Receiver.(VariableExpression)
	if !ok || receiver.Name.Kind != lexer.Self {
		t.Fatalf("receiver = %#v, want the self token", property.Receiver)
	}
}
