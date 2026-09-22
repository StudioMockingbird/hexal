package checker

import (
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkCursorMethodCall dispatches the shared text cursor surface: has_next,
// next, peek, and offset. offset is always a byte offset on every cursor, the
// one unit all three share and the one slice() takes.
func checkCursorMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, _ checkContext) checkedExpression {
	name := callee.Property.Lexeme
	fail := func(message string) checkedExpression {
		diagnostic := typeErrorAt(callee.Property, message)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	var result compilerTypes.Type
	switch name {
	case "has_next":
		result = compilerTypes.Bool
	case "next", "peek":
		// The element type follows the cursor: a ByteCursor yields Byte, a
		// RuneCursor yields Rune, and a GraphemeCursor yields Grapheme.
		switch {
		case compilerTypes.IsByteCursor(receiver.typ):
			result = compilerTypes.UInt8
		case compilerTypes.IsGraphemeCursor(receiver.typ):
			result = compilerTypes.GraphemeType
		default:
			result = compilerTypes.Rune
		}
	case "offset":
		result = compilerTypes.SizeType
	default:
		return fail(receiver.typ.Name + " has no method " + name)
	}
	if len(call.Arguments) != 0 {
		return fail(name + " expects no arguments")
	}
	if len(call.TypeArguments) != 0 {
		return fail(name + " takes no type arguments")
	}
	// next advances the cursor, so it takes the address of the receiver and
	// the receiver must be a mutable binding. peek, has_next, and offset read a
	// copy. A GraphemeCursor's peek caches its lookahead so a following next
	// shares the one break-state advance, so it takes the address too.
	address := name == "next" || (name == "peek" && compilerTypes.IsGraphemeCursor(receiver.typ))
	if address && !receiver.source.Writable {
		return fail(name + " mutates its cursor, so the receiver must be a mutable binding")
	}
	node := Expression{
		Kind:        CursorMethodCallExpression,
		Name:        name,
		Operand:     &receiver.source.Node,
		OperandType: receiver.typ,
		ResultType:  result,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
	return checkedExpression{source: source, typ: result, token: callee.Property}
}
