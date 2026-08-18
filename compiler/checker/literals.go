package checker

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func contextualIntegerType(expected compilerTypes.Type) compilerTypes.Type {
	if compilerTypes.IsInteger(expected) {
		return expected
	}
	return compilerTypes.Int32
}

func contextualFloatType(expected compilerTypes.Type) compilerTypes.Type {
	if compilerTypes.IsFloat(expected) {
		return expected
	}
	return compilerTypes.Float64
}

func negatedInitializer(expression parser.NegatedNumericLiteral, expected compilerTypes.Type) initializerValue {
	switch literal := expression.Literal.(type) {
	case parser.IntegerLiteral:
		if compilerTypes.IsUnsignedInteger(expected) {
			return initializerValue{typ: expected, token: expression.Minus, diagnostic: diagnosticAt(typeErrorAt(expression.Minus, "negated integer literal requires a signed destination"))}
		}
		return integerInitializer(literal.Token, contextualIntegerType(expected), true)
	case parser.DecimalLiteral:
		return floatInitializer(literal.Token, contextualFloatType(expected), true)
	default:
		return initializerValue{typ: compilerTypes.Int32, token: expression.Minus, diagnostic: diagnosticAt(typeErrorAt(expression.Minus, "unsupported negated literal"))}
	}
}

func integerInitializer(token lexer.Token, typ compilerTypes.Type, negative ...bool) initializerValue {
	isNegative := len(negative) > 0 && negative[0]
	normalized := strings.ReplaceAll(token.Lexeme, "_", "")
	value := constant.MakeFromLiteral(normalized, gotoken.INT, 0)
	if value == nil || value.Kind() == constant.Unknown {
		return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
	}
	if isNegative {
		value = constant.UnaryOp(gotoken.SUB, value, 0)
	}
	minimum, maximum := integerBounds(typ)
	if constant.Compare(value, gotoken.LSS, minimum) || constant.Compare(value, gotoken.GTR, maximum) {
		return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
	}
	source := constantOperand(typ, value, normalized)
	source.Radix = literalRadix(token.Kind)
	source.Negative = isNegative
	return initializerValue{source: source, typ: typ, token: token}
}

func integerBounds(typ compilerTypes.Type) (constant.Value, constant.Value) {
	zero := constant.MakeInt64(0)
	if compilerTypes.IsUnsignedInteger(typ) {
		return zero, constant.MakeUint64(^uint64(0) >> (64 - typ.Bits))
	}
	if typ.Bits == 64 {
		return constant.MakeInt64(math.MinInt64), constant.MakeInt64(math.MaxInt64)
	}
	maximum := int64(1<<(typ.Bits-1) - 1)
	return constant.MakeInt64(-maximum - 1), constant.MakeInt64(maximum)
}

func literalRadix(kind lexer.TokenKind) LiteralRadix {
	switch kind {
	case lexer.HexInteger:
		return HexadecimalRadix
	case lexer.BinaryInteger:
		return BinaryRadix
	case lexer.OctalInteger:
		return OctalRadix
	default:
		return DecimalRadix
	}
}

func floatInitializer(token lexer.Token, typ compilerTypes.Type, negative ...bool) initializerValue {
	isNegative := len(negative) > 0 && negative[0]
	normalized := strings.ReplaceAll(token.Lexeme, "_", "")
	value := constant.MakeFromLiteral(normalized, gotoken.FLOAT, 0)
	if value == nil || value.Kind() == constant.Unknown {
		return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
	}
	source := constantOperand(typ, value, normalized)
	source.Negative = isNegative
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		converted, _ := constant.Float32Val(value)
		if math.IsInf(float64(converted), 0) {
			return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
		}
		if isNegative {
			converted = -converted
		}
		source.FloatBits = uint64(math.Float32bits(converted))
	} else {
		converted, _ := constant.Float64Val(value)
		if math.IsInf(converted, 0) {
			return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
		}
		if isNegative {
			converted = -converted
		}
		source.FloatBits = math.Float64bits(converted)
	}
	return initializerValue{source: source, typ: typ, token: token}
}

func valueOutOfRangeDiagnostic(token lexer.Token, valueType compilerTypes.Type) *compilerTypes.Diagnostic {
	return diagnosticAt(typeErrorAt(token, fmt.Sprintf("given value is outside the %s range", valueType.Name)))
}
