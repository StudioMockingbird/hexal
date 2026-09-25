// node_validation.go owns per-form expression validators: view
// provenance, metadata, call, member, and address checks, plus place
// reconstruction and the shared unknown-expression diagnostic.
package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// validateViewProvenance checks the structural consistency of checker-computed
// borrow metadata: a Bindings record names at least one root, and any named
// roots require the Bindings kind. Generation never reconstructs borrow
// facts, so inconsistent provenance is an internal failure, never a silent
// acceptance.
func validateViewProvenance(node checker.Expression) error {
	if node.RootKind == checker.ViewRootBindings && len(node.ViewRoots) == 0 {
		return unknownExpressionDiagnostic("view provenance names no root")
	}
	if len(node.ViewRoots) > 0 && node.RootKind != checker.ViewRootBindings {
		return unknownExpressionDiagnostic("view roots without a bindings provenance")
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
			return unknownExpressionDiagnostic("expression metadata does not match its expected type")
		}
		metadataType = typ
	}
	return nil
}

// validateFunctionReference accepts a declared function used as a Fun<...> value.
// A function is not a place, so no addressability metadata is consulted.
func validateFunctionReference(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("function reference without a checked Fun type")
	}
	if node.LocalHelperOrdinal != 0 {
		// A local named function's generated symbol is the shared
		// hex_fun_<ordinal> stream, never an entry in the module's
		// source-name-keyed declaration table.
	} else if !validSourceName(node.Name) {
		return unknownExpressionDiagnostic("function reference without a source name")
	} else if state.functions != nil && node.Module == "" {
		// A cross-module callee is not in the local declaration table; the
		// checker resolved it against the target module's exported records,
		// and the checked Fun type is authoritative.
		declared, ok := state.functions[node.Name]
		if !ok || !compilerTypes.Equal(declared, node.ResultType) {
			return unknownExpressionDiagnostic("function reference is not a declared checked function")
		}
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, node.ResultType) {
		return unknownExpressionDiagnostic("function reference metadata does not match its checked type")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("function reference type does not match its expected type")
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
		return unknownExpressionDiagnostic("function literal has invalid checked metadata")
	}
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) || !compilerTypes.Equal(node.ResultType, node.Function.Type) {
		return unknownExpressionDiagnostic("function literal without a checked Fun type")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("function literal type does not match its expected type")
	}
	return nil
}

// validateCallExpression checks a call against its callee's signature. The
// arguments carry no ordering metadata: C's unspecified argument evaluation
// order is inherited rather than fixed with temporaries.
func validateCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("call without a checked callee")
	}
	signature := node.OperandType.Signature
	if signature == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("call callee is not a checked Fun type")
	}
	if node.Rest {
		// The checked call's rest metadata must agree with the callee's
		// canonical signature; a forged boundary, element type, or Slice would
		// otherwise lower a frame or argument list that does not match.
		if !signature.Rest || node.RestStart != len(signature.Parameters)-1 ||
			!compilerTypes.Equal(node.RestElement, signature.Parameters[node.RestStart]) ||
			!compilerTypes.Equal(node.RestSlice, signature.RestSlice) {
			return unknownExpressionDiagnostic("rest call metadata does not match its checked signature")
		}
	} else if signature.Rest {
		return unknownExpressionDiagnostic("rest call omitted its checked rest boundary")
	}
	if node.Rest {
		if len(node.Arguments) < node.RestStart {
			return unknownExpressionDiagnostic("call argument count is below its rest boundary")
		}
	} else if len(signature.Parameters) != len(node.Arguments) {
		return unknownExpressionDiagnostic("call argument count does not match its checked signature")
	}
	if signature.Result == nil {
		if node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("call result type does not match its checked signature")
		}
		if expected != nil {
			return unknownExpressionDiagnostic("a call producing no value has no expected type")
		}
	} else {
		if !compilerTypes.Equal(*signature.Result, node.ResultType) {
			return unknownExpressionDiagnostic("call result type does not match its checked signature")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("call result type does not match its expected type")
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
			return unknownExpressionDiagnostic("call argument type does not match its checked parameter")
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

func validateMethodCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Owner == nil || !validSourceName(compilerTypes.SanitizeIdentifier(node.Owner.Name)) || !validSourceName(node.Name) || node.Operand == nil {
		return unknownExpressionDiagnostic("method call has incomplete checked metadata")
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
			return unknownExpressionDiagnostic("method call does not name a declared checked method")
		}
		if !compilerTypes.Equal(node.OperandType, declared.SelfType) || !restCallArityOK(node, len(declared.Parameters)) {
			return unknownExpressionDiagnostic("method call does not match its checked signature")
		}
		declaredRest := len(declared.Parameters) > 0 && declared.Parameters[len(declared.Parameters)-1].Rest
		if node.Rest != declaredRest {
			return unknownExpressionDiagnostic("method call rest metadata does not match its checked signature")
		}
		if node.Rest && (node.RestStart != len(declared.Parameters)-1 || !compilerTypes.Equal(node.RestElement, declared.Parameters[node.RestStart].RestElement)) {
			return unknownExpressionDiagnostic("method call rest metadata does not match its checked signature")
		}
		if declared.Result == nil {
			if node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
				return unknownExpressionDiagnostic("method call result type does not match its checked signature")
			}
		} else {
			if node.ResultType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, *declared.Result) || expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("method call result type does not match its checked signature")
			}
		}
	} else if node.ResultType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("method call result type is not a supported checked type")
	}
	receiverType, receiverErr := methodReceiverType(*node.Operand, node.OperandType, state)
	if receiverErr != nil {
		return receiverErr
	}
	if !crossModule && !generatedAssignable(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic("method call receiver type does not match its checked receiver")
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
			return unknownExpressionDiagnostic("method call argument type does not match its checked parameter")
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
			return compilerTypes.Type{}, unknownExpressionDiagnostic("method receiver address-of has no checked pointer result")
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
		return unknownExpressionDiagnostic("address-of without an operand")
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("address-of has an invalid operand type")
	}
	resultType, hasResult := compilerTypes.Type{}, expected != nil
	if hasResult {
		resultType = *expected
	}
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) || !isPointerType(node.ResultType) {
			return unknownExpressionDiagnostic("address-of result is not a valid pointer type")
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic("address-of result type does not match its expected type")
		}
		resultType, hasResult = node.ResultType, true
	}
	if !hasResult || !isPointerType(resultType) {
		return unknownExpressionDiagnostic("address-of result is not a pointer type")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(*resultType.Element, node.OperandType) {
		return unknownExpressionDiagnostic("address-of operand type does not match its result type")
	}
	if err := validateExpressionChildWithState(node.Operand, *resultType.Element, state); err != nil {
		return err
	}
	place, err := checkedPlaceMetadata(*node.Operand, state)
	if err != nil {
		return err
	}
	if !place.addressable {
		return unknownExpressionDiagnostic("address-of child is not addressable")
	}
	if !compilerTypes.Equal(place.typ, *resultType.Element) {
		return unknownExpressionDiagnostic("address-of child type does not match its result type")
	}
	if place.writable != resultType.PointeeWritable {
		return unknownExpressionDiagnostic("address-of result writability does not match its place")
	}
	return nil
}

func validateDereferenceExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("dereference without an operand")
	}
	resultType, hasResult := compilerTypes.Type{}, expected != nil
	if hasResult {
		resultType = *expected
	}
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("dereference result type is not supported")
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic("dereference result type does not match its expected type")
		}
		resultType, hasResult = node.ResultType, true
	}
	if node.OperandType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.OperandType, state) || !isPointerType(node.OperandType) {
			return unknownExpressionDiagnostic("dereference operand is not a valid pointer type")
		}
		if hasResult && !compilerTypes.Equal(*node.OperandType.Element, resultType) {
			return unknownExpressionDiagnostic("dereference result type does not match its operand type")
		}
	}

	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
		return unknownExpressionDiagnostic("dereference receiver is not a checked pointer expression")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(receiverType, node.OperandType) {
		return unknownExpressionDiagnostic("dereference receiver type does not match its checked operand type")
	}
	if !hasResult {
		resultType, hasResult = *receiverType.Element, true
	}
	if !hasResult || !compilerTypes.Equal(*receiverType.Element, resultType) {
		return unknownExpressionDiagnostic("dereference receiver element does not match its result type")
	}
	return validateExpressionChildWithState(node.Operand, receiverType, state)
}

func validateMemberExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) || !supportedGeneratedTypeWithState(node.Member.Type, state) {
		return unknownExpressionDiagnostic("member selection has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.Member.Type) {
		return unknownExpressionDiagnostic("member type does not match its expected type")
	}
	if node.ResultType != (compilerTypes.Type{}) && (!supportedGeneratedTypeWithState(node.ResultType, state) || !compilerTypes.Equal(node.ResultType, node.Member.Type) || expected != nil && !compilerTypes.Equal(*expected, node.ResultType)) {
		return unknownExpressionDiagnostic("member result type does not match its checked member")
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("member receiver has an invalid checked type")
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
		return err
	}
	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || receiverType.Object == nil {
		return unknownExpressionDiagnostic("member receiver is not a checked object expression")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic("member receiver type does not match its checked receiver")
	}
	canonical, pointerOK := objectMember(receiverType.Object, node.Member)
	byName, nameOK := receiverType.Object.Member(node.Member.Name)
	if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
		return unknownExpressionDiagnostic("member is not part of its checked object")
	}
	return nil
}

func validateUnaryMetadata(node checker.Expression) error {
	switch node.Operator {
	case checker.NegateOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsSignedInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("negation has invalid checked types")
		}
	case checker.LogicalNotOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
		}
	case checker.BitwiseNotOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsInteger(node.OperandType) {
			return unknownExpressionDiagnostic("complement has invalid checked types")
		}
	default:
		return unknownExpressionDiagnostic("unknown unary operator")
	}
	return nil
}

func validateBinaryMetadata(node checker.Expression) error {
	resultIsBool := false
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator, checker.DivideOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("arithmetic operation with an unsupported type")
		}
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("arithmetic result type does not match its operand type")
		}
	case checker.RemainderOperator:
		if !compilerTypes.IsInteger(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("remainder operation has invalid checked types")
		}
	case checker.EqualOperator, checker.NotEqualOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("equality operation must produce Bool")
		}
		resultIsBool = true
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("ordering operation has invalid checked types")
		}
		resultIsBool = true
	case checker.LogicalAndOperator, checker.LogicalOrOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
		}
		resultIsBool = true
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator,
		checker.ShiftLeftOperator, checker.ShiftRightOperator:
		if !compilerTypes.IsInteger(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("bitwise or shift operation has invalid checked types")
		}
	default:
		return unknownExpressionDiagnostic("unknown binary operator")
	}
	if resultIsBool != compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("binary operation has an invalid result type")
	}
	return nil
}

// checkedPlaceMetadata reconstructs place capabilities from generated bindings
// and nominal type metadata instead of trusting forged operand flags.
func checkedPlaceMetadata(node checker.Expression, state *expressionValidation) (generatedPlace, error) {
	switch node.Kind {
	case checker.VariableExpression:
		if !validSourceName(node.Name) || state == nil || state.variables == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place variable binding metadata is unavailable")
		}
		binding, ok := state.bindingFor(node)
		if !ok {
			return generatedPlace{}, unknownExpressionDiagnostic("place variable is not present in checked bindings")
		}
		for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
			if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
				return generatedPlace{}, unknownExpressionDiagnostic("place variable metadata does not match its checked binding")
			}
		}
		return generatedPlace{typ: binding.typ, addressable: true, writable: binding.mutable}, nil
	case checker.ModuleValueExpression:
		if node.Name == "" || node.ResultType == (compilerTypes.Type{}) {
			return generatedPlace{}, unknownExpressionDiagnostic("place module value has invalid checked metadata")
		}
		return generatedPlace{typ: node.ResultType, addressable: true, writable: node.Mutable}, nil
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member has invalid checked metadata")
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if receiver.typ.Object == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place member receiver is not a checked object")
		}
		canonical, pointerOK := objectMember(receiver.typ.Object, node.Member)
		byName, nameOK := receiver.typ.Object.Member(node.Member.Name)
		if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member is not part of its checked object")
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiver.typ) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member receiver type does not match its checked receiver")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member result type does not match its checked member")
		}
		return generatedPlace{
			typ:         node.Member.Type,
			addressable: receiver.addressable,
			writable:    receiver.writable && node.Member.Mutable,
		}, nil
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference has no pointer receiver")
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
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference receiver is not a checked pointer")
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference receiver type does not match its checked receiver")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, *receiverType.Element) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference result type does not match its pointee")
		}
		return generatedPlace{typ: *receiverType.Element, addressable: true, writable: receiverType.PointeeWritable}, nil
	case checker.PointerIndexExpression:
		if err := validatePointerIndex(node, nil, state); err != nil {
			return generatedPlace{}, err
		}
		return generatedPlace{typ: node.ResultType, addressable: true, writable: node.OperandType.PointeeWritable}, nil
	case checker.UnionPayloadExpression:
		if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || node.ResultType == (compilerTypes.Type{}) {
			return generatedPlace{}, unknownExpressionDiagnostic("union payload place has invalid checked metadata")
		}
		members := compilerTypes.UnionMembers(node.OperandType)
		if node.MemberIndex < 0 || node.MemberIndex >= members.Len() {
			return generatedPlace{}, unknownExpressionDiagnostic("union payload place has an invalid member")
		}
		member, _ := members.At(node.MemberIndex)
		if !compilerTypes.Equal(member, node.ResultType) {
			return generatedPlace{}, unknownExpressionDiagnostic("union payload place member does not match its result")
		}
		parent, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if !compilerTypes.Equal(parent.typ, node.OperandType) {
			return generatedPlace{}, unknownExpressionDiagnostic("union payload place receiver does not match its checked union")
		}
		return generatedPlace{typ: node.ResultType, addressable: parent.addressable, writable: parent.writable}, nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Array == nil && node.OperandType.Slice == nil && node.OperandType.List == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place index has invalid checked metadata")
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
				return generatedPlace{}, unknownExpressionDiagnostic("place index receiver type does not match its checked receiver")
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
			return generatedPlace{}, unknownExpressionDiagnostic("place index result type does not match its element type")
		}
		// A Slice element place is never writable; a mutable Array place or
		// any live List reference is.
		writable := node.OperandType.Array != nil && receiver.writable || node.OperandType.List != nil
		return generatedPlace{typ: element, addressable: receiver.addressable, writable: writable}, nil
	default:
		return generatedPlace{}, unknownExpressionDiagnostic("checked expression is not a place")
	}
}

func unknownExpressionDiagnostic(detail string) error {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.UnknownError,
		Stage:    "generator",
		Message:  detail,
	}
}
