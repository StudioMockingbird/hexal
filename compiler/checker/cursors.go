package checker

import (
	diag "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

// checkCursorMethodCall dispatches the shared text cursor surface: has_next,
// next, peek, and offset. offset is always a byte offset on every cursor, the
// one unit all three share and the one slice() takes.
func checkCursorMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	fail := func(message diag.Message) checkedExpression {
		diagnostic := messageAt(call.callee.Property, message)
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	var result compilerTypes.Type
	switch name {
	case "has_next":
		result = compilerTypes.Bool
	case "next", "peek":
		// The element type follows the cursor: a ByteCursor yields Byte, a
		// RuneCursor yields Rune, and a GraphemeCursor yields Grapheme.
		switch {
		case compilerTypes.IsByteCursor(call.receiver.typ):
			result = compilerTypes.UInt8
		case compilerTypes.IsGraphemeCursor(call.receiver.typ):
			result = compilerTypes.GraphemeType
		default:
			result = compilerTypes.Rune
		}
	case "offset":
		result = compilerTypes.SizeType
	default:
		return fail(diag.CursorMethodNotFound(call.receiver.typ.Name, name))
	}
	if len(call.call.Arguments) != 0 {
		return fail(diag.CursorMethodNoValueArguments(name))
	}
	if len(call.call.TypeArguments) != 0 {
		return fail(diag.CursorMethodNoTypeArguments(name))
	}
	// next advances the cursor, so it takes the address of the receiver and
	// the receiver must be a mutable binding. peek, has_next, and offset read a
	// copy. A GraphemeCursor's peek caches its lookahead so a following next
	// shares the one break-state advance, so it takes the address too.
	address := name == "next" || (name == "peek" && compilerTypes.IsGraphemeCursor(call.receiver.typ))
	if address && !call.receiver.source.Writable {
		return fail(diag.CursorMethodRequiresMutableBinding(name))
	}
	node := Expression{
		Kind:        CursorMethodCallExpression,
		Name:        name,
		Operand:     &call.receiver.source.Node,
		OperandType: call.receiver.typ,
		ResultType:  result,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
	return checkedExpression{source: source, typ: result, token: call.callee.Property}
}
