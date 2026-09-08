package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// supportedInterpolationValueType reports whether typ may appear as a
// checked String.interpolate value segment: the same closed set the checker
// enforces (Bool, Rune, every fixed-width integer, Size, Byte, Float32,
// Float64, String, Strand). The generator re-verifies its own checked input
// independently of the checker.
func supportedInterpolationValueType(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		return true
	case compilerTypes.IsRune(typ):
		return true
	case compilerTypes.IsSignedInteger(typ), compilerTypes.IsUnsignedInteger(typ):
		return true
	case compilerTypes.Equal(typ, compilerTypes.Float32), compilerTypes.Equal(typ, compilerTypes.Float64):
		return true
	case compilerTypes.IsString(typ), compilerTypes.IsStrand(typ):
		return true
	}
	return false
}

// hoistInterpolateInStatement walks one checked statement's expressions and
// emits each String.interpolate prologue -- the typed Heap and value
// captures, exact-length measurement, one allocation, and ordered writes --
// before the statement renders. Each interpolate node is then replaced by
// its hoisted result pointer.
func hoistInterpolateInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, indent string) error {
	return walkStatementExpressions(statement, func(node checker.Expression) error {
		if node.Kind == checker.StringInterpolateExpression && node.Operand != nil {
			return hoistStringInterpolate(node, body, state, indent)
		}
		return nil
	})
}

// interpolationSegmentPlan is one hoisted segment's contribution to the
// final copy: a rendered C byte-source expression and its rendered C byte-
// length and rune-length expressions.
type interpolationSegmentPlan struct {
	data    string
	byteLen string
	runeLen string
}

// hoistStringInterpolate emits one String.interpolate call's complete
// lowering: the Heap evaluates first and exactly once, then each segment's
// literal text (read from its interned literal object) or embedded
// expression (captured once into a typed temporary) contributes a byte
// source and a byte/rune length; the lengths are checked-summed, the result
// is allocated once, and every segment is copied in source order.
func hoistStringInterpolate(node checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if node.Operand == nil || len(node.InterpolationSegments) == 0 {
		return unknownExpressionDiagnostic("String.interpolate has invalid checked metadata")
	}
	state.interpolationCounter++
	ordinal := state.interpolationCounter

	heap, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return err
	}
	heapTemp := fmt.Sprintf("hex_interp_heap_%d", ordinal)
	fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(compilerTypes.Heap, heapTemp, false), heap)
	// hex_heap_allocate takes no heap argument (Heap is a capability token,
	// not a distinct allocator identity; hex_string_from_bytes ignores its
	// own Heap parameter identically), so the captured temporary is unused
	// beyond proving the Heap expression was evaluated exactly once.
	fmt.Fprintf(body, "%s(void)%s;\n", indent, heapTemp)

	plans := make([]interpolationSegmentPlan, 0, len(node.InterpolationSegments))
	valueOrdinal := 0
	for _, segment := range node.InterpolationSegments {
		if !segment.IsValue {
			if state.strings == nil {
				return unknownExpressionDiagnostic("string literal rendering requires a literal registry")
			}
			handle, ok := state.strings.Lookup(segment.Text)
			if !ok {
				return unknownExpressionDiagnostic("interpolation literal segment is missing from the checked literal registry")
			}
			name := state.strings.CName(handle)
			plans = append(plans, interpolationSegmentPlan{
				data:    name + ".data",
				byteLen: name + ".byte_length",
				runeLen: name + ".rune_length",
			})
			continue
		}
		valueOrdinal++
		plan, planErr := hoistInterpolationValueSegment(segment.Value, ordinal, valueOrdinal, body, state, indent)
		if planErr != nil {
			return planErr
		}
		plans = append(plans, plan)
	}

	totalTemp := fmt.Sprintf("hex_interp_total_%d", ordinal)
	runesTemp := fmt.Sprintf("hex_interp_runes_%d", ordinal)
	fmt.Fprintf(body, "%ssize_t %s = 0;\n", indent, totalTemp)
	fmt.Fprintf(body, "%ssize_t %s = 0;\n", indent, runesTemp)
	for _, plan := range plans {
		fmt.Fprintf(body, "%sif (ckd_add(&%s, %s, %s)) {\n", indent, totalTemp, totalTemp, plan.byteLen)
		fmt.Fprintf(body, "%s    hex_runtime_trap(\"[Runtime Error] string allocation size overflow\\n\");\n", indent)
		fmt.Fprintf(body, "%s}\n", indent)
		fmt.Fprintf(body, "%s%s += %s;\n", indent, runesTemp, plan.runeLen)
	}
	sizeTemp := fmt.Sprintf("hex_interp_size_%d", ordinal)
	fmt.Fprintf(body, "%ssize_t %s;\n", indent, sizeTemp)
	fmt.Fprintf(body, "%sif (ckd_add(&%s, sizeof(hex_string_storage), %s) || ckd_add(&%s, %s, 1)) {\n", indent, sizeTemp, totalTemp, sizeTemp, sizeTemp)
	fmt.Fprintf(body, "%s    hex_runtime_trap(\"[Runtime Error] string allocation size overflow\\n\");\n", indent)
	fmt.Fprintf(body, "%s}\n", indent)
	storageTemp := fmt.Sprintf("hex_interp_storage_%d", ordinal)
	fmt.Fprintf(body, "%shex_string_storage *%s = hex_heap_allocate(%s);\n", indent, storageTemp, sizeTemp)
	offsetTemp := fmt.Sprintf("hex_interp_offset_%d", ordinal)
	fmt.Fprintf(body, "%ssize_t %s = 0;\n", indent, offsetTemp)
	for _, plan := range plans {
		fmt.Fprintf(body, "%sif (%s != 0) {\n", indent, plan.byteLen)
		fmt.Fprintf(body, "%s    memcpy(%s->bytes + %s, %s, %s);\n", indent, storageTemp, offsetTemp, plan.data, plan.byteLen)
		fmt.Fprintf(body, "%s}\n", indent)
		fmt.Fprintf(body, "%s%s += %s;\n", indent, offsetTemp, plan.byteLen)
	}
	fmt.Fprintf(body, "%s%s->bytes[%s] = 0;\n", indent, storageTemp, totalTemp)
	fmt.Fprintf(body, "%s%s->header = (hex_string){ .data = %s->bytes, .byte_length = %s, .rune_length = %s, .storage_kind = HEX_STRING_OWNED };\n",
		indent, storageTemp, storageTemp, totalTemp, runesTemp)
	resultTemp := fmt.Sprintf("hex_interp_result_%d", ordinal)
	fmt.Fprintf(body, "%sconst hex_string *const %s = &%s->header;\n", indent, resultTemp, storageTemp)

	if state.hoistedInterpolations == nil {
		state.hoistedInterpolations = make(map[*checker.Expression]string)
	}
	state.hoistedInterpolations[node.Operand] = resultTemp
	return nil
}

// hoistInterpolationValueSegment captures one embedded expression's value
// into a typed temporary exactly once, then emits whatever byte formatting
// that type requires, returning the plan the final copy pass consumes.
func hoistInterpolationValueSegment(value checker.Operand, ordinal, valueOrdinal int, body *strings.Builder, state *expressionValidation, indent string) (interpolationSegmentPlan, error) {
	rendered, err := renderOperandWithState(value, state)
	if err != nil {
		return interpolationSegmentPlan{}, err
	}
	valueTemp := fmt.Sprintf("hex_interp_val_%d_%d", ordinal, valueOrdinal)
	fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(value.Type, valueTemp, false), rendered)

	typ := value.Type
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		dataTemp := fmt.Sprintf("hex_interp_data_%d_%d", ordinal, valueOrdinal)
		lenTemp := fmt.Sprintf("hex_interp_len_%d_%d", ordinal, valueOrdinal)
		fmt.Fprintf(body, "%sconst uint8_t *%s = %s ? (const uint8_t *)\"true\" : (const uint8_t *)\"false\";\n", indent, dataTemp, valueTemp)
		fmt.Fprintf(body, "%ssize_t %s = %s ? 4 : 5;\n", indent, lenTemp, valueTemp)
		return interpolationSegmentPlan{data: dataTemp, byteLen: lenTemp, runeLen: lenTemp}, nil
	case compilerTypes.IsRune(typ):
		bufTemp := fmt.Sprintf("hex_interp_buf_%d_%d", ordinal, valueOrdinal)
		lenTemp := fmt.Sprintf("hex_interp_len_%d_%d", ordinal, valueOrdinal)
		fmt.Fprintf(body, "%suint8_t %s[4];\n", indent, bufTemp)
		fmt.Fprintf(body, "%ssize_t %s = hex_utf8_encode(%s, %s);\n", indent, lenTemp, bufTemp, valueTemp)
		return interpolationSegmentPlan{data: bufTemp, byteLen: lenTemp, runeLen: "1"}, nil
	case compilerTypes.IsString(typ):
		return interpolationSegmentPlan{
			data:    valueTemp + "->data",
			byteLen: valueTemp + "->byte_length",
			runeLen: valueTemp + "->rune_length",
		}, nil
	case compilerTypes.IsStrand(typ):
		lenTemp := fmt.Sprintf("hex_interp_len_%d_%d", ordinal, valueOrdinal)
		fmt.Fprintf(body, "%ssize_t %s = hex_strand_byte_length(%s);\n", indent, lenTemp, valueTemp)
		return interpolationSegmentPlan{
			data:    valueTemp + ".data",
			byteLen: lenTemp,
			runeLen: fmt.Sprintf("hex_strand_rune_length(%s)", valueTemp),
		}, nil
	}
	formatFunction, bufferSize, ok := interpolationScalarFormatter(typ)
	if !ok {
		return interpolationSegmentPlan{}, unknownExpressionDiagnostic("string interpolation does not support " + typ.Name)
	}
	bufTemp := fmt.Sprintf("hex_interp_buf_%d_%d", ordinal, valueOrdinal)
	lenTemp := fmt.Sprintf("hex_interp_len_%d_%d", ordinal, valueOrdinal)
	fmt.Fprintf(body, "%schar %s[%d];\n", indent, bufTemp, bufferSize)
	fmt.Fprintf(body, "%ssize_t %s = %s(%s, %s);\n", indent, lenTemp, formatFunction, bufTemp, valueTemp)
	return interpolationSegmentPlan{data: bufTemp, byteLen: lenTemp, runeLen: lenTemp}, nil
}

// interpolationScalarFormatter selects the String component's format helper
// and stack-buffer size for one integer or float type. Size is checked
// before the generic unsigned-integer widths since it is a distinct
// canonical type that may share a width with a fixed-width alias.
func interpolationScalarFormatter(typ compilerTypes.Type) (string, int, bool) {
	if compilerTypes.IsSize(typ) {
		return "hex_string_format_size", 32, true
	}
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		return "hex_string_format_float32", 64, true
	}
	if compilerTypes.Equal(typ, compilerTypes.Float64) {
		return "hex_string_format_float64", 64, true
	}
	if compilerTypes.IsSignedInteger(typ) {
		switch typ.Bits {
		case 8:
			return "hex_string_format_int8", 32, true
		case 16:
			return "hex_string_format_int16", 32, true
		case 32:
			return "hex_string_format_int32", 32, true
		case 64:
			return "hex_string_format_int64", 32, true
		}
	}
	if compilerTypes.IsUnsignedInteger(typ) {
		switch typ.Bits {
		case 8:
			return "hex_string_format_uint8", 32, true
		case 16:
			return "hex_string_format_uint16", 32, true
		case 32:
			return "hex_string_format_uint32", 32, true
		case 64:
			return "hex_string_format_uint64", 32, true
		}
	}
	return "", 0, false
}

// renderStringInterpolate renders a hoisted String.interpolate call as its
// result pointer. The prologue was emitted before the enclosing statement;
// an unhoisted interpolate expression is an internal compiler failure.
func renderStringInterpolate(node checker.Expression, state *expressionValidation) (string, error) {
	if state.hoistedInterpolations == nil {
		return "", unknownExpressionDiagnostic("String.interpolate expression reached generation without hoisting")
	}
	name, ok := state.hoistedInterpolations[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic("String.interpolate expression reached generation without hoisting")
	}
	return name, nil
}
