package generator

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// literalHandle identifies one payload in a literalRegistry.
type literalHandle struct{ index int }

// literalRegistry owns the program-wide literal order and the only valid
// mapping from a payload to its generated C object. It also records which
// inline String<N> capacities the program uses, since each is one C struct
// emitted once into hexal/string.h.
type literalRegistry struct {
	payloads []string
	seen     map[string]literalHandle
	used     bool
	inline   map[uint64]string
}

func newLiteralRegistry() *literalRegistry {
	return &literalRegistry{seen: make(map[string]literalHandle), inline: make(map[uint64]string)}
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

// requireInline records that the program uses one String<N> type.
func (registry *literalRegistry) requireInline(typ compilerTypes.Type) {
	if registry.inline == nil {
		registry.inline = make(map[uint64]string)
	}
	registry.inline[typ.InlineString.Capacity] = typ.CName
}

// requireErrorText records the two inline types Error's representation names:
// the ErrorKind.Other header and the Error message. Anything that selects the
// Error object selects them.
func (registry *literalRegistry) requireErrorText() {
	registry.used = true
	registry.requireInline(compilerTypes.ErrorHeaderText)
	registry.requireInline(compilerTypes.ErrorMessageText)
}

// inlineCapacities returns the recorded capacities in ascending order.
func (registry *literalRegistry) inlineCapacities() []uint64 {
	capacities := make([]uint64, 0, len(registry.inline))
	for capacity := range registry.inline {
		capacities = append(capacities, capacity)
	}
	slices.Sort(capacities)
	return capacities
}

// discoverGeneratedStrings interns checked string payloads and reports
// whether this module needs the String component.
func discoverGeneratedStrings(program checker.Program, registry *literalRegistry) bool {
	used := false
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsString(typ) {
				used = true
				registry.used = true
				return nil
			}
			if compilerTypes.IsInlineString(typ) {
				used = true
				registry.used = true
				registry.requireInline(typ)
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.StringLiteralExpression {
				used = true
				registry.used = true
				if compilerTypes.IsInlineString(node.ResultType) {
					registry.requireInline(node.ResultType)
				} else {
					registry.Intern(node.Name)
				}
			}
			if node.Kind == checker.StringInterpolateExpression || node.Kind == checker.InlineStringConstructExpression && node.Name == "interpolate" {
				used = true
				registry.used = true
				for _, segment := range node.InterpolationSegments {
					if !segment.IsValue {
						registry.Intern(segment.Text)
					}
				}
			}
			if node.Kind == checker.InlineStringConstructExpression {
				used = true
				registry.used = true
				registry.requireInline(node.OperandType)
			}
			if node.Kind == checker.TextCoerceExpression {
				used = true
				registry.used = true
				registry.requireInline(node.ResultType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	return used
}

// discoverInterpolationUsed reports whether the program contains a checked
// interpolate call on either text form, which selects the String component's
// demand-driven formatter helpers.
func discoverInterpolationUsed(program checker.Program) bool {
	used := false
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.StringInterpolateExpression || node.Kind == checker.InlineStringConstructExpression && node.Name == "interpolate" {
				used = true
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	return used
}

// stringLiteralCName returns the object base name of one literal.
func stringLiteralCName(index int) string {
	return fmt.Sprintf("hex_lit_%d", index)
}

// textPlace reports whether a checked expression renders as a C lvalue whose
// address can be taken: a binding, or a member, element, or dereference rooted
// in one. Anything else is a temporary value.
func textPlace(node *checker.Expression) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case checker.VariableExpression:
		return true
	case checker.MemberExpression, checker.IndexExpression, checker.DereferenceExpression:
		return textPlace(node.Operand)
	}
	return false
}

// textView spells the (bytes, length) view of one text operand. A heap String
// is a handle and views directly. An inline value views the storage it lives
// in, so a place is addressed in place and a temporary is first copied into a
// compound literal that lives to the end of the enclosing block; either way
// the operand is evaluated exactly once. address forces the address of the
// operand itself, for bytes() and slice(), whose result must point into the
// receiver rather than a copy of it (the checker admits only a place there).
func textView(operand *checker.Expression, typ compilerTypes.Type, address bool, state *expressionValidation) (string, error) {
	if operand == nil {
		return "", unknownExpressionDiagnostic("text operand is missing")
	}
	rendered, _, err := renderHoistedExpressionNode(operand, &typ, state)
	if err != nil {
		return "", err
	}
	if compilerTypes.IsString(typ) {
		return "hex_text_heap(" + rendered + ")", nil
	}
	if !compilerTypes.IsInlineString(typ) {
		return "", unknownExpressionDiagnostic("text view over a non-text operand")
	}
	if address || textPlace(operand) {
		return "hex_text_inline(&(" + rendered + "))", nil
	}
	if _, hoisted := hoistedSequenceValue(state, operand); hoisted {
		return "hex_text_inline(&" + rendered + ")", nil
	}
	return "hex_text_inline((" + typ.CName + "[1]){ " + rendered + " })", nil
}

// boundedTextTraps is the trap taken when text coerced into a bounded capacity
// does not fit, one complete message per destination: the message of
// Error(kind, message) and the header of ErrorKind.Other.
var boundedTextTraps = map[string]string{
	"Error message":          "[Runtime Error] Error message exceeds 256 bytes\n",
	"ErrorKind.Other header": "[Runtime Error] ErrorKind.Other header exceeds 128 bytes\n",
}

// textFill spells an inline value of type typ holding the view's text. The
// destination is a zeroed compound literal, so the value is complete in one
// expression; checkedMessage, when not empty, is the trap taken when the text
// does not fit, and empty means the caller has proven it does.
func textFill(typ compilerTypes.Type, view, checkedMessage string) string {
	if checkedMessage == "" {
		return "(*(" + typ.CName + " *)hex_text_fill(&(" + typ.CName + "){0}, " + view + "))"
	}
	return "(*(" + typ.CName + " *)hex_text_fill_checked(&(" + typ.CName + "){0}, " + strconv.FormatUint(typ.InlineString.Capacity, 10) + ", " + view + ", " + strconv.Quote(checkedMessage) + "))"
}

func validateTextExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.StringLiteralExpression:
		if !compilerTypes.IsText(node.ResultType) {
			return unknownExpressionDiagnostic("string literal has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("string literal type does not match its expected type")
		}
		if compilerTypes.IsInlineString(node.ResultType) && uint64(len(node.Name)) > node.ResultType.InlineString.Capacity {
			return unknownExpressionDiagnostic("string literal exceeds its inline capacity")
		}
		return nil
	case checker.StringMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsText(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("string method call has invalid checked metadata")
		}
		inline := compilerTypes.IsInlineString(node.OperandType)
		switch node.Name {
		case "c_pointer":
			if inline || len(node.Arguments) != 0 || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string c_pointer call has invalid checked metadata")
			}
		case "length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("text length call has invalid checked metadata")
			}
		case "rune_length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("text rune length call has invalid checked metadata")
			}
		case "grapheme_length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("text grapheme length call has invalid checked metadata")
			}
		case "byte_cursor":
			if len(node.Arguments) != 0 || !compilerTypes.IsByteCursor(node.ResultType) {
				return unknownExpressionDiagnostic("text byte_cursor call has invalid checked metadata")
			}
		case "rune_cursor":
			if len(node.Arguments) != 0 || !compilerTypes.IsRuneCursor(node.ResultType) {
				return unknownExpressionDiagnostic("text rune_cursor call has invalid checked metadata")
			}
		case "grapheme_cursor":
			if len(node.Arguments) != 0 || !compilerTypes.IsGraphemeCursor(node.ResultType) {
				return unknownExpressionDiagnostic("text grapheme_cursor call has invalid checked metadata")
			}
		case "bytes":
			if len(node.Arguments) != 0 || node.ResultType.Slice == nil || !compilerTypes.Equal(node.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string bytes call has invalid checked metadata")
			}
		case "slice":
			if len(node.Arguments) != 2 || node.ResultType.Slice == nil || !compilerTypes.Equal(node.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string slice call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "copy":
			if len(node.Arguments) != 1 || !compilerTypes.IsString(node.ResultType) {
				return unknownExpressionDiagnostic("text copy call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "widen":
			if !inline || len(node.Arguments) != 0 || !compilerTypes.IsInlineString(node.ResultType) ||
				node.ResultType.InlineString.Capacity < node.OperandType.InlineString.Capacity {
				return unknownExpressionDiagnostic("text widen call has invalid checked metadata")
			}
		case "concat":
			if len(node.Arguments) != 2 || node.SourceLine == 0 || !textFailureResult(node.ResultType, compilerTypes.StringType) {
				return unknownExpressionDiagnostic("string concat call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "casefold":
			if len(node.Arguments) != 1 || node.SourceLine == 0 || !textFailureResult(node.ResultType, compilerTypes.StringType) {
				return unknownExpressionDiagnostic("string casefold call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "normalize":
			if len(node.Arguments) != 2 || node.SourceLine == 0 || !textFailureResult(node.ResultType, compilerTypes.StringType) {
				return unknownExpressionDiagnostic("string normalize call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "free":
			if inline || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("string free call has invalid checked metadata")
			}
			if expected != nil {
				return unknownExpressionDiagnostic("string free produces no value")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic("unknown text method")
		}
		if node.Name != "free" && expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("text method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.StringFromBytesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.SourceLine == 0 || !compilerTypes.IsHeap(node.OperandType) ||
			!textFailureResult(node.ResultType, compilerTypes.StringType) || node.Arguments[0].Type.Slice == nil {
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
		if node.Operand == nil || len(node.Arguments) != 1 || node.SourceLine == 0 || !compilerTypes.IsHeap(node.OperandType) ||
			!textFailureResult(node.ResultType, compilerTypes.StringType) || node.Arguments[0].Type.Slice == nil ||
			!compilerTypes.Equal(node.Arguments[0].Type.Slice.Element, compilerTypes.Rune) {
			return unknownExpressionDiagnostic("String.from_runes has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("String.from_runes result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.InlineStringConstructExpression:
		if !compilerTypes.IsInlineString(node.OperandType) || node.SourceLine == 0 || !textFailureResult(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("inline string constructor has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("inline string constructor result does not match its expected type")
		}
		switch node.Name {
		case "from_bytes":
			if len(node.Arguments) != 1 {
				return unknownExpressionDiagnostic("inline from_bytes has invalid checked metadata")
			}
		case "concat":
			if len(node.Arguments) != 2 {
				return unknownExpressionDiagnostic("inline concat has invalid checked metadata")
			}
		case "interpolate":
			return validateInterpolationSegments(node, "String<N>.interpolate", state)
		default:
			return unknownExpressionDiagnostic("unknown inline string constructor " + node.Name)
		}
		for _, argument := range node.Arguments {
			if argument.Type.Slice == nil {
				return unknownExpressionDiagnostic("inline string constructor operand is not a byte slice")
			}
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.TextCoerceExpression:
		if _, known := boundedTextTraps[node.Name]; node.Operand == nil || !compilerTypes.IsText(node.OperandType) || !compilerTypes.IsInlineString(node.ResultType) || !known {
			return unknownExpressionDiagnostic("text coercion has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("text coercion result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.StringInterpolateExpression:
		if node.Operand == nil || !compilerTypes.IsHeap(node.OperandType) || !compilerTypes.IsString(node.ResultType) {
			return unknownExpressionDiagnostic("String.interpolate has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("String.interpolate result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateInterpolationSegments(node, "String.interpolate", state)
	}
	return unknownExpressionDiagnostic("unsupported text expression")
}

// textFailureResult reports whether typ is exactly `success | Error`.
func textFailureResult(typ, success compilerTypes.Type) bool {
	if typ.Union == nil {
		return false
	}
	members := compilerTypes.UnionMembers(typ)
	return members.Len() == 2 && unionHasMember(members, success) && unionHasMember(members, compilerTypes.ErrorType)
}

// validateInterpolationSegments checks one interpolate node's segments: at
// least one value, each of a supported checked type.
func validateInterpolationSegments(node checker.Expression, label string, state *expressionValidation) error {
	if len(node.InterpolationSegments) == 0 {
		return unknownExpressionDiagnostic(label + " has no checked segments")
	}
	hasValue := false
	for _, segment := range node.InterpolationSegments {
		if !segment.IsValue {
			continue
		}
		hasValue = true
		if !supportedInterpolationValueType(segment.Value.Type) {
			return unknownExpressionDiagnostic(label + " segment has an unsupported checked type")
		}
		if err := validateCheckedOperandWithState(segment.Value, state); err != nil {
			return err
		}
	}
	if !hasValue {
		return unknownExpressionDiagnostic(label + " has no checked value segment")
	}
	return nil
}

// renderTextComparison renders an ordering operator over two text operands of
// any forms: bytewise, through the one shared compare helper.
func renderTextComparison(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.StringCompareExpression:
		if node.Left == nil || node.Right == nil {
			return "", unknownExpressionDiagnostic("text ordering without both operands")
		}
		left, leftErr := textView(node.Left, node.OperandType, false, state)
		if leftErr != nil {
			return "", leftErr
		}
		right, rightErr := textView(node.Right, node.RightType, false, state)
		if rightErr != nil {
			return "", rightErr
		}
		comparison := " < 0"
		switch node.Operator {
		case checker.LessEqualOperator:
			comparison = " <= 0"
		case checker.GreaterOperator:
			comparison = " > 0"
		case checker.GreaterEqualOperator:
			comparison = " >= 0"
		}
		return "(hex_compare_text(" + left + ", " + right + ")" + comparison + ")", nil
	}
	return "", unknownExpressionDiagnostic("unsupported text comparison")
}

// renderTextEquality renders == and != over two text operands of any forms.
func renderTextEquality(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Left == nil || node.Right == nil {
		return "", unknownExpressionDiagnostic("text equality without both operands")
	}
	left, leftErr := textView(node.Left, node.OperandType, false, state)
	if leftErr != nil {
		return "", leftErr
	}
	right, rightErr := textView(node.Right, node.RightType, false, state)
	if rightErr != nil {
		return "", rightErr
	}
	result := "hex_equal_text(" + left + ", " + right + ")"
	if node.Operator == checker.NotEqualOperator {
		return "(!" + result + ")", nil
	}
	return result, nil
}

func renderTextExpression(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.StringLiteralExpression:
		if compilerTypes.IsInlineString(node.ResultType) {
			// An inline literal is a complete value: its length and its bytes.
			// The rest of the array is zero from the initializer, unread.
			if node.Name == "" {
				return "(" + node.ResultType.CName + "){ .byte_length = 0 }", nil
			}
			var builder strings.Builder
			fmt.Fprintf(&builder, "(%s){ .byte_length = %d, .data = {", node.ResultType.CName, len(node.Name))
			for _, character := range []byte(node.Name) {
				fmt.Fprintf(&builder, " %d,", character)
			}
			builder.WriteString(" } }")
			return builder.String(), nil
		}
		if state.strings == nil {
			// A registry-less state reaching String rendering is a generator
			// defect; it fails closed here instead of dereferencing nil.
			return "", unknownExpressionDiagnostic("string literal rendering requires a literal registry")
		}
		handle, ok := state.strings.Lookup(node.Name)
		if !ok {
			return "", unknownExpressionDiagnostic("string literal is missing from the checked literal registry: " + node.Name)
		}
		return "&" + state.strings.CName(handle), nil
	case checker.StringMethodCallExpression:
		return renderTextMethod(node, state)
	case checker.StringFromBytesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("String.from_bytes without checked operands")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		bytes, viewErr := renderOperandWithState(node.Arguments[0], state)
		if viewErr != nil {
			return "", viewErr
		}
		return fmt.Sprintf("hex_string_from_bytes_%s(%s, %s, %d, %d)", streamAdapterSuffix(node.ResultType), heap, bytes, node.SourceLine, node.SourceColumn), nil
	case checker.StringFromRunesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("String.from_runes without checked operands")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		runes, runesErr := renderOperandWithState(node.Arguments[0], state)
		if runesErr != nil {
			return "", runesErr
		}
		return fmt.Sprintf("hex_string_from_runes_%s(%s, %s, %d, %d)", streamAdapterSuffix(node.ResultType), heap, runes, node.SourceLine, node.SourceColumn), nil
	case checker.InlineStringConstructExpression:
		return renderInlineStringConstruct(node, state)
	case checker.TextCoerceExpression:
		view, err := textView(node.Operand, node.OperandType, false, state)
		if err != nil {
			return "", err
		}
		message := ""
		if node.MemberIndex == 1 {
			var known bool
			if message, known = boundedTextTraps[node.Name]; !known {
				return "", unknownExpressionDiagnostic("text coercion names an unknown bounded destination: " + node.Name)
			}
		}
		return textFill(node.ResultType, view, message), nil
	case checker.StringInterpolateExpression:
		return renderStringInterpolate(node, state)
	}
	return "", unknownExpressionDiagnostic("unsupported text expression")
}

// renderTextMethod renders one method call on either text form.
func renderTextMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("string method without a checked receiver")
	}
	if node.Name == "c_pointer" {
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		// The String header's data pointer is the immutable byte address,
		// already terminated by the allocation's existing zero. No scan,
		// copy, or cast is introduced.
		return "(" + receiver + ")->data", nil
	}
	if node.Name == "free" {
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_string_free(" + heap + ", " + receiver + ")", nil
	}
	// bytes(), slice(), and the cursor constructors must point into the
	// receiver itself.
	address := node.Name == "bytes" || node.Name == "slice" || node.Name == "byte_cursor" || node.Name == "rune_cursor"
	view, viewErr := textView(node.Operand, node.OperandType, address, state)
	if viewErr != nil {
		return "", viewErr
	}
	switch node.Name {
	case "length":
		return "(" + view + ").length", nil
	case "rune_length":
		// Counting scalars steps the utf8proc adapter the validator uses, so
		// it is O(n) and never allocation-free text construction.
		return "hex_text_rune_length(" + view + ")", nil
	case "grapheme_length":
		// Counting clusters steps the utf8proc break-state helper, which must
		// see every adjacent scalar pair in order.
		return "hex_text_grapheme_length(" + view + ")", nil
	case "byte_cursor":
		return "hex_text_byte_cursor(" + view + ")", nil
	case "rune_cursor":
		return "hex_text_rune_cursor(" + view + ")", nil
	case "grapheme_cursor":
		return "hex_text_grapheme_cursor(" + view + ")", nil
	case "bytes":
		return "hex_text_bytes(" + view + ")", nil
	case "slice":
		start, startErr := renderOperandWithState(node.Arguments[0], state)
		if startErr != nil {
			return "", startErr
		}
		end, endErr := renderOperandWithState(node.Arguments[1], state)
		if endErr != nil {
			return "", endErr
		}
		return "hex_text_slice(" + view + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
	case "copy":
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_string_make(" + heap + ", " + view + ")", nil
	case "widen":
		return textFill(node.ResultType, view, ""), nil
	case "concat":
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		other, otherErr := renderOperandWithState(node.Arguments[1], state)
		if otherErr != nil {
			return "", otherErr
		}
		return fmt.Sprintf("hex_string_concat_%s(%s, %s, %s, %d, %d)", streamAdapterSuffix(node.ResultType), heap, view, other, node.SourceLine, node.SourceColumn), nil
	case "casefold":
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		return fmt.Sprintf("hex_string_casefold_%s(%s, %s, %d, %d)", streamAdapterSuffix(node.ResultType), heap, view, node.SourceLine, node.SourceColumn), nil
	case "normalize":
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		form, formErr := renderOperandWithState(node.Arguments[1], state)
		if formErr != nil {
			return "", formErr
		}
		return fmt.Sprintf("hex_string_normalize_%s(%s, %s, (%s).tag, %d, %d)", streamAdapterSuffix(node.ResultType), heap, view, form, node.SourceLine, node.SourceColumn), nil
	}
	return "", unknownExpressionDiagnostic("unknown string method")
}

// renderInlineStringConstruct renders String<N>.from_bytes and .concat through
// their per-module adapters; interpolate was hoisted before its statement.
func renderInlineStringConstruct(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Name {
	case "from_bytes", "concat":
		arguments := make([]string, len(node.Arguments))
		for index := range node.Arguments {
			rendered, err := renderOperandWithState(node.Arguments[index], state)
			if err != nil {
				return "", err
			}
			arguments[index] = rendered
		}
		return fmt.Sprintf("hex_text_%s_%s(%s, %d, %d)", node.Name, streamAdapterSuffix(node.ResultType), strings.Join(arguments, ", "), node.SourceLine, node.SourceColumn), nil
	case "interpolate":
		return renderStringInterpolate(node, state)
	}
	return "", unknownExpressionDiagnostic("unknown inline string constructor " + node.Name)
}

// generatedTextState records one module's text operations that can fail and
// the result union each produces. Every one is lowered through a module-owned
// adapter, since only the module knows the union's tags and its own file
// literal for the Errors it builds.
type generatedTextState struct {
	used bool
	// runeLength is true when the module counts text scalars, which steps the
	// utf8proc adapter without performing a fallible text operation.
	runeLength bool
	// runeUtf8Length is true when the module asks one Rune for its encoded
	// byte length, selecting the module-local width helper.
	runeUtf8Length bool
	// runeProperties is true when the module reads a Tier 2 Rune property,
	// selecting the utf8proc property helpers.
	runeProperties bool
	// runeCategory is true when the module asks one Rune for its general
	// category, selecting the utf8proc category helper and the module-local
	// variant-tag table.
	runeCategory bool
	// graphemeLength is true when the module counts text grapheme clusters,
	// selecting the utf8proc break-state helper.
	graphemeLength bool
	// graphemeCursor is true when the module scans text with a GraphemeCursor,
	// selecting the borrowed-range struct and the stateful segmenter.
	graphemeCursor bool
	// runeIteration is true when the module iterates text by Rune, which steps
	// the utf8proc decode helper without performing a fallible text operation.
	runeIteration bool
	// byteCursor is true when the module scans text with a ByteCursor, which
	// needs only index arithmetic.
	byteCursor bool
	// runeCursor is true when the module scans text with a RuneCursor, which
	// steps the shared utf8proc decode helper.
	runeCursor bool
	// runeFrom collects the result unions of Rune.from calls, each needing a
	// module-local adapter that builds the Error arm.
	runeFrom []compilerTypes.Type
	// heapFromRunes collects the result unions of String.from_runes calls, each
	// needing a module-local adapter that builds the Error arm.
	heapFromRunes []compilerTypes.Type
	// casefold collects the result unions of String.casefold calls, each needing
	// a module-local adapter that builds the Error arm.
	casefold []compilerTypes.Type
	// normalize collects the result unions of String.normalize calls, each
	// needing a module-local adapter and tag-to-form map.
	normalize       []compilerTypes.Type
	heapFromBytes   []compilerTypes.Type
	heapConcat      []compilerTypes.Type
	inlineFromBytes []compilerTypes.Type
	inlineConcat    []compilerTypes.Type
	fileLiteral     literalHandle
}

const (
	textMessageInvalidUTF8     = "invalid UTF-8 in string"
	textMessageOverCapacity    = "string exceeds capacity"
	textMessageInvalidScalar   = "invalid Unicode scalar value"
	textMessageTransformFailed = "Unicode transform failed"
)

// discoverGeneratedText walks one module for the fallible text operations.
func discoverGeneratedText(program checker.Program, logicalKey string, literals *literalRegistry) *generatedTextState {
	state := &generatedTextState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch {
			case node.Kind == checker.StringFromBytesExpression:
				state.heapFromBytes = appendUnionOnce(state.heapFromBytes, node.ResultType)
			case node.Kind == checker.StringMethodCallExpression && node.Name == "concat":
				state.heapConcat = appendUnionOnce(state.heapConcat, node.ResultType)
			case node.Kind == checker.InlineStringConstructExpression && node.Name == "from_bytes":
				state.inlineFromBytes = appendUnionOnce(state.inlineFromBytes, node.ResultType)
			case node.Kind == checker.InlineStringConstructExpression && node.Name == "concat":
				state.inlineConcat = appendUnionOnce(state.inlineConcat, node.ResultType)
			case node.Kind == checker.StringMethodCallExpression && node.Name == "byte_cursor":
				state.byteCursor = true
				return nil
			case node.Kind == checker.StringMethodCallExpression && node.Name == "rune_cursor":
				state.runeCursor = true
				return nil
			case node.Kind == checker.StringMethodCallExpression && node.Name == "rune_length":
				// Counting scalars steps the same utf8proc adapter the
				// validator uses, so it selects the validator without adding a
				// fallible text operation.
				state.runeLength = true
				return nil
			case node.Kind == checker.StringMethodCallExpression && node.Name == "grapheme_length":
				// Counting clusters steps the utf8proc break-state helper.
				state.graphemeLength = true
				return nil
			case node.Kind == checker.StringMethodCallExpression && node.Name == "grapheme_cursor":
				// The stateful segmenter and its borrowed range.
				state.graphemeCursor = true
				return nil
			case node.Kind == checker.RuneMethodCallExpression && node.Name == "utf8_length":
				// One Rune's encoded width needs no utf8proc step, only the
				// module-local range helper.
				state.runeUtf8Length = true
				return nil
			case node.Kind == checker.RuneMethodCallExpression && runePropertyMethod(node.Name):
				// The Tier 2 property surface reads utf8proc's tables.
				state.runeProperties = true
				return nil
			case node.Kind == checker.RuneMethodCallExpression && node.Name == "category":
				// The category tag comes from utf8proc's table plus the
				// module-local variant-tag table.
				state.runeCategory = true
				return nil
			case node.Kind == checker.RuneMethodCallExpression && node.Name == "from":
				// The scalar adapter builds the Rune | Error union, so it is
				// module-owned like every other fallible text operation.
				state.runeFrom = appendUnionOnce(state.runeFrom, node.ResultType)
				return nil
			case node.Kind == checker.StringFromRunesExpression:
				// Encoding a scalar sequence is fallible on an invalid scalar,
				// so it is module-owned like every other fallible text
				// operation.
				state.heapFromRunes = appendUnionOnce(state.heapFromRunes, node.ResultType)
				return nil
			case node.Kind == checker.StringMethodCallExpression && node.Name == "casefold":
				// The transform is fallible and allocates, so it is
				// module-owned like every other fallible text operation.
				state.casefold = appendUnionOnce(state.casefold, node.ResultType)
				return nil
			case node.Kind == checker.StringMethodCallExpression && node.Name == "normalize":
				// The transform is fallible and allocates, so it is
				// module-owned like every other fallible text operation.
				state.normalize = appendUnionOnce(state.normalize, node.ResultType)
				return nil
			default:
				return nil
			}
			state.used = true
			return nil
		},
		Statement: func(statement checker.Statement) error {
			loop, ok := statement.(checker.ForStatement)
			if !ok || !compilerTypes.IsText(loop.Source.Type) {
				return nil
			}
			if compilerTypes.IsRune(loop.Binders[len(loop.Binders)-1].Type) {
				// Iterating text by Rune steps the utf8proc decode helper, so
				// it selects the validator without a fallible operation.
				state.runeIteration = true
			}
			if compilerTypes.IsGrapheme(loop.Binders[len(loop.Binders)-1].Type) {
				// Iterating text by Grapheme steps the stateful segmenter.
				state.graphemeCursor = true
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used || discoverInlineInterpolation(program) || len(state.runeFrom) > 0 || len(state.heapFromRunes) > 0 || len(state.casefold) > 0 {
		state.fileLiteral = literals.Intern(logicalKey)
		literals.Intern(textMessageInvalidUTF8)
		literals.Intern(textMessageOverCapacity)
	}
	if len(state.runeFrom) > 0 || len(state.heapFromRunes) > 0 {
		literals.Intern(textMessageInvalidScalar)
	}
	if len(state.casefold) > 0 || len(state.normalize) > 0 {
		literals.Intern(textMessageTransformFailed)
	}
	return state
}

// discoverInlineInterpolation reports whether the module builds an inline
// String<N> by interpolation, which constructs the same Errors.
func discoverInlineInterpolation(program checker.Program) bool {
	used := false
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.InlineStringConstructExpression && node.Name == "interpolate" {
				used = true
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	return used
}

// textErrorArm spells one Error member of a result union for a failure the text
// operations report: the kind, the module's file literal, and a fixed message.
// line and column are C expressions for the source site.
func textErrorArm(union compilerTypes.Type, kind, message, file, line, column string, literals *literalRegistry, tags *tagRegistry) (string, error) {
	handle, ok := literals.Lookup(message)
	if !ok {
		return "", unknownExpressionDiagnostic("text failure message is missing from the literal registry: " + message)
	}
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_error_make((hex_t_ErrorKind){ .tag = %s }, %s, %s, &%s, &%s) }",
		union.CName, tag, field, errorKindTag(tags, kind), line, column, file, literals.CName(handle)), nil
}

// writeTextInlineHelpers emits the module-owned adapters that wrap the text
// core in each result union, plus the module-local Rune helpers. Failure kinds
// and messages are fixed: malformed UTF-8 is InvalidInput and an inline
// overflow is ResourceExhausted, never embedding a length or offset. The
// capacity is checked before the content, so an input that is both too long
// and malformed reports the capacity.
func writeTextInlineHelpers(result *strings.Builder, state *generatedTextState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil {
		return nil
	}
	if state.runeUtf8Length {
		// One module-local helper serves every Rune.utf8_length call: a Rune is
		// already a valid scalar, so only the encoding range decides the width
		// and no utf8proc step is needed.
		result.WriteString("\n// hex_rune_utf8_length returns the encoded UTF-8 byte length of one\n" +
			"// Unicode scalar: 1 through 4. The value is already a valid scalar, so\n" +
			"// only the encoding range decides the width.\n" +
			"static inline size_t hex_rune_utf8_length(uint32_t value) {\n" +
			"    if (value < 0x80) {\n" +
			"        return 1;\n" +
			"    }\n" +
			"    if (value < 0x800) {\n" +
			"        return 2;\n" +
			"    }\n" +
			"    if (value < 0x10000) {\n" +
			"        return 3;\n" +
			"    }\n" +
			"    return 4;\n" +
			"}\n")
	}
	if len(state.runeFrom) > 0 {
		fileName := literals.CName(state.fileLiteral)
		for _, union := range state.runeFrom {
			success, field := streamMemberRef(tags, union, compilerTypes.Rune)
			invalid, err := textErrorArm(union, "InvalidInput", textMessageInvalidScalar, fileName, "line", "column", literals, tags)
			if err != nil {
				return err
			}
			fmt.Fprintf(result,
				"\n// hex_rune_from_%s turns a UInt32 into a checked Unicode scalar. It\n"+
					"// rejects surrogates and values above U+10FFFF and reports, never traps.\n"+
					"static inline %s hex_rune_from_%s(uint32_t value, size_t line, size_t column) {\n"+
					"    if (value <= 0x10FFFF && !(value >= 0xD800 && value <= 0xDFFF)) {\n"+
					"        return (%s){ .tag = %s, .payload.%s = value };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				streamAdapterSuffix(union), union.CName, streamAdapterSuffix(union), union.CName, success, field, invalid)
		}
	}
	if len(state.heapFromRunes) > 0 {
		fileName := literals.CName(state.fileLiteral)
		for _, union := range state.heapFromRunes {
			success, field := streamMemberRef(tags, union, compilerTypes.StringType)
			invalid, err := textErrorArm(union, "InvalidInput", textMessageInvalidScalar, fileName, "line", "column", literals, tags)
			if err != nil {
				return err
			}
			fmt.Fprintf(result,
				"\n// hex_string_from_runes_%s encodes a scalar sequence into text. It\n"+
					"// rejects surrogates and values above U+10FFFF and reports, never traps.\n"+
					"static inline %s hex_string_from_runes_%s(hex_heap h, hex_slice_Rune runes, size_t line, size_t column) {\n"+
					"    for (size_t index = 0; index < runes.length; index++) {\n"+
					"        if (!hex_rune_valid(runes.data[index])) {\n"+
					"            return %s;\n"+
					"        }\n"+
					"    }\n"+
					"    return (%s){ .tag = %s, .payload.%s = hex_string_from_runes(h, runes.data, runes.length) };\n"+
					"}\n",
				streamAdapterSuffix(union), union.CName, streamAdapterSuffix(union), invalid, union.CName, success, field)
		}
	}
	if state.runeCategory {
		// hex_rune_categories maps a utf8proc general-category ordinal to the
		// program-wide UnicodeCategory tag, in declaration order.
		result.WriteString("\n// hex_rune_categories maps the runtime's general-category ordinal to the\n" +
			"// program-wide UnicodeCategory tag, in declaration order.\n" +
			fmt.Sprintf("static const hex_tag hex_rune_categories[%d] = {\n", len(compilerTypes.UnicodeCategoryVariantNames)))
		for index := range compilerTypes.UnicodeCategoryVariantNames {
			fmt.Fprintf(result, "    %s,\n", unicodeCategoryTag(tags, index))
		}
		result.WriteString("};\n")
	}
	if len(state.casefold) > 0 {
		fileName := literals.CName(state.fileLiteral)
		for _, union := range state.casefold {
			success, field := streamMemberRef(tags, union, compilerTypes.StringType)
			failed, err := textErrorArm(union, "InvalidInput", textMessageTransformFailed, fileName, "line", "column", literals, tags)
			if err != nil {
				return err
			}
			fmt.Fprintf(result,
				"\n// hex_string_casefold_%s folds text and reports a failed transform as\n"+
					"// an Error. The transform buffer never escapes the runtime core.\n"+
					"static inline %s hex_string_casefold_%s(hex_heap h, hex_text text, size_t line, size_t column) {\n"+
					"    const hex_string *folded = hex_text_casefold(h, text);\n"+
					"    if (folded == nullptr) {\n"+
					"        return %s;\n"+
					"    }\n"+
					"    return (%s){ .tag = %s, .payload.%s = folded };\n"+
					"}\n",
				streamAdapterSuffix(union), union.CName, streamAdapterSuffix(union), failed, union.CName, success, field)
		}
	}
	if len(state.normalize) > 0 {
		result.WriteString("\n// hex_normalize_form maps a NormalizationForm tag to the runtime's fixed\n" +
			"// form index (0 NFC, 1 NFD, 2 NFKC, 3 NFKD); an unknown tag reports -1.\n" +
			"static inline int32_t hex_normalize_form(hex_tag tag) {\n")
		for index := range compilerTypes.NormalizationFormVariantNames {
			fmt.Fprintf(result, "    if (tag == %s) {\n        return %d;\n    }\n", normalizationFormTag(tags, index), index)
		}
		result.WriteString("    return -1;\n}\n")
		fileName := literals.CName(state.fileLiteral)
		for _, union := range state.normalize {
			success, field := streamMemberRef(tags, union, compilerTypes.StringType)
			failed, err := textErrorArm(union, "InvalidInput", textMessageTransformFailed, fileName, "line", "column", literals, tags)
			if err != nil {
				return err
			}
			fmt.Fprintf(result,
				"\n// hex_string_normalize_%s normalizes text and reports a failed transform\n"+
					"// as an Error. The transform buffer never escapes the runtime core.\n"+
					"static inline %s hex_string_normalize_%s(hex_heap h, hex_text text, hex_tag form, size_t line, size_t column) {\n"+
					"    const hex_string *normalized = hex_text_normalize(h, text, hex_normalize_form(form));\n"+
					"    if (normalized == nullptr) {\n"+
					"        return %s;\n"+
					"    }\n"+
					"    return (%s){ .tag = %s, .payload.%s = normalized };\n"+
					"}\n",
				streamAdapterSuffix(union), union.CName, streamAdapterSuffix(union), failed, union.CName, success, field)
		}
	}
	if !state.used {
		return nil
	}
	fileName := literals.CName(state.fileLiteral)
	for _, union := range state.heapFromBytes {
		success, field := streamMemberRef(tags, union, compilerTypes.StringType)
		invalid, err := textErrorArm(union, "InvalidInput", textMessageInvalidUTF8, fileName, "line", "column", literals, tags)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_string_from_bytes_%s(hex_heap h, hex_slice_UInt8 bytes, size_t line, size_t column) {\n"+
				"    if (hex_utf8_valid(bytes.data, bytes.length)) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = hex_string_make(h, hex_text_view(bytes)) };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union), union.CName, success, field, invalid)
	}
	for _, union := range state.heapConcat {
		success, field := streamMemberRef(tags, union, compilerTypes.StringType)
		invalid, err := textErrorArm(union, "InvalidInput", textMessageInvalidUTF8, fileName, "line", "column", literals, tags)
		if err != nil {
			return err
		}
		// The receiver is already well-formed text, so only what is appended
		// needs validating.
		fmt.Fprintf(result,
			"\nstatic inline %s hex_string_concat_%s(hex_heap h, hex_text left, hex_slice_UInt8 right, size_t line, size_t column) {\n"+
				"    if (hex_utf8_valid(right.data, right.length)) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = hex_string_join(h, left, hex_text_view(right)) };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union), union.CName, success, field, invalid)
	}
	inlineArms := func(union compilerTypes.Type) (destination compilerTypes.Type, tag, field, invalid, overflow string, err error) {
		members := compilerTypes.UnionMembers(union)
		for index := 0; index < members.Len(); index++ {
			if member, _ := members.At(index); compilerTypes.IsInlineString(member) {
				destination = member
			}
		}
		if !compilerTypes.IsInlineString(destination) {
			return destination, "", "", "", "", unknownExpressionDiagnostic("inline text result union has no String<N> member")
		}
		tag, field = streamMemberRef(tags, union, destination)
		if invalid, err = textErrorArm(union, "InvalidInput", textMessageInvalidUTF8, fileName, "line", "column", literals, tags); err != nil {
			return
		}
		overflow, err = textErrorArm(union, "ResourceExhausted", textMessageOverCapacity, fileName, "line", "column", literals, tags)
		return
	}
	for _, union := range state.inlineFromBytes {
		destination, tag, field, invalid, overflow, err := inlineArms(union)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_text_from_bytes_%s(hex_slice_UInt8 bytes, size_t line, size_t column) {\n"+
				"    if (bytes.length > %d) {\n"+
				"        return %s;\n"+
				"    }\n"+
				"    if (!hex_utf8_valid(bytes.data, bytes.length)) {\n"+
				"        return %s;\n"+
				"    }\n"+
				"    return (%s){ .tag = %s, .payload.%s = %s };\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union), destination.InlineString.Capacity, overflow, invalid,
			union.CName, tag, field, textFill(destination, "hex_text_view(bytes)", ""))
	}
	for _, union := range state.inlineConcat {
		destination, tag, field, invalid, overflow, err := inlineArms(union)
		if err != nil {
			return err
		}
		// Validation covers the joined result, not each operand: a multi-byte
		// sequence split across the two is well-formed once joined.
		fmt.Fprintf(result,
			"\nstatic inline %s hex_text_concat_%s(hex_slice_UInt8 left, hex_slice_UInt8 right, size_t line, size_t column) {\n"+
				"    size_t total;\n"+
				"    if (ckd_add(&total, left.length, right.length) || total > %d) {\n"+
				"        return %s;\n"+
				"    }\n"+
				"    %s value = { .byte_length = total };\n"+
				"    if (left.length != 0) {\n"+
				"        memcpy(value.data, left.data, left.length);\n"+
				"    }\n"+
				"    if (right.length != 0) {\n"+
				"        memcpy(value.data + left.length, right.data, right.length);\n"+
				"    }\n"+
				"    if (!hex_utf8_valid(value.data, total)) {\n"+
				"        return %s;\n"+
				"    }\n"+
				"    return (%s){ .tag = %s, .payload.%s = value };\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union), destination.InlineString.Capacity, overflow, destination.CName, invalid,
			union.CName, tag, field)
	}
	return nil
}
