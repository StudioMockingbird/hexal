package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// bitCastEligibleType reports whether typ may be a bit_cast source or
// destination: a fixed-representation scalar at 8, 16, 32, or 64 bits.
// Size, Rune, Bool, pointers, aggregates, and managed values are rejected.
func bitCastEligibleType(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Int8), compilerTypes.Equal(typ, compilerTypes.UInt8),
		compilerTypes.Equal(typ, compilerTypes.Int16), compilerTypes.Equal(typ, compilerTypes.UInt16),
		compilerTypes.Equal(typ, compilerTypes.Int32), compilerTypes.Equal(typ, compilerTypes.UInt32),
		compilerTypes.Equal(typ, compilerTypes.Int64), compilerTypes.Equal(typ, compilerTypes.UInt64),
		compilerTypes.Equal(typ, compilerTypes.Float32), compilerTypes.Equal(typ, compilerTypes.Float64):
		return true
	}
	return false
}

// endianEligibleType reports whether typ provides endian byte conversion:
// every fixed-width integer, excluding Size (whose width follows the target)
// and Rune (whose value is a Unicode scalar, not arbitrary 32-bit payload).
func endianEligibleType(typ compilerTypes.Type) bool {
	return compilerTypes.IsInteger(typ) && !compilerTypes.IsRune(typ) && !compilerTypes.Equal(typ, compilerTypes.SizeType)
}

// checkBitCastCall resolves `receiver.bit_cast<Dest>()`. The
// method takes exactly one explicit type argument and no value arguments;
// source and destination must be same-width eligible scalars.
func checkBitCastCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "bit_cast requires exactly 1 explicit type argument"))}
	}
	if len(call.Arguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "bit_cast accepts no value arguments"))}
	}
	targetUse, diagnostic := resolveTypeUse(call.TypeArguments[0], call.OpenParen, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	target := targetUse.Type
	if !bitCastEligibleType(receiver.typ) || !bitCastEligibleType(target) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "bit_cast requires equal-width eligible scalar types; got "+receiver.typ.Name+" and "+target.Name))}
	}
	if receiver.typ.Bits != target.Bits {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, fmt.Sprintf("bit_cast requires equal-width eligible scalar types; got %s and %s", receiver.typ.Name, target.Name)))}
	}
	node := Expression{Kind: BitCastExpression, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: target}
	source := Operand{Kind: ExpressionOperand, Type: target, Name: "bit_cast", Node: node}
	return checkedExpression{source: source, typ: target, token: callee.Property}
}

// checkEndianToBytesCall resolves `value.to_le_bytes()` and
// `value.to_be_bytes()`. The result is Array<Byte, width / 8>.
func checkEndianToBytesCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, callee.Property.Lexeme+" takes no arguments"))}
	}
	if !endianEligibleType(receiver.typ) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, callee.Property.Lexeme+" requires a fixed-width integer receiver; got "+receiver.typ.Name))}
	}
	array := typeEnvironment.ArrayType(compilerTypes.UInt8, uint64(receiver.typ.Bits/8))
	if array == (compilerTypes.Type{}) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(unknownAt(callee.Property, "could not construct the endian byte array type"))}
	}
	memberIndex := 0
	if callee.Property.Lexeme == "to_be_bytes" {
		memberIndex = 1
	}
	node := Expression{Kind: EndianConversionExpression, Name: "to", Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: array, Element: receiver.typ, MemberIndex: memberIndex}
	source := Operand{Kind: ExpressionOperand, Type: array, Name: callee.Property.Lexeme, Node: node}
	return checkedExpression{source: source, typ: array, token: callee.Property}
}

// checkEndianFromBytesCall resolves the type-qualified
// `Int32.from_le_bytes(bytes)` and `Int32.from_be_bytes(bytes)` intrinsics.
// The argument must be exactly Array<Byte, width / 8>.
func checkEndianFromBytesCall(call parser.CallExpression, callee lexer.Token, typeEnvironment *compilerTypes.Environment, names *scope) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	integerType, ok := typeEnvironment.Lookup(callee.Lexeme)
	if !ok || !endianEligibleType(integerType) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, callee.Lexeme+" has no such operation; from_le_bytes and from_be_bytes require a fixed-width integer type"))}
	}
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, property.Lexeme+" expects exactly 1 argument"))}
	}
	array := typeEnvironment.ArrayType(compilerTypes.UInt8, uint64(integerType.Bits/8))
	if array == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the endian byte array type"))}
	}
	bytes := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(array), tokenOf(call.Arguments[0]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(bytes); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !compilerTypes.Equal(bytes.typ, array) {
		return checkedExpression{token: bytes.token, diagnostic: diagnosticAt(typeErrorAt(bytes.token, fmt.Sprintf("%s.from_%s expects Array<Byte, %d>; got %s", callee.Lexeme, endianOrderName(property.Lexeme), integerType.Bits/8, bytes.typ.Name)))}
	}
	memberIndex := 0
	if property.Lexeme == "from_be_bytes" {
		memberIndex = 1
	}
	node := Expression{Kind: EndianConversionExpression, Name: "from", Operand: &bytes.source.Node, Arguments: []Operand{bytes.source}, OperandType: array, ResultType: integerType, Element: integerType, MemberIndex: memberIndex}
	source := Operand{Kind: ExpressionOperand, Type: integerType, Name: property.Lexeme, Node: node}
	return checkedExpression{source: source, typ: integerType, token: property}
}

func endianOrderName(name string) string {
	if name == "from_be_bytes" || name == "to_be_bytes" {
		return "be"
	}
	return "le"
}
