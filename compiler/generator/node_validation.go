// node_validation.go owns per-form expression validators: view
// provenance, metadata, call, member, and address checks, plus place
// reconstruction and the shared unknown-expression diagnostic.
package generator

import (
	"hexal/compiler/checker"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// validateViewProvenance checks the structural consistency of checker-computed
// borrow metadata: a Bindings record names at least one root, and any named
// roots require the Bindings kind. Generation never reconstructs borrow
// facts, so inconsistent provenance is an internal failure, never a silent
// acceptance.
func validateViewProvenance(node checker.Expression) error {
	if node.RootKind == checker.ViewRootBindings && len(node.ViewRoots) == 0 {
		return unknownExpressionDiagnostic()
	}
	if len(node.ViewRoots) > 0 && node.RootKind != checker.ViewRootBindings {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateExpressionMetadata(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	var metadataType compilerTypes.Type
	for _, typ := range []compilerTypes.Type{node.OperandType, node.ResultType} {
		if typ == (compilerTypes.Type{}) {
			continue
		}
		if !supportedGeneratedTypeWithState(typ, state) || expected != nil && !compilerTypes.Equal(*expected, typ) || metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(metadataType, typ) {
			return unknownExpressionDiagnostic()
		}
		metadataType = typ
	}
	return nil
}

// validateFunctionReference accepts a declared function used as a Fun<...> value.
// A function is not a place, so no addressability metadata is consulted.
func validateFunctionReference(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic()
	}
	if node.LocalHelperOrdinal != 0 {
		// A local named function's generated symbol is the shared
		// hex_fun_<ordinal> stream, never an entry in the module's
		// source-name-keyed declaration table.
	} else if !validSourceName(node.Name) {
		return unknownExpressionDiagnostic()
	} else if len(state.functions) > 0 && node.Module == "" {
		// A cross-module callee is not in the local declaration table; the
		// checker resolved it against the target module's exported records,
		// and the checked Fun type is authoritative.
		declared, ok := state.functions[node.Name]
		if !ok || !compilerTypes.Equal(declared, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

// validateFunctionLiteralExpression accepts an anonymous function literal
// used as a Fun<...> value. Its own body is not re-validated here: like an
// ordinary named function's body, it is checked directly when
// writeLocalHelperDefinitions emits it, not through this preflight sweep,
// which exists for concrete generic specializations' substituted types.
// This function only proves the value-position metadata is self-consistent.
func validateFunctionLiteralExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Function == nil || node.LocalHelperOrdinal == 0 || node.LocalHelperOrdinal != node.Function.HelperOrdinal {
		return unknownExpressionDiagnostic()
	}
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) || !compilerTypes.Equal(node.ResultType, node.Function.Type) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

// validateCallExpression checks a call against its callee's signature. The
// arguments carry no ordering metadata: C's unspecified argument evaluation
// order is inherited rather than fixed with temporaries.
func validateCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	signature := node.OperandType.Signature
	if signature == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic()
	}
	if node.Rest {
		// The checked call's rest metadata must agree with the callee's
		// canonical signature; a forged boundary, element type, or Slice would
		// otherwise lower a frame or argument list that does not match.
		if !signature.Rest || node.RestStart != len(signature.Parameters)-1 ||
			!compilerTypes.Equal(node.RestElement, signature.Parameters[node.RestStart]) ||
			!compilerTypes.Equal(node.RestSlice, signature.RestSlice) {
			return unknownExpressionDiagnostic()
		}
	} else if signature.Rest {
		return unknownExpressionDiagnostic()
	}
	if node.Rest {
		if len(node.Arguments) < node.RestStart {
			return unknownExpressionDiagnostic()
		}
	} else if len(signature.Parameters) != len(node.Arguments) {
		return unknownExpressionDiagnostic()
	}
	if signature.Result == nil {
		if node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil {
			return unknownExpressionDiagnostic()
		}
	} else {
		if !compilerTypes.Equal(*signature.Result, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	for index, argument := range node.Arguments {
		var expectedType compilerTypes.Type
		if node.Rest && index >= node.RestStart {
			expectedType = node.RestElement
		} else {
			expectedType = signature.Parameters[index]
		}
		if !generatedAssignable(expectedType, argument.Type) {
			return unknownExpressionDiagnostic()
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

func validateMethodCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Owner == nil || !validSourceName(compilerTypes.SanitizeIdentifier(node.Owner.Name)) || !validSourceName(node.Name) || node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	// A method whose receiver type another module declares is not in the
	// local declaration table; the checker resolved it against the defining
	// module's exported records, so the checked node is authoritative for a
	// cross-module call (mirroring the cross-module function reference
	// path).
	crossModule := moduleOwner(node.Owner.ModuleID, state.owner) != state.owner
	declared, ok := state.methods[methodKey(node.Owner, node.Name)]
	if !crossModule {
		if !ok || declared.Object != node.Owner {
			return unknownExpressionDiagnostic()
		}
		if !compilerTypes.Equal(node.OperandType, declared.SelfType) || !restCallArityOK(node, len(declared.Parameters)) {
			return unknownExpressionDiagnostic()
		}
		declaredRest := len(declared.Parameters) > 0 && declared.Parameters[len(declared.Parameters)-1].Rest
		if node.Rest != declaredRest {
			return unknownExpressionDiagnostic()
		}
		if node.Rest && (node.RestStart != len(declared.Parameters)-1 || !compilerTypes.Equal(node.RestElement, declared.Parameters[node.RestStart].RestElement)) {
			return unknownExpressionDiagnostic()
		}
		if declared.Result == nil {
			if node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
				return unknownExpressionDiagnostic()
			}
		} else {
			if node.ResultType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, *declared.Result) || expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic()
			}
		}
	} else if node.ResultType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic()
	}
	receiverType, receiverErr := methodReceiverType(*node.Operand, node.OperandType, state)
	if receiverErr != nil {
		return receiverErr
	}
	if !crossModule && !generatedAssignable(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, receiverType, state); err != nil {
		return err
	}
	if crossModule {
		return nil
	}
	for index, argument := range node.Arguments {
		var parameterType compilerTypes.Type
		if node.Rest && index >= node.RestStart {
			parameterType = node.RestElement
		} else {
			parameterType = declared.Parameters[index].Type
		}
		if !generatedAssignable(parameterType, argument.Type) {
			return unknownExpressionDiagnostic()
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

// restCallArityOK reports whether a checked call's argument count satisfies its
// resolved signature arity, honoring a rest boundary when present.
func restCallArityOK(node checker.Expression, parameterCount int) bool {
	if node.Rest {
		return len(node.Arguments) >= node.RestStart
	}
	return len(node.Arguments) == parameterCount
}

// methodReceiverType recovers the actual checked type of an adapted receiver.
// Address-of receivers carry their interned canonical pointer result from the
// checker, so the Ptr<T>/Ptr<mut T> distinction is read metadata, never a
// fresh construction compared against interned identities.
func methodReceiverType(node checker.Expression, target compilerTypes.Type, state *expressionValidation) (compilerTypes.Type, error) {
	if node.Kind == checker.AddressOfExpression {
		if node.ResultType == (compilerTypes.Type{}) || !isPointerType(node.ResultType) {
			return compilerTypes.Type{}, unknownExpressionDiagnostic()
		}
		return node.ResultType, nil
	}
	if typ, ok := expressionTypeWithState(node, state); ok {
		// The checker only adapted a nullable receiver after a null test
		// narrowed it to its pointer member, so when the binding still holds
		// the declared nullable type and the method's self type is that
		// member, the receiver's effective type is the non-null member.
		if base, nullable := compilerTypes.NullableBase(typ); nullable && compilerTypes.Equal(base, target) {
			return base, nil
		}
		return typ, nil
	}
	return target, nil
}

func validateAddressExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic()
	}
	resultType, hasResult := compilerTypes.Type{}, expected != nil
	if hasResult {
		resultType = *expected
	}
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) || !isPointerType(node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		resultType, hasResult = node.ResultType, true
	}
	if !hasResult || !isPointerType(resultType) {
		return unknownExpressionDiagnostic()
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(*resultType.Element, node.OperandType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, *resultType.Element, state); err != nil {
		return err
	}
	place, err := checkedPlaceMetadata(*node.Operand, state)
	if err != nil {
		return err
	}
	if !place.addressable {
		return unknownExpressionDiagnostic()
	}
	if !compilerTypes.Equal(place.typ, *resultType.Element) {
		return unknownExpressionDiagnostic()
	}
	if place.writable != resultType.PointeeWritable {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateDereferenceExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	resultType, hasResult := compilerTypes.Type{}, expected != nil
	if hasResult {
		resultType = *expected
	}
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic()
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		resultType, hasResult = node.ResultType, true
	}
	if node.OperandType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.OperandType, state) || !isPointerType(node.OperandType) {
			return unknownExpressionDiagnostic()
		}
		if hasResult && !compilerTypes.Equal(*node.OperandType.Element, resultType) {
			return unknownExpressionDiagnostic()
		}
	}

	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
		return unknownExpressionDiagnostic()
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(receiverType, node.OperandType) {
		return unknownExpressionDiagnostic()
	}
	if !hasResult {
		resultType, hasResult = *receiverType.Element, true
	}
	if !hasResult || !compilerTypes.Equal(*receiverType.Element, resultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, receiverType, state)
}

// memberResultMatchesDeclared accepts a checked member type as a faithful
// resolution of the member's declared type. Identity is the ordinary case;
// a List<T> field of a builtin declared at package init() is re-resolved
// through the compilation arena before it reaches any check, and that live
// type keeps the declared type's canonical key while differing in interned
// identity. Canonical-key equality with a non-empty key admits exactly that
// case and nothing else: every other re-resolution would be a compiler bug
// and fails closed.
func memberResultMatchesDeclared(result, declared compilerTypes.Type) bool {
	if compilerTypes.Equal(result, declared) {
		return true
	}
	return declared.CanonicalKey != "" && result.CanonicalKey == declared.CanonicalKey
}

func validateMemberExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) || !supportedGeneratedTypeWithState(node.Member.Type, state) {
		return unknownExpressionDiagnostic()
	}
	checkedType := node.Member.Type
	if node.ResultType != (compilerTypes.Type{}) {
		checkedType = node.ResultType
	}
	if expected != nil && !compilerTypes.Equal(*expected, checkedType) {
		return unknownExpressionDiagnostic()
	}
	if node.ResultType != (compilerTypes.Type{}) && (!supportedGeneratedTypeWithState(node.ResultType, state) || !memberResultMatchesDeclared(node.ResultType, node.Member.Type)) {
		return unknownExpressionDiagnostic()
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
		return err
	}
	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || receiverType.Object == nil {
		return unknownExpressionDiagnostic()
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic()
	}
	canonical, pointerOK := objectMember(receiverType.Object, node.Member)
	byName, nameOK := receiverType.Object.Member(node.Member.Name)
	if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateUnaryMetadata(node checker.Expression) error {
	switch node.Operator {
	case checker.NegateOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsSignedInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic()
		}
	case checker.LogicalNotOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic()
		}
	case checker.BitwiseNotOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsInteger(node.OperandType) {
			return unknownExpressionDiagnostic()
		}
	default:
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateBinaryMetadata(node checker.Expression) error {
	resultIsBool := false
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator, checker.DivideOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic()
		}
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
	case checker.RemainderOperator:
		if !compilerTypes.IsInteger(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
	case checker.EqualOperator, checker.NotEqualOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic()
		}
		resultIsBool = true
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic()
		}
		resultIsBool = true
	case checker.LogicalAndOperator, checker.LogicalOrOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic()
		}
		resultIsBool = true
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator,
		checker.ShiftLeftOperator, checker.ShiftRightOperator:
		if !compilerTypes.IsInteger(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
	default:
		return unknownExpressionDiagnostic()
	}
	if resultIsBool != compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

// checkedPlaceMetadata reconstructs place capabilities from generated bindings
// and nominal type metadata instead of trusting forged operand flags.
func checkedPlaceMetadata(node checker.Expression, state *expressionValidation) (generatedPlace, error) {
	switch node.Kind {
	case checker.VariableExpression:
		if !validSourceName(node.Name) || state == nil {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		binding, ok := state.bindingFor(node)
		if !ok {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
			if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
				return generatedPlace{}, unknownExpressionDiagnostic()
			}
		}
		return generatedPlace{typ: binding.typ, addressable: true, writable: binding.mutable}, nil
	case checker.ModuleValueExpression:
		if node.Name == "" || node.ResultType == (compilerTypes.Type{}) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		return generatedPlace{typ: node.ResultType, addressable: true, writable: node.Mutable}, nil
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if receiver.typ.Object == nil {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		canonical, pointerOK := objectMember(receiver.typ.Object, node.Member)
		byName, nameOK := receiver.typ.Object.Member(node.Member.Name)
		if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiver.typ) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		if node.ResultType != (compilerTypes.Type{}) && !memberResultMatchesDeclared(node.ResultType, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		placeType := node.Member.Type
		if node.ResultType != (compilerTypes.Type{}) {
			placeType = node.ResultType
		}
		return generatedPlace{
			typ:         placeType,
			addressable: receiver.addressable,
			writable:    receiver.writable && node.Member.Mutable,
		}, nil
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		var receiverType compilerTypes.Type
		var ok bool
		switch node.Operand.Kind {
		case checker.VariableExpression, checker.MemberExpression, checker.DereferenceExpression:
			receiver, err := checkedPlaceMetadata(*node.Operand, state)
			if err != nil {
				return generatedPlace{}, err
			}
			receiverType, ok = receiver.typ, true
		default:
			receiverType, ok = expressionTypeWithState(*node.Operand, state)
		}
		if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, *receiverType.Element) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		return generatedPlace{typ: *receiverType.Element, addressable: true, writable: receiverType.PointeeWritable}, nil
	case checker.PointerIndexExpression:
		if err := validatePointerIndex(node, nil, state); err != nil {
			return generatedPlace{}, err
		}
		return generatedPlace{typ: node.ResultType, addressable: true, writable: node.OperandType.PointeeWritable}, nil
	case checker.UnionPayloadExpression:
		if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || node.ResultType == (compilerTypes.Type{}) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		members := compilerTypes.UnionMembers(node.OperandType)
		if node.MemberIndex < 0 || node.MemberIndex >= members.Len() {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		member, _ := members.At(node.MemberIndex)
		if !compilerTypes.Equal(member, node.ResultType) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		parent, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if !compilerTypes.Equal(parent.typ, node.OperandType) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		return generatedPlace{typ: node.ResultType, addressable: parent.addressable, writable: parent.writable}, nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Array == nil && node.OperandType.Slice == nil && node.OperandType.List == nil {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if !compilerTypes.Equal(node.OperandType, receiver.typ) {
			// A null-test may narrow the indexed binding from a union to its
			// Slice, Array, or List member. The place metadata recovers the
			// declared binding type, so accept only an exact union member and
			// use the checked effective type for element and write capability.
			if !compilerTypes.Assignable(receiver.typ, node.OperandType) {
				return generatedPlace{}, unknownExpressionDiagnostic()
			}
			receiver.typ = node.OperandType
		}
		var element compilerTypes.Type
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.Slice != nil {
			element = node.OperandType.Slice.Element
		} else {
			element = node.OperandType.List.Element
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, element) {
			return generatedPlace{}, unknownExpressionDiagnostic()
		}
		// A Slice element place is never writable; a mutable Array place or
		// any live List reference is.
		writable := node.OperandType.Array != nil && receiver.writable || node.OperandType.List != nil
		return generatedPlace{typ: element, addressable: receiver.addressable, writable: writable}, nil
	default:
		return generatedPlace{}, unknownExpressionDiagnostic()
	}
}

func unknownExpressionDiagnostic() error {
	return generatorDiagnostic()
}

// unknownExpressionDiagnosticAt is the whole-compilation constructor's
// source-aware sibling: it attaches the checked node's carried span and the
// position the compilation's source table resolves it to, so an internal
// generation failure that names a real node renders with its source location.
// A node without a span (a hand-built checked node, or a compiler-invariant
// failure with no node at all) keeps the historical zero location.
func unknownExpressionDiagnosticAt(state *expressionValidation, s span.Span) error {
	diagnostic := generatorDiagnostic()
	if s.File == "" {
		return diagnostic
	}
	position := state.position(s)
	diagnostic.Span = s
	diagnostic.Module = s.File
	diagnostic.Position = position
	return diagnostic
}

func validateVariableExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if !validSourceName(node.Name) {
		return unknownExpressionDiagnostic()
	}
	if state != nil && (state.variables != nil || state.bindings != nil) {
		binding, ok := state.bindingFor(node)
		if !ok {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(binding.typ, *expected) {
			// A null test narrows a local binding's reads to its non-Nil
			// base (or to Nil) inside the branch where the test holds;
			// the binding itself still holds the declared nullable type,
			// so a narrowed read is a stricter type.
			if !compilerTypes.Assignable(binding.typ, *expected) {
				return unknownExpressionDiagnostic()
			}
		}
		for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
			if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
				return unknownExpressionDiagnostic()
			}
		}
	}
	return validateExpressionMetadata(node, expected, state)
}

func validateForeignFunctionReferenceExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ForeignCName == "" || node.ResultType == (compilerTypes.Type{}) || node.ResultType.Signature == nil {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateForeignConstantExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ForeignCName == "" {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateForeignGlobalExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ForeignCName == "" {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateObjectExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Object == nil {
		return unknownExpressionDiagnostic()
	}
	if err := validateObjectValue(node.Object, state); err != nil {
		return err
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.Object.Type) {
		return unknownExpressionDiagnostic()
	}
	if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Object.Type) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionMetadata(node, expected, state)
}

func validateConstantExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Constant == nil || node.Constant.Kind != checker.ConstantOperand && node.Constant.Kind != checker.ObjectOperand ||
		!compilerTypes.Equal(node.ResultType, node.Constant.Type) ||
		!supportedGeneratedScalarType(node.ResultType) && node.Constant.Type.Object == nil && node.Constant.Type.Union == nil {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateConstantOperand(*node.Constant)
}

func validateUnaryOperationExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	if node.Operator == checker.LogicalNotOperator {
		// not accepts any value-producing operand; the operand is
		// validated through its truthiness.
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		return validateTruthinessChild(node.Operand, state)
	}
	if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateUnaryMetadata(node); err != nil {
		return err
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateBinaryOperationExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Left == nil || node.Right == nil {
		return unknownExpressionDiagnostic()
	}
	if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
		// and/or accept any value-producing operands, mixed types
		// included; each side is validated through its truthiness.
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		if err := validateTruthinessChild(node.Left, state); err != nil {
			return err
		}
		return validateTruthinessChild(node.Right, state)
	}
	if !supportedGeneratedScalarType(node.OperandType) && node.OperandType.Element == nil || !supportedGeneratedScalarType(node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
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
}

func validateNullTestExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	// == nil and != nil test a nullable operand's active member. The
	// operand carries the pre-test nullable type; the result is Bool.
	if node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.IsUnion(node.OperandType) || !compilerTypes.ContainsUnionMember(node.OperandType, compilerTypes.Nil) || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic()
	}
	if node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
		return unknownExpressionDiagnostic()
	}
	if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, compilerTypes.Bool) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateHeapAllocateExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) || node.Element.Signature != nil || !supportedGeneratedTypeWithState(node.ResultType, state) || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
		return err
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

func validateHeapAllocateAlignedExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || len(node.Arguments) != 2 || node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) || node.Element.Signature != nil || !supportedGeneratedTypeWithState(node.ResultType, state) || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
		return unknownExpressionDiagnostic()
	}
	if !compilerTypes.Equal(node.Arguments[1].Type, compilerTypes.SizeType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
		return err
	}
	if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
		return err
	}
	return validateCheckedOperandWithState(node.Arguments[1], state)
}

func validateHeapFreeExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic()
	}
	if node.Arguments[0].Type.Element == nil {
		return unknownExpressionDiagnostic()
	}
	if expected != nil {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
		return err
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

func validateAdtConstructExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	adt := node.ResultType.Adt
	if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic()
	}
	variant := &adt.Variants[node.VariantIndex]
	if len(node.Arguments) != len(variant.Payload) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	for index, member := range variant.Payload {
		if err := validateCheckedOperandWithState(node.Arguments[index], state); err != nil {
			return err
		}
		if !generatedAssignable(member.Type, node.Arguments[index].Type) {
			return unknownExpressionDiagnostic()
		}
	}
	return nil
}

func validateAdtPayloadExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	adt := node.OperandType.Adt
	if node.Operand == nil || adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || node.MemberIndex < 0 || node.MemberIndex >= len(adt.Variants[node.VariantIndex].Payload) || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic()
	}
	member := &adt.Variants[node.VariantIndex].Payload[node.MemberIndex]
	checkedType := node.ResultType
	if checkedType == (compilerTypes.Type{}) {
		checkedType = member.Type
	}
	if !memberResultMatchesDeclared(checkedType, member.Type) || expected != nil && !compilerTypes.Equal(*expected, checkedType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateMatchExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || len(node.Arguments) != len(node.MemberMap) || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	for armIndex, arm := range node.Arguments {
		if !generatedAssignable(node.ResultType, arm.Type) {
			return unknownExpressionDiagnostic()
		}
		if err := validateCheckedOperandWithState(arm, state); err != nil {
			return err
		}
		if node.MemberMap[armIndex] == checker.MatchScalarTag {
			if armIndex >= len(node.MatchConstants) || node.MatchConstants[armIndex].Kind != checker.ConstantOperand {
				return unknownExpressionDiagnostic()
			}
			if !compilerTypes.Equal(node.MatchConstants[armIndex].Type, node.OperandType) {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(node.MatchConstants[armIndex], state); err != nil {
				return err
			}
		}
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateWideningExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic()
	}
	if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if common, ok := compilerTypes.LosslessCommonType(node.OperandType, node.ResultType); !ok || !compilerTypes.Equal(common, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateDeepEqualityExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Left == nil || node.Right == nil || node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
		return unknownExpressionDiagnostic()
	}
	// Two text operands may be different forms; every other comparison
	// has one compared type.
	rightExpected := node.OperandType
	if compilerTypes.IsText(node.OperandType) {
		if !compilerTypes.IsText(node.RightType) {
			return unknownExpressionDiagnostic()
		}
		rightExpected = node.RightType
	}
	leftType, leftOK := expressionTypeWithState(*node.Left, state)
	rightType, rightOK := expressionTypeWithState(*node.Right, state)
	if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, rightExpected) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return err
	}
	return validateExpressionChildWithState(node.Right, rightExpected, state)
}

func validateConversionExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 2 {
		return unknownExpressionDiagnostic()
	}
	if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) || node.MemberIndex != 0 && (!compilerTypes.IsInteger(node.OperandType) || !compilerTypes.IsInteger(node.ResultType)) || node.MemberIndex == 0 && !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.WidensTo(node.ResultType, *expected) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateBitCastExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !checker.BitCastEligibleType(node.OperandType) || !checker.BitCastEligibleType(node.ResultType) || node.OperandType.Bits != node.ResultType.Bits {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateErrorHeaderExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsError(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.ErrorHeaderText) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateErrorKindHeaderExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsErrorKind(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.ErrorHeaderText) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateModuleValueExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Name == "" || node.ResultType == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateEndianConversionExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 1 {
		return unknownExpressionDiagnostic()
	}
	if node.Name == "from" {
		if len(node.Arguments) != 1 || node.ResultType == (compilerTypes.Type{}) || node.OperandType.Array == nil {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	}
	if len(node.Arguments) != 0 || node.ResultType.Array == nil {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateTryExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.OperandType.Union == nil {
		return unknownExpressionDiagnostic()
	}
	if unionMemberIndex(node.OperandType, compilerTypes.ErrorType) != node.MemberIndex {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateLayoutExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) || node.Name != "size_of" && node.Name != "align_of" {
		return unknownExpressionDiagnostic()
	}
	if !layoutEligibleGenerated(node.OperandType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func validateVolatileReadExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType.Element == nil || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || !compilerTypes.Equal(node.ResultType, node.Element) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateVolatileWriteExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType.Element == nil || len(node.Arguments) != 1 || !node.OperandType.PointeeWritable || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || node.ResultType != (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

func validatePrintExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if len(node.Arguments) == 0 || node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
		return unknownExpressionDiagnostic()
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

func validateStringCompareExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Left == nil || node.Right == nil || !compilerTypes.IsText(node.OperandType) || !compilerTypes.IsText(node.RightType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return unknownExpressionDiagnostic()
	}
	switch node.Operator {
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
	default:
		return unknownExpressionDiagnostic()
	}
	leftType, leftOK := expressionTypeWithState(*node.Left, state)
	rightType, rightOK := expressionTypeWithState(*node.Right, state)
	if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, node.RightType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return err
	}
	return validateExpressionChildWithState(node.Right, node.RightType, state)
}
