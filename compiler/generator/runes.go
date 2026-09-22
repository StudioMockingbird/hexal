package generator

import (
	"fmt"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// runePropertyMethod reports whether name is one of the Tier 2 Rune property
// methods, which all route through the utf8proc property helpers.
func runePropertyMethod(name string) bool {
	switch name {
	case "is_lower", "is_upper", "is_alphabetic", "is_numeric", "is_whitespace",
		"to_lower", "to_upper", "to_title", "display_width", "combining_class":
		return true
	}
	return false
}

// unicodeCategoryTag resolves one UnicodeCategory variant's generated tag
// constant. The nil fallback only serves isolated component-rendering tests
// that build a bare programEmission without running full discovery.
func unicodeCategoryTag(tags *tagRegistry, index int) string {
	if tags == nil {
		return "hex_tag_UnicodeCategory_" + compilerTypes.UnicodeCategoryVariantNames[index]
	}
	return tags.adtVariantTag(compilerTypes.UnicodeCategoryType.Adt, index)
}

// normalizationFormTag resolves one NormalizationForm variant's generated tag
// constant. The nil fallback only serves isolated component-rendering tests
// that build a bare programEmission without running full discovery.
func normalizationFormTag(tags *tagRegistry, index int) string {
	if tags == nil {
		return "hex_tag_NormalizationForm_" + compilerTypes.NormalizationFormVariantNames[index]
	}
	return tags.adtVariantTag(compilerTypes.NormalizationFormType.Adt, index)
}

// validateRuneMethod checks one Rune method's checked metadata: value yields
// UInt32 and utf8_length yields Size, both pure reads of a receiver; from takes
// one UInt32 and yields Rune | Error; the Tier 2 properties yield Bool, Rune,
// Int32, or UInt8.
func validateRuneMethod(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if !compilerTypes.IsRune(node.OperandType) {
		return unknownExpressionDiagnostic("rune method has invalid checked metadata")
	}
	switch node.Name {
	case "value":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.UInt32) {
			return unknownExpressionDiagnostic("rune value has invalid checked metadata")
		}
	case "utf8_length":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
			return unknownExpressionDiagnostic("rune utf8_length has invalid checked metadata")
		}
	case "is_lower", "is_upper", "is_alphabetic", "is_numeric", "is_whitespace":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("rune " + node.Name + " has invalid checked metadata")
		}
	case "to_lower", "to_upper", "to_title":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.IsRune(node.ResultType) {
			return unknownExpressionDiagnostic("rune " + node.Name + " has invalid checked metadata")
		}
	case "display_width":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Int32) {
			return unknownExpressionDiagnostic("rune display_width has invalid checked metadata")
		}
	case "combining_class":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.UInt8) {
			return unknownExpressionDiagnostic("rune combining_class has invalid checked metadata")
		}
	case "category":
		if node.Operand == nil || len(node.Arguments) != 0 || !compilerTypes.IsUnicodeCategory(node.ResultType) {
			return unknownExpressionDiagnostic("rune category has invalid checked metadata")
		}
	case "from":
		if node.Operand != nil || len(node.Arguments) != 1 || node.SourceLine == 0 || !textFailureResult(node.ResultType, compilerTypes.Rune) {
			return unknownExpressionDiagnostic("Rune.from has invalid checked metadata")
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	default:
		return unknownExpressionDiagnostic("unknown rune method " + node.Name)
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.WidensTo(node.ResultType, *expected) {
		return unknownExpressionDiagnostic("rune method result does not match its expected type")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

// renderRuneMethod renders one Rune method. value is the identity to the
// underlying uint32_t scalar; utf8_length calls the module-local width helper;
// from calls the module-local scalar adapter; the Tier 2 properties call the
// shared utf8proc property helpers.
func renderRuneMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Name == "from" {
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("Rune.from without a checked argument")
		}
		value, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_rune_from_%s(%s, %d, %d)", streamAdapterSuffix(node.ResultType), value, node.SourceLine, node.SourceColumn), nil
	}
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("rune method without a checked receiver")
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "value":
		return "((uint32_t)" + receiver + ")", nil
	case "utf8_length":
		return "hex_rune_utf8_length(" + receiver + ")", nil
	case "category":
		return "(hex_t_UnicodeCategory){ .tag = hex_rune_categories[hex_rune_category_index(" + receiver + ")] }", nil
	}
	if runePropertyMethod(node.Name) {
		return "hex_rune_" + node.Name + "(" + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic("unknown rune method " + node.Name)
}
