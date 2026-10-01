package checker

import (
	diag "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

// BitCastEligibleType reports whether typ may be a bit_cast source or
// destination: a fixed-representation scalar at 8, 16, 32, or 64 bits.
// Size, Bool, pointers, aggregates, and managed values are rejected.
func BitCastEligibleType(typ compilerTypes.Type) bool {
	return bitCastEligibleType(typ)
}

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
// every fixed-width integer, excluding Size (whose width follows the target).
func endianEligibleType(typ compilerTypes.Type) bool {
	return compilerTypes.IsInteger(typ) && !compilerTypes.Equal(typ, compilerTypes.SizeType)
}

// checkBitCastCall resolves `receiver.bit_cast<Dest>()`. The
// method takes exactly one explicit type argument and no value arguments;
// source and destination must be same-width eligible scalars.
func checkBitCastCall(call methodCall) checkedExpression {
	if len(call.call.TypeArguments) != 1 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.BitCastTypeArgumentCount()))}
	}
	if len(call.call.Arguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.BitCastValueArgumentCount()))}
	}
	targetUse, diagnostic := resolveTypeUse(call.call.TypeArguments[0], call.call.OpenParen, call.ctx.typeEnvironment, call.ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	target := targetUse.Type
	if !bitCastEligibleType(call.receiver.typ) || !bitCastEligibleType(target) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.BitCastIneligibleTypes(call.receiver.typ.Name, target.Name)))}
	}
	if call.receiver.typ.Bits != target.Bits {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.BitCastWidthMismatch(call.receiver.typ.Name, target.Name)))}
	}
	node := Expression{Kind: BitCastExpression, Operand: &call.receiver.source.Node, OperandType: call.receiver.typ, ResultType: target}
	source := Operand{Kind: ExpressionOperand, Type: target, Name: "bit_cast", Node: node}
	return checkedExpression{source: source, typ: target, token: call.callee.Property}
}

// checkEndianToBytesCall resolves `value.to_le_bytes()` and
// `value.to_be_bytes()`. The result is List<Byte, width / 8>.
func checkEndianToBytesCall(call methodCall) checkedExpression {
	if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.EndianConversionNoArguments(call.callee.Property.Lexeme)))}
	}
	if !endianEligibleType(call.receiver.typ) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.EndianConversionInvalidReceiver(call.callee.Property.Lexeme, call.receiver.typ.Name)))}
	}
	array := call.ctx.typeEnvironment.InlineListType(compilerTypes.UInt8, uint64(call.receiver.typ.Bits/8))
	if array == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	memberIndex := 0
	if call.callee.Property.Lexeme == "to_be_bytes" {
		memberIndex = 1
	}
	node := Expression{Kind: EndianConversionExpression, Name: "to", Operand: &call.receiver.source.Node, OperandType: call.receiver.typ, ResultType: array, Element: call.receiver.typ, MemberIndex: memberIndex}
	source := Operand{Kind: ExpressionOperand, Type: array, Name: call.callee.Property.Lexeme, Node: node}
	return checkedExpression{source: source, typ: array, token: call.callee.Property}
}

// checkEndianDecodeCall resolves `bytes.decode_le<Int32>()` and
// `bytes.decode_be<Int32>()` on an inline List<Byte, N>. N must equal the
// integer's width in bytes, so a wrong-sized receiver fails at the call
// rather than reading a partial value.
func checkEndianDecodeCall(call methodCall) checkedExpression {
	property := call.callee.Property
	if len(call.call.TypeArguments) != 1 || len(call.call.Arguments) != 0 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.EndianDecodeArgumentCount(property.Lexeme)))}
	}
	targetUse, diagnostic := resolveTypeUse(call.call.TypeArguments[0], call.call.OpenParen, call.ctx.typeEnvironment, call.ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	integerType := targetUse.Type
	if !endianEligibleType(integerType) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.EndianDecodeInvalidType(property.Lexeme, integerType.Name)))}
	}
	array := call.ctx.typeEnvironment.InlineListType(compilerTypes.UInt8, uint64(integerType.Bits/8))
	if array == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
	}
	if !compilerTypes.Equal(call.receiver.typ, array) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.EndianDecodeTypeMismatch(property.Lexeme, integerType.Name, integerType.Bits/8, call.receiver.typ.Name)))}
	}
	memberIndex := 0
	if property.Lexeme == "decode_be" {
		memberIndex = 1
	}
	node := Expression{Kind: EndianConversionExpression, Name: "from", Operand: &call.receiver.source.Node, Arguments: []Operand{call.receiver.source}, OperandType: array, ResultType: integerType, Element: integerType, MemberIndex: memberIndex}
	source := Operand{Kind: ExpressionOperand, Type: integerType, Name: property.Lexeme, Node: node}
	return checkedExpression{source: source, typ: integerType, token: property}
}
