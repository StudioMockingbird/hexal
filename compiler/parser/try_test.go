package parser

import "testing"

func TestParseTryStatement(t *testing.T) {
	program := parseOneItem(t, "try flush()").(TryStatement)
	if _, ok := program.Operand.(CallExpression); !ok {
		t.Fatalf("operand = %#v, want call", program.Operand)
	}

	value := parseOneItem(t, "try value").(TryStatement)
	if _, ok := value.Operand.(VariableExpression); !ok {
		t.Fatalf("operand = %#v, want variable", value.Operand)
	}
}
