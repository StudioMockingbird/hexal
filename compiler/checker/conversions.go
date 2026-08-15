package checker

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// truncateTowardZero applies the exact integer part of an arbitrary
// rational value toward zero, without any fixed-width intermediate.
// go/constant.ToInt only converts whole values (a fraction returns
// Unknown), so truncation divides the exact numerator by the exact
// denominator with big.Int truncated division, which is defined for every
// magnitude and sign.
func truncateTowardZero(value constant.Value) constant.Value {
	numerator := constant.Num(value)
	if numerator.Kind() == constant.Unknown {
		return numerator
	}
	return constant.BinaryOp(numerator, gotoken.QUO_ASSIGN, constant.Denom(value))
}

// The one explicit scalar conversion spelling is `source.to<Dest>()`. Every
// conversion is checked: a known-invalid constant fails compilation, and an
// invalid runtime value traps before any unsafe C conversion.

// checkConversionCall resolves the compiler-owned `to<Dest>()` method on an
// eligible scalar receiver. The receiver-scoped builtin resolves before
// ordinary method lookup; an unrelated nominal object may declare its own
// method named `to`.
func checkConversionCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	source := receiver.typ
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "to requires exactly 1 explicit type argument",
		}}
	}
	if len(call.Arguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "to accepts no value arguments",
		}}
	}
	targetUse, diagnostic := resolveTypeUse(call.TypeArguments[0], call.OpenParen, typeEnvironment, environment.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	target := targetUse.Type
	if !conversionPairValid(source, target) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "numeric conversion requires a supported scalar source and destination; got " + source.Name + " and " + target.Name,
		}}
	}

	// A known-invalid constant conversion is a compile-time error; valid
	// constants fold to their destination value. A negative float literal
	// keeps its positive Constant with the sign in FloatBits/Negative, so
	// the fold must apply the sign itself. The fold reasons from the
	// source type's already-rounded checked IEEE value, never the exact
	// lexical constant: a Float32 literal may round before conversion, and
	// only the checked bits carry that rounding. The bits store the full
	// signed value, and Negative mirrors the sign bit, so rebuild the
	// magnitude from the bits and apply the operand's sign.
	if receiver.source.Kind == ConstantOperand && receiver.source.Constant != nil {
		value := receiver.source.Constant
		if compilerTypes.IsFloat(source) {
			if compilerTypes.Equal(source, compilerTypes.Float32) {
				bits := uint32(receiver.source.FloatBits) &^ (uint32(1) << 31)
				value = constant.MakeFloat64(float64(math.Float32frombits(bits)))
			} else {
				bits := receiver.source.FloatBits &^ (uint64(1) << 63)
				value = constant.MakeFloat64(math.Float64frombits(bits))
			}
			if receiver.source.Negative {
				value = constant.UnaryOp(gotoken.SUB, value, 0)
			}
		}
		if folded, diagnostic := foldNumericConversion(value, source, target, callee.Property); diagnostic != nil || folded != nil {
			if diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
			// A folded conversion is no longer the original literal, so it
			// carries no literal text for the generator to re-validate.
			foldedSource := constantOperand(target, folded, "")
			if compilerTypes.IsFloat(target) {
				// Record the folded float's rounded IEEE bits so a chained
				// conversion reasons from the rounded value rather than the
				// exact rational the fold keeps.
				if compilerTypes.Equal(target, compilerTypes.Float32) {
					converted, _ := constant.Float32Val(folded)
					foldedSource.FloatBits = uint64(math.Float32bits(converted))
				} else {
					converted, _ := constant.Float64Val(folded)
					foldedSource.FloatBits = math.Float64bits(converted)
				}
			}
			foldedSource.Node = constantNode(foldedSource)
			return checkedExpression{source: foldedSource, typ: target, token: callee.Property, known: &foldedSource}
		}
	}

	node := Expression{
		Kind:        ConversionExpression,
		Name:        "to",
		Operand:     &receiver.source.Node,
		OperandType: source,
		ResultType:  target,
	}
	sourceOperand := Operand{Kind: ExpressionOperand, Type: target, Name: "to", Node: node}
	return checkedExpression{source: sourceOperand, typ: target, token: callee.Property}
}

// conversionPairValid applies the source/destination conversion matrix.
// Integer
// means the eight fixed-width integer types plus Size; Rune is not a numeric
// arithmetic type and participates only in checked integer conversion.
func conversionPairValid(source, target compilerTypes.Type) bool {
	if compilerTypes.ContainsTypeParameter(source) || compilerTypes.ContainsTypeParameter(target) {
		// Dependent conversion: the closed specialization must resolve one
		// eligible pair before generation.
		return true
	}
	switch {
	case compilerTypes.IsFloat(source) && compilerTypes.IsFloat(target):
		return true
	case compilerTypes.IsFloat(source) && compilerTypes.IsInteger(target) && !compilerTypes.IsRune(target):
		return true
	case compilerTypes.IsInteger(source) && compilerTypes.IsFloat(target):
		return true
	case compilerTypes.IsInteger(source) && compilerTypes.IsInteger(target):
		// Integer-to-Rune checks Unicode scalar validity; Rune is excluded
		// from Rune-to-Rune and from arithmetic participation.
		if compilerTypes.IsRune(source) && compilerTypes.IsRune(target) {
			return false
		}
		return true
	}
	return false
}

// foldNumericConversion evaluates a constant conversion. It returns a folded
// value, or a diagnostic for a known-invalid conversion, or nil for both
// when the operand is not a constant after all.
func foldNumericConversion(value constant.Value, source, target compilerTypes.Type, token lexer.Token) (constant.Value, *compilerTypes.Diagnostic) {
	if compilerTypes.IsInteger(source) {
		if compilerTypes.IsInteger(target) {
			return foldIntegerConversion(value, source, target, token)
		}
		// Integer to float: nearest representable value, ties-to-even.
		floatValue := constant.ToFloat(value)
		if floatValue.Kind() == constant.Unknown {
			return nil, &compilerTypes.Diagnostic{Category: compilerTypes.TypeError, Stage: "checker", Line: token.Line, Column: token.Column, Message: "value cannot be represented as " + target.Name}
		}
		return floatValue, nil
	}
	if compilerTypes.IsFloat(source) {
		if compilerTypes.IsInteger(target) {
			// Truncate the already-rounded source value toward zero without
			// an Int64 intermediate, then range-check the arbitrary-precision
			// result against the destination's exact bounds. go/constant.ToInt
			// only accepts whole values, so truncation uses exact rational
			// arithmetic; a value in (-1, 0) truncates to signed zero and is
			// valid for an unsigned destination.
			if value.Kind() == constant.Unknown {
				diagnostic := typeErrorAt(token, "floating value cannot be converted to "+target.Name)
				return nil, &diagnostic
			}
			truncated := truncateTowardZero(value)
			return foldIntegerConversion(truncated, compilerTypes.Int64, target, token)
		}
		// Float to float: nearest representable value.
		floatValue := constant.ToFloat(value)
		if floatValue.Kind() == constant.Unknown {
			return nil, &compilerTypes.Diagnostic{Category: compilerTypes.TypeError, Stage: "checker", Line: token.Line, Column: token.Column, Message: "value cannot be represented as " + target.Name}
		}
		return floatValue, nil
	}
	return nil, nil
}

func foldIntegerConversion(value constant.Value, source, target compilerTypes.Type, token lexer.Token) (constant.Value, *compilerTypes.Diagnostic) {
	integer := constant.ToInt(value)
	if integer.Kind() == constant.Unknown {
		return nil, &compilerTypes.Diagnostic{Category: compilerTypes.TypeError, Stage: "checker", Line: token.Line, Column: token.Column, Message: "value is not an integer"}
	}
	if compilerTypes.IsRune(target) {
		// Integer-to-Rune checks Unicode scalar validity: the value must be
		// in U+0000..U+10FFFF and outside the surrogate range.
		if constant.Compare(integer, gotoken.LSS, constant.MakeInt64(0)) ||
			constant.Compare(integer, gotoken.GTR, constant.MakeInt64(0x10FFFF)) ||
			constant.Compare(integer, gotoken.GEQ, constant.MakeInt64(0xD800)) &&
				constant.Compare(integer, gotoken.LEQ, constant.MakeInt64(0xDFFF)) {
			diagnostic := typeErrorAt(token, fmt.Sprintf("value %s is not a valid Unicode scalar value for Rune", integer.String()))
			return nil, &diagnostic
		}
		return integer, nil
	}
	minimum, maximum := constantIntegerRange(target)
	if constant.Compare(integer, gotoken.LSS, minimum) || constant.Compare(integer, gotoken.GTR, maximum) {
		diagnostic := typeErrorAt(token, fmt.Sprintf("value %s is outside the range of %s", integer.String(), target.Name))
		return nil, &diagnostic
	}
	return reduceSigned(integer, target), nil
}

// reduceSigned reinterprets a non-negative residue as the destination
// integer using the defined two's-complement representation.
func reduceSigned(value constant.Value, target compilerTypes.Type) constant.Value {
	if !compilerTypes.IsSignedInteger(target) {
		return value
	}
	width := target.Bits
	half := constant.MakeUint64(uint64(1) << uint(width-1))
	if constant.Compare(value, gotoken.LSS, half) {
		return value
	}
	subtract := constant.MakeUint64(uint64(1) << uint(width))
	return constant.BinaryOp(value, gotoken.SUB, subtract)
}

func constantIntegerRange(typ compilerTypes.Type) (constant.Value, constant.Value) {
	if compilerTypes.IsUnsignedInteger(typ) {
		if typ.Bits == 64 {
			return constant.MakeUint64(0), constant.MakeUint64(^uint64(0))
		}
		return constant.MakeUint64(0), constant.MakeUint64(uint64(1)<<uint(typ.Bits) - 1)
	}
	if typ.Bits == 64 {
		return constant.MakeInt64(-1 << 63), constant.MakeInt64(1<<63 - 1)
	}
	maximum := int64(1)<<uint(typ.Bits-1) - 1
	return constant.MakeInt64(-maximum - 1), constant.MakeInt64(maximum)
}
