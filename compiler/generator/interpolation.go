package generator

import (
	"fmt"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// supportedInterpolationValueType reports whether typ may appear as a
// checked interpolate value segment: the same closed set the checker enforces
// (Bool, every fixed-width integer, Size, Byte, Float32, Float64, and text of
// any form). The generator re-verifies its own checked input independently of
// the checker.
func supportedInterpolationValueType(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		return true
	case compilerTypes.IsSignedInteger(typ), compilerTypes.IsUnsignedInteger(typ):
		return true
	case compilerTypes.Equal(typ, compilerTypes.Float32), compilerTypes.Equal(typ, compilerTypes.Float64):
		return true
	case compilerTypes.IsText(typ):
		return true
	}
	return false
}

// hoistInterpolateInStatement walks one checked statement's expressions and
// emits each interpolate prologue -- the typed Heap and value captures,
// exact-length measurement, and the ordered writes -- before the statement
// renders. Each interpolate node is then replaced by its hoisted result.
func hoistInterpolateInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, indent string) error {
	return walkStatementExpressions(statement, func(node checker.Expression) error {
		if node.Kind == checker.StringInterpolateExpression && node.Operand != nil {
			return hoistStringInterpolate(node, body, state, indent)
		}
		if node.Kind == checker.InlineStringConstructExpression && node.Name == "interpolate" {
			return hoistInlineInterpolate(node, body, state, indent)
		}
		return nil
	})
}

// interpolationSegmentPlan is one hoisted segment's contribution to the
// final copy: a rendered C byte-source expression and its rendered C byte-
// length expression.
type interpolationSegmentPlan struct {
	data    string
	byteLen string
}

// hoistInterpolationSegments captures every segment in written order: literal
// text reads from its interned object, and each embedded expression is
// captured once into a typed temporary. The plans are what the copy pass
// consumes; nothing has been sized or written yet.
func hoistInterpolationSegments(node checker.Expression, ordinal int, body *strings.Builder, state *expressionValidation, indent string) ([]interpolationSegmentPlan, error) {
	plans := make([]interpolationSegmentPlan, 0, len(node.InterpolationSegments))
	valueOrdinal := 0
	for _, segment := range node.InterpolationSegments {
		if !segment.IsValue {
			if state.strings == nil {
				return nil, unknownExpressionDiagnostic("string literal rendering requires a literal registry")
			}
			handle, ok := state.strings.Lookup(segment.Text)
			if !ok {
				return nil, unknownExpressionDiagnostic("interpolation literal segment is missing from the checked literal registry")
			}
			name := state.strings.CName(handle)
			plans = append(plans, interpolationSegmentPlan{data: name + ".data", byteLen: name + ".byte_length"})
			continue
		}
		valueOrdinal++
		plan, err := hoistInterpolationValueSegment(segment.Value, ordinal, valueOrdinal, body, state, indent)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// interpCkdModel carries one checked-add guard's decided counter name and the
// length operand added into it.
type interpCkdModel struct {
	Indent string
	Name   string
	Extra  string
}

// interpCapacityModel carries one inline-capacity guard: the overflow flag,
// running total, and destination capacity.
type interpCapacityModel struct {
	Indent   string
	Over     string
	Total    string
	Capacity string
}

// interpResultModel carries one fitted union result store: the decided
// temporary, result type, variant tag, payload field, and value temporary.
type interpResultModel struct {
	Indent string
	Name   string
	Type   string
	Tag    string
	Field  string
	Value  string
}

// interpSizeModel carries interpolation's size-carrying declarations and
// storage stores; each template reads the fields it spells.
type interpSizeModel struct {
	Indent  string
	Name    string
	Size    string
	Total   string
	Storage string
}

// interpByteModel carries one segment memcpy: the destination base, running
// offset, byte source, and byte length.
type interpByteModel struct {
	Indent string
	Dest   string
	Offset string
	Data   string
	Len    string
}

// interpFormatModel carries one scalar format call's decided helper, buffer,
// and value temporaries.
type interpFormatModel struct {
	Indent   string
	Name     string
	Function string
	Buf      string
	Value    string
}

// hoistInlineInterpolate emits one String<N>.interpolate call's complete
// lowering. Every embedded expression evaluates exactly once, in order, before
// capacity is checked, so an overflow never skips or repeats a side effect. The
// total is then checked against N and the result union is built: the value on
// a fit, and ResourceExhausted with a fixed message when it does not. Nothing
// is truncated and no Heap is involved.
func hoistInlineInterpolate(node checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if len(node.InterpolationSegments) == 0 || !compilerTypes.IsInlineString(node.OperandType) || node.ResultType.Union == nil {
		return unknownExpressionDiagnostic("String<N>.interpolate has invalid checked metadata")
	}
	state.interpolationCounter++
	ordinal := state.interpolationCounter
	plans, err := hoistInterpolationSegments(node, ordinal, body, state, indent)
	if err != nil {
		return err
	}
	destination := node.OperandType
	totalTemp := fmt.Sprintf("hex_interp_total_%d", ordinal)
	overTemp := fmt.Sprintf("hex_interp_over_%d", ordinal)
	if err := renderInto(body, "module.c", "ordinal_decl", forStmtLineModel{Indent: indent, Name: totalTemp, Value: "0"}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "bool_decl", forStmtLineModel{Indent: indent, Name: overTemp}); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := renderInto(body, "module.c", "ckd_add_open", interpCkdModel{Indent: indent, Name: totalTemp, Extra: plan.byteLen}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "assign_true", forStmtLineModel{Indent: indent, Name: overTemp}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	}
	fileHandle, ok := state.strings.Lookup(state.filename)
	if !ok {
		return unknownExpressionDiagnostic("inline interpolation is missing its module file literal")
	}
	overflow, err := textErrorArm(node.ResultType, "ResourceExhausted", textMessageOverCapacity, state.strings.CName(fileHandle),
		strconv.Itoa(state.line(node.Span)), strconv.Itoa(state.column(node.Span)), state.strings, state.tags)
	if err != nil {
		return err
	}
	tag, field := streamMemberRef(state.tags, node.ResultType, destination)
	resultTemp := fmt.Sprintf("hex_interp_result_%d", ordinal)
	valueTemp := fmt.Sprintf("hex_interp_value_%d", ordinal)
	offsetTemp := fmt.Sprintf("hex_interp_offset_%d", ordinal)
	if err := renderInto(body, "module.c", "var_decl", forStmtLineModel{Indent: indent, Type: node.ResultType.CName, Name: resultTemp}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "capacity_guard_open", interpCapacityModel{
		Indent:   indent,
		Over:     overTemp,
		Total:    totalTemp,
		Capacity: fmt.Sprintf("%d", destination.InlineString.Capacity),
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "for_assign", forStmtLineModel{Indent: indent, Name: resultTemp, Value: overflow}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "else_open", indentModel{Indent: indent}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "value_init", forStmtLineModel{Indent: indent, Type: destination.CName, Name: valueTemp, Value: totalTemp}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "ordinal_decl", forStmtLineModel{Indent: indent + "    ", Name: offsetTemp, Value: "0"}); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := renderInto(body, "module.c", "nonzero_guard_open", forStmtLineModel{Indent: indent + "    ", Value: plan.byteLen}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "memcpy_inline", interpByteModel{Indent: indent, Dest: valueTemp, Offset: offsetTemp, Data: plan.data, Len: plan.byteLen}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "inner_close", indentModel{Indent: indent}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "offset_add", forStmtLineModel{Indent: indent + "    ", Name: offsetTemp, Value: plan.byteLen}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "result_assign", interpResultModel{
		Indent: indent,
		Name:   resultTemp,
		Type:   node.ResultType.CName,
		Tag:    tag,
		Field:  field,
		Value:  valueTemp,
	}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	if state.hoistedSequencing == nil {
		state.hoistedSequencing = make(map[*checker.Expression]string)
	}
	if state.hoistedInlineInterpolations == nil {
		state.hoistedInlineInterpolations = make(map[*checker.Expression]string)
	}
	state.hoistedInlineInterpolations[&node.InterpolationSegments[0].Value.Node] = resultTemp
	return nil
}

// hoistStringInterpolate emits one String.interpolate call's complete
// lowering: the Heap evaluates first and exactly once, then each segment's
// literal text (read from its interned literal object) or embedded
// expression (captured once into a typed temporary) contributes a byte
// source and a byte length; the lengths are checked-summed, the result
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
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declaration(compilerTypes.Heap, heapTemp, false), Value: heap}); err != nil {
		return err
	}
	// hex_heap_allocate takes no heap argument (Heap is a capability token,
	// not a distinct allocator identity; hex_string_make ignores its
	// own Heap parameter identically), so the captured temporary is unused
	// beyond proving the Heap expression was evaluated exactly once.
	if err := renderInto(body, "module.c", "void_use", forStmtLineModel{Indent: indent, Name: heapTemp}); err != nil {
		return err
	}

	plans, err := hoistInterpolationSegments(node, ordinal, body, state, indent)
	if err != nil {
		return err
	}

	totalTemp := fmt.Sprintf("hex_interp_total_%d", ordinal)
	if err := renderInto(body, "module.c", "ordinal_decl", forStmtLineModel{Indent: indent, Name: totalTemp, Value: "0"}); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := renderInto(body, "module.c", "ckd_add_open", interpCkdModel{Indent: indent, Name: totalTemp, Extra: plan.byteLen}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "overflow_trap", indentModel{Indent: indent}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	}
	sizeTemp := fmt.Sprintf("hex_interp_size_%d", ordinal)
	if err := renderInto(body, "module.c", "var_decl", forStmtLineModel{Indent: indent, Type: "size_t", Name: sizeTemp}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "storage_size_open", interpCkdModel{Indent: indent, Name: sizeTemp, Extra: totalTemp}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "overflow_trap", indentModel{Indent: indent}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	storageTemp := fmt.Sprintf("hex_interp_storage_%d", ordinal)
	if err := renderInto(body, "module.c", "storage_alloc", interpSizeModel{Indent: indent, Name: storageTemp, Size: sizeTemp}); err != nil {
		return err
	}
	offsetTemp := fmt.Sprintf("hex_interp_offset_%d", ordinal)
	if err := renderInto(body, "module.c", "ordinal_decl", forStmtLineModel{Indent: indent, Name: offsetTemp, Value: "0"}); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := renderInto(body, "module.c", "nonzero_guard_open", forStmtLineModel{Indent: indent, Value: plan.byteLen}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "memcpy_owned", interpByteModel{Indent: indent, Dest: storageTemp, Offset: offsetTemp, Data: plan.data, Len: plan.byteLen}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "offset_add", forStmtLineModel{Indent: indent, Name: offsetTemp, Value: plan.byteLen}); err != nil {
			return err
		}
	}
	if err := renderInto(body, "module.c", "nul_terminate", interpSizeModel{Indent: indent, Name: storageTemp, Total: totalTemp}); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "header_init", interpSizeModel{Indent: indent, Name: storageTemp, Total: totalTemp}); err != nil {
		return err
	}
	resultTemp := fmt.Sprintf("hex_interp_result_%d", ordinal)
	if err := renderInto(body, "module.c", "result_header", interpSizeModel{Indent: indent, Name: resultTemp, Storage: storageTemp}); err != nil {
		return err
	}

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
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declaration(value.Type, valueTemp, false), Value: rendered}); err != nil {
		return interpolationSegmentPlan{}, err
	}

	typ := value.Type
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		dataTemp := fmt.Sprintf("hex_interp_data_%d_%d", ordinal, valueOrdinal)
		lenTemp := fmt.Sprintf("hex_interp_len_%d_%d", ordinal, valueOrdinal)
		if err := renderInto(body, "module.c", "bool_text", forStmtLineModel{Indent: indent, Name: dataTemp, Value: valueTemp}); err != nil {
			return interpolationSegmentPlan{}, err
		}
		if err := renderInto(body, "module.c", "bool_len", forStmtLineModel{Indent: indent, Name: lenTemp, Value: valueTemp}); err != nil {
			return interpolationSegmentPlan{}, err
		}
		return interpolationSegmentPlan{data: dataTemp, byteLen: lenTemp}, nil
	case compilerTypes.IsString(typ):
		return interpolationSegmentPlan{data: valueTemp + "->data", byteLen: valueTemp + "->byte_length"}, nil
	case compilerTypes.IsInlineString(typ):
		// The value was captured into its own temporary, so its bytes and
		// length are read in place, never past the length.
		return interpolationSegmentPlan{data: valueTemp + ".data", byteLen: valueTemp + ".byte_length"}, nil
	}
	formatFunction, bufferSize, ok := interpolationScalarFormatter(typ)
	if !ok {
		return interpolationSegmentPlan{}, unknownExpressionDiagnostic("string interpolation does not support " + typ.Name)
	}
	bufTemp := fmt.Sprintf("hex_interp_buf_%d_%d", ordinal, valueOrdinal)
	lenTemp := fmt.Sprintf("hex_interp_len_%d_%d", ordinal, valueOrdinal)
	if err := renderInto(body, "module.c", "buf_decl", interpSizeModel{Indent: indent, Name: bufTemp, Size: strconv.Itoa(bufferSize)}); err != nil {
		return interpolationSegmentPlan{}, err
	}
	if err := renderInto(body, "module.c", "format_len", interpFormatModel{Indent: indent, Name: lenTemp, Function: formatFunction, Buf: bufTemp, Value: valueTemp}); err != nil {
		return interpolationSegmentPlan{}, err
	}
	return interpolationSegmentPlan{data: bufTemp, byteLen: lenTemp}, nil
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
	if node.Kind == checker.InlineStringConstructExpression {
		if len(node.InterpolationSegments) == 0 {
			return "", unknownExpressionDiagnostic("String<N>.interpolate has no checked segments")
		}
		name, ok := state.hoistedInlineInterpolations[&node.InterpolationSegments[0].Value.Node]
		if !ok {
			return "", unknownExpressionDiagnostic("String<N>.interpolate expression reached generation without hoisting")
		}
		return name, nil
	}
	if state.hoistedInterpolations == nil {
		return "", unknownExpressionDiagnostic("String.interpolate expression reached generation without hoisting")
	}
	name, ok := state.hoistedInterpolations[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic("String.interpolate expression reached generation without hoisting")
	}
	return name, nil
}
