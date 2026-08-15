package generator

import (
	"fmt"
	"go/constant"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0038 conversion lowering, refined by RFC 0068: every explicit
// conversion is classified as identity (the operand itself), direct (one C
// cast that cannot trap), or checked (one deduplicated guard-and-cast
// helper). Identity and direct conversions render inline; only checked pairs
// enter the helper set, so a safe-only program selects no conversion helper
// and no runtime trap.

type conversionSpec struct {
	source compilerTypes.Type
	target compilerTypes.Type
}

// conversionKind classifies one explicit conversion for lowering.
type conversionKind int

const (
	// conversionIdentity: source and destination are one canonical type;
	// the operand renders as itself.
	conversionIdentity conversionKind = iota
	// conversionDirect: one parenthesized C cast; the conversion may round
	// but cannot produce an invalid value or trap.
	conversionDirect
	// conversionChecked: one deduplicated guard-and-cast helper.
	conversionChecked
)

// classifyConversion decides how one explicit conversion lowers. Direct
// requires proof over the complete source domain; Size is target-sized, so
// its synthetic Bits field is a placeholder, never evidence, and every
// non-identity pair involving Size stays checked (RFC 0068).
func classifyConversion(source, target compilerTypes.Type) conversionKind {
	if compilerTypes.Equal(source, target) {
		// Identity; Byte/UInt8 identity follows canonical aliasing.
		return conversionIdentity
	}
	// Fixed-width integer or Rune to Float32/Float64: every core integer
	// value fits the float exponent range, so the cast is direct. This
	// includes Size to float, which is direct today and needs no width
	// evidence.
	if compilerTypes.IsFloat(target) && (compilerTypes.IsInteger(source) || compilerTypes.IsRune(source)) {
		return conversionDirect
	}
	// Float32 to Float64 widens; a widening cannot overflow.
	if compilerTypes.Equal(source, compilerTypes.Float32) && compilerTypes.Equal(target, compilerTypes.Float64) {
		return conversionDirect
	}
	if compilerTypes.IsInteger(source) && compilerTypes.IsInteger(target) {
		// Integer to Rune validates Unicode scalar range, so it is always
		// checked. Rune to UInt32 is same width, so its old vacuous
		// <= UINT32_MAX guard was never a check; the cast is direct.
		if !compilerTypes.IsRune(target) &&
			(integerRangeFits(source, target) || compilerTypes.IsRune(source) && compilerTypes.Equal(target, compilerTypes.UInt32)) {
			return conversionDirect
		}
		return conversionChecked
	}
	// Everything else is checked: Float64-to-Float32 finite-overflow
	// narrowing and every Float-to-integer conversion.
	return conversionChecked
}

// discoverGeneratedConversions collects the checked conversion helpers the
// program needs and the Size-typed integer literal values whose fit depends
// on the C target (RFC 0049 item 6). SIZE_MAX is at least 65535 on every
// conforming target, so only literals above that are target-dependent.
// Identity and direct conversions remain in the checked program but never
// enter the helper set (RFC 0068).
func discoverGeneratedConversions(program checker.Program) ([]conversionSpec, []string, error) {
	var specs []conversionSpec
	seen := make(map[string]bool)
	var sizeLiterals []string
	seenSize := make(map[string]bool)
	visitor := &programVisitor{
		// Literal constants carry no checked node (the value lives in the
		// operand), so the Size target guard runs on every operand.
		Operand: func(source checker.Operand) error {
			if compilerTypes.Equal(source.Type, compilerTypes.SizeType) && source.Constant != nil {
				if unsigned, ok := constant.Uint64Val(source.Constant); ok && unsigned > 65535 {
					digits := formatInteger(unsigned, checker.DecimalRadix)
					if !seenSize[digits] {
						seenSize[digits] = true
						sizeLiterals = append(sizeLiterals, digits)
					}
				}
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.ConversionExpression && node.Operand != nil && classifyConversion(node.OperandType, node.ResultType) == conversionChecked {
				key := node.OperandType.Name + ">" + node.ResultType.Name
				if !seen[key] {
					seen[key] = true
					specs = append(specs, conversionSpec{source: node.OperandType, target: node.ResultType})
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, nil, err
	}
	return specs, sizeLiterals, nil
}

// writeConversionDefinitions emits one helper per checked conversion spec.
// Guards run before any C conversion that could be invalid; the shared
// runtime trap never executes the invalid operation.
func writeConversionDefinitions(result *strings.Builder, specs []conversionSpec) {
	if len(specs) == 0 {
		return
	}
	for _, spec := range specs {
		writeConversionHelper(result, spec)
	}
}

// containsSizeConversion reports whether any generated conversion targets
// Size, which requires the 64-bit size_t target profile assertion.
func containsSizeConversion(specs []conversionSpec) bool {
	for _, spec := range specs {
		if compilerTypes.Equal(spec.target, compilerTypes.SizeType) {
			return true
		}
	}
	return false
}

func conversionHelperName(spec conversionSpec) string {
	sourceSuffix := spec.source.CName
	if compilerTypes.IsRune(spec.source) {
		sourceSuffix = "rune"
	}
	targetSuffix := spec.target.CName
	if compilerTypes.IsRune(spec.target) {
		targetSuffix = "rune"
	}
	return "hex_convert_" + sourceSuffix + "_" + targetSuffix
}

func writeConversionHelper(result *strings.Builder, spec conversionSpec) {
	source := spec.source
	target := spec.target
	sourceC := source.CName
	targetC := target.CName
	body := ""
	switch {
	case compilerTypes.IsRune(target):
		// RFC 0038: Integer-to-Rune checks Unicode scalar validity, not just
		// the 32-bit range: the value must be in U+0000..U+10FFFF and
		// outside the surrogate range. The negative check applies only to
		// signed sources; an unsigned C type makes `value < 0` a
		// always-false comparison.
		negative := ""
		if compilerTypes.IsSignedInteger(source) {
			negative = "value < 0 || "
		}
		body = "    if (" + negative + "value > 0x10FFFF || (value >= 0xD800 && value <= 0xDFFF)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n"
		body += "    return (" + targetC + ")value;\n"
	case compilerTypes.IsInteger(source) && compilerTypes.IsInteger(target):
		if integerRangeFits(source, target) {
			body = "    return (" + targetC + ")value;\n"
		} else {
			body = writeCheckedIntegerConversion(source, target)
		}
	case compilerTypes.IsInteger(source):
		// Integer to float: nearest representable value; all core integer
		// types fit the float exponent range.
		body = "    return (" + targetC + ")value;\n"
	case compilerTypes.IsFloat(source) && compilerTypes.IsInteger(target):
		body = writeFloatToIntegerConversion(source, target)
	default:
		// Float to float: rounding may overflow to infinity for finite
		// sources.
		if compilerTypes.Equal(source, target) {
			body = "    return value;\n"
		} else {
			body = "    " + targetC + " result = (" + targetC + ")value;\n"
			body += "    if (isfinite(value) && isinf(result)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n"
			body += "    return result;\n"
		}
	}
	fmt.Fprintf(result, "\nstatic inline %s %s(%s value) {\n", targetC, conversionHelperName(spec), sourceC)
	result.WriteString(body)
	result.WriteString("}\n")
}

// integerRangeFits reports whether every source value fits the destination
// integer type. Size is target-sized, so its synthetic Bits field is a
// placeholder, not representation evidence: no non-identity pair involving
// Size is proven to fit here (RFC 0068), and Size conversions stay on the
// checked path.
func integerRangeFits(source, target compilerTypes.Type) bool {
	if compilerTypes.Equal(source, target) {
		return true
	}
	if compilerTypes.IsSize(source) || compilerTypes.IsSize(target) {
		return false
	}
	if compilerTypes.IsSignedInteger(source) && compilerTypes.IsSignedInteger(target) ||
		compilerTypes.IsUnsignedInteger(source) && compilerTypes.IsUnsignedInteger(target) {
		return source.Bits < target.Bits
	}
	if compilerTypes.IsSignedInteger(source) {
		return false
	}
	return source.Bits < target.Bits
}

func writeCheckedIntegerConversion(source, target compilerTypes.Type) string {
	low := ""
	high := ""
	minimum := integerMinimumMacro(target)
	maximum := integerMaximumMacro(target)
	switch {
	case compilerTypes.IsSignedInteger(source) && compilerTypes.IsSignedInteger(target):
		low = "value >= " + minimum
		high = "value <= " + maximum
	case compilerTypes.IsUnsignedInteger(source) && compilerTypes.IsUnsignedInteger(target):
		low = ""
		high = "value <= " + maximum
	case compilerTypes.IsSignedInteger(source):
		low = "value >= 0"
		high = "value <= " + maximum
	default:
		low = ""
		high = "value <= " + maximum
	}
	condition := ""
	if low != "" && high != "" {
		condition = low + " && " + high
	} else {
		condition = high
	}
	return "    if (!(" + condition + ")) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n    return (" + target.CName + ")value;\n"
}

// writeFloatToIntegerConversion emits a checked Float-to-integer helper: the
// value is truncated exactly once into a temporary of the source floating
// type, that temporary is checked against exact power-of-two destination
// bounds, and only the temporary is cast (RFC 0068). Integer maximum macros
// are never converted to Float and compared, because the converted upper
// bound rounds and can admit the first unrepresentable value; fromfp and
// ufromfp are not used because their domain-error result is not a direct
// success test.
func writeFloatToIntegerConversion(source, target compilerTypes.Type) string {
	trunc := "truncf"
	if compilerTypes.Equal(source, compilerTypes.Float64) {
		trunc = "trunc"
	}
	body := "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n"
	body += "    " + source.CName + " truncated = " + trunc + "(value);\n"
	bound := ""
	switch {
	case compilerTypes.IsSignedInteger(target):
		// -2^(N-1) <= truncated < 2^(N-1), both exactly representable
		// powers of two.
		bound = fmt.Sprintf("!(truncated >= -0x1p%d && truncated < 0x1p%d)", target.Bits-1, target.Bits-1)
	case compilerTypes.IsSize(target):
		// Size: the target's size_t range limit 2^N is derived from
		// SIZE_MAX converted to the source floating type before adding
		// floating one; the addition never happens in size_t, and no
		// integer maximum macro is compared.
		suffix := ""
		if compilerTypes.Equal(source, compilerTypes.Float32) {
			suffix = "f"
		}
		bound = "!(truncated >= 0.0 && truncated < (" + source.CName + ")SIZE_MAX + 1.0" + suffix + ")"
	default:
		// 0 <= truncated < 2^N, exactly representable.
		bound = fmt.Sprintf("!(truncated >= 0.0 && truncated < 0x1p%d)", target.Bits)
	}
	body += "    if (" + bound + ") {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n"
	return body + "    return (" + target.CName + ")truncated;\n"
}

// integerMinimumMacro returns the C limit macro for the minimum of an
// integer type, or a literal for types without a macro.
func integerMinimumMacro(typ compilerTypes.Type) string {
	if !compilerTypes.IsSignedInteger(typ) {
		return "0"
	}
	return signedMinimumMacro(typ)
}

// integerMaximumMacro returns the C limit macro for the maximum of an
// integer type.
func integerMaximumMacro(typ compilerTypes.Type) string {
	if compilerTypes.IsSignedInteger(typ) {
		if typ.Bits == 64 {
			return "INT64_MAX"
		}
		return fmt.Sprintf("INT%d_MAX", typ.Bits)
	}
	if compilerTypes.Equal(typ, compilerTypes.SizeType) {
		return "SIZE_MAX"
	}
	if typ.Bits == 64 {
		return "UINT64_MAX"
	}
	return fmt.Sprintf("UINT%d_MAX", typ.Bits)
}

// renderConversion renders an explicit conversion by classification: the
// operand itself for identity, one parenthesized cast for direct, and the
// deduplicated guard-and-cast helper for checked. The operand is rendered
// exactly once.
func renderConversion(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("numeric conversion without an operand")
	}
	operand, atomic, operandErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if operandErr != nil {
		return "", operandErr
	}
	if !atomic {
		operand = "(" + operand + ")"
	}
	spec := conversionSpec{source: node.OperandType, target: node.ResultType}
	switch classifyConversion(spec.source, spec.target) {
	case conversionIdentity:
		// Identity: the operand already has the destination type; no cast,
		// no helper. Parenthesizing a non-atomic operand keeps the result
		// one C expression.
		return operand, nil
	case conversionDirect:
		// Direct: one parenthesized cast over the single rendered operand.
		return "(" + spec.target.CName + ")" + operand, nil
	}
	return conversionHelperName(spec) + "(" + operand + ")", nil
}
