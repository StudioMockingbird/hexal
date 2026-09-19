package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

// A named function, method, and anonymous literal each parse one final rest
// parameter and preserve its marker.
func TestParseRestParameterOnEachDeclarationForm(t *testing.T) {
	function := parseOneItem(t, "fun sum(values: Int32...) : Int32 do\nreturn 0\nend").(FunctionDeclaration)
	if len(function.Parameters) != 1 || !function.Parameters[0].Rest {
		t.Fatalf("function parameters = %#v, want one rest parameter", function.Parameters)
	}
	if function.Parameters[0].Ellipsis.Kind != lexer.Ellipsis {
		t.Fatalf("function rest marker = %#v, want Ellipsis", function.Parameters[0].Ellipsis)
	}

	method := parseOneItem(t, "method Worker.run(values: String...) do\nend").(MethodDeclaration)
	if len(method.Parameters) != 1 || !method.Parameters[0].Rest {
		t.Fatalf("method parameters = %#v, want one rest parameter", method.Parameters)
	}

	literal := parseInitializer(t, "let f: Fun<(String...)> = fun (values: String...) do\nend").(AnonymousFunctionLiteral)
	if len(literal.Parameters) != 1 || !literal.Parameters[0].Rest {
		t.Fatalf("literal parameters = %#v, want one rest parameter", literal.Parameters)
	}
}

// A Fun type records which final parameter is written `T...`.
func TestParseRestFunctionTypeParameter(t *testing.T) {
	function := parseOneItem(t, "fun f(cb: Fun<(String, Byte...) : Int32>) : Nil do\nreturn nil\nend").(FunctionDeclaration)
	typ, ok := function.Parameters[0].Type.(FunctionTypeExpression)
	if !ok {
		t.Fatalf("parameter type = %#v, want FunctionTypeExpression", function.Parameters[0].Type)
	}
	if len(typ.Parameters) != 2 || len(typ.RestFlags) != 2 {
		t.Fatalf("type parameters = %#v, flags = %v", typ.Parameters, typ.RestFlags)
	}
	if typ.RestFlags[0] || !typ.RestFlags[1] {
		t.Fatalf("rest flags = %v, want [false true]", typ.RestFlags)
	}
	if typ.RestTokens[1].Kind != lexer.Ellipsis {
		t.Fatalf("rest token = %#v, want Ellipsis", typ.RestTokens[1])
	}
}

// A declaration or Fun type with a parameter after `T...` is rejected; this
// also covers two rest parameters.
func TestParseRejectsParameterAfterRest(t *testing.T) {
	for _, source := range []string{
		"fun f(a: Int32..., b: Int32) do\nend",
		"fun f(a: Int32..., b: Int32...) do\nend",
		"fun f(cb: Fun<(Int32..., Byte)>) do\nend",
	} {
		if message := parseError(t, source); !strings.Contains(message, "rest parameter must be final") {
			t.Errorf("Parse(%q) error = %q, want rest-final", source, message)
		}
	}
}

// An ellipsis after a call argument is a syntax error; there is no call-site
// spread.
func TestParseRejectsSpreadCallArgument(t *testing.T) {
	message := parseError(t, "f(1, value...)")
	if !strings.Contains(message, "spread arguments are not supported; pass explicit values") {
		t.Fatalf("Parse error = %q, want the spread diagnostic", message)
	}
}
