package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// size_of<T>(), align_of<T>(), and volatile integer pointer accesses.

// layoutBuiltins is the set of protected layout-query names.
var layoutBuiltins = map[string]bool{
	"size_of":  true,
	"align_of": true,
}

// checkLayoutCall resolves size_of<T>() and align_of<T>(): exactly one
// explicit type argument, no value arguments, result Size. The type must be
// complete and representable; a type parameter defers validation to the
// specialization pass, which re-checks the body with concrete arguments.
func checkLayoutCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, callee.Lexeme+" requires exactly one type argument"))}
	}
	if len(call.Arguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, callee.Lexeme+" takes no value arguments"))}
	}
	use, diagnostic := resolveTypeUse(call.TypeArguments[0], callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if !layoutEligible(use.Type) {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, callee.Lexeme+" requires one complete finite-sized type; got "+use.Type.Name))}
	}
	node := Expression{Kind: LayoutExpression, Name: callee.Lexeme, OperandType: use.Type, ResultType: compilerTypes.SizeType}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: callee.Lexeme, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee}
}

// layoutEligible reports whether one type has a settled generated-C layout:
// complete and finite-sized, excluding Unknown, incomplete types, and
// no-result function types. A type parameter defers the decision to
// specialization, where the concrete argument is validated.
func layoutEligible(typ compilerTypes.Type) bool {
	if typ == (compilerTypes.Type{}) {
		return false
	}
	if compilerTypes.ContainsTypeParameter(typ) {
		return true
	}
	if compilerTypes.IsUnknown(typ) || typ.Incomplete {
		return false
	}
	if typ.Signature != nil {
		return typ.Signature.Result != nil
	}
	return compilerTypes.IsCompleteValue(typ)
}

// volatileEligibleType reports whether one element type supports volatile
// access in v1: exactly the integer storage types.
func volatileEligibleType(typ compilerTypes.Type) bool {
	return compilerTypes.Equal(typ, compilerTypes.Int8) ||
		compilerTypes.Equal(typ, compilerTypes.Int16) ||
		compilerTypes.Equal(typ, compilerTypes.Int32) ||
		compilerTypes.Equal(typ, compilerTypes.Int64) ||
		compilerTypes.Equal(typ, compilerTypes.UInt8) ||
		compilerTypes.Equal(typ, compilerTypes.UInt16) ||
		compilerTypes.Equal(typ, compilerTypes.UInt32) ||
		compilerTypes.Equal(typ, compilerTypes.UInt64) ||
		compilerTypes.IsSize(typ)
}

// checkVolatileCall resolves read_volatile() and write_volatile(value) on
// Ptr<T> and MutPtr<T> receivers whose element is an integer storage type.
func checkVolatileCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	element := *receiver.typ.Element
	if !volatileEligibleType(element) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "volatile access is supported only for integer storage types; got "+element.Name))}
	}
	if diagnostic := freedPointeeDiagnostic(receiver, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	switch name {
	case "read_volatile":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "read_volatile expects no arguments"))}
		}
		node := Expression{Kind: VolatileReadExpression, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: element, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: element, Name: name, Node: node}
		return checkedExpression{source: source, typ: element, token: callee.Property}
	case "write_volatile":
		if receiver.typ.PointeeWritable == false {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Ptr<"+element.Name+"> is read-only; volatile write requires MutPtr<"+element.Name+">"))}
		}
		if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "write_volatile expects 1 argument"))}
		}
		value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, value.typ) {
			return checkedExpression{token: value.token, diagnostic: diagnosticAt(typeErrorAt(value.token, fmt.Sprintf("write_volatile requires %s; got %s", element.Name, value.typ.Name)))}
		}
		node := Expression{Kind: VolatileWriteExpression, Operand: &receiver.source.Node, Arguments: []Operand{value.source}, OperandType: receiver.typ, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	}
	return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "volatile access supports read_volatile and write_volatile only"))}
}
