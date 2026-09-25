// node_validation.go owns per-form expression validators: view
// provenance, metadata, call, member, and address checks, plus place
// reconstruction and the shared unknown-expression diagnostic.
package generator

import (
	"fmt"

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
	} else if len(state.functions) > 0 && node.Module == "" {
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
		return unknownExpressionDiagnostic("member selection has invalid checked metadata")
	}
	checkedType := node.Member.Type
	if node.ResultType != (compilerTypes.Type{}) {
		checkedType = node.ResultType
	}
	if expected != nil && !compilerTypes.Equal(*expected, checkedType) {
		return unknownExpressionDiagnostic("member type does not match its expected type")
	}
	if node.ResultType != (compilerTypes.Type{}) && (!supportedGeneratedTypeWithState(node.ResultType, state) || !memberResultMatchesDeclared(node.ResultType, node.Member.Type)) {
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
		if !validSourceName(node.Name) || state == nil {
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
		if node.ResultType != (compilerTypes.Type{}) && !memberResultMatchesDeclared(node.ResultType, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member result type does not match its checked member")
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

// unknownExpressionDiagnosticAt is the whole-compilation constructor's
// source-aware sibling: it attaches the checked node's carried span and the
// position the compilation's source table resolves it to, so an internal
// generation failure that names a real node renders with its source location.
// A node without a span (a hand-built checked node, or a compiler-invariant
// failure with no node at all) keeps the historical zero location.
func unknownExpressionDiagnosticAt(state *expressionValidation, s span.Span, detail string) error {
	diagnostic := compilerTypes.Diagnostic{
		Category: compilerTypes.UnknownError,
		Stage:    "generator",
		Message:  detail,
	}
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
}

func validateForeignFunctionReferenceExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ForeignCName == "" || node.ResultType == (compilerTypes.Type{}) || node.ResultType.Signature == nil {
		return unknownExpressionDiagnostic("foreign function reference without a checked signature")
	}
	if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
		return unknownExpressionDiagnostic("foreign function reference type does not match its expected type")
	}
	return nil
}

func validateForeignConstantExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ForeignCName == "" {
		return unknownExpressionDiagnostic("foreign constant without a C spelling")
	}
	if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
		return unknownExpressionDiagnostic("foreign constant type does not match its expected type")
	}
	return nil
}

func validateForeignGlobalExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ForeignCName == "" {
		return unknownExpressionDiagnostic("foreign global without a C spelling")
	}
	if expected != nil && !compilerTypes.Equal(node.ResultType, *expected) && !compilerTypes.Assignable(node.ResultType, *expected) {
		return unknownExpressionDiagnostic("foreign global type does not match its expected type")
	}
	return nil
}

func validateObjectExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateConstantExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateUnaryOperationExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateBinaryOperationExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateNullTestExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateHeapAllocateExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateHeapAllocateAlignedExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateHeapFreeExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateAdtConstructExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateAdtPayloadExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	adt := node.OperandType.Adt
	if node.Operand == nil || adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || node.MemberIndex < 0 || node.MemberIndex >= len(adt.Variants[node.VariantIndex].Payload) || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("ADT payload read has invalid checked metadata")
	}
	member := &adt.Variants[node.VariantIndex].Payload[node.MemberIndex]
	checkedType := node.ResultType
	if checkedType == (compilerTypes.Type{}) {
		checkedType = member.Type
	}
	if !memberResultMatchesDeclared(checkedType, member.Type) || expected != nil && !compilerTypes.Equal(*expected, checkedType) {
		return unknownExpressionDiagnostic("ADT payload read result does not match its checked field")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateMatchExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateWideningExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateDeepEqualityExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateConversionExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateBitCastExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !checker.BitCastEligibleType(node.OperandType) || !checker.BitCastEligibleType(node.ResultType) || node.OperandType.Bits != node.ResultType.Bits {
		return unknownExpressionDiagnostic("bit cast has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("bit cast result does not match its expected type")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateErrorHeaderExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsError(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.ErrorHeaderText) {
		return unknownExpressionDiagnostic("Error.header has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("Error.header result does not match its expected type")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateErrorKindHeaderExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsErrorKind(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.ErrorHeaderText) {
		return unknownExpressionDiagnostic("ErrorKind.header has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("ErrorKind.header result does not match its expected type")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateModuleValueExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Name == "" || node.ResultType == (compilerTypes.Type{}) {
		return unknownExpressionDiagnostic("module value reference has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("module value reference does not match its expected type")
	}
	return nil
}

func validateEndianConversionExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateTryExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.OperandType.Union == nil {
		return unknownExpressionDiagnostic("try expression has invalid checked metadata")
	}
	if unionMemberIndex(node.OperandType, compilerTypes.ErrorType) != node.MemberIndex {
		return unknownExpressionDiagnostic("try expression error member does not match its source union")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateLayoutExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validateVolatileReadExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.OperandType.Element == nil || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || !compilerTypes.Equal(node.ResultType, node.Element) {
		return unknownExpressionDiagnostic("volatile read has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("volatile read result type does not match its expected type")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateVolatileWriteExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
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
}

func validatePrintExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if len(node.Arguments) == 0 || node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
		return unknownExpressionDiagnostic("print call has invalid checked metadata")
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
}
