// expression_validation.go owns the expression dispatcher:
// validateExpressionNode's kind switch and the shared child-validation
// helpers it routes through.
package generator

import (
	"fmt"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func validateExpressionNode(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if expected != nil && !supportedGeneratedTypeWithState(*expected, state) {
		return unknownExpressionDiagnostic("expression has an unsupported expected type")
	}
	if err := validateViewProvenance(node); err != nil {
		return err
	}
	switch node.Kind {
	case checker.NilExpression:
		if !compilerTypes.IsNil(node.ResultType) || expected != nil && !compilerTypes.IsNil(*expected) {
			return unknownExpressionDiagnostic("nil expression has invalid checked metadata")
		}
		return nil
	case checker.EosExpression:
		if !compilerTypes.IsEoS(node.ResultType) || expected != nil && !compilerTypes.IsEoS(*expected) {
			return unknownExpressionDiagnostic("eos expression has invalid checked metadata")
		}
		return nil
	case checker.VariableExpression:
		if !validSourceName(node.Name) {
			return unknownExpressionDiagnostic("variable without a source name")
		}
		if state != nil && (state.variables != nil || state.bindings != nil) {
			binding, ok := state.bindingFor(node)
			if !ok {
				return unknownExpressionDiagnostic("variable is not present in checked bindings")
			}
			if expected != nil && !compilerTypes.Equal(binding.typ, *expected) {
				// A null test narrows a local binding's reads to its non-Nil
				// base (or to Nil) inside the branch where the test holds;
				// the binding itself still holds the declared nullable type,
				// so a narrowed read is a stricter type.
				if !compilerTypes.Assignable(binding.typ, *expected) {
					return unknownExpressionDiagnostic("variable type does not match its checked type")
				}
			}
			for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
				if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
					return unknownExpressionDiagnostic("variable metadata does not match its checked binding")
				}
			}
		}
		return validateExpressionMetadata(node, expected, state)
	case checker.FunctionReferenceExpression:
		return validateFunctionReference(node, expected, state)
	case checker.ForeignFunctionReferenceExpression:
		if node.ForeignCName == "" || node.ResultType == (compilerTypes.Type{}) || node.ResultType.Signature == nil {
			return unknownExpressionDiagnostic("foreign function reference without a checked signature")
		}
		if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
			return unknownExpressionDiagnostic("foreign function reference type does not match its expected type")
		}
		return nil
	case checker.ForeignConstantExpression:
		if node.ForeignCName == "" {
			return unknownExpressionDiagnostic("foreign constant without a C spelling")
		}
		if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
			return unknownExpressionDiagnostic("foreign constant type does not match its expected type")
		}
		return nil
	case checker.ForeignGlobalExpression:
		if node.ForeignCName == "" {
			return unknownExpressionDiagnostic("foreign global without a C spelling")
		}
		if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
			return unknownExpressionDiagnostic("foreign global type does not match its expected type")
		}
		return nil
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
		if node.Object == nil {
			return unknownExpressionDiagnostic("object expression without a checked object value")
		}
		if err := validateObjectValue(node.Object, state); err != nil {
			return err
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.Object.Type) {
			return unknownExpressionDiagnostic("object expression type does not match its expected type")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Object.Type) {
			return unknownExpressionDiagnostic("object expression result type does not match its checked object")
		}
		return validateExpressionMetadata(node, expected, state)
	case checker.ConstantExpression:
		if node.Constant == nil || node.Constant.Kind != checker.ConstantOperand && node.Constant.Kind != checker.ObjectOperand ||
			!compilerTypes.Equal(node.ResultType, node.Constant.Type) ||
			!supportedGeneratedScalarType(node.ResultType) && node.Constant.Type.Object == nil && node.Constant.Type.Union == nil {
			detail := ""
			if node.Constant != nil {
				detail = fmt.Sprintf(" result=%s const=%s kind=%d literal=%q object=%v union=%v equal=%v", node.ResultType.Name, node.Constant.Type.Name, node.Constant.Kind, node.Constant.Literal, node.Constant.Type.Object != nil, node.Constant.Type.Union != nil, compilerTypes.Equal(node.ResultType, node.Constant.Type))
			}
			return unknownExpressionDiagnostic("constant expression without a checked constant" + detail)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("constant expression type does not match its expected type")
		}
		return validateConstantOperand(*node.Constant)
	case checker.UnaryOperationExpression:
		if node.Operand == nil {
			return unknownExpressionDiagnostic("unary operation with invalid checked metadata")
		}
		if node.Operator == checker.LogicalNotOperator {
			// not accepts any value-producing operand; the operand is
			// validated through its truthiness.
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
				return unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
			}
			if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("unary operation result type does not match its expected type")
			}
			return validateTruthinessChild(node.Operand, state)
		}
		if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
			return unknownExpressionDiagnostic("unary operation with invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("unary operation result type does not match its expected type")
		}
		if err := validateUnaryMetadata(node); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.BinaryOperationExpression:
		if node.Left == nil || node.Right == nil {
			return unknownExpressionDiagnostic("binary operation with invalid checked metadata")
		}
		if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
			// and/or accept any value-producing operands, mixed types
			// included; each side is validated through its truthiness.
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
				return unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
			}
			if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("binary operation result type does not match its expected type")
			}
			if err := validateTruthinessChild(node.Left, state); err != nil {
				return err
			}
			return validateTruthinessChild(node.Right, state)
		}
		if !supportedGeneratedScalarType(node.OperandType) && node.OperandType.Element == nil || !supportedGeneratedScalarType(node.ResultType) {
			return unknownExpressionDiagnostic("binary operation with invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("binary operation result type does not match its expected type")
		}
		if err := validateBinaryMetadata(node); err != nil {
			return err
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		// A shift count keeps its own integer type; it never takes the left
		// operand's type, unlike every other binary operator here.
		rightExpected := node.OperandType
		if node.Operator == checker.ShiftLeftOperator || node.Operator == checker.ShiftRightOperator {
			if rightType, ok := expressionTypeWithState(*node.Right, state); ok {
				rightExpected = rightType
			}
		}
		return validateExpressionChildWithState(node.Right, rightExpected, state)
	case checker.NullTestExpression:
		// == nil and != nil test a nullable operand's active member. The
		// operand carries the pre-test nullable type; the result is Bool.
		if node.Operand == nil {
			return unknownExpressionDiagnostic("null test without a checked operand")
		}
		if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.IsUnion(node.OperandType) || !compilerTypes.ContainsUnionMember(node.OperandType, compilerTypes.Nil) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("null test has an invalid nullable operand type")
		}
		if node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
			return unknownExpressionDiagnostic("null test has an invalid operator")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("null test result type is not Bool")
		}
		if expected != nil && !compilerTypes.Equal(*expected, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("null test result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
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
		if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) || node.Element.Signature != nil || !supportedGeneratedTypeWithState(node.ResultType, state) || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
			return unknownExpressionDiagnostic("heap allocation has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("heap allocation result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.HeapAllocateAlignedExpression:
		if node.Operand == nil || len(node.Arguments) != 2 || node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) || node.Element.Signature != nil || !supportedGeneratedTypeWithState(node.ResultType, state) || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
			return unknownExpressionDiagnostic("aligned heap allocation has invalid checked metadata")
		}
		if !compilerTypes.Equal(node.Arguments[1].Type, compilerTypes.SizeType) {
			return unknownExpressionDiagnostic("aligned heap allocation alignment is not a Size")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("aligned heap allocation result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[1], state)
	case checker.HeapFreeExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("heap free has invalid checked metadata")
		}
		if node.Arguments[0].Type.Element == nil {
			return unknownExpressionDiagnostic("heap free operand is not a pointer")
		}
		if expected != nil {
			return unknownExpressionDiagnostic("heap free produces no value")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.AdtConstructExpression:
		adt := node.ResultType.Adt
		if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("ADT construction has invalid checked metadata")
		}
		variant := &adt.Variants[node.VariantIndex]
		if len(node.Arguments) != len(variant.Payload) {
			return unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("ADT construction result does not match its expected type")
		}
		for index, member := range variant.Payload {
			if err := validateCheckedOperandWithState(node.Arguments[index], state); err != nil {
				return err
			}
			if !generatedAssignable(member.Type, node.Arguments[index].Type) {
				return unknownExpressionDiagnostic("ADT construction payload does not match its variant field")
			}
		}
		return nil
	case checker.AdtPayloadExpression:
		adt := node.OperandType.Adt
		if node.Operand == nil || adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || node.MemberIndex < 0 || node.MemberIndex >= len(adt.Variants[node.VariantIndex].Payload) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("ADT payload read has invalid checked metadata")
		}
		member := &adt.Variants[node.VariantIndex].Payload[node.MemberIndex]
		if !compilerTypes.Equal(node.ResultType, member.Type) || expected != nil && !compilerTypes.Equal(*expected, member.Type) {
			return unknownExpressionDiagnostic("ADT payload read result does not match its checked field")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.MatchExpression:
		if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || len(node.Arguments) != len(node.MemberMap) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("match expression has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("match result does not match its expected type")
		}
		for armIndex, arm := range node.Arguments {
			if !generatedAssignable(node.ResultType, arm.Type) {
				return unknownExpressionDiagnostic("match arm does not match its checked result type")
			}
			if err := validateCheckedOperandWithState(arm, state); err != nil {
				return err
			}
			if node.MemberMap[armIndex] == checker.MatchScalarTag {
				if armIndex >= len(node.MatchConstants) || node.MatchConstants[armIndex].Kind != checker.ConstantOperand {
					return unknownExpressionDiagnostic("scalar match arm without a checked constant")
				}
				if !compilerTypes.Equal(node.MatchConstants[armIndex].Type, node.OperandType) {
					return unknownExpressionDiagnostic("scalar match constant does not match the scrutinee type")
				}
				if err := validateCheckedOperandWithState(node.MatchConstants[armIndex], state); err != nil {
					return err
				}
			}
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ArrayLiteralExpression, checker.IndexExpression, checker.CollectionMethodCallExpression, checker.CollectionSliceExpression:
		return validateCollectionExpression(node, expected, state)
	case checker.StringLiteralExpression, checker.StringMethodCallExpression, checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.StringInterpolateExpression,
		checker.InlineStringConstructExpression, checker.TextCoerceExpression:
		return validateTextExpression(node, expected, state)
	case checker.ListNewExpression, checker.DictNewExpression:
		return validateCollectionConstructor(node, expected, state)
	case checker.WideningExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("widening expression has invalid checked metadata")
		}
		if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) {
			return unknownExpressionDiagnostic("widening destination is not numeric")
		}
		if common, ok := compilerTypes.LosslessCommonType(node.OperandType, node.ResultType); !ok || !compilerTypes.Equal(common, node.ResultType) {
			return unknownExpressionDiagnostic("widening is not a proven lossless conversion")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("widening result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.DeepEqualityExpression:
		if node.Left == nil || node.Right == nil || node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
			return unknownExpressionDiagnostic("deep equality has invalid checked metadata")
		}
		// Two text operands may be different forms; every other comparison
		// has one compared type.
		rightExpected := node.OperandType
		if compilerTypes.IsText(node.OperandType) {
			if !compilerTypes.IsText(node.RightType) {
				return unknownExpressionDiagnostic("text equality has a non-text right operand")
			}
			rightExpected = node.RightType
		}
		leftType, leftOK := expressionTypeWithState(*node.Left, state)
		rightType, rightOK := expressionTypeWithState(*node.Right, state)
		if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, rightExpected) {
			return unknownExpressionDiagnostic("deep equality operand does not match its compared type")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("deep equality result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, rightExpected, state)
	case checker.ConversionExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 2 {
			return unknownExpressionDiagnostic("numeric conversion has invalid checked metadata")
		}
		if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) || node.MemberIndex != 0 && (!compilerTypes.IsInteger(node.OperandType) || !compilerTypes.IsInteger(node.ResultType)) || node.MemberIndex == 0 && !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("numeric conversion has invalid checked types")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.WidensTo(node.ResultType, *expected) {
			return unknownExpressionDiagnostic("numeric conversion result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.BitCastExpression:
		if node.Operand == nil || !checker.BitCastEligibleType(node.OperandType) || !checker.BitCastEligibleType(node.ResultType) || node.OperandType.Bits != node.ResultType.Bits {
			return unknownExpressionDiagnostic("bit cast has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("bit cast result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.RuneMethodCallExpression:
		return validateRuneMethod(node, expected, state)
	case checker.CursorMethodCallExpression:
		return validateCursorMethod(node, expected, state)
	case checker.GraphemeMethodCallExpression:
		return validateGraphemeMethod(node, expected, state)
	case checker.ErrorHeaderExpression:
		if node.Operand == nil || !compilerTypes.IsError(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.ErrorHeaderText) {
			return unknownExpressionDiagnostic("Error.header has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("Error.header result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ErrorKindHeaderExpression:
		if node.Operand == nil || !compilerTypes.IsErrorKind(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.ErrorHeaderText) {
			return unknownExpressionDiagnostic("ErrorKind.header has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("ErrorKind.header result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ModuleValueExpression:
		if node.Name == "" || node.ResultType == (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("module value reference has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("module value reference does not match its expected type")
		}
		return nil
	case checker.EndianConversionExpression:
		if node.Operand == nil || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 1 {
			return unknownExpressionDiagnostic("endian conversion has invalid checked metadata")
		}
		if node.Name == "from" {
			if len(node.Arguments) != 1 || node.ResultType == (compilerTypes.Type{}) || node.OperandType.Array == nil {
				return unknownExpressionDiagnostic("endian from conversion has invalid checked metadata")
			}
			if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("endian from result does not match its expected type")
			}
			return validateCheckedOperandWithState(node.Arguments[0], state)
		}
		if len(node.Arguments) != 0 || node.ResultType.Array == nil {
			return unknownExpressionDiagnostic("endian to conversion has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("endian to result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.TryExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.OperandType.Union == nil {
			return unknownExpressionDiagnostic("try expression has invalid checked metadata")
		}
		if unionMemberIndex(node.OperandType, compilerTypes.ErrorType) != node.MemberIndex {
			return unknownExpressionDiagnostic("try expression error member does not match its source union")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression, checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression, checker.MutexConstructorExpression, checker.MutexMethodCallExpression, checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression:
		return validateConcurrencyExpression(node, expected, state)
	case checker.StashConstructorExpression, checker.StashMethodCallExpression:
		return validateStashExpression(node, expected, state)
	case checker.PoolConstructorExpression, checker.PoolMethodCallExpression:
		return validatePoolExpression(node, expected, state)
	case checker.LayoutExpression:
		if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) || node.Name != "size_of" && node.Name != "align_of" {
			return unknownExpressionDiagnostic("layout query has invalid checked metadata")
		}
		if !layoutEligibleGenerated(node.OperandType) {
			return unknownExpressionDiagnostic("layout query has an ineligible type")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("layout query result type does not match its expected type")
		}
		return nil
	case checker.VolatileReadExpression:
		if node.Operand == nil || node.OperandType.Element == nil || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || !compilerTypes.Equal(node.ResultType, node.Element) {
			return unknownExpressionDiagnostic("volatile read has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("volatile read result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.VolatileWriteExpression:
		if node.Operand == nil || node.OperandType.Element == nil || len(node.Arguments) != 1 || !node.OperandType.PointeeWritable || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("volatile write has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("volatile write result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.PointerOffsetExpression:
		return validatePointerOffset(node, expected, state)
	case checker.PointerIndexExpression:
		return validatePointerIndex(node, expected, state)
	case checker.PointerCastExpression:
		return validatePointerCast(node, expected, state)
	case checker.SliceBridgeExpression:
		return validateSliceBridgeExpression(node, expected, state)
	case checker.PrintExpression:
		if len(node.Arguments) == 0 || node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
			return unknownExpressionDiagnostic("print call has invalid checked metadata")
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.StringCompareExpression:
		if node.Left == nil || node.Right == nil || !compilerTypes.IsText(node.OperandType) || !compilerTypes.IsText(node.RightType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("text ordering has invalid checked metadata")
		}
		switch node.Operator {
		case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		default:
			return unknownExpressionDiagnostic("text ordering has an invalid operator")
		}
		leftType, leftOK := expressionTypeWithState(*node.Left, state)
		rightType, rightOK := expressionTypeWithState(*node.Right, state)
		if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, node.RightType) {
			return unknownExpressionDiagnostic("text ordering operand does not match its compared type")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("text ordering result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, node.RightType, state)
	default:
		return unknownExpressionDiagnostic("unsupported checked expression")
	}
}

func validateExpressionChildWithState(child *checker.Expression, expected compilerTypes.Type, state *expressionValidation) error {
	if child == nil {
		return unknownExpressionDiagnostic("operation without a checked child")
	}
	if state.expressions == nil {
		state.expressions = make(map[*checker.Expression]bool)
	}
	if state.expressions[child] {
		return unknownExpressionDiagnostic("cyclic checked expression")
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
