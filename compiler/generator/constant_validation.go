// constant_validation.go owns operand constant validation: float,
// integer, and object constant checks and the literal parsers behind them.
package generator

import (
	"go/constant"
	gotoken "go/token"
	"math"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func validateConstantOperand(source checker.Operand) error {
	// Object constants (Error.new results wrapped by union injection)
	// validate their object value.
	if source.Object != nil {
		return validateObjectValue(source.Object, newExpressionValidation())
	}
	// Nil is the singleton type: its one value is nullptr and it carries no
	// go/constant, so it is validated before the constant value is required.
	if compilerTypes.IsNil(source.Type) {
		return nil
	}
	// EoS is a singleton: its one value is a tag-only marker and
	// carries no go/constant, like Nil.
	if compilerTypes.IsEoS(source.Type) {
		return nil
	}
	// Heap is a singleton handle: Heap.new() carries no go/constant.
	if compilerTypes.IsHeap(source.Type) {
		return nil
	}
	if source.Constant == nil {
		return unknownExpressionDiagnostic("constant operand without a checked value")
	}
	switch source.Type.ScalarKind {
	case compilerTypes.ScalarBool:
		if !compilerTypes.Equal(source.Type, compilerTypes.Bool) || source.Constant.Kind() != constant.Bool {
			return unknownExpressionDiagnostic("invalid checked Bool constant")
		}
	case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
		if !supportedGeneratedScalarType(source.Type) || source.Constant.Kind() != constant.Int {
			return unknownExpressionDiagnostic("invalid checked integer constant")
		}
		if _, err := integerLiteral(source); err != nil {
			return err
		}
	case compilerTypes.ScalarFloat:
		return validateFloatConstant(source)
	default:
		return unknownExpressionDiagnostic("unsupported checked constant type")
	}
	return nil
}

func validateFloatConstant(source checker.Operand) error {
	bitSize := 64
	if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
		bitSize = 32
	} else if !compilerTypes.Equal(source.Type, compilerTypes.Float64) {
		return unknownExpressionDiagnostic("invalid checked float constant")
	}
	if bitSize == 32 && source.FloatBits > math.MaxUint32 {
		return unknownExpressionDiagnostic("Float32 constant has bits outside its declared width")
	}

	bits := source.FloatBits
	if bitSize == 32 {
		bits = uint64(uint32(bits))
	}
	signBit, special := floatSignAndSpecial(bits, bitSize)
	if source.Negative != signBit {
		return unknownExpressionDiagnostic("float sign metadata does not match its checked value")
	}
	if source.Constant == nil {
		return unknownExpressionDiagnostic("float constant without a checked value")
	}

	if special {
		if source.Constant.Kind() != constant.Unknown || source.Literal != "" {
			return unknownExpressionDiagnostic("special float constant has malformed metadata")
		}
		return nil
	}
	if source.Constant.Kind() != constant.Int && source.Constant.Kind() != constant.Float {
		return unknownExpressionDiagnostic("float constant is not numeric")
	}

	if source.Literal != "" {
		literal := strings.ReplaceAll(source.Literal, "_", "")
		if strings.HasPrefix(literal, "+") || strings.HasPrefix(literal, "-") {
			return unknownExpressionDiagnostic("float literal sign is stored in malformed metadata")
		}
		literalValue := constant.MakeFromLiteral(literal, gotoken.FLOAT, 0)
		if literalValue == nil || literalValue.Kind() == constant.Unknown || (literalValue.Kind() != constant.Int && literalValue.Kind() != constant.Float) || constant.Sign(source.Constant) < 0 || !constant.Compare(source.Constant, gotoken.EQL, literalValue) {
			return unknownExpressionDiagnostic("checked float literal does not match its value")
		}
		if floatBitsForConstant(literalValue, bitSize, source.Negative) != bits {
			return unknownExpressionDiagnostic("checked float literal does not match its rounded bits")
		}
		return nil
	}
	if floatBitsForConstant(source.Constant, bitSize, source.Negative) != bits {
		return unknownExpressionDiagnostic("checked float does not match its rounded bits")
	}
	valueSign := constant.Sign(source.Constant)
	if valueSign < 0 && !signBit || valueSign > 0 && signBit {
		return unknownExpressionDiagnostic("float sign metadata does not match its checked value")
	}
	return nil
}

func floatSignAndSpecial(bits uint64, bitSize int) (bool, bool) {
	if bitSize == 32 {
		value := uint32(bits)
		return value>>31 != 0, value&0x7f800000 == 0x7f800000
	}
	return bits>>63 != 0, bits&0x7ff0000000000000 == 0x7ff0000000000000
}

func floatBitsForConstant(value constant.Value, bitSize int, negative bool) uint64 {
	if bitSize == 32 {
		converted, _ := constant.Float32Val(value)
		bits := uint64(math.Float32bits(converted))
		if negative {
			bits |= uint64(1) << 31
		}
		return bits
	}
	converted, _ := constant.Float64Val(value)
	bits := math.Float64bits(converted)
	if negative {
		bits |= uint64(1) << 63
	}
	return bits
}

func validateObjectValue(value *checker.ObjectValue, state *expressionValidation) error {
	if value == nil || value.Type.Object == nil || !supportedGeneratedTypeWithState(value.Type, state) {
		return unknownExpressionDiagnostic("object operand without a checked object value")
	}
	if state.objects[value] {
		return unknownExpressionDiagnostic("cyclic checked object value")
	}
	state.objects[value] = true
	defer delete(state.objects, value)

	seen := make(map[*compilerTypes.ObjectMember]bool, len(value.Initializers))
	for _, initializer := range value.Initializers {
		if initializer.Member == nil {
			return unknownExpressionDiagnostic("object initializer without a checked member")
		}
		canonical, ok := objectMember(value.Type.Object, initializer.Member)
		if !ok || seen[initializer.Member] || !compilerTypes.Equal(canonical.Type, initializer.Member.Type) {
			return unknownExpressionDiagnostic("object initializer has a forged checked member")
		}
		seen[initializer.Member] = true
		if !generatedAssignable(canonical.Type, initializer.Source.Type) {
			return unknownExpressionDiagnostic("object initializer type does not match its checked member")
		}
		if err := validateCheckedOperandWithState(initializer.Source, state); err != nil {
			return err
		}
	}
	if len(seen) != len(value.Type.Object.Members) {
		return unknownExpressionDiagnostic("incomplete checked object value")
	}
	return nil
}

func objectMember(object *compilerTypes.ObjectType, member *compilerTypes.ObjectMember) (*compilerTypes.ObjectMember, bool) {
	if object == nil || member == nil {
		return nil, false
	}
	for index := range object.Members {
		if &object.Members[index] == member {
			return &object.Members[index], true
		}
	}
	return nil, false
}

// generatedAssignable re-validates the checker's assignment relation so the
// generator never accepts a program the checker rejected. It is the complete
// type-level relation: weakening, nullable injection (P or Nil into P | Nil),
// and the one-row Unknown erasure/recovery table.
func generatedAssignable(target, source compilerTypes.Type) bool {
	return compilerTypes.Assignable(target, source)
}

func validateCheckedOperandWithState(source checker.Operand, state *expressionValidation) error {
	if !supportedGeneratedTypeWithState(source.Type, state) {
		return unknownExpressionDiagnostic("operand has an unsupported checked type")
	}
	switch source.Kind {
	case checker.ObjectOperand:
		if source.Object == nil || !compilerTypes.Equal(source.Type, source.Object.Type) {
			return unknownExpressionDiagnostic("object operand has mismatched checked types")
		}
		return validateObjectValue(source.Object, state)
	case checker.VariableOperand, checker.ExpressionOperand:
		if err := validateExpressionNode(source.Node, &source.Type, state); err != nil {
			return err
		}
		if expressionType, ok := expressionResultType(source.Node); ok && !compilerTypes.Equal(source.Type, expressionType) && !compilerTypes.WidensTo(expressionType, source.Type) {
			return unknownExpressionDiagnostic("operand expression type does not match its checked type")
		}
	case checker.ConstantOperand:
		return validateConstantOperand(source)
	default:
		return unknownExpressionDiagnostic("unsupported checked operand")
	}
	return nil
}

func validateIntegerConstant(source checker.Operand) error {
	if source.Kind != checker.ConstantOperand || source.Constant == nil || source.Constant.Kind() != constant.Int || !supportedGeneratedScalarType(source.Type) || !compilerTypes.IsInteger(source.Type) {
		return unknownExpressionDiagnostic("invalid checked integer constant")
	}
	if source.Radix < checker.DecimalRadix || source.Radix > checker.OctalRadix {
		return unknownExpressionDiagnostic("checked integer has an invalid radix")
	}
	value := source.Constant
	sign := constant.Sign(value)
	if compilerTypes.IsUnsignedInteger(source.Type) && (source.Negative || sign < 0) {
		return unknownExpressionDiagnostic("negative value for an unsigned integer constant")
	}
	if source.Negative && sign > 0 || !source.Negative && sign < 0 {
		return unknownExpressionDiagnostic("integer sign metadata does not match its checked value")
	}
	// Folded constants carry no literal text; only original literals are
	// re-validated against their value.
	if source.Literal == "" {
		return nil
	}
	magnitude, literalNegative, ok := parseIntegerLiteral(source.Literal, source.Radix)
	if !ok {
		return unknownExpressionDiagnostic("checked integer has an invalid literal value")
	}
	if literalNegative && !source.Negative {
		return unknownExpressionDiagnostic("integer literal sign does not match its checked metadata")
	}
	if source.Negative {
		literalValue := constant.UnaryOp(gotoken.SUB, magnitude, 0)
		if !constant.Compare(value, gotoken.EQL, literalValue) {
			return unknownExpressionDiagnostic("checked integer literal does not match its value")
		}
	} else if !constant.Compare(value, gotoken.EQL, magnitude) {
		return unknownExpressionDiagnostic("checked integer literal does not match its value")
	}
	return nil
}

func parseIntegerLiteral(literal string, radix checker.LiteralRadix) (constant.Value, bool, bool) {
	literal = strings.ReplaceAll(literal, "_", "")
	if literal == "" {
		return nil, false, false
	}
	negative := strings.HasPrefix(literal, "-")
	if negative {
		literal = literal[1:]
	}
	if literal == "" || strings.HasPrefix(literal, "+") {
		return nil, false, false
	}
	base := 10
	switch radix {
	case checker.DecimalRadix:
		if strings.HasPrefix(literal, "0x") || strings.HasPrefix(literal, "0b") || strings.HasPrefix(literal, "0o") {
			return nil, false, false
		}
	case checker.HexadecimalRadix:
		if !strings.HasPrefix(literal, "0x") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 16
	case checker.BinaryRadix:
		if !strings.HasPrefix(literal, "0b") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 2
	case checker.OctalRadix:
		if !strings.HasPrefix(literal, "0o") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 8
	default:
		return nil, false, false
	}
	if literal == "" {
		return nil, false, false
	}
	value, err := strconv.ParseUint(literal, base, 64)
	if err != nil {
		return nil, false, false
	}
	return constant.MakeUint64(value), negative, true
}
