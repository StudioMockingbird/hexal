// expression_validation.go owns the expression dispatcher:
// validateExpressionNode's kind switch and the shared child-validation
// helpers it routes through.
package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func validateExpressionNode(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if expected != nil && !supportedGeneratedTypeWithState(*expected, state) {
		return unknownExpressionDiagnostic()
	}
	if err := validateViewProvenance(node); err != nil {
		return err
	}
	switch node.Kind {
	case checker.NilExpression:
		if !compilerTypes.IsNil(node.ResultType) || expected != nil && !compilerTypes.IsNil(*expected) {
			return unknownExpressionDiagnostic()
		}
		return nil
	case checker.EosExpression:
		if !compilerTypes.IsEoS(node.ResultType) || expected != nil && !compilerTypes.IsEoS(*expected) {
			return unknownExpressionDiagnostic()
		}
		return nil
	case checker.VariableExpression:
		return validateVariableExpression(node, expected, state)
	case checker.FunctionReferenceExpression:
		return validateFunctionReference(node, expected, state)
	case checker.ForeignFunctionReferenceExpression:
		return validateForeignFunctionReferenceExpression(node, expected, state)
	case checker.ForeignConstantExpression:
		return validateForeignConstantExpression(node, expected, state)
	case checker.ForeignGlobalExpression:
		return validateForeignGlobalExpression(node, expected, state)
	case checker.FunctionLiteralExpression:
		return validateFunctionLiteralExpression(node, expected, state)
	case checker.CallExpression:
		return validateCallExpression(node, expected, state)
	case checker.MethodCallExpression:
		return validateMethodCallExpression(node, expected, state)
	case checker.AddressOfExpression:
		return validateAddressExpression(node, expected, state)
	case checker.DereferenceExpression:
		return validateDereferenceExpression(node, expected, state)
	case checker.MemberExpression:
		return validateMemberExpression(node, expected, state)
	case checker.ObjectExpression:
		return validateObjectExpression(node, expected, state)
	case checker.ConstantExpression:
		return validateConstantExpression(node, expected, state)
	case checker.UnaryOperationExpression:
		return validateUnaryOperationExpression(node, expected, state)
	case checker.BinaryOperationExpression:
		return validateBinaryOperationExpression(node, expected, state)
	case checker.NullTestExpression:
		return validateNullTestExpression(node, expected, state)
	case checker.UnionInjectionExpression:
		return validateUnionInjection(node, expected, state)
	case checker.StreamConstructorExpression:
		return validateStreamConstructor(node, expected, state)
	case checker.BytesOverExpression:
		return validateBytesOverExpression(node, expected, state)
	case checker.StreamMethodCallExpression:
		return validateStreamMethodCall(node, expected, state)
	case checker.TimeExpression:
		return validateTimeExpression(node, expected, state)
	case checker.NetworkExpression:
		return validateNetworkExpression(node, expected, state)
	case checker.CorelibCallExpression:
		return validateCorelibCallExpression(node, expected, state)
	case checker.UnionWidenExpression:
		return validateUnionWiden(node, expected, state)
	case checker.UnionTestExpression:
		return validateUnionTest(node, expected, state)
	case checker.UnionPayloadExpression:
		return validateUnionPayload(node, expected, state)
	case checker.UnionEqualityExpression:
		return validateUnionEquality(node, expected, state)
	case checker.HeapAllocateExpression:
		return validateHeapAllocateExpression(node, expected, state)
	case checker.HeapAllocateAlignedExpression:
		return validateHeapAllocateAlignedExpression(node, expected, state)
	case checker.HeapFreeExpression:
		return validateHeapFreeExpression(node, expected, state)
	case checker.AdtConstructExpression:
		return validateAdtConstructExpression(node, expected, state)
	case checker.AdtPayloadExpression:
		return validateAdtPayloadExpression(node, expected, state)
	case checker.MatchExpression:
		return validateMatchExpression(node, expected, state)
	case checker.ArrayLiteralExpression, checker.IndexExpression, checker.CollectionMethodCallExpression, checker.CollectionSliceExpression:
		return validateCollectionExpression(node, expected, state)
	case checker.StringLiteralExpression, checker.StringMethodCallExpression, checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.StringInterpolateExpression,
		checker.InlineStringConstructExpression, checker.TextCoerceExpression:
		return validateTextExpression(node, expected, state)
	case checker.ListNewExpression, checker.DictNewExpression:
		return validateCollectionConstructor(node, expected, state)
	case checker.WideningExpression:
		return validateWideningExpression(node, expected, state)
	case checker.DeepEqualityExpression:
		return validateDeepEqualityExpression(node, expected, state)
	case checker.ConversionExpression:
		return validateConversionExpression(node, expected, state)
	case checker.BitCastExpression:
		return validateBitCastExpression(node, expected, state)
	case checker.RuneMethodCallExpression:
		return validateRuneMethod(node, expected, state)
	case checker.CursorMethodCallExpression:
		return validateCursorMethod(node, expected, state)
	case checker.GraphemeMethodCallExpression:
		return validateGraphemeMethod(node, expected, state)
	case checker.ErrorHeaderExpression:
		return validateErrorHeaderExpression(node, expected, state)
	case checker.ErrorKindHeaderExpression:
		return validateErrorKindHeaderExpression(node, expected, state)
	case checker.ModuleValueExpression:
		return validateModuleValueExpression(node, expected, state)
	case checker.EndianConversionExpression:
		return validateEndianConversionExpression(node, expected, state)
	case checker.TryExpression:
		return validateTryExpression(node, expected, state)
	case checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression, checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression, checker.MutexConstructorExpression, checker.MutexMethodCallExpression, checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression:
		return validateConcurrencyExpression(node, expected, state)
	case checker.StashConstructorExpression, checker.StashMethodCallExpression:
		return validateStashExpression(node, expected, state)
	case checker.PoolConstructorExpression, checker.PoolMethodCallExpression:
		return validatePoolExpression(node, expected, state)
	case checker.LayoutExpression:
		return validateLayoutExpression(node, expected, state)
	case checker.VolatileReadExpression:
		return validateVolatileReadExpression(node, expected, state)
	case checker.VolatileWriteExpression:
		return validateVolatileWriteExpression(node, expected, state)
	case checker.PointerOffsetExpression:
		return validatePointerOffset(node, expected, state)
	case checker.PointerIndexExpression:
		return validatePointerIndex(node, expected, state)
	case checker.PointerCastExpression:
		return validatePointerCast(node, expected, state)
	case checker.SliceBridgeExpression:
		return validateSliceBridgeExpression(node, expected, state)
	case checker.PrintExpression:
		return validatePrintExpression(node, expected, state)
	case checker.StringCompareExpression:
		return validateStringCompareExpression(node, expected, state)
	default:
		return unknownExpressionDiagnostic()
	}
}

func validateExpressionChildWithState(child *checker.Expression, expected compilerTypes.Type, state *expressionValidation) error {
	if child == nil {
		return unknownExpressionDiagnostic()
	}
	if state.expressions[child] {
		return unknownExpressionDiagnostic()
	}
	state.expressions[child] = true
	defer delete(state.expressions, child)
	return validateExpressionNode(*child, optionalType(expected, expected != compilerTypes.Type{}), state)
}

// validateTruthinessChild validates a logical operand through its
// truthiness. The nil literal is checker-supported but its other generator
// paths fail closed; truthiness contexts accept it as the constant false.
func validateTruthinessChild(child *checker.Expression, state *expressionValidation) error {
	if child.Kind == checker.NilExpression {
		return nil
	}
	return validateExpressionChildWithState(child, compilerTypes.Type{}, state)
}

func isPointerType(typ compilerTypes.Type) bool {
	return typ.Element != nil && typ.Object == nil && typ.ScalarKind == compilerTypes.ScalarNone && typ.Bits == 0
}
