package parser

// Methods, calls, and returns: impl receivers, call chains and member
// selection, argument lists, return forms, and same-line call and return
// rules.

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseImplReceiverForms(t *testing.T) {
	for _, testCase := range []struct {
		source     string
		writable   bool
		pointer    bool
		typeLexeme string
	}{
		{source: "method Point.translate(dx: Int32) do\nend", typeLexeme: "Point"},
		{source: "method Ptr<Point>.length() do\nend", pointer: true, typeLexeme: "Point"},
		{source: "method Ptr<mut Point>.reset() do\nend", pointer: true, writable: true, typeLexeme: "Point"},
	} {
		item := parseOneItem(t, testCase.source)
		method, ok := item.(MethodDeclaration)
		if !ok {
			t.Fatalf("item for %q = %#v, want MethodDeclaration", testCase.source, item)
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
	method := parseOneItem(t, "method Point.translate(dx: Int32) do\nself.x = dx\nend").(MethodDeclaration)
	if method.Name.Lexeme != "translate" {
		t.Fatalf("method name = %q, want translate", method.Name.Lexeme)
	}
	if len(method.Body) != 1 {
		t.Fatalf("body length = %d, want 1", len(method.Body))
	}
}

func TestParseNestedCallArguments(t *testing.T) {
	call, ok := parseInitializer(t, "let x: Int32 = f(g(1), 2)").(CallExpression)
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
	call := parseInitializer(t, "let x: Int32 = now()").(CallExpression)
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
	property, ok := parseInitializer(t, "let x: Int32 = point.translate(1, 2).x").(PropertyExpression)
	if !ok {
		t.Fatal("initializer is not a property selection")
	}
	if _, ok := property.Receiver.(CallExpression); !ok {
		t.Fatalf("receiver = %#v, want call", property.Receiver)
	}
}

func TestParseChainEndingInMemberIsNotAStatement(t *testing.T) {
	message := parseError(t, "point.x")
	if !strings.Contains(message, "expected '=' for an assignment") {
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
	if _, ok := parseInitializer(t, "let result: Int32 = compute(value)").(CallExpression); !ok {
		t.Fatal("same-line call was not parsed as a call")
	}

	// Positive: line breaks inside the argument list are fine.
	if _, ok := parseInitializer(t, "let result: Int32 = compute(\nvalue,\n2\n)").(CallExpression); !ok {
		t.Fatal("call with a multi-line argument list was not parsed as a call")
	}

	// Negative: a newline between callee and '(' splits the two items.
	message := parseError(t, "let result: Int32 = compute\n(value)")
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
