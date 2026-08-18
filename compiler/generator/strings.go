package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// literalHandle identifies one payload in a literalRegistry.
type literalHandle struct{ index int }

// literalRegistry owns the program-wide literal order and the only valid
// mapping from a payload to its generated C object.
type literalRegistry struct {
	payloads []string
	seen     map[string]literalHandle
	used     bool
	strand   bool
}

func newLiteralRegistry() *literalRegistry {
	return &literalRegistry{seen: make(map[string]literalHandle)}
}

func (registry *literalRegistry) Intern(payload string) literalHandle {
	if handle, exists := registry.seen[payload]; exists {
		return handle
	}
	handle := literalHandle{index: len(registry.payloads)}
	registry.payloads = append(registry.payloads, payload)
	registry.seen[payload] = handle
	return handle
}

func (registry *literalRegistry) CName(handle literalHandle) string {
	return stringLiteralCName(handle.index)
}

func (registry *literalRegistry) Lookup(payload string) (literalHandle, bool) {
	handle, exists := registry.seen[payload]
	return handle, exists
}

func (registry *literalRegistry) All() []string {
	return registry.payloads
}

// discoverGeneratedStrings interns checked string payloads and reports
// whether this module needs the String component.
func discoverGeneratedStrings(program checker.Program, registry *literalRegistry) bool {
	used := false
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if compilerTypes.IsString(typ) {
				used = true
				registry.used = true
				return
			}
			if compilerTypes.IsStrand(typ) {
				used = true
				registry.used = true
				registry.strand = true
				return
			}
		},
		Expression: func(node checker.Expression) {
			if node.Kind == checker.StringLiteralExpression {
				used = true
				registry.used = true
				registry.Intern(node.Name)
			}
		},
	}
	walkProgram(program, visitor)
	return used
}

// stringLiteralCName returns the object base name of one literal.
func stringLiteralCName(index int) string {
	return fmt.Sprintf("hex_lit_%d", index)
}

func validateTextExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.StringLiteralExpression:
		if !compilerTypes.IsString(node.ResultType) && !compilerTypes.IsStrand(node.ResultType) {
			return unknownExpressionDiagnostic("string literal has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("string literal type does not match its expected type")
		}
		return nil
	case checker.StringMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("string method call has invalid checked metadata")
		}
		strand := compilerTypes.IsStrand(node.OperandType)
		switch node.Name {
		case "length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("text length call has invalid checked metadata")
			}
		case "is_empty":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("text is_empty call has invalid checked metadata")
			}
		case "bytes":
			if strand || len(node.Arguments) != 0 || node.ResultType.View == nil || !compilerTypes.Equal(node.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string bytes call has invalid checked metadata")
			}
		case "slice":
			if strand || len(node.Arguments) != 2 || node.ResultType.View == nil || !compilerTypes.Equal(node.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string slice call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "rune_cursor":
			if strand || len(node.Arguments) != 0 || !compilerTypes.IsRuneCursor(node.ResultType) {
				return unknownExpressionDiagnostic("string rune_cursor call has invalid checked metadata")
			}
		case "to_string":
			if len(node.Arguments) != 1 || !compilerTypes.IsString(node.ResultType) {
				return unknownExpressionDiagnostic("text to_string call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "concat", "free":
			if strand {
				return unknownExpressionDiagnostic("strand has no " + node.Name + " method")
			}
			if len(node.Arguments) != 2 && node.Name == "concat" || len(node.Arguments) != 1 && node.Name == "free" {
				return unknownExpressionDiagnostic("string " + node.Name + " call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
			if node.Name == "free" {
				if node.ResultType != (compilerTypes.Type{}) {
					return unknownExpressionDiagnostic("string free call has invalid checked metadata")
				}
				if expected != nil {
					return unknownExpressionDiagnostic("string free produces no value")
				}
			} else if !compilerTypes.IsString(node.ResultType) {
				return unknownExpressionDiagnostic("string concat call has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown text method")
		}
		if node.Name != "free" && expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("text method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.StringFromBytesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || !compilerTypes.IsHeap(node.OperandType) || !compilerTypes.IsString(node.ResultType) || node.Arguments[0].Type.View == nil {
			return unknownExpressionDiagnostic("String.from_bytes has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("String.from_bytes result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.StringFromRunesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || !compilerTypes.IsHeap(node.OperandType) || !compilerTypes.IsString(node.ResultType) || node.Arguments[0].Type.View == nil {
			return unknownExpressionDiagnostic("String.from_runes has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("String.from_runes result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.RuneCursorMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsRuneCursor(node.OperandType) {
			return unknownExpressionDiagnostic("rune cursor method has invalid checked metadata")
		}
		switch node.Name {
		case "has_next":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("rune cursor has_next has invalid checked metadata")
			}
		case "next":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Rune) {
				return unknownExpressionDiagnostic("rune cursor next has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown rune cursor method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("rune cursor method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	}
	return unknownExpressionDiagnostic("unsupported text expression")
}

func renderTextComparison(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.StringCompareExpression:
		if node.Left == nil || node.Right == nil {
			return "", unknownExpressionDiagnostic("text ordering without both operands")
		}
		left, _, leftErr := renderExpressionNodeWithExpectedState(*node.Left, &node.OperandType, state)
		if leftErr != nil {
			return "", leftErr
		}
		right, _, rightErr := renderExpressionNodeWithExpectedState(*node.Right, &node.OperandType, state)
		if rightErr != nil {
			return "", rightErr
		}
		if compilerTypes.IsStrand(node.OperandType) {
			// Strand ordering is a direct memcmp of the canonical 32-byte
			// zero-filled representation; its sign is the ordering result.
			comparison := " < 0"
			switch node.Operator {
			case checker.LessEqualOperator:
				comparison = " <= 0"
			case checker.GreaterOperator:
				comparison = " > 0"
			case checker.GreaterEqualOperator:
				comparison = " >= 0"
			}
			return "(memcmp(" + left + ".data, " + right + ".data, 32)" + comparison + ")", nil
		}
		helper := "hex_compare_hex_string"
		comparison := " < 0"
		switch node.Operator {
		case checker.LessEqualOperator:
			comparison = " <= 0"
		case checker.GreaterOperator:
			comparison = " > 0"
		case checker.GreaterEqualOperator:
			comparison = " >= 0"
		}
		return "(" + helper + "(" + left + ", " + right + ")" + comparison + ")", nil
	}
	return "", unknownExpressionDiagnostic("unsupported text comparison")
}

func renderTextExpression(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.StringLiteralExpression:
		handle, ok := state.strings.Lookup(node.Name)
		if !ok {
			return "", unknownExpressionDiagnostic("string literal is missing from the checked literal registry: " + node.Name)
		}
		if compilerTypes.IsStrand(node.ResultType) {
			// A Strand is a 32-byte zero-padded inline value.
			payload := node.Name
			var builder strings.Builder
			builder.WriteString("(hex_strand){{")
			for _, character := range []byte(payload) {
				fmt.Fprintf(&builder, " %d,", character)
			}
			builder.WriteString(" 0 }}")
			return builder.String(), nil
		}
		return "&" + state.strings.CName(handle), nil
	case checker.StringMethodCallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("string method without a checked receiver")
		}
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		switch node.Name {
		case "length":
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_rune_length(" + receiver + ")", nil
			}
			return "hex_string_rune_length(" + receiver + ")", nil
		case "is_empty":
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_is_empty(" + receiver + ")", nil
			}
			return "hex_string_is_empty(" + receiver + ")", nil
		case "rune_cursor":
			return "hex_string_rune_cursor(" + receiver + ")", nil
		case "bytes":
			return "hex_string_bytes(" + receiver + ")", nil
		case "slice":
			if len(node.Arguments) != 2 {
				return "", unknownExpressionDiagnostic("string slice without checked bounds")
			}
			start, startErr := renderOperandWithState(node.Arguments[0], state)
			if startErr != nil {
				return "", startErr
			}
			end, endErr := renderOperandWithState(node.Arguments[1], state)
			if endErr != nil {
				return "", endErr
			}
			return "hex_string_slice(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		case "to_string":
			if len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("string to_string without a checked heap")
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_to_string(" + heap + ", " + receiver + ")", nil
			}
			return "hex_string_to_string(" + heap + ", " + receiver + ")", nil
		case "concat":
			if len(node.Arguments) != 2 {
				return "", unknownExpressionDiagnostic("string concat without checked operands")
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			other, otherErr := renderOperandWithState(node.Arguments[1], state)
			if otherErr != nil {
				return "", otherErr
			}
			return "hex_string_concat(" + heap + ", " + receiver + ", " + other + ")", nil
		case "free":
			if len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("string free without a checked heap")
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			return "hex_string_free(" + heap + ", " + receiver + ")", nil
		}
		return "", unknownExpressionDiagnostic("unknown string method")
	case checker.StringFromBytesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("String.from_bytes without checked operands")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		view, viewErr := renderOperandWithState(node.Arguments[0], state)
		if viewErr != nil {
			return "", viewErr
		}
		return "hex_string_from_bytes(" + heap + ", (" + view + ").data, (" + view + ").length)", nil
	case checker.StringFromRunesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("String.from_runes without checked operands")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		view, viewErr := renderOperandWithState(node.Arguments[0], state)
		if viewErr != nil {
			return "", viewErr
		}
		return "hex_string_from_runes(" + heap + ", (" + view + ").data, (" + view + ").length)", nil
	case checker.RuneCursorMethodCallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("rune cursor method without a checked receiver")
		}
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		switch node.Name {
		case "has_next":
			return "hex_rune_cursor_has_next(" + receiver + ")", nil
		case "next":
			return "hex_rune_cursor_next(&(" + receiver + "))", nil
		}
		return "", unknownExpressionDiagnostic("unknown rune cursor method " + node.Name)
	}
	return "", unknownExpressionDiagnostic("unsupported text expression")
}
