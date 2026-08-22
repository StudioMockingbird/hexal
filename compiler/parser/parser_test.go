package parser

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

func TestParseDeclaration(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := 13")
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
	tokens, err := lexer.Lex("x: Int32 := 13 y")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	_, err = Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted trailing tokens")
	}
}

func TestParseBooleanLiteral(t *testing.T) {
	tokens, err := lexer.Lex("enabled: Bool := true")
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
	tokens, err := lexer.Lex("mask: Int32 := 0xFF")
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
	tokens, err := lexer.Lex("mut x: Int32 := 13 p: Ptr<Int32> := ref x y: Int32 := p.value")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration := program.Statements[1].(Declaration)
	ref, ok := declaration.Initializer.(RefExpression)
	if !ok || ref.Keyword.Kind != lexer.Ref {
		t.Fatalf("initializer = %#v, want ref expression", declaration.Initializer)
	}
	if variable, ok := ref.Place.(VariableExpression); !ok || variable.Name.Lexeme != "x" {
		t.Fatalf("ref place = %#v, want x", ref.Place)
	}
	value := program.Statements[2].(Declaration).Initializer.(PropertyExpression)
	if value.Property.Lexeme != "value" {
		t.Fatalf("dereference property = %q, want value", value.Property.Lexeme)
	}
}

func TestParseObjectLiteralPreservesInitializerOrder(t *testing.T) {
	tokens, err := lexer.Lex("type Point = { x: Int32, y: Int32, } point: Point := Point { y = 2, x = 1, }.x")
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
	literal, ok := selection.Receiver.(ObjectLiteral)
	if !ok {
		t.Fatalf("selection receiver = %#v, want object literal", selection.Receiver)
	}
	if literal.TypeName.Lexeme != "Point" || len(literal.Initializers) != 2 {
		t.Fatalf("literal = %#v, want Point with two initializers", literal)
	}
	if got, want := literal.Initializers[0].Name.Lexeme, "y"; got != want {
		t.Fatalf("first initializer = %q, want %q", got, want)
	}
	if got, want := literal.Initializers[1].Name.Lexeme, "x"; got != want {
		t.Fatalf("second initializer = %q, want %q", got, want)
	}
}

func TestParseDeclarationStoresDeclarationOperator(t *testing.T) {
	tokens, err := lexer.Lex("typed: Int32 := 1 inferred := typed")
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
	if typed.Operator.Kind != lexer.ColonEqual || typed.Type == nil {
		t.Fatalf("typed declaration = %#v, want := and a type", typed)
	}
	inferred := program.Statements[1].(Declaration)
	if inferred.Operator.Kind != lexer.ColonEqual || inferred.Type != nil {
		t.Fatalf("inferred declaration = %#v, want := and no type", inferred)
	}
}

func TestParseRejectsMutInObjectLiteral(t *testing.T) {
	tokens, err := lexer.Lex("point: Point := Point { mut x = 1 }")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	_, err = Parse(tokens)
	want := "[Syntax Error] mut is not allowed in an object literal at 1:25"
	if err == nil || err.Error() != want {
		t.Fatalf("Parse error = %v, want %q", err, want)
	}
}

func TestParseGeneralDottedMemberNames(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := point.foo.bar point.addr = x")
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
		{"x: Int32 := mut ref y", 13},
		{"x: Int32 := mut y", 13},
		{"p: Ptr<Int32> := mut x", 18},
	} {
		tokens, err := lexer.Lex(testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		_, err = Parse(tokens)
		want := fmt.Sprintf("[Syntax Error] mut is not valid on the right-hand side; use ref value at 1:%d", testCase.column)
		if err == nil || err.Error() != want {
			t.Fatalf("Parse(%q) error = %v, want %q", testCase.source, err, want)
		}
	}
}

func TestParsePointerExpressionDoesNotRemoveBuiltInMismatchHandling(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := true")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if _, err = Parse(tokens); err != nil {
		t.Fatalf("Parse rejected a literal whose type belongs to checking: %v", err)
	}
}

func TestParseAcceptsGeneralDottedProperty(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := 13 y: Int32 := x.foo")
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
	tokens, err := lexer.Lex("x: Int32 := point.")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	_, err = Parse(tokens)
	if err == nil || err.Error() != "[Syntax Error] expected an identifier after '.' at 1:19" {
		t.Fatalf("Parse error = %v, want missing member-name diagnostic", err)
	}
}

func TestParseReportsExpectedDeclaredTypeValue(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := ")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	_, err = Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted a missing initializer")
	}
	if got, want := err.Error(), "[Syntax Error] expected a value at 1:13"; got != want {
		t.Fatalf("Parse error = %q, want %q", got, want)
	}
}

func TestParseRejectsLiteralForWrongDeclaredType(t *testing.T) {
	for _, source := range []string{"x: Int32 := true", "flag: Bool := 1"} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err = Parse(tokens); err != nil {
			t.Fatalf("Parse rejected %q; checker owns typed compatibility: %v", source, err)
		}
	}
}

func TestParseMultipleStatements(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := 13 x = 14 flag: Bool := true flag = false")
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

func TestParseRecoversAtNextStatement(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := 13 y z: Int32 := 14")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted an invalid statement")
	}
	if got, want := err.Error(), "[Syntax Error] expected ':' or ':=' for a declaration, or '=' for an assignment at 1:18"; got != want {
		t.Fatalf("Parse error = %q, want %q", got, want)
	}
	if got, want := len(program.Statements), 2; got != want {
		t.Fatalf("recovered statement count = %d, want %d", got, want)
	}
	if declaration, ok := program.Statements[1].(Declaration); !ok || declaration.Name.Lexeme != "z" {
		t.Fatalf("recovered statement = %#v, want declaration z", program.Statements[1])
	}
}

func TestParseRecoversAtDottedAssignment(t *testing.T) {
	tokens, err := lexer.Lex("x: Int32 := 13 invalid z: Int32 := 14 point.foo = 15")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted an invalid statement")
	}
	if got, want := len(program.Statements), 3; got != want {
		t.Fatalf("recovered statement count = %d, want %d", got, want)
	}
	assignment, ok := program.Statements[2].(Assignment)
	if !ok {
		t.Fatalf("recovered statement = %#v, want dotted assignment", program.Statements[2])
	}
	if property, ok := assignment.Target.(PropertyExpression); !ok || property.Property.Lexeme != "foo" {
		t.Fatalf("recovered target = %#v, want point.foo", assignment.Target)
	}
}

func TestSynchronizeAdvancesWhenNoTokensWereConsumed(t *testing.T) {
	tokens, err := lexer.Lex("type")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	parser := Parser{tokens: tokens}
	parser.synchronize(0)
	if got, want := parser.current, 1; got != want {
		t.Fatalf("recovery cursor = %d, want %d", got, want)
	}
}

func TestParseReturnsDiagnosticsForRepeatedStatementKeywords(t *testing.T) {
	for _, source := range []string{"type type", "mut mut"} {
		t.Run(source, func(t *testing.T) {
			tokens, err := lexer.Lex(source)
			if err != nil {
				t.Fatalf("Lex(%q) returned an error: %v", source, err)
			}

			result := make(chan error, 1)
			go func() {
				_, err := Parse(tokens)
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil {
					t.Fatalf("Parse(%q) accepted malformed input", source)
				}
			case <-time.After(time.Second):
				t.Fatalf("Parse(%q) did not return promptly", source)
			}
		})
	}
}

// The import prefix closes at the first non-import top-level item; an import
// after a type, function, or impl declaration, or after an executable
// statement, is a positioned Syntax Error, while imports-only-first programs
// keep parsing.
func TestParseRejectsImportAfterTopLevelItem(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		source     string
		importLine int
	}{
		{"type declaration", "type T = { n: Int32 }\nmodule a = import \"./a\"\n", 2},
		{"function declaration", "fun f(): Int32 do\n    return 1\nend\nmodule a = import \"./a\"\n", 4},
		{"impl declaration", "type T = { n: Int32 }\nimpl T.act() do\nend\nmodule a = import \"./a\"\n", 4},
		{"executable statement", "x: Int32 := 1\nmodule a = import \"./a\"\n", 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, err := lexer.Lex(testCase.source)
			if err != nil {
				t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
			}
			_, err = Parse(tokens)
			if err == nil {
				t.Fatalf("Parse(%q) accepted a misplaced import", testCase.source)
			}
			diagnostics, ok := err.(compilerTypes.Diagnostics)
			if !ok {
				t.Fatalf("Parse error = %T, want Diagnostics", err)
			}
			var positioned *compilerTypes.Diagnostic
			for index := range diagnostics {
				if diagnostics[index].Message == "imports must precede all other top-level items" {
					positioned = &diagnostics[index]
				}
			}
			if positioned == nil {
				t.Fatalf("diagnostics = %v, want the misplaced-import error", diagnostics)
			}
			if positioned.Category != compilerTypes.SyntaxError || positioned.Line != testCase.importLine || positioned.Column == 0 {
				t.Fatalf("misplaced-import diagnostic = %#v, want Syntax Error at line %d", positioned, testCase.importLine)
			}
		})
	}
	if _, err := Parse(mustLex(t, "module a = import \"./a\"\nmodule b = import \"./b\"\nx: Int32 := 1\n")); err != nil {
		t.Fatalf("Parse rejected an imports-first program: %v", err)
	}
}

func TestParseRecoversAfterConsumedMalformedStatement(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		source     string
		wantName   string
		wantAssign bool
	}{
		{name: "declaration", source: "broken value: Int32 := 1", wantName: "value"},
		{name: "assignment", source: "broken value = 1", wantName: "value", wantAssign: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, err := lexer.Lex(testCase.source)
			if err != nil {
				t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
			}

			program, err := Parse(tokens)
			if err == nil {
				t.Fatalf("Parse(%q) accepted malformed input", testCase.source)
			}
			if len(program.Statements) != 1 {
				t.Fatalf("recovered statement count = %d, want 1", len(program.Statements))
			}
			if testCase.wantAssign {
				assignment, ok := program.Statements[0].(Assignment)
				if !ok || assignment.Name.Lexeme != testCase.wantName {
					t.Fatalf("recovered statement = %#v, want assignment to %q", program.Statements[0], testCase.wantName)
				}
				return
			}
			declaration, ok := program.Statements[0].(Declaration)
			if !ok || declaration.Name.Lexeme != testCase.wantName {
				t.Fatalf("recovered statement = %#v, want declaration %q", program.Statements[0], testCase.wantName)
			}
		})
	}
}

func TestParseRecoveryPreservesNestedBlockDelimiters(t *testing.T) {
	tokens, err := lexer.Lex("if true then while false do else end end recovered: Int32 := 1")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted malformed nested blocks")
	}
	if strings.Contains(err.Error(), "unexpected 'end' outside a block") {
		t.Fatalf("Parse diagnostics = %q, nested while should own its end", err)
	}
	if len(program.Statements) != 2 {
		t.Fatalf("recovered statement count = %d, want outer if and recovered declaration", len(program.Statements))
	}
	if _, ok := program.Statements[0].(IfStatement); !ok {
		t.Fatalf("recovered statement = %#v, want outer if", program.Statements[0])
	}
	declaration, ok := program.Statements[1].(Declaration)
	if !ok || declaration.Name.Lexeme != "recovered" {
		t.Fatalf("recovered statement = %#v, want declaration recovered", program.Statements[1])
	}
}

func TestParseRecoveryKeepsInvalidDelimiterInsideWhile(t *testing.T) {
	tokens, err := lexer.Lex("if true then while false do else sibling: Int32 := 1 end after: Int32 := 2 end")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err == nil || !strings.Contains(err.Error(), "'else' cannot appear inside a while body") {
		t.Fatalf("Parse diagnostics = %v, want the nested-while diagnostic", err)
	}
	if len(program.Statements) != 1 {
		t.Fatalf("recovered statement count = %d, want outer if", len(program.Statements))
	}
	conditional, ok := program.Statements[0].(IfStatement)
	if !ok {
		t.Fatalf("recovered statement = %#v, want outer if", program.Statements[0])
	}
	if conditional.Else != nil {
		t.Fatalf("outer else body = %#v, while-local else must not become an outer clause", conditional.Else)
	}
	if len(conditional.Then) != 2 {
		t.Fatalf("outer then body length = %d, want loop and declaration after", len(conditional.Then))
	}
	loop, ok := conditional.Then[0].(WhileStatement)
	if !ok || len(loop.Body) != 1 {
		t.Fatalf("recovered loop = %#v, want one loop-local sibling", conditional.Then[0])
	}
	if declaration, ok := loop.Body[0].(Declaration); !ok || declaration.Name.Lexeme != "sibling" {
		t.Fatalf("loop body = %#v, want declaration sibling", loop.Body[0])
	}
	if declaration, ok := conditional.Then[1].(Declaration); !ok || declaration.Name.Lexeme != "after" {
		t.Fatalf("recovered sibling = %#v, want declaration after", conditional.Then[1])
	}
}

func TestParseRecoveryKeepsValidReturnSibling(t *testing.T) {
	tokens, err := lexer.Lex("fun choose(): Int32 do if true then broken return 1 else return 2 end end")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted the malformed sibling")
	}
	function, ok := program.Items[0].(FunctionDeclaration)
	if !ok || len(function.Body) != 1 {
		t.Fatalf("recovered function = %#v, want one conditional body", program.Items)
	}
	conditional, ok := function.Body[0].(IfStatement)
	if !ok || len(conditional.Then) != 1 {
		t.Fatalf("recovered conditional = %#v, want one then statement", function.Body[0])
	}
	if returned, ok := conditional.Then[0].(ReturnStatement); !ok || returned.Value == nil {
		t.Fatalf("recovered then statement = %#v, want valued return", conditional.Then[0])
	}
}

func TestParseRecoveryReportsMissingEndAfterMalformedStatement(t *testing.T) {
	message := parseError(t, "if true then broken")
	if !strings.Contains(message, "expected end to close if") {
		t.Fatalf("Parse error = %q, want missing-if-end diagnostic", message)
	}
}

func TestParseRecoveryKeepsSelfAssignmentSibling(t *testing.T) {
	tokens, err := lexer.Lex("if true then broken self.value = 1 end")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted the malformed sibling")
	}
	conditional, ok := program.Statements[0].(IfStatement)
	if !ok || len(conditional.Then) != 1 {
		t.Fatalf("recovered conditional = %#v, want one then statement", program.Statements)
	}
	assignment, ok := conditional.Then[0].(Assignment)
	if !ok {
		t.Fatalf("recovered then statement = %#v, want self assignment", conditional.Then[0])
	}
	target, ok := assignment.Target.(PropertyExpression)
	if !ok {
		t.Fatalf("recovered assignment target = %#v, want self.value", assignment.Target)
	}
	receiver, ok := target.Receiver.(VariableExpression)
	if !ok || receiver.Name.Kind != lexer.Self {
		t.Fatalf("recovered assignment receiver = %#v, want self", target.Receiver)
	}
}

func TestParseRecoveryKeepsSiblingInsideMalformedNestedLoop(t *testing.T) {
	tokens, err := lexer.Lex("fun keep() do while true do else value: Int32 := 1 end after: Int32 := 2 end")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted the malformed nested loop")
	}
	function, ok := program.Items[0].(FunctionDeclaration)
	if !ok || len(function.Body) != 2 {
		t.Fatalf("recovered function = %#v, want loop and sibling declaration", program.Items)
	}
	loop, ok := function.Body[0].(WhileStatement)
	if !ok || len(loop.Body) != 1 {
		t.Fatalf("recovered loop = %#v, want one loop-local sibling", function.Body[0])
	}
	if declaration, ok := loop.Body[0].(Declaration); !ok || declaration.Name.Lexeme != "value" {
		t.Fatalf("loop body = %#v, want declaration value", loop.Body[0])
	}
	if declaration, ok := function.Body[1].(Declaration); !ok || declaration.Name.Lexeme != "after" {
		t.Fatalf("function sibling = %#v, want declaration after", function.Body[1])
	}
}

func TestParseRecoveryKeepsDottedCallSibling(t *testing.T) {
	tokens, err := lexer.Lex("if true then broken point.step(1) end")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted the malformed sibling")
	}
	conditional, ok := program.Statements[0].(IfStatement)
	if !ok || len(conditional.Then) != 1 {
		t.Fatalf("recovered conditional = %#v, want one call sibling", program.Statements)
	}
	call, ok := conditional.Then[0].(CallExpression)
	if !ok {
		t.Fatalf("recovered then statement = %#v, want dotted call", conditional.Then[0])
	}
	callee, ok := call.Callee.(PropertyExpression)
	if !ok || callee.Property.Lexeme != "step" {
		t.Fatalf("recovered call callee = %#v, want point.step", call.Callee)
	}
}

func TestParseGeneralUnaryMinus(t *testing.T) {
	initializer := parseInitializer(t, "x: Int32 := -value")
	unary, ok := initializer.(UnaryExpression)
	if !ok || unary.Operator.Kind != lexer.Minus {
		t.Fatalf("initializer = %#v, want unary minus", initializer)
	}
	operand, ok := unary.Operand.(VariableExpression)
	if !ok || operand.Name.Lexeme != "value" {
		t.Fatalf("operand = %#v, want variable value", unary.Operand)
	}
}

func TestParseLogicalNot(t *testing.T) {
	initializer := parseInitializer(t, "flag: Bool := !ready")
	unary, ok := initializer.(UnaryExpression)
	if !ok || unary.Operator.Kind != lexer.Bang {
		t.Fatalf("initializer = %#v, want logical not", initializer)
	}
	operand, ok := unary.Operand.(VariableExpression)
	if !ok || operand.Name.Lexeme != "ready" {
		t.Fatalf("operand = %#v, want variable ready", unary.Operand)
	}
}

func TestParseUnaryOperatorsAssociateRight(t *testing.T) {
	initializer := parseInitializer(t, "x: Int32 := - -value")
	outer, ok := initializer.(UnaryExpression)
	if !ok || outer.Operator.Kind != lexer.Minus {
		t.Fatalf("initializer = %#v, want outer unary minus", initializer)
	}
	inner, ok := outer.Operand.(UnaryExpression)
	if !ok || inner.Operator.Kind != lexer.Minus {
		t.Fatalf("outer operand = %#v, want inner unary minus", outer.Operand)
	}
	if operand, ok := inner.Operand.(VariableExpression); !ok || operand.Name.Lexeme != "value" {
		t.Fatalf("inner operand = %#v, want variable value", inner.Operand)
	}
}

func TestParseDirectMinusLiteralRetainsLiteralNode(t *testing.T) {
	for _, source := range []string{"x: Int8 := -128", "x: Float32 := -1.5"} {
		initializer := parseInitializer(t, source)
		negative, ok := initializer.(NegatedNumericLiteral)
		if !ok {
			t.Fatalf("initializer for %q = %#v, want negated numeric literal", source, initializer)
		}
		switch literal := negative.Literal.(type) {
		case IntegerLiteral:
			if literal.Token.Lexeme != "128" {
				t.Fatalf("integer literal for %q = %q, want 128", source, literal.Token.Lexeme)
			}
		case DecimalLiteral:
			if literal.Token.Lexeme != "1.5" {
				t.Fatalf("decimal literal for %q = %q, want 1.5", source, literal.Token.Lexeme)
			}
		default:
			t.Fatalf("literal for %q = %#v, want numeric literal", source, negative.Literal)
		}
	}
}

func TestParseBinaryPrecedenceAndGrouping(t *testing.T) {
	multiplicative := parseInitializer(t, "x: Int32 := 2 + 3 * 4").(BinaryExpression)
	if multiplicative.Operator.Kind != lexer.Plus {
		t.Fatalf("root operator = %v, want +", multiplicative.Operator.Kind)
	}
	right, ok := multiplicative.Right.(BinaryExpression)
	if !ok || right.Operator.Kind != lexer.Star {
		t.Fatalf("right operand = %#v, want multiplication", multiplicative.Right)
	}

	grouped := parseInitializer(t, "x: Int32 := (2 + 3) * 4").(BinaryExpression)
	if grouped.Operator.Kind != lexer.Star {
		t.Fatalf("grouped root operator = %v, want *", grouped.Operator.Kind)
	}
	left, ok := grouped.Left.(BinaryExpression)
	if !ok || left.Operator.Kind != lexer.Plus {
		t.Fatalf("grouped left operand = %#v, want addition", grouped.Left)
	}
}

func TestParseBinaryOperatorsAssociateLeft(t *testing.T) {
	initializer := parseInitializer(t, "x: Int32 := a - b - c")
	outer, ok := initializer.(BinaryExpression)
	if !ok || outer.Operator.Kind != lexer.Minus {
		t.Fatalf("initializer = %#v, want outer subtraction", initializer)
	}
	inner, ok := outer.Left.(BinaryExpression)
	if !ok || inner.Operator.Kind != lexer.Minus {
		t.Fatalf("left operand = %#v, want inner subtraction", outer.Left)
	}
}

func TestParseAllExpressionPrecedenceLevels(t *testing.T) {
	initializer := parseInitializer(t, "result: Bool := a + 1 > b and !done or ready == loaded")
	orExpression, ok := initializer.(BinaryExpression)
	if !ok || orExpression.Operator.Kind != lexer.Or {
		t.Fatalf("initializer = %#v, want or expression", initializer)
	}

	andExpression, ok := orExpression.Left.(BinaryExpression)
	if !ok || andExpression.Operator.Kind != lexer.And {
		t.Fatalf("or left = %#v, want and expression", orExpression.Left)
	}
	relational, ok := andExpression.Left.(BinaryExpression)
	if !ok || relational.Operator.Kind != lexer.Greater {
		t.Fatalf("and left = %#v, want relational expression", andExpression.Left)
	}
	additive, ok := relational.Left.(BinaryExpression)
	if !ok || additive.Operator.Kind != lexer.Plus {
		t.Fatalf("relational left = %#v, want additive expression", relational.Left)
	}
	logicalNot, ok := andExpression.Right.(UnaryExpression)
	if !ok || logicalNot.Operator.Kind != lexer.Bang {
		t.Fatalf("and right = %#v, want logical not", andExpression.Right)
	}
	equality, ok := orExpression.Right.(BinaryExpression)
	if !ok || equality.Operator.Kind != lexer.EqualEqual {
		t.Fatalf("or right = %#v, want equality expression", orExpression.Right)
	}
}

func TestParseEveryBinaryOperator(t *testing.T) {
	for _, testCase := range []struct {
		source   string
		operator lexer.TokenKind
	}{
		{"x: Int32 := a * b", lexer.Star},
		{"x: Int32 := a / b", lexer.Slash},
		{"x: Int32 := a % b", lexer.Percent},
		{"x: Int32 := a + b", lexer.Plus},
		{"x: Int32 := a - b", lexer.Minus},
		{"x: Bool := a < b", lexer.Less},
		{"x: Bool := a <= b", lexer.LessEqual},
		{"x: Bool := a > b", lexer.Greater},
		{"x: Bool := a >= b", lexer.GreaterEqual},
		{"x: Bool := a == b", lexer.EqualEqual},
		{"x: Bool := a != b", lexer.BangEqual},
		{"x: Bool := a and b", lexer.And},
		{"x: Bool := a or b", lexer.Or},
	} {
		binary, ok := parseInitializer(t, testCase.source).(BinaryExpression)
		if !ok || binary.Operator.Kind != testCase.operator {
			t.Errorf("initializer for %q = %#v, want %v", testCase.source, binary, testCase.operator)
		}
	}
}

func TestParseRejectsMalformedOperatorsAndGrouping(t *testing.T) {
	for _, source := range []string{
		"x: Int32 := 1 +",
		"x: Bool := true and",
		"x: Bool := !",
		"x: Int32 := (1 + 2",
		"x: Int32 := 1 * / 2",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Errorf("Parse(%q) accepted malformed expression", source)
		}
	}
}

func TestParseReservesLogicalKeywords(t *testing.T) {
	for _, source := range []string{"and: Bool := true", "or: Bool := false"} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Errorf("Parse(%q) accepted reserved logical keyword as a name", source)
		}
	}
}

func TestParseRefRemainsPlaceOnly(t *testing.T) {
	for _, source := range []string{"p: Ptr<Int32> := ref 42", "p: Ptr<Int32> := ref nil"} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		_, err = Parse(tokens)
		if err == nil || err.Error() != "[Syntax Error] expected a place identifier at 1:22" {
			t.Fatalf("Parse(%q) error = %v, want place-only ref diagnostic", source, err)
		}
	}
}

func TestParseRefRejectsCalls(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"p: Ptr<Int32> := ref f()", "[Syntax Error] ref requires a place at 1:23"},
		{"p: Ptr<Int32> := ref value.method()", "[Syntax Error] ref requires a place at 1:34"},
	} {
		tokens, err := lexer.Lex(testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		_, err = Parse(tokens)
		if err == nil || err.Error() != testCase.want {
			t.Errorf("Parse(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}

func parseInitializer(t *testing.T, source string) Expression {
	t.Helper()
	tokens, err := lexer.Lex(source)
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
	tokens, err := lexer.Lex(source)
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
	tokens, err := lexer.Lex(source)
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

func TestParseImplReceiverForms(t *testing.T) {
	for _, testCase := range []struct {
		source     string
		writable   bool
		pointer    bool
		typeLexeme string
	}{
		{source: "impl Point.translate(dx: Int32) do\nend", typeLexeme: "Point"},
		{source: "impl Ptr<Point>.length() do\nend", pointer: true, typeLexeme: "Point"},
		{source: "impl MutPtr<Point>.reset() do\nend", pointer: true, writable: true, typeLexeme: "Point"},
	} {
		item := parseOneItem(t, testCase.source)
		method, ok := item.(ImplDeclaration)
		if !ok {
			t.Fatalf("item for %q = %#v, want ImplDeclaration", testCase.source, item)
		}
		if testCase.pointer {
			pointer, ok := method.SelfType.(PtrTypeExpression)
			if !ok {
				t.Fatalf("self type for %q = %#v, want PtrTypeExpression", testCase.source, method.SelfType)
			}
			if pointer.Writable != testCase.writable {
				t.Fatalf("writable for %q = %v, want %v", testCase.source, pointer.Writable, testCase.writable)
			}
			named, ok := pointer.Element.(NamedTypeExpression)
			if !ok || named.Name.Lexeme != testCase.typeLexeme {
				t.Fatalf("element for %q = %#v, want %q", testCase.source, pointer.Element, testCase.typeLexeme)
			}
			continue
		}
		named, ok := method.SelfType.(NamedTypeExpression)
		if !ok || named.Name.Lexeme != testCase.typeLexeme {
			t.Fatalf("self type for %q = %#v, want %q", testCase.source, method.SelfType, testCase.typeLexeme)
		}
	}
}

func TestParseImplMethodName(t *testing.T) {
	method := parseOneItem(t, "impl Point.translate(dx: Int32) do\nself.x = dx\nend").(ImplDeclaration)
	if method.Name.Lexeme != "translate" {
		t.Fatalf("method name = %q, want translate", method.Name.Lexeme)
	}
	if len(method.Body) != 1 {
		t.Fatalf("body length = %d, want 1", len(method.Body))
	}
}

func TestParseNestedCallArguments(t *testing.T) {
	call, ok := parseInitializer(t, "x: Int32 := f(g(1), 2)").(CallExpression)
	if !ok {
		t.Fatalf("initializer is not a call")
	}
	if callee, ok := call.Callee.(VariableExpression); !ok || callee.Name.Lexeme != "f" {
		t.Fatalf("callee = %#v, want f", call.Callee)
	}
	if len(call.Arguments) != 2 {
		t.Fatalf("argument count = %d, want 2", len(call.Arguments))
	}
	inner, ok := call.Arguments[0].(CallExpression)
	if !ok || len(inner.Arguments) != 1 {
		t.Fatalf("first argument = %#v, want call g(1)", call.Arguments[0])
	}
}

func TestParseZeroArgumentCall(t *testing.T) {
	call := parseInitializer(t, "x: Int32 := now()").(CallExpression)
	if len(call.Arguments) != 0 {
		t.Fatalf("argument count = %d, want 0", len(call.Arguments))
	}
}

func TestParseMethodCallChainStatement(t *testing.T) {
	item := parseOneItem(t, "a.b.c(1)")
	call, ok := item.(CallExpression)
	if !ok {
		t.Fatalf("item = %#v, want CallExpression statement", item)
	}
	property, ok := call.Callee.(PropertyExpression)
	if !ok || property.Property.Lexeme != "c" {
		t.Fatalf("callee = %#v, want property .c", call.Callee)
	}
	inner, ok := property.Receiver.(PropertyExpression)
	if !ok || inner.Property.Lexeme != "b" {
		t.Fatalf("receiver = %#v, want property .b", property.Receiver)
	}
}

func TestParseCallThenMemberSelection(t *testing.T) {
	property, ok := parseInitializer(t, "x: Int32 := point.translate(1, 2).x").(PropertyExpression)
	if !ok {
		t.Fatal("initializer is not a property selection")
	}
	if _, ok := property.Receiver.(CallExpression); !ok {
		t.Fatalf("receiver = %#v, want call", property.Receiver)
	}
}

func TestParseChainEndingInMemberIsNotAStatement(t *testing.T) {
	message := parseError(t, "point.x")
	if !strings.Contains(message, "expected ':' or ':=' for a declaration, or '=' for an assignment") {
		t.Fatalf("error = %q, want a statement-form diagnostic", message)
	}
}

func TestParseReturnForms(t *testing.T) {
	function := parseOneItem(t, "fun f() : Int32 do\nreturn 1\nend").(FunctionDeclaration)
	valued, ok := function.Body[0].(ReturnStatement)
	if !ok || valued.Value == nil {
		t.Fatalf("body[0] = %#v, want a valued return", function.Body[0])
	}

	function = parseOneItem(t, "fun f() do\nreturn\nend").(FunctionDeclaration)
	bare, ok := function.Body[0].(ReturnStatement)
	if !ok || bare.Value != nil {
		t.Fatalf("body[0] = %#v, want a bare return", function.Body[0])
	}

	// Match is value-only: on return's own line it is the return value,
	// never the next statement.
	function = parseOneItem(t, "fun f() : Int32 do\nreturn match true | true then 1 | false then 0 end\nend").(FunctionDeclaration)
	matched, ok := function.Body[0].(ReturnStatement)
	if !ok || matched.Value == nil {
		t.Fatalf("body[0] = %#v, want a valued return", function.Body[0])
	}
	if _, ok := matched.Value.(MatchExpression); !ok {
		t.Fatalf("return value = %#v, want a match expression", matched.Value)
	}
}

func TestParseReturnNil(t *testing.T) {
	function := parseOneItem(t, "fun find() : Nil do\nreturn nil\nend").(FunctionDeclaration)
	statement, ok := function.Body[0].(ReturnStatement)
	if !ok || statement.Value == nil {
		t.Fatalf("body[0] = %#v, want a valued return", function.Body[0])
	}
	if got := fmt.Sprintf("%T", statement.Value); got != "parser.NilLiteral" {
		t.Fatalf("return value type = %q, want parser.NilLiteral", got)
	}
}

func TestParseReturnNilUsesValueOnlyRecovery(t *testing.T) {
	message := parseError(t, "fun find() : Nil do\nreturn\nnil\nend")
	if !strings.Contains(message, "a return value must begin on the same line as return") {
		t.Fatalf("Parse error = %q, want same-line return diagnostic", message)
	}
}

func TestParseBareReturnFollowedByCall(t *testing.T) {
	function := parseOneItem(t, "fun f() do\nreturn\ncleanup()\nend").(FunctionDeclaration)
	if len(function.Body) != 2 {
		t.Fatalf("body length = %d, want 2", len(function.Body))
	}
	if statement, ok := function.Body[0].(ReturnStatement); !ok || statement.Value != nil {
		t.Fatalf("body[0] = %#v, want a bare return", function.Body[0])
	}
	if _, ok := function.Body[1].(CallExpression); !ok {
		t.Fatalf("body[1] = %#v, want a call statement", function.Body[1])
	}
}

func TestParseCallSameLineRule(t *testing.T) {
	// Positive: the '(' follows its callee on the same line.
	if _, ok := parseInitializer(t, "result: Int32 := compute(value)").(CallExpression); !ok {
		t.Fatal("same-line call was not parsed as a call")
	}

	// Positive: line breaks inside the argument list are fine.
	if _, ok := parseInitializer(t, "result: Int32 := compute(\nvalue,\n2\n)").(CallExpression); !ok {
		t.Fatal("call with a multi-line argument list was not parsed as a call")
	}

	// Negative: a newline between callee and '(' splits the two items.
	message := parseError(t, "result: Int32 := compute\n(value)")
	if !strings.Contains(message, "a call's ( must follow its callee on the same line") {
		t.Fatalf("error = %q, want the same-line call diagnostic", message)
	}
}

func TestParseReturnSameLineRule(t *testing.T) {
	// Positive: the value begins on the return's line.
	function := parseOneItem(t, "fun f() : Int32 do\nreturn 1 +\n2\nend").(FunctionDeclaration)
	if statement := function.Body[0].(ReturnStatement); statement.Value == nil {
		t.Fatal("return value starting on the return line was dropped")
	}

	// Negative: a value-only token on the next line cannot be a statement.
	message := parseError(t, "fun f() : Int32 do\nreturn\n1\nend")
	if !strings.Contains(message, "a return value must begin on the same line as return") {
		t.Fatalf("error = %q, want the same-line return diagnostic", message)
	}
}

func TestParseFunctionDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun adder(left: Int32) do\nreturn left", "expected end to close function adder"},
		{"fun adder(left)\nend", "function parameters require type annotations"},
		{"mut fun adder()\nend", "mut cannot modify a function declaration; declare a mut Fun binding"},
		{"fun outer() do\nfun inner()\nend\nend", "function declarations are module-level only"},
		{"fun outer() do\nimpl Point.m()\nend\nend", "impl declarations are module-level only"},
	} {
		message := parseError(t, testCase.source)
		if !strings.Contains(message, testCase.want) {
			t.Errorf("error for %q = %q, want %q", testCase.source, message, testCase.want)
		}
	}
}

func TestParseRejectsModuleLevelReturn(t *testing.T) {
	message := parseError(t, "return 1")
	if !strings.Contains(message, "return is only valid inside a function or method body") {
		t.Fatalf("error = %q, want a module-level return diagnostic", message)
	}
}

func TestParseSelfReceiverExpression(t *testing.T) {
	method := parseOneItem(t, "impl Point.grow() do\nself.x = 1\nend").(ImplDeclaration)
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
