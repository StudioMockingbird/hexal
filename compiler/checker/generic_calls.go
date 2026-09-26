// generic_calls.go owns generic call checking: argument inference and
// unification, generic function and method call resolution, and the
// concrete call construction it produces.
package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// inferTypeArguments unifies the open declaration's signature (resolved under
// placeholder parameters) against concrete call argument types, binding each
// parameter to exactly one canonical type.
func inferTypeArguments(open *openGenericFunction, actual []compilerTypes.Type, ctx checkContext) ([]compilerTypes.Type, *compilerTypes.Diagnostic) {
	generics := ctx.names.generics
	previousFrame := generics.frame
	placeholderFrame := make(map[string]compilerTypes.Type, len(open.Parameters))
	placeholders := make([]compilerTypes.Type, open.Generic.Arity)
	for index, parameter := range open.Parameters {
		placeholder := ctx.typeEnvironment.TypeParameter(open.Generic, index)
		placeholders[index] = placeholder
		placeholderFrame[parameter.Lexeme] = placeholder
	}
	generics.frame = mergedFrame(previousFrame, placeholderFrame)
	expected := make([]compilerTypes.Type, 0, len(open.Declaration.Parameters))
	for _, parameter := range open.Declaration.Parameters {
		use, diagnostic := resolveTypeUse(parameter.Type, parameter.Name, ctx.typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return nil, diagnostic
		}
		expected = append(expected, use.Type)
	}
	generics.frame = previousFrame
	rest := len(open.Declaration.Parameters) > 0 && open.Declaration.Parameters[len(open.Declaration.Parameters)-1].Rest
	fixed := len(expected)
	if rest {
		fixed--
		if len(actual) < fixed {
			return nil, diagnosticAt(messageAt(open.Declaration.Name, diag.FunctionArity(open.Name, fixed, len(actual), true)))
		}
	} else if len(expected) != len(actual) {
		return nil, diagnosticAt(messageAt(open.Declaration.Name, diag.FunctionArity(open.Name, len(expected), len(actual), false)))
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range actual {
		expectedIndex := index
		if rest && index >= fixed {
			expectedIndex = fixed
		}
		if !unifyTypes(expected[expectedIndex], actual[index], bindings, open.Generic) {
			conflictingLexeme := open.Parameters[0].Lexeme
			if expected[expectedIndex].Generic != nil && expected[expectedIndex].Generic == open.Generic && expected[expectedIndex].GenericIndex >= 0 && expected[expectedIndex].GenericIndex < len(open.Parameters) {
				conflictingLexeme = open.Parameters[expected[expectedIndex].GenericIndex].Lexeme
			} else {
				for paramIndex, placeholder := range placeholders {
					if typeContainsPlaceholder(expected[expectedIndex], placeholder) {
						conflictingLexeme = open.Parameters[paramIndex].Lexeme
						break
					}
				}
			}
			return nil, diagnosticAt(messageAt(open.Declaration.Name, diag.ConflictingInferredTypes(conflictingLexeme)))
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, diagnosticAt(messageAt(open.Declaration.Name, diag.GenericParameterCannotBeInferred(open.Parameters[index].Lexeme, open.Name)))
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, diagnosticAt(messageAt(open.Declaration.Name, diag.GenericSpecializationHasUnresolvedArguments(open.Name)))
		}
	}
	return bindings, nil
}

// unifyTypes binds placeholders in expected against concrete actual types.
func unifyTypes(expected, actual compilerTypes.Type, bindings []compilerTypes.Type, declaration *compilerTypes.GenericDeclaration) bool {
	if expected.Generic != nil {
		if expected.Generic != declaration {
			return false
		}
		index := expected.GenericIndex
		if index < 0 || index >= len(bindings) {
			return false
		}
		if bindings[index] == (compilerTypes.Type{}) {
			bindings[index] = actual
			return true
		}
		return compilerTypes.Equal(bindings[index], actual)
	}
	if compilerTypes.Equal(expected, actual) {
		return true
	}
	if expected.Element != nil && actual.Element != nil {
		if compilerTypes.ContainsTypeParameter(*expected.Element) || compilerTypes.ContainsTypeParameter(*actual.Element) {
			if expected.PointeeWritable != actual.PointeeWritable && !expected.PointeeWritable {
				return unifyTypes(*expected.Element, *actual.Element, bindings, declaration)
			}
			if expected.PointeeWritable == actual.PointeeWritable {
				return unifyTypes(*expected.Element, *actual.Element, bindings, declaration)
			}
		}
		return false
	}
	expectedMembers, expectedUnion := unionTypeMembers(expected)
	actualMembers, actualUnion := unionTypeMembers(actual)
	if expectedUnion && actualUnion && len(expectedMembers) == len(actualMembers) {
		used := make([]bool, len(expectedMembers))
		for _, actualMember := range actualMembers {
			matched := false
			for index, expectedMember := range expectedMembers {
				if !used[index] && unifyTypes(expectedMember, actualMember, bindings, declaration) {
					used[index] = true
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	}
	return false
}

func typeContainsPlaceholder(typ, placeholder compilerTypes.Type) bool {
	if compilerTypes.Equal(typ, placeholder) {
		return true
	}
	if typ.Element != nil && typeContainsPlaceholder(*typ.Element, placeholder) {
		return true
	}
	if typ.NullableBase != nil && typeContainsPlaceholder(*typ.NullableBase, placeholder) {
		return true
	}
	if typ.Array != nil && typeContainsPlaceholder(typ.Array.Element, placeholder) {
		return true
	}
	if typ.Slice != nil && typeContainsPlaceholder(typ.Slice.Element, placeholder) {
		return true
	}
	if typ.List != nil && typeContainsPlaceholder(typ.List.Element, placeholder) {
		return true
	}
	if typ.Dict != nil && (typeContainsPlaceholder(typ.Dict.Key, placeholder) || typeContainsPlaceholder(typ.Dict.Value, placeholder)) {
		return true
	}
	if typ.Task != nil && typeContainsPlaceholder(typ.Task.Result, placeholder) {
		return true
	}
	if typ.Channel != nil && typeContainsPlaceholder(typ.Channel.Element, placeholder) {
		return true
	}
	if typ.Atomic != nil && typeContainsPlaceholder(typ.Atomic.Element, placeholder) {
		return true
	}
	if typ.Stash != nil && typeContainsPlaceholder(typ.Stash.Element, placeholder) {
		return true
	}
	if typ.Pool != nil && typeContainsPlaceholder(typ.Pool.Element, placeholder) {
		return true
	}
	if typ.Union != nil {
		for _, member := range typ.Union.Members {
			if typeContainsPlaceholder(member, placeholder) {
				return true
			}
		}
	}
	if typ.Signature != nil {
		for _, parameter := range typ.Signature.Parameters {
			if typeContainsPlaceholder(parameter, placeholder) {
				return true
			}
		}
		if typ.Signature.Result != nil && typeContainsPlaceholder(*typ.Signature.Result, placeholder) {
			return true
		}
	}
	if typ.Object != nil {
		for _, member := range typ.Object.Members {
			if typeContainsPlaceholder(member.Type, placeholder) {
				return true
			}
		}
	}
	if typ.Adt != nil {
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if typeContainsPlaceholder(member.Type, placeholder) {
					return true
				}
			}
		}
	}
	return false
}

func unionTypeMembers(typ compilerTypes.Type) ([]compilerTypes.Type, bool) {
	if compilerTypes.IsUnion(typ) {
		members := compilerTypes.UnionMembers(typ)
		result := make([]compilerTypes.Type, 0, members.Len())
		for index := 0; index < members.Len(); index++ {
			member, _ := members.At(index)
			result = append(result, member)
		}
		return result, true
	}
	if compilerTypes.IsNullable(typ) {
		base, _ := compilerTypes.NullableBase(typ)
		return []compilerTypes.Type{base, compilerTypes.Nil}, true
	}
	return nil, false
}

// checkGenericCall infers or validates the type arguments of a generic
// function call, specializes the callee, and returns the checked concrete
// call.
func checkGenericCall(call parser.CallExpression, bound binding, name string, token lexer.Token, ctx checkContext) checkedExpression {
	open := bound.genericFunction
	if open == nil {
		diagnostic := unknownAt(token)
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	var arguments []compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		arguments = make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, token, ctx.typeEnvironment, ctx.names.generics)
			if diagnostic != nil {
				return checkedExpression{token: token, diagnostic: diagnostic}
			}
			arguments = append(arguments, argumentUse.Type)
		}
		if len(arguments) != open.Generic.Arity {
			return checkedExpression{token: token, diagnostic: diagnosticAt(messageAt(token, diag.ExplicitGenericArgumentCountMismatch()))}
		}
		// A nested specialization whose own arguments are open type
		// parameters is substitution-dependent; defer it.
		if genericOpen(ctx) && dependentArgument(arguments) {
			return deferGenericCall(open, call, ctx, token)
		}
	} else {
		argumentTypes := make([]compilerTypes.Type, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			checked := checkValue(argument, ctx)
			if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
				return checkedExpression{token: token, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if checked.typ == (compilerTypes.Type{}) {
				return checkedExpression{token: token, diagnostic: diagnosticAt(messageAt(token, diag.GenericCallCannotInferParameter(name)))}
			}
			argumentTypes = append(argumentTypes, checked.typ)
		}
		// Inferring a specialization from a dependent argument would bind a
		// type parameter to an open placeholder; defer instead.
		if genericOpen(ctx) && dependentArgument(argumentTypes) {
			return deferGenericCall(open, call, ctx, token)
		}
		inferred, diagnostic := inferTypeArguments(open, argumentTypes, ctx)
		if diagnostic != nil {
			return checkedExpression{token: token, diagnostic: diagnostic}
		}
		arguments = inferred
	}
	specialized, diagnostic := specializeFunction(open, arguments, ctx)
	if diagnostic != nil {
		return checkedExpression{token: token, diagnostic: diagnostic}
	}
	return buildConcreteCall(call, specialized, ctx, token)
}

// buildConcreteCall checks a call against a concrete specialized signature and
// builds the checked call node.
func buildConcreteCall(call parser.CallExpression, specialized FunctionDeclaration, ctx checkContext, token lexer.Token) checkedExpression {
	signature := specialized.Type.Signature
	if !aritySatisfied(signature, len(call.Arguments)) {
		return checkedExpression{token: token, diagnostic: diagnosticAt(messageAt(token, arityDiagnostic(specialized.Name, signature, len(call.Arguments))))}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	arguments, diagnostics := checkArgumentsWithRest(specialized.Name, parameterUses, call.Arguments, signature.Rest, token, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: token, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	calleeNode := Expression{Kind: FunctionReferenceExpression, Name: specialized.Name, ResultType: specialized.Type}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	node := Expression{
		Kind:        CallExpression,
		Operand:     &calleeNode,
		Arguments:   arguments,
		OperandType: specialized.Type,
		ResultType:  resultType,
	}
	applyRestMetadata(&node, signature, ctx.typeEnvironment)
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: specialized.Name, Node: node},
		typ:    resultType,
		token:  token,
	}
}

// lookupGenericMethod finds an open generic method whose receiver object
// matches the specialized receiver object.
func lookupGenericMethod(names *scope, object *compilerTypes.ObjectType, name string) *openGenericMethod {
	open, ok := names.generics.objectOpen[object]
	if !ok {
		return nil
	}
	return names.generics.methods[open.Name+"."+name]
}

// checkGenericMethodCall specializes a generic method for the concrete
// receiver and infers or validates the method's own type arguments.
func checkGenericMethodCall(call parser.CallExpression, callee parser.PropertyExpression, open *openGenericMethod, object *compilerTypes.ObjectType, receiver checkedExpression, ctx checkContext) checkedExpression {
	receiverArguments := ctx.names.generics.objectArguments[object]
	if receiverArguments == nil {
		diagnostic := unknownAt(callee.Property)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	var methodArguments []compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		methodArguments = make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, callee.Property, ctx.typeEnvironment, ctx.names.generics)
			if diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
			methodArguments = append(methodArguments, argumentUse.Type)
		}
		if len(methodArguments) != open.Generic.Arity {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(messageAt(callee.Property, diag.ExplicitGenericArgumentCountMismatch()))}
		}
	} else {
		inferred, diagnostic := inferMethodArguments(open, receiverArguments, call.Arguments, callee.Property, ctx)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		methodArguments = inferred
	}
	// A receiver reached through a pointer specializes the method on the
	// pointee struct, never on the pointer: the specialization is keyed by
	// the owner's type arguments alone, so a pointer type here would decide
	// the receiver form for every later call site as well, and would emit a
	// pointer-receiver definition the language does not have.
	// buildConcreteMethodCall's adaptation inserts the one-layer copy.
	receiverValue := receiver.typ
	if receiverValue.Object == nil && receiverValue.Element != nil {
		receiverValue = *receiverValue.Element
	}
	specialized, diagnostic := specializeMethod(open, object, receiverValue, receiverArguments, methodArguments, ctx, ctx.names.generics.methodSpecializations)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	return buildConcreteMethodCall(call, callee, specialized, receiver, ctx)
}

// inferMethodArguments infers a method's own arguments from the call
// arguments under the combined receiver and method parameter frame.
func inferMethodArguments(open *openGenericMethod, receiverArguments []compilerTypes.Type, written []parser.Expression, token lexer.Token, ctx checkContext) ([]compilerTypes.Type, *compilerTypes.Diagnostic) {
	generics := ctx.names.generics
	previousFrame := generics.frame
	frame := parameterFrame(open.ReceiverParameters, receiverArguments)
	placeholders := make([]compilerTypes.Type, open.Generic.Arity)
	for index, parameter := range open.Parameters {
		placeholder := ctx.typeEnvironment.TypeParameter(open.Generic, index)
		placeholders[index] = placeholder
		frame[parameter.Lexeme] = placeholder
	}
	generics.frame = frame
	expected := make([]compilerTypes.Type, 0, len(open.Declaration.Parameters))
	for _, parameter := range open.Declaration.Parameters {
		use, diagnostic := resolveTypeUse(parameter.Type, token, ctx.typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return nil, diagnostic
		}
		expected = append(expected, use.Type)
	}
	generics.frame = previousFrame
	rest := len(open.Declaration.Parameters) > 0 && open.Declaration.Parameters[len(open.Declaration.Parameters)-1].Rest
	fixed := len(expected)
	if rest {
		fixed--
		if len(written) < fixed {
			return nil, diagnosticAt(messageAt(token, diag.FunctionArity(open.Name, fixed, len(written), true)))
		}
	} else if len(expected) != len(written) {
		return nil, diagnosticAt(messageAt(token, diag.FunctionArity(open.Name, len(expected), len(written), false)))
	}
	actual := make([]compilerTypes.Type, 0, len(written))
	for _, argument := range written {
		checked := checkValue(argument, ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return nil, &diagnostics[0]
		}
		actual = append(actual, checked.typ)
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range actual {
		expectedIndex := index
		if rest && index >= fixed {
			expectedIndex = fixed
		}
		if !unifyTypes(expected[expectedIndex], actual[index], bindings, open.Generic) {
			return nil, diagnosticAt(messageAt(token, diag.ConflictingInferredTypes(open.Parameters[expectedIndex].Lexeme)))
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, diagnosticAt(messageAt(token, diag.GenericMethodParameterCannotBeInferred(open.Parameters[index].Lexeme, open.Name)))
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, diagnosticAt(messageAt(token, diag.GenericSpecializationHasUnresolvedArguments(open.Name)))
		}
	}
	return bindings, nil
}

// buildConcreteMethodCall checks a call against a specialized method and
// builds the checked method-call node.
func buildConcreteMethodCall(call parser.CallExpression, callee parser.PropertyExpression, specialized MethodDeclaration, receiver checkedExpression, ctx checkContext) checkedExpression {
	if !parameterAritySatisfied(specialized.Parameters, len(call.Arguments)) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(messageAt(callee.Property, parameterArityDiagnostic(specialized.Name, specialized.Parameters, len(call.Arguments))))}
	}
	adapted, diagnostic := adaptReceiver(receiver, specialized, callee, ctx.typeEnvironment, ctx.names.flow)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	expected := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		expected = append(expected, parameter.TypeUse)
	}
	_, methodRest := parameterRestFixed(specialized.Parameters)
	arguments, diagnostics := checkArgumentsWithRest(specialized.Name, expected, call.Arguments, methodRest, callee.Property, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultType compilerTypes.Type
	if specialized.Result != nil {
		resultType = *specialized.Result
	}
	node := Expression{
		Kind:        MethodCallExpression,
		Name:        specialized.Name,
		Owner:       specialized.Object,
		Operand:     &adapted.Node,
		Arguments:   arguments,
		OperandType: specialized.SelfType,
		ResultType:  resultType,
	}
	applyParameterRestMetadata(&node, specialized.Parameters)
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: specialized.Name, Node: node},
		typ:    resultType,
		token:  callee.Property,
	}
}
