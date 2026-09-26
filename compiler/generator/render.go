// render.go owns expression type inference and operand rendering: the
// expressionResultType walk and the literal, object, and line-directive
// rendering every expression dispatch bottoms out in.
package generator

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func expressionResultType(node checker.Expression) (compilerTypes.Type, bool) {
	switch node.Kind {
	case checker.NilExpression:
		return node.ResultType, true
	case checker.VariableExpression, checker.AddressOfExpression, checker.DereferenceExpression:
		if node.ResultType != (compilerTypes.Type{}) {
			return node.ResultType, true
		}
	case checker.ConstantExpression, checker.UnaryOperationExpression, checker.BinaryOperationExpression,
		checker.FunctionReferenceExpression, checker.NullTestExpression, checker.UnionInjectionExpression,
		checker.UnionWidenExpression, checker.UnionTestExpression, checker.UnionPayloadExpression,
		checker.UnionEqualityExpression, checker.HeapAllocateExpression, checker.HeapAllocateAlignedExpression,
		checker.AdtConstructExpression, checker.AdtPayloadExpression, checker.MatchExpression,
		checker.ArrayLiteralExpression, checker.IndexExpression, checker.CollectionMethodCallExpression,
		checker.CollectionSliceExpression, checker.StringLiteralExpression, checker.StringMethodCallExpression,
		checker.StringFromBytesExpression, checker.StringInterpolateExpression, checker.InlineStringConstructExpression, checker.TextCoerceExpression,
		checker.ListNewExpression, checker.DictNewExpression,
		checker.DeepEqualityExpression, checker.StringCompareExpression, checker.WideningExpression, checker.ConversionExpression,
		checker.RuneMethodCallExpression,
		checker.CursorMethodCallExpression,
		checker.GraphemeMethodCallExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.StashConstructorExpression, checker.StashMethodCallExpression,
		checker.PoolConstructorExpression, checker.PoolMethodCallExpression,
		checker.LayoutExpression, checker.VolatileReadExpression, checker.VolatileWriteExpression, checker.SliceBridgeExpression,
		checker.TimeExpression, checker.ErrorHeaderExpression, checker.ErrorKindHeaderExpression, checker.ModuleValueExpression,
		checker.NetworkExpression:
		return node.ResultType, true
	case checker.HeapFreeExpression:
		return compilerTypes.Type{}, false
	case checker.CallExpression, checker.MethodCallExpression:
		// A call that produces no value has no type to report.
		return node.ResultType, node.ResultType != (compilerTypes.Type{})
	case checker.MemberExpression:
		// A member read stamps its arena-resolved checked type, which
		// outranks the declared member type: a builtin List field's declared
		// identity predates the compilation arena and differs from the live
		// one even though both describe the same generated C type.
		if node.ResultType != (compilerTypes.Type{}) {
			return node.ResultType, true
		}
		if node.Member != nil {
			return node.Member.Type, true
		}
	case checker.ObjectExpression:
		if node.Object != nil {
			return node.Object.Type, true
		}
	}
	return compilerTypes.Type{}, false
}

func expressionTypeWithState(node checker.Expression, state *expressionValidation) (compilerTypes.Type, bool) {
	return expressionTypeWithStateSeen(node, state, make(map[*checker.Expression]bool))
}

func expressionTypeWithStateSeen(node checker.Expression, state *expressionValidation, active map[*checker.Expression]bool) (compilerTypes.Type, bool) {
	if typ, ok := expressionResultType(node); ok {
		return typ, true
	}
	switch node.Kind {
	case checker.VariableExpression:
		if state != nil {
			binding, ok := state.bindingFor(node)
			return binding.typ, ok
		}
	case checker.AddressOfExpression:
		// The checker stamps every address-of node with its interned
		// canonical pointer result; recovery reads that metadata and never
		// reconstructs a fresh type for comparison.
		if node.ResultType != (compilerTypes.Type{}) {
			return node.ResultType, true
		}
		return compilerTypes.Type{}, false
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return compilerTypes.Type{}, false
		}
		if active[node.Operand] {
			return compilerTypes.Type{}, false
		}
		active[node.Operand] = true
		receiverType, ok := expressionTypeWithStateSeen(*node.Operand, state, active)
		delete(active, node.Operand)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			receiverType, ok = node.OperandType, true
		}
		if ok && isPointerType(receiverType) {
			return *receiverType.Element, true
		}
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil {
			return compilerTypes.Type{}, false
		}
		if active[node.Operand] {
			return compilerTypes.Type{}, false
		}
		active[node.Operand] = true
		receiverType, ok := expressionTypeWithStateSeen(*node.Operand, state, active)
		delete(active, node.Operand)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			receiverType, ok = node.OperandType, true
		}
		if ok && receiverType.Object != nil {
			return node.Member.Type, true
		}
	}
	return compilerTypes.Type{}, false
}

func writeLineDirective(body *strings.Builder, line int, filename string) error {
	if line <= 0 {
		return nil
	}
	return renderInto(body, "module.c", "line_directive", lineDirectiveModel{Line: fmt.Sprintf("%d", line), File: filename})
}

// renderOperand renders one operand under a fresh validation state bound to
// the required literal registry, mirroring renderExpression.
func renderOperand(source checker.Operand, registry *literalRegistry) (string, error) {
	state := newExpressionValidation()
	state.strings = registry
	// A whole-operand render with no checked program seeds a source-name table
	// so a variable resolves to its bare generated name.
	seedRenderOnlyVariables(&source.Node, source.Type, state)
	return renderOperandWithState(source, state)
}

func renderOperandWithState(source checker.Operand, state *expressionValidation) (string, error) {
	if err := validateCheckedOperandWithState(source, state); err != nil {
		return "", err
	}
	switch source.Kind {
	case checker.ObjectOperand:
		if source.Object == nil || !compilerTypes.Equal(source.Type, source.Object.Type) {
			return "", unknownExpressionDiagnostic()
		}
		return objectLiteralWithState(source.Object, state)
	case checker.VariableOperand, checker.ExpressionOperand:
		if expressionType, ok := expressionResultType(source.Node); ok && !compilerTypes.Equal(source.Type, expressionType) && !compilerTypes.WidensTo(expressionType, source.Type) {
			return "", unknownExpressionDiagnostic()
		}
		return renderExpressionExpectedWithState(source.Node, &source.Type, state)
	case checker.ConstantOperand:
		// An object constant (Error.new result wrapped by union injection)
		// renders its object value.
		if source.Object != nil {
			return objectLiteralWithState(source.Object, state)
		}
		// Nil is the singleton type: its one value lowers to the C23 nullptr
		// predefined constant and carries no go/constant.
		if compilerTypes.IsNil(source.Type) {
			return "nullptr", nil
		}
		// EoS is a singleton: its one value is the tag-only marker and
		// carries no go/constant.
		if compilerTypes.IsEoS(source.Type) {
			return "((hex_eos){ 0 })", nil
		}
		// Heap is a value token: one default allocator, no runtime state to
		// select, and no allocation performed by Heap.new() itself.
		if compilerTypes.IsHeap(source.Type) {
			return "((hex_heap)0)", nil
		}
		if source.Constant == nil {
			return "", unknownExpressionDiagnostic()
		}
		switch source.Type.ScalarKind {
		case compilerTypes.ScalarBool:
			if !compilerTypes.Equal(source.Type, compilerTypes.Bool) || source.Constant.Kind() != constant.Bool {
				return "", unknownExpressionDiagnostic()
			}
		case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
			if _, ok := unsignedCName(source.Type); !ok || source.Constant.Kind() != constant.Int {
				return "", unknownExpressionDiagnostic()
			}
		case compilerTypes.ScalarFloat:
			return renderFloatLiteral(source)
		default:
			return "", unknownExpressionDiagnostic()
		}
	default:
		return "", unknownExpressionDiagnostic()
	}
	switch source.Type.ScalarKind {
	case compilerTypes.ScalarBool:
		return strconv.FormatBool(constant.BoolVal(source.Constant)), nil
	case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
		return integerLiteral(source)
	case compilerTypes.ScalarFloat:
		return renderFloatLiteral(source)
	default:
		return "", generatorDiagnostic()
	}
}

func objectLiteralWithState(value *checker.ObjectValue, state *expressionValidation) (string, error) {
	if err := validateObjectValue(value, state); err != nil {
		return "", err
	}
	// byMemberIndex keeps the initializer's position in value.Initializers
	// (written order), not the Operand itself, so the render loop below can
	// still reach hoistObjectSequence's key (&value.Initializers[i].Source.Node)
	// for a hoisted written-order temporary.
	byMemberIndex := make(map[*compilerTypes.ObjectMember]int, len(value.Initializers))
	for index, initializer := range value.Initializers {
		if initializer.Member == nil {
			return "", unknownExpressionDiagnostic()
		}
		byMemberIndex[initializer.Member] = index
	}
	model := objectLiteralModel{Type: value.Type.CName}
	for index := range value.Type.Object.Members {
		member := &value.Type.Object.Members[index]
		sourceIndex, ok := byMemberIndex[member]
		if !ok {
			return "", unknownExpressionDiagnostic()
		}
		rendered, err := renderHoistedOperand(&value.Initializers[sourceIndex].Source.Node, value.Initializers[sourceIndex].Source, state)
		if err != nil {
			return "", err
		}
		model.Fields = append(model.Fields, objectFieldModel{Name: privateCName(memberName, member.Name, ""), Value: rendered})
	}
	var result strings.Builder
	if err := renderInto(&result, "module.c", "object_literal", model); err != nil {
		return "", err
	}
	return result.String(), nil
}

func renderFloatLiteral(source checker.Operand) (string, error) {
	if err := validateFloatConstant(source); err != nil {
		return "", err
	}
	bitSize := 64
	bits := source.FloatBits
	if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
		bitSize = 32
		bits = uint64(uint32(bits))
	}
	_, special := floatSignAndSpecial(bits, bitSize)
	if special {
		fraction := bits & ((uint64(1) << 52) - 1)
		if bitSize == 32 {
			fraction = bits & ((uint64(1) << 23) - 1)
		}
		literal := "INFINITY"
		if fraction != 0 {
			literal = "NAN"
		}
		if source.Negative {
			literal = "-" + literal
		}
		return literal, nil
	}
	if bitSize == 32 {
		return formatDecimalFloat(bits, 32) + "f", nil
	}
	return formatDecimalFloat(bits, 64), nil
}

func integerLiteral(source checker.Operand) (string, error) {
	if err := validateIntegerConstant(source); err != nil {
		return "", err
	}
	value := source.Constant
	negative := source.Negative
	if constant.Sign(value) < 0 {
		value = constant.UnaryOp(gotoken.SUB, value, 0)
	}
	unsigned, ok := constant.Uint64Val(value)
	if !ok {
		return "", unknownExpressionDiagnostic()
	}
	if compilerTypes.IsSignedInteger(source.Type) {
		limit := uint64(1) << (source.Type.Bits - 1)
		if unsigned > limit || (!negative && unsigned == limit) {
			return "", unknownExpressionDiagnostic()
		}
	} else {
		limit := ^uint64(0)
		if source.Type.Bits < 64 {
			limit = uint64(1)<<source.Type.Bits - 1
		}
		if unsigned > limit {
			return "", unknownExpressionDiagnostic()
		}
	}
	if negative && unsigned == uint64(1)<<(source.Type.Bits-1) && compilerTypes.IsSignedInteger(source.Type) {
		minimum, minimumErr := signedMinimumMacro(source.Type)
		if minimumErr != nil {
			return "", minimumErr
		}
		return minimum, nil
	}
	digits := formatInteger(unsigned, source.Radix)
	if compilerTypes.Equal(source.Type, compilerTypes.Int64) {
		if negative {
			return "-INT64_C(" + digits + ")", nil
		}
		return "INT64_C(" + digits + ")", nil
	}
	if compilerTypes.Equal(source.Type, compilerTypes.UInt64) {
		return "UINT64_C(" + digits + ")", nil
	}
	if negative {
		return "-" + digits, nil
	}
	return digits, nil
}

func signedMinimumMacro(typ compilerTypes.Type) (string, error) {
	switch typ.Name {
	case compilerTypes.Int8.Name:
		return "INT8_MIN", nil
	case compilerTypes.Int16.Name:
		return "INT16_MIN", nil
	case compilerTypes.Int32.Name:
		return "INT32_MIN", nil
	case compilerTypes.Int64.Name:
		return "INT64_MIN", nil
	default:
		return "", unknownExpressionDiagnostic()
	}
}

func formatInteger(value uint64, radix checker.LiteralRadix) string {
	switch radix {
	case checker.HexadecimalRadix:
		return "0x" + strings.ToUpper(strconv.FormatUint(value, 16))
	case checker.BinaryRadix:
		return "0b" + strconv.FormatUint(value, 2)
	default:
		return strconv.FormatUint(value, 10)
	}
}

// formatDecimalFloat renders an already rounded IEEE value as the shortest
// readable decimal C literal that round-trips to the same bits.
// Formatting starts from the checked rounded bits, never from the original
// source spelling; the standard formatter produces the shortest decimal that
// reparses to those exact bits. An integral-looking mantissa receives a
// fractional point so the token stays a C floating constant.
func formatDecimalFloat(bits uint64, bitSize int) string {
	var value float64
	if bitSize == 32 {
		value = float64(math.Float32frombits(uint32(bits)))
	} else {
		value = math.Float64frombits(bits)
	}
	text := strconv.FormatFloat(value, 'g', -1, bitSize)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return text
}

// optionalType packages a (value, present) pair as the optional-expected-type
// pointer the validation and render entry points take. It exists only for the
// two callers that still receive the pair from elsewhere; nil means absent.
func optionalType(typ compilerTypes.Type, present bool) *compilerTypes.Type {
	if !present {
		return nil
	}
	return &typ
}
