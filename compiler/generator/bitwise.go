package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// Low-level integer and bit operations: bitwise &, ^, |, unary ~, defined
// << and >>, bit_cast<T>(), and endian byte conversion.

type shiftSpec struct {
	operator checker.Operator
	typ      compilerTypes.Type
}

// discoverGeneratedShifts collects the guarded shift helpers the program
// needs. Shifts go through helpers so the count and left operand each
// evaluate exactly once while the runtime trap runs before any C shift.
func discoverGeneratedShifts(program checker.Program) []shiftSpec {
	seen := make(map[string]bool)
	var specs []shiftSpec
	visitor := &programVisitor{
		Expression: func(node checker.Expression) {
			if node.Kind == checker.BinaryOperationExpression &&
				(node.Operator == checker.ShiftLeftOperator || node.Operator == checker.ShiftRightOperator) {
				key := node.Operator.String() + node.OperandType.Name
				if !seen[key] {
					seen[key] = true
					specs = append(specs, shiftSpec{operator: node.Operator, typ: node.OperandType})
				}
			}
		},
	}
	walkProgram(program, visitor)
	return specs
}

// writeShiftDefinitions emits one guarded helper per (operator, type) pair.
// The count is validated against the left operand's width before any C
// shift executes.
func writeShiftDefinitions(result *strings.Builder, specs []shiftSpec) {
	if len(specs) == 0 {
		return
	}
	for _, spec := range specs {
		writeShiftHelper(result, spec)
	}
}

func shiftHelperName(spec shiftSpec) string {
	prefix := "hex_shl_"
	if spec.operator == checker.ShiftRightOperator {
		prefix = "hex_shr_"
	}
	return prefix + spec.typ.CName
}

func writeShiftHelper(result *strings.Builder, spec shiftSpec) {
	typ := spec.typ
	unsigned, ok := unsignedCName(typ)
	if !ok {
		return
	}
	width := uint(typ.Bits)
	var shifted string
	switch {
	case spec.operator == checker.ShiftLeftOperator:
		// Wrapping bit-pattern behavior for both signed and unsigned types.
		shifted = fmt.Sprintf("(%s)((%s)left << (uint64_t)count)", unsigned, unsigned)
	case compilerTypes.IsUnsignedInteger(typ):
		shifted = fmt.Sprintf("(%s)((%s)left >> (uint64_t)count)", unsigned, unsigned)
	default:
		// Signed right shift is arithmetic: zero-fill the magnitude, then
		// explicitly fill the high bits with the sign bit. The count == 0
		// case is separate so the sign-fill shift never uses the full width.
		// The mask uses the exact-width unsigned type so an Int64 operand
		// never shifts a 32-bit 1u by 32 or more; the inner parens keep the
		// shift inside the subtraction.
		mask := fmt.Sprintf("(left < 0 ? (%s)(0 - ((%s)1 << (%s)(%d - (uint64_t)count))) : 0)", unsigned, unsigned, unsigned, width)
		shifted = fmt.Sprintf("((uint64_t)count == 0 ? (%s)left : (%s)(((%s)left >> (uint64_t)count) | %s))", unsigned, unsigned, unsigned, mask)
	}
	if compilerTypes.IsSignedInteger(typ) {
		// The qualified GCC/Clang targets convert an out-of-range
		// same-width unsigned value modulo the destination width, so the
		// signed result is a plain cast.
		shifted = fmt.Sprintf("(%s)(%s)", typ.CName, shifted)
	}
	fmt.Fprintf(result, "\nstatic inline %s %s(%s left, uint64_t count) {\n", typ.CName, shiftHelperName(spec), typ.CName)
	fmt.Fprintf(result, "    if (!(count < %dULL)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n", width)
	fmt.Fprintf(result, "    return %s;\n}\n", shifted)
}

// renderBitwiseOperation lowers &, ^, and | at the selected exact width:
// operands convert to the unsigned representation, the operation runs in a
// promotion-safe unsigned type, and a signed result is a direct cast under
// the qualified modular-conversion contract.
func renderBitwiseOperation(operator checker.Operator, typ compilerTypes.Type, left, right string) (string, error) {
	unsigned, ok := unsignedCName(typ)
	if !ok {
		return "", unknownExpressionDiagnostic("bitwise operation with an unsupported width")
	}
	operatorText := ""
	switch operator {
	case checker.BitwiseAndOperator:
		operatorText = "&"
	case checker.BitwiseXorOperator:
		operatorText = "^"
	case checker.BitwiseOrOperator:
		operatorText = "|"
	default:
		return "", unknownExpressionDiagnostic("unknown bitwise operator")
	}
	unsignedExpr := fmt.Sprintf("(%s)((%s)%s %s (%s)%s)", unsigned, unsigned, left, operatorText, unsigned, right)
	if compilerTypes.IsSignedInteger(typ) {
		return fmt.Sprintf("(%s)(%s)", typ.CName, unsignedExpr), nil
	}
	return unsignedExpr, nil
}

// renderBitwiseComplement lowers unary ~ at the exact width.
func renderBitwiseComplement(typ compilerTypes.Type, operand string) (string, error) {
	unsigned, ok := unsignedCName(typ)
	if !ok {
		return "", unknownExpressionDiagnostic("bitwise complement with an unsupported width")
	}
	unsignedExpr := fmt.Sprintf("(%s)~((uint64_t)%s)", unsigned, operand)
	if compilerTypes.IsSignedInteger(typ) {
		return fmt.Sprintf("(%s)(%s)", typ.CName, unsignedExpr), nil
	}
	return unsignedExpr, nil
}

// bitCastSpec is one concrete same-width scalar bit cast pair.
type bitCastSpec struct {
	source compilerTypes.Type
	target compilerTypes.Type
}

func bitCastHelperName(spec bitCastSpec) string {
	return "hex_bitcast_" + spec.source.CName + "_" + spec.target.CName
}

// discoverGeneratedBitCasts collects the bit_cast<T>() pairs the program
// needs.
func discoverGeneratedBitCasts(program checker.Program) []bitCastSpec {
	seen := make(map[string]bool)
	var specs []bitCastSpec
	visitor := &programVisitor{
		Expression: func(node checker.Expression) {
			if node.Kind == checker.BitCastExpression && node.Operand != nil {
				key := node.OperandType.Name + ">" + node.ResultType.Name
				if !seen[key] {
					seen[key] = true
					specs = append(specs, bitCastSpec{source: node.OperandType, target: node.ResultType})
				}
			}
		},
	}
	walkProgram(program, visitor)
	return specs
}

// writeBitCastDefinitions emits one memcpy-based helper per pair. The bits
// copy directly from the checked source object into the exact destination
// object with no signed-source cast, unsigned intermediate, or post-copy
// conversion. memcpy is available because hexal.h includes <string.h>.
func writeBitCastDefinitions(result *strings.Builder, specs []bitCastSpec) {
	if len(specs) == 0 {
		return
	}
	for _, spec := range specs {
		targetC := spec.target.CName
		fmt.Fprintf(result, "\nstatic inline %s %s(%s value) {\n    %s result;\n    memcpy(&result, &value, sizeof(result));\n    return result;\n}\n", targetC, bitCastHelperName(spec), spec.source.CName, targetC)
	}
}

// endianSpec is one to/from byte conversion for a fixed-width integer type.
type endianSpec struct {
	typ    compilerTypes.Type
	bigEnd bool
	from   bool
}

func endianHelperName(spec endianSpec) string {
	order := "le"
	if spec.bigEnd {
		order = "be"
	}
	prefix := "hex_to_" + order + "_bytes_"
	if spec.from {
		prefix = "hex_from_" + order + "_bytes_"
	}
	return prefix + spec.typ.CName
}

// discoverGeneratedEndian collects the endian conversions the program needs.
func discoverGeneratedEndian(program checker.Program) []endianSpec {
	seen := make(map[string]bool)
	var specs []endianSpec
	visitor := &programVisitor{
		Expression: func(node checker.Expression) {
			if node.Kind == checker.EndianConversionExpression && node.Element != (compilerTypes.Type{}) {
				key := node.Name + fmt.Sprint(node.MemberIndex) + node.Element.Name
				if !seen[key] {
					seen[key] = true
					specs = append(specs, endianSpec{typ: node.Element, bigEnd: node.MemberIndex == 1, from: node.Name == "from"})
				}
			}
		},
	}
	walkProgram(program, visitor)
	return specs
}

// writeEndianDefinitions emits the byte-order conversion helpers. Byte order
// is defined by significance, independent of host endianness.
func writeEndianDefinitions(result *strings.Builder, specs []endianSpec) {
	if len(specs) == 0 {
		return
	}
	for _, spec := range specs {
		writeEndianHelper(result, spec)
	}
}

func writeEndianHelper(result *strings.Builder, spec endianSpec) {
	typ := spec.typ
	width := uint(typ.Bits)
	bytes := width / 8
	arrayType := compilerTypes.NewEnvironment().ArrayType(compilerTypes.UInt8, uint64(bytes))
	if arrayType == (compilerTypes.Type{}) {
		return
	}
	unsigned, ok := unsignedCName(typ)
	if !ok {
		return
	}
	if !spec.from {
		// to_le_bytes / to_be_bytes: value is the unsigned bit pattern.
		fmt.Fprintf(result, "\nstatic inline %s %s(%s value) {\n    %s result = ( %s ){{0}};\n", arrayType.CName, endianHelperName(spec), typ.CName, arrayType.CName, arrayType.CName)
		for index := uint(0); index < bytes; index++ {
			shift := uint(0)
			if spec.bigEnd {
				shift = (bytes - 1 - index) * 8
			} else {
				shift = index * 8
			}
			fmt.Fprintf(result, "    result.data[%d] = (uint8_t)((%s)value >> %d);\n", index, unsigned, shift)
		}
		fmt.Fprintf(result, "    return result;\n}\n")
		return
	}
	// from_le_bytes / from_be_bytes: assemble the unsigned pattern.
	fmt.Fprintf(result, "\nstatic inline %s %s(const %s *bytes) {\n    %s value = 0;\n", typ.CName, endianHelperName(spec), arrayType.CName, unsigned)
	for index := uint(0); index < bytes; index++ {
		shift := uint(0)
		if spec.bigEnd {
			shift = (bytes - 1 - index) * 8
		} else {
			shift = index * 8
		}
		fmt.Fprintf(result, "    value |= (%s)(bytes->data[%d]) << %d;\n", unsigned, index, shift)
	}
	if compilerTypes.IsSignedInteger(typ) {
		// Direct modular cast: same-width unsigned-to-signed conversion is
		// modular on the pinned GCC/Clang targets.
		fmt.Fprintf(result, "    return (%s)value;\n}\n", typ.CName)
	} else {
		fmt.Fprintf(result, "    return value;\n}\n")
	}
}

// renderBitCast renders a bit_cast<T>() call through its helper.
func renderBitCast(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("bit cast without a receiver")
	}
	operand, atomic, operandErr := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
	if operandErr != nil {
		return "", operandErr
	}
	if !atomic {
		operand = "(" + operand + ")"
	}
	spec := bitCastSpec{source: node.OperandType, target: node.ResultType}
	return bitCastHelperName(spec) + "(" + operand + ")", nil
}

// renderEndianConversion renders a to/from byte conversion through its
// helper.
func renderEndianConversion(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.Element == (compilerTypes.Type{}) {
		return "", unknownExpressionDiagnostic("endian conversion without a receiver")
	}
	spec := endianSpec{typ: node.Element, bigEnd: node.MemberIndex == 1, from: node.Name == "from"}
	if spec.from {
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("endian from conversion without bytes")
		}
		bytes, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return endianHelperName(spec) + "(&(" + bytes + "))", nil
	}
	operand, atomic, operandErr := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
	if operandErr != nil {
		return "", operandErr
	}
	if !atomic {
		operand = "(" + operand + ")"
	}
	return endianHelperName(spec) + "(" + operand + ")", nil
}

// bitCastEligible reports whether typ may be a bit_cast source or
// destination: a fixed-representation scalar at 8, 16, 32, or 64 bits.
func bitCastEligible(typ compilerTypes.Type) bool {
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

// endianEligible reports whether typ provides endian byte conversion: every
// fixed-width integer, excluding Size and Rune.

var _ = strings.TrimPrefix
