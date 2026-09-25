package parser

// Statement parsing and recovery: synchronisation, repeated-keyword and
// import placement rejection, module reference forms, and
// delimiter-preserving recovery around nested statements.

import (
	"strings"
	"testing"
	"time"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

func TestParseRecoversAtNextStatement(t *testing.T) {
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = 13 y let z: Int32 = 14")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	program, err := Parse(tokens)
	if err == nil {
		t.Fatal("Parse accepted an invalid statement")
	}
	if got, want := err.Error(), "[Syntax Error] expected '=' for an assignment at 1:21"; got != want {
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
	tokens, err := lexer.Lex("test.hex", "let x: Int32 = 13 invalid let z: Int32 = 14 point.foo = 15")
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
	tokens, err := lexer.Lex("test.hex", "type")
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
			tokens, err := lexer.Lex("test.hex", source)
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
// after a type, function, or method declaration, or after an executable
// statement, is a positioned Syntax Error, while imports-only-first programs
// keep parsing.
func TestParseRejectsImportAfterTopLevelItem(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		source     string
		importLine int
	}{
		{"type declaration", "type T is struct n: Int32 end\nimport\n    a from \"./a\"\nend\n", 2},
		{"function declaration", "fun f(): Int32 do\n    return 1\nend\nimport\n    a from \"./a\"\nend\n", 4},
		{"method declaration", "type T is struct n: Int32 end\nmethod T.act() do\nend\nimport\n    a from \"./a\"\nend\n", 4},
		{"executable statement", "let x: Int32 = 1\nimport\n    a from \"./a\"\nend\n", 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, err := lexer.Lex("test.hex", testCase.source)
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
				if diagnostics[index].Message == "import block must be the first top-level construct" {
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
	if _, err := Parse(mustLex(t, "import\n    a from \"./a\"\n,\n    b from \"./b\"\nend\nlet x: Int32 = 1\n")); err != nil {
		t.Fatalf("Parse rejected an imports-first program: %v", err)
	}
}

// A module reference is either a quoted relative source-map path or a dotted
// std.<component> standard-library reference. Every retired or malformed form
// reports its exact Syntax Error at the specified anchor.
func TestParseModuleReferenceForms(t *testing.T) {
	accepted := []string{
		"import\n    a from \"./a\"\n,\n    b from \"../b\"\n,\n    c from std.io\nend\n",
		"import\n    h from std.crypto.hash\nend\n",
		"import\n    p from std.program\nend\n",
	}
	for _, source := range accepted {
		if _, err := Parse(mustLex(t, source)); err != nil {
			t.Errorf("Parse(%q) rejected an accepted module reference: %v", source, err)
		}
	}
	for _, testCase := range []struct {
		source  string
		message string
		line    int
		column  int
	}{
		{"import\n    Io from \"std/io\"\nend\n", "standard-library imports use dotted paths; write std.io", 2, 13},
		{"import\n    X from \"vendor/x\"\nend\n", "quoted import paths must begin with ./ or ../", 2, 12},
		{"import\n    X from std\nend\n", "standard-library import requires a component after std.", 2, 12},
		{"import\n    X from std/io\nend\n", "standard-library imports use dots between components", 2, 15},
		{"import\n    X from std..io\nend\n", "expected a standard-library module component after '.'", 2, 16},
		{"import\n    X from std.for\nend\n", "expected a standard-library module component after '.'", 2, 16},
		{"import\n    X from\n        std.io\nend\n", "module reference must begin on the same line as 'from'", 3, 9},
	} {
		tokens, err := lexer.Lex("test.hex", testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		_, parseErr := Parse(tokens)
		if parseErr == nil {
			t.Errorf("Parse(%q) accepted an invalid module reference", testCase.source)
			continue
		}
		diagnostics, ok := parseErr.(compilerTypes.Diagnostics)
		if !ok {
			t.Fatalf("Parse error = %T, want Diagnostics", parseErr)
		}
		var found *compilerTypes.Diagnostic
		for index := range diagnostics {
			if diagnostics[index].Message == testCase.message {
				found = &diagnostics[index]
				break
			}
		}
		if found == nil {
			t.Errorf("Parse(%q) diagnostics = %v, want message %q", testCase.source, diagnostics, testCase.message)
			continue
		}
		if found.Category != compilerTypes.SyntaxError || found.Line != testCase.line || found.Column != testCase.column {
			t.Errorf("Parse(%q) diagnostic = %#v, want Syntax Error at %d:%d", testCase.source, found, testCase.line, testCase.column)
		}
	}
}

func TestParseRecoversAfterConsumedMalformedStatement(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		source     string
		wantName   string
		wantAssign bool
	}{
		{name: "declaration", source: "broken let value: Int32 = 1", wantName: "value"},
		{name: "assignment", source: "broken value = 1", wantName: "value", wantAssign: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, err := lexer.Lex("test.hex", testCase.source)
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
	tokens, err := lexer.Lex("test.hex", "if true then while false do else end end let recovered: Int32 = 1")
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
	tokens, err := lexer.Lex("test.hex", "if true then while false do else let sibling: Int32 = 1 end let after: Int32 = 2 end")
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
	tokens, err := lexer.Lex("test.hex", "fun choose(): Int32 do if true then broken return 1 else return 2 end end")
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
	tokens, err := lexer.Lex("test.hex", "if true then broken self.value = 1 end")
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
	tokens, err := lexer.Lex("test.hex", "fun keep() do while true do else let value: Int32 = 1 end let after: Int32 = 2 end")
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
	tokens, err := lexer.Lex("test.hex", "if true then broken point.step(1) end")
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
