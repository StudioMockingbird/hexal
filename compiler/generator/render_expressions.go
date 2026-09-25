// render_expressions.go owns expression dispatch: the renderExpression
// family and the node and receiver dispatch shared by every operation path.
package generator

import (
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// renderExpression renders one node under a fresh validation state bound to
// the required literal registry. Production rendering shares the program-wide
// registry through renderExpressionWithState; the parameter keeps every
// direct call site honest about where String payloads resolve.
func renderExpression(node checker.Expression, registry *literalRegistry) (string, error) {
	state := newExpressionValidation()
	state.strings = registry
	// A whole-expression render with no checked program seeds a source-name
	// table so a variable operand resolves to its bare generated name; a real
	// program walk populates the same table through allocateBinding.
	seedRenderOnlyVariables(&node, compilerTypes.Type{}, state)
	return renderExpressionWithState(node, state)
}

// seedRenderOnlyVariables records every bare variable name in the checked
// subtree under the ordinary generated name, so a helper that renders one
// expression in isolation resolves a variable operand the way a program walk
// would. A test-built subtree puts the operand type on the operation, not on
// the variable node, so the enclosing operand type is threaded down.
func seedRenderOnlyVariables(node *checker.Expression, operandType compilerTypes.Type, state *expressionValidation) {
	if node == nil {
		return
	}
	if node.Kind == checker.VariableExpression && node.Binding == 0 && node.Name != "" {
		typ := node.ResultType
		if typ == (compilerTypes.Type{}) {
			typ = operandType
		}
		state.variables[node.Name] = generatedBinding{typ: typ}
		return
	}
	childType := node.OperandType
	seedRenderOnlyVariables(node.Operand, childType, state)
	seedRenderOnlyVariables(node.Left, childType, state)
	seedRenderOnlyVariables(node.Right, childType, state)
	for index := range node.Arguments {
		argument := node.Arguments[index].Node
		seedRenderOnlyVariables(&argument, argument.ResultType, state)
	}
}

func renderExpressionWithState(node checker.Expression, state *expressionValidation) (string, error) {
	return renderExpressionExpectedWithState(node, nil, state)
}

func renderExpressionExpectedWithState(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) (string, error) {
	if err := validateExpressionNode(node, expected, state); err != nil {
		return "", err
	}
	return renderExpressionUncheckedWithState(node, state)
}

func renderExpressionUncheckedWithState(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.NilExpression:
		return "nullptr", nil
	case checker.EosExpression:
		return "((hex_eos){ 0 })", nil
	case checker.VariableExpression:
		if node.Name == "" {
			return "", unknownExpressionDiagnostic("variable without a source name")
		}
		name, ok := state.cNameFor(node)
		if !ok {
			return "", unknownExpressionDiagnostic("variable binding is not active")
		}
		return name, nil
	case checker.FunctionReferenceExpression:
		if node.LocalHelperOrdinal != 0 {
			return localHelperCName(node.LocalHelperOrdinal), nil
		}
		if node.Name == "" {
			return "", unknownExpressionDiagnostic("function reference without a source name")
		}
		return privateCName(functionNameKind, node.Name, moduleOwner(node.Module, state.owner)), nil
	case checker.ModuleValueExpression:
		if node.Name == "" {
			return "", unknownExpressionDiagnostic("module value without a source name")
		}
		return moduleValueCName(node.Name, moduleOwner(node.Module, state.owner)), nil
	case checker.ForeignFunctionReferenceExpression, checker.ForeignConstantExpression, checker.ForeignGlobalExpression:
		// A foreign reference lowers to the exact recorded C symbol. No
		// forwarding wrapper is generated to rename it, so the name is
		// emitted verbatim.
		if node.ForeignCName == "" {
			return "", unknownExpressionDiagnostic("foreign reference without a C spelling")
		}
		return node.ForeignCName, nil
	case checker.FunctionLiteralExpression:
		if node.LocalHelperOrdinal == 0 {
			return "", unknownExpressionDiagnostic("function literal without an assigned helper ordinal")
		}
		return localHelperCName(node.LocalHelperOrdinal), nil
	case checker.CallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("call without a checked callee")
		}
		callee, atomic, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		if !atomic {
			callee = "(" + callee + ")"
		}
		arguments := make([]string, len(node.Arguments))
		for index, argument := range node.Arguments {
			rendered, argumentErr := renderHoistedOperand(&node.Arguments[index].Node, argument, state)
			if argumentErr != nil {
				return "", argumentErr
			}
			// A foreign signature records the exact C spelling of each
			// position; when it differs from the checked representation the
			// call adds one direct boundary cast. No general pointer
			// conversion is introduced.
			if node.Operand.Kind == checker.ForeignFunctionReferenceExpression &&
				index < len(node.Operand.ForeignParameters) && node.Operand.ForeignParameters[index] != "" &&
				typeSpelling(argument.Type) != node.Operand.ForeignParameters[index] {
				rendered = "(" + node.Operand.ForeignParameters[index] + ")(" + rendered + ")"
			}
			arguments[index] = rendered
		}
		arguments = renderRestSliceArgument(&node, arguments)
		if node.Operand.Kind == checker.FunctionReferenceExpression && node.Operand.Name != "" && state.envFunctions[node.Operand.Name] {
			if state.envPointer == "" {
				return "", unknownExpressionDiagnostic("call to an environment-dependent function without an environment")
			}
			arguments = append([]string{state.envPointer}, arguments...)
		}
		call := callee + "(" + strings.Join(arguments, ", ") + ")"
		if node.Operand.Kind == checker.ForeignFunctionReferenceExpression && node.Operand.ForeignResult != "" &&
			node.ResultType != (compilerTypes.Type{}) && typeSpelling(node.ResultType) != node.Operand.ForeignResult {
			// The recorded spelling is the header's result type; the value is
			// converted back to the checked representation with one direct
			// cast, so a char-pointer result assigns to a Byte pointer.
			call = "(" + typeSpelling(node.ResultType) + ")(" + call + ")"
		}
		return call, nil
	case checker.MethodCallExpression:
		if node.Owner == nil || node.Operand == nil {
			return "", unknownExpressionDiagnostic("method call without a checked receiver")
		}
		var receiver string
		if name, ok := hoistedSequenceValue(state, node.Operand); ok {
			receiver = name
		} else {
			receiverType, receiverErr := methodReceiverType(*node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			rendered, _, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, &receiverType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			receiver = rendered
		}
		arguments := make([]string, len(node.Arguments))
		for index, argument := range node.Arguments {
			rendered, argumentErr := renderHoistedOperand(&node.Arguments[index].Node, argument, state)
			if argumentErr != nil {
				return "", argumentErr
			}
			arguments[index] = rendered
		}
		arguments = renderRestSliceArgument(&node, arguments)
		allArguments := append([]string{receiver}, arguments...)
		if state.envMethods[node.Name] {
			if state.envPointer == "" {
				return "", unknownExpressionDiagnostic("call to an environment-dependent method without an environment")
			}
			allArguments = append([]string{state.envPointer}, allArguments...)
		}
		return methodCName(node.Owner, node.Name, moduleOwner(node.Owner.ModuleID, state.owner)) + "(" + strings.Join(allArguments, ", ") + ")", nil
	case checker.AddressOfExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("address-of without an operand")
		}
		operandType, hasOperandType := node.OperandType, node.OperandType != (compilerTypes.Type{})
		if !hasOperandType && node.ResultType.Element != nil {
			operandType, hasOperandType = *node.ResultType.Element, true
		}
		if !hasOperandType {
			operandType, hasOperandType = expressionTypeWithState(*node.Operand, state)
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, optionalType(operandType, hasOperandType), state)
		if err != nil {
			return "", err
		}
		if atomic {
			return "&" + operand, nil
		}
		return "&(" + operand + ")", nil
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("dereference without an operand")
		}
		operandType, ok := expressionTypeWithState(*node.Operand, state)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			operandType, ok = node.OperandType, true
		}
		if !ok {
			return "", unknownExpressionDiagnostic("dereference receiver type is unavailable")
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &operandType, state)
		if err != nil {
			return "", err
		}
		if atomic {
			return "*" + operand, nil
		}
		return "*(" + operand + ")", nil
	case checker.IndexExpression, checker.ArrayLiteralExpression, checker.CollectionMethodCallExpression, checker.CollectionSliceExpression:
		return renderCollectionExpression(node, state)
	case checker.StringLiteralExpression, checker.StringMethodCallExpression, checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.StringInterpolateExpression,
		checker.InlineStringConstructExpression, checker.TextCoerceExpression:
		return renderTextExpression(node, state)
	case checker.ListNewExpression, checker.DictNewExpression:
		return renderCollectionConstructor(node, state)
	case checker.WideningExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("widening without an operand")
		}
		operand, atomic, operandErr := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
		if operandErr != nil {
			return "", operandErr
		}
		if !atomic {
			operand = "(" + operand + ")"
		}
		return "(" + node.ResultType.CName + ")(" + operand + ")", nil
	case checker.DeepEqualityExpression:
		if node.Left == nil || node.Right == nil {
			return "", unknownExpressionDiagnostic("deep equality without both operands")
		}
		if compilerTypes.IsText(node.OperandType) {
			return renderTextEquality(node, state)
		}
		left, _, leftErr := renderHoistedExpressionNode(node.Left, &node.OperandType, state)
		if leftErr != nil {
			return "", leftErr
		}
		right, _, rightErr := renderHoistedExpressionNode(node.Right, &node.OperandType, state)
		if rightErr != nil {
			return "", rightErr
		}
		if !compilerTypes.IsList(node.OperandType) {
			left = "&(" + left + ")"
			right = "&(" + right + ")"
		}
		result := equalityHelperName(node.OperandType) + "(" + left + ", " + right + ")"
		if node.Operator == checker.NotEqualOperator {
			result = "(!" + result + ")"
		}
		return result, nil
	case checker.StringCompareExpression:
		return renderTextComparison(node, state)
	case checker.ConversionExpression:
		return renderConversion(node, state)
	case checker.RuneMethodCallExpression:
		return renderRuneMethod(node, state)
	case checker.CursorMethodCallExpression:
		return renderCursorMethod(node, state)
	case checker.GraphemeMethodCallExpression:
		return renderGraphemeMethod(node, state)
	case checker.BitCastExpression:
		return renderBitCast(node, state)
	case checker.EndianConversionExpression:
		return renderEndianConversion(node, state)
	case checker.TryExpression:
		return renderTryExpression(node, state)
	case checker.SpawnExpression:
		return renderSpawnExpression(node, state)
	case checker.TaskYieldExpression:
		return "hex_task_yield()", nil
	case checker.TaskMethodCallExpression:
		return renderTaskMethod(node, state)
	case checker.ChannelConstructorExpression:
		return renderChannelConstructor(node, state)
	case checker.ChannelMethodCallExpression:
		return renderChannelMethod(node, state)
	case checker.MutexConstructorExpression:
		return renderMutexConstructor(node, state)
	case checker.MutexMethodCallExpression:
		return renderMutexMethod(node, state)
	case checker.AtomicConstructorExpression:
		return renderAtomicConstructor(node, state)
	case checker.AtomicMethodCallExpression:
		return renderAtomicMethod(node, state)
	case checker.StashConstructorExpression:
		return renderStashConstructor(node, state)
	case checker.StashMethodCallExpression:
		return renderStashMethod(node, state)
	case checker.PoolConstructorExpression:
		return renderPoolConstructor(node, state)
	case checker.PoolMethodCallExpression:
		return renderPoolMethod(node, state)
	case checker.StreamConstructorExpression:
		return renderStreamConstructor(node, state)
	case checker.BytesOverExpression:
		return renderBytesOver(node, state)
	case checker.StreamMethodCallExpression:
		return renderStreamMethod(node, state)
	case checker.TimeExpression:
		return renderTimeExpression(node, state)
	case checker.NetworkExpression:
		return renderNetworkExpression(node, state)
	case checker.CorelibCallExpression:
		return renderCorelibCallExpression(node, state)
	case checker.LayoutExpression:
		// The C23 compiler is the final authority for the selected target
		// layout; the checker already proved T complete.
		if node.Name == "align_of" {
			return "(size_t)alignof(" + typeSpelling(node.OperandType) + ")", nil
		}
		return "(size_t)sizeof(" + typeSpelling(node.OperandType) + ")", nil
	case checker.VolatileReadExpression:
		if node.Operand == nil || node.OperandType.Element == nil {
			return "", unknownExpressionDiagnostic("volatile read without a checked pointer")
		}
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		qualifier := "volatile "
		if !node.OperandType.PointeeWritable {
			qualifier = "volatile const "
		}
		return "*(" + qualifier + typeSpelling(node.Element) + " *)(" + receiver + ")", nil
	case checker.VolatileWriteExpression:
		if node.Operand == nil || node.OperandType.Element == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("volatile write without checked operands")
		}
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		value, valueErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		return "*(volatile " + typeSpelling(node.Element) + " *)(" + receiver + ") = " + value, nil
	case checker.PointerOffsetExpression:
		return renderPointerOffset(node, state)
	case checker.PointerIndexExpression:
		return renderPointerIndex(node, state)
	case checker.PointerCastExpression:
		return renderPointerCast(node, state)
	case checker.SliceBridgeExpression:
		return renderSliceBridgeExpression(node, state)
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil {
			return "", unknownExpressionDiagnostic("member selection without a receiver or member")
		}
		receiverType, ok := expressionTypeWithState(*node.Operand, state)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			receiverType, ok = node.OperandType, true
		}
		if !ok {
			return "", unknownExpressionDiagnostic("member receiver type is unavailable")
		}
		receiver, err := renderReceiver(node.Operand, receiverType, state)
		if err != nil {
			return "", err
		}
		// A foreign record member keeps its original C field spelling; every
		// other member renders through the ordinary owner-qualified name.
		if node.Member.CName != "" {
			return receiver + "." + node.Member.CName, nil
		}
		return receiver + "." + privateCName(memberName, node.Member.Name, ""), nil
	case checker.NullTestExpression:
		// The nullable union shares its base pointer's null niche, so the
		// test lowers to the ordinary C null pointer comparison.
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("null test without a checked operand")
		}
		operator := "=="
		if node.Operator == checker.NotEqualOperator {
			operator = "!="
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		if !atomic {
			operand = "(" + operand + ")"
		}
		if compilerTypes.IsNullable(node.OperandType) {
			return operand + " " + operator + " nullptr", nil
		}
		representation, index, ok := remapUnionMember(node.Operand, node.OperandType, unionMemberIndex(node.OperandType, compilerTypes.Nil), state)
		if !ok {
			representation, index = node.OperandType, unionMemberIndex(node.OperandType, compilerTypes.Nil)
		}
		nilRepresentationMember, _ := compilerTypes.UnionMembers(representation).At(index)
		return operand + ".tag " + operator + " " + state.tags.unionMemberTag(nilRepresentationMember), nil
	case checker.UnionInjectionExpression:
		return renderUnionInjection(node, state)
	case checker.UnionWidenExpression:
		return renderUnionWiden(node, state)
	case checker.UnionTestExpression:
		return renderUnionTest(node, state)
	case checker.UnionPayloadExpression:
		return renderUnionPayload(node, state)
	case checker.UnionEqualityExpression:
		return renderUnionEquality(node, state)
	case checker.HeapAllocateExpression:
		return renderHeapAllocate(node, state)
	case checker.HeapAllocateAlignedExpression:
		return renderHeapAllocateAligned(node, state)
	case checker.HeapFreeExpression:
		return renderHeapFree(node, state)
	case checker.AdtConstructExpression:
		return renderAdtConstruct(node, state)
	case checker.AdtPayloadExpression:
		return renderAdtPayload(node, state)
	case checker.ErrorHeaderExpression:
		return renderErrorHeader(node, state)
	case checker.ErrorKindHeaderExpression:
		return renderErrorKindHeader(node, state)
	case checker.MatchExpression:
		return "", unknownExpressionDiagnostic("match expressions lower at statement level")
	case checker.PrintExpression:
		return "", unknownExpressionDiagnostic("print expressions lower at statement level")
	case checker.ObjectExpression:
		return objectLiteralWithState(node.Object, state)
	case checker.ConstantExpression, checker.UnaryOperationExpression, checker.BinaryOperationExpression:
		return renderOperationWithState(node, state)
	default:
		return "", unknownExpressionDiagnostic("unsupported checked expression")
	}
}

func renderExpressionNodeWithExpectedState(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) (string, bool, error) {
	value, err := renderExpressionExpectedWithState(node, expected, state)
	if err != nil {
		return "", false, err
	}
	return value, node.Kind == checker.VariableExpression || node.Kind == checker.ObjectExpression ||
		node.Kind == checker.MemberExpression || node.Kind == checker.FunctionReferenceExpression ||
		node.Kind == checker.ForeignFunctionReferenceExpression || node.Kind == checker.ForeignConstantExpression || node.Kind == checker.ForeignGlobalExpression ||
		node.Kind == checker.CallExpression || node.Kind == checker.MethodCallExpression || node.Kind == checker.NilExpression ||
		node.Kind == checker.IndexExpression || node.Kind == checker.CollectionMethodCallExpression || node.Kind == checker.CollectionSliceExpression ||
		node.Kind == checker.StringLiteralExpression || node.Kind == checker.StringMethodCallExpression || node.Kind == checker.StringFromBytesExpression || node.Kind == checker.InlineStringConstructExpression || node.Kind == checker.TextCoerceExpression || node.Kind == checker.ListNewExpression || node.Kind == checker.DictNewExpression ||
		node.Kind == checker.DeepEqualityExpression || node.Kind == checker.StringCompareExpression || node.Kind == checker.WideningExpression || node.Kind == checker.ConversionExpression ||
		node.Kind == checker.RuneMethodCallExpression || node.Kind == checker.CursorMethodCallExpression ||
		node.Kind == checker.GraphemeMethodCallExpression, nil
}

// renderReceiver renders one method receiver with its checked expected type
// and parenthesizes it unless it is already one C atom. The receiver-render-
// and-parenthesize block lives only here; the nil guard keeps every call
// site safe even where earlier whole-expression validation already proved
// the operand present.
func renderReceiver(operand *checker.Expression, expected compilerTypes.Type, state *expressionValidation) (string, error) {
	if operand == nil {
		return "", unknownExpressionDiagnostic("receiver expression is missing")
	}
	receiver, atomic, err := renderExpressionNodeWithExpectedState(*operand, &expected, state)
	if err != nil {
		return "", err
	}
	if !atomic {
		receiver = "(" + receiver + ")"
	}
	return receiver, nil
}
