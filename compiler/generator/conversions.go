package generator

import (
	"fmt"
	"go/constant"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0038 conversion lowering: one helper per concrete source and
// destination pair, with guards before any C conversion that could be
// invalid. There are no wrapping or saturating modes.

type conversionSpec struct {
	source compilerTypes.Type
	target compilerTypes.Type
}

// discoverGeneratedConversions collects the conversion helpers the program
// needs and the Size-typed integer literal values whose fit depends on the
// C target (RFC 0049 item 6). SIZE_MAX is at least 65535 on every
// conforming target, so only literals above that are target-dependent.
func discoverGeneratedConversions(program checker.Program) ([]conversionSpec, []string, error) {
	var specs []conversionSpec
	seen := make(map[string]bool)
	var sizeLiterals []string
	seenSize := make(map[string]bool)
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkExpression = func(node checker.Expression) error {
		if node.Kind == checker.ConversionExpression && node.Operand != nil {
			key := node.OperandType.Name + ">" + node.ResultType.Name
			if !seen[key] {
				seen[key] = true
				specs = append(specs, conversionSpec{source: node.OperandType, target: node.ResultType})
			}
		}
		if node.Operand != nil {
			if err := walkExpression(*node.Operand); err != nil {
				return err
			}
		}
		if node.Left != nil {
			if err := walkExpression(*node.Left); err != nil {
				return err
			}
		}
		if node.Right != nil {
			if err := walkExpression(*node.Right); err != nil {
				return err
			}
		}
		for _, argument := range node.Arguments {
			if err := walkOperand(argument); err != nil {
				return err
			}
		}
		return nil
	}
	walkOperand = func(source checker.Operand) error {
		// Literal constants carry no checked node (Kind is InvalidExpression
		// and the value lives in Constant), so the Size target guard is
		// collected before the node-kind dispatch.
		if compilerTypes.Equal(source.Type, compilerTypes.SizeType) && source.Constant != nil {
			if unsigned, ok := constant.Uint64Val(source.Constant); ok && unsigned > 65535 {
				digits := formatInteger(unsigned, checker.DecimalRadix)
				if !seenSize[digits] {
					seenSize[digits] = true
					sizeLiterals = append(sizeLiterals, digits)
				}
			}
		}
		if source.Node.Kind != checker.InvalidExpression {
			return walkExpression(source.Node)
		}
		return nil
	}
	walkStatements = func(statements []checker.Statement) error {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.Assignment:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkOperand(statement.Target); err != nil {
					return err
				}
			case checker.CallStatement:
				if err := walkExpression(statement.Call.Node); err != nil {
					return err
				}
			case checker.ReturnStatement:
				if statement.Value != nil {
					if err := walkOperand(*statement.Value); err != nil {
						return err
					}
				}
			case checker.IfStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Then); err != nil {
					return err
				}
				for _, branch := range statement.ElseIf {
					if err := walkOperand(branch.Condition); err != nil {
						return err
					}
					if err := walkStatements(branch.Body); err != nil {
						return err
					}
				}
				if statement.Else != nil {
					if err := walkStatements(statement.Else); err != nil {
						return err
					}
				}
			case checker.ForStatement:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.WhileStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.FunctionDeclaration:
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.MethodDeclaration:
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walkStatements(program.Statements); err != nil {
		return nil, nil, err
	}
	for _, function := range program.SpecializedFunctions {
		if err := walkStatements(function.Body); err != nil {
			return nil, nil, err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := walkStatements(method.Body); err != nil {
			return nil, nil, err
		}
	}
	return specs, sizeLiterals, nil
}

// writeConversionDefinitions emits the shared numeric trap plus one helper
// per conversion spec. Guards run before any C conversion that could be
// invalid; the trap never executes the invalid operation.
func writeConversionDefinitions(result *strings.Builder, specs []conversionSpec) {
	if len(specs) == 0 {
		return
	}
	// The trap is shared with RFC 0017's guarded division helpers; the
	// include guard keeps the definition single even when both writers run.
	result.WriteString("\n#ifndef HEX_NUMERIC_TRAP_DEFINED\n#define HEX_NUMERIC_TRAP_DEFINED\n")
	result.WriteString("static void hex_numeric_trap(void) {\n")
	result.WriteString("    fputs(\"[Runtime Error] numeric operation failed\\n\", stderr);\n    abort();\n}\n")
	result.WriteString("#endif\n")
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
		// outside the surrogate range.
		body = "    if (value < 0 || value > 0x10FFFF || (value >= 0xD800 && value <= 0xDFFF)) {\n        hex_numeric_trap();\n    }\n"
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
			body += "    if (isfinite(value) && isinf(result)) {\n        hex_numeric_trap();\n    }\n"
			body += "    return result;\n"
		}
	}
	fmt.Fprintf(result, "\nstatic inline %s %s(%s value) {\n", targetC, conversionHelperName(spec), sourceC)
	result.WriteString(body)
	result.WriteString("}\n")
}

// integerRangeFits reports whether every source value fits the destination
// integer type.
func integerRangeFits(source, target compilerTypes.Type) bool {
	if compilerTypes.Equal(source, target) {
		return true
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
	return "    if (!(" + condition + ")) {\n        hex_numeric_trap();\n    }\n    return (" + target.CName + ")value;\n"
}

func writeFloatToIntegerConversion(source, target compilerTypes.Type) string {
	minimum := integerMinimumMacro(target)
	maximum := integerMaximumMacro(target)
	body := "    if (isnan(value) || isinf(value)) {\n        hex_numeric_trap();\n    }\n"
	if compilerTypes.IsSignedInteger(target) {
		body += "    if (!(value >= " + minimum + " && value <= " + maximum + ")) {\n        hex_numeric_trap();\n    }\n"
	} else {
		body += "    if (!(value >= 0.0 && value <= " + maximum + ")) {\n        hex_numeric_trap();\n    }\n"
	}
	return body + "    return (" + target.CName + ")value;\n"
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

// renderConversion renders a checked conversion node through its helper.
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
	return conversionHelperName(spec) + "(" + operand + ")", nil
}
