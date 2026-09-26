// generic_specialization.go owns template specialization: function and
// method specialization from explicit or expected argument lists, with its
// diagnostic propagation.
package checker

import (
	diag "hexal/compiler/diagnostics"
	"strings"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// specializeFunction creates or reuses the concrete declaration for one
// specialization of a generic function. The signature is resolved under the
// argument frame and the body is re-checked with concrete types, so dependent
// operations are validated at specialization time. The record is cached in
// the requesting module's own table; imports of another module's generic go
// through specializeFunctionIn with the defining module's collection
// instead.
func specializeFunction(open *openGenericFunction, arguments []compilerTypes.Type, ctx checkContext) (FunctionDeclaration, *compilerTypes.Diagnostic) {
	return specializeFunctionIn(open, arguments, ctx, ctx.names.generics.functionSpecializations)
}

// specializeFunctionIn is the shared specialization engine behind
// specializeFunction and the imported-generic path. It re-checks the
// template's signature and body under concrete arguments and caches the
// resulting declaration in collection -- the requesting module's own table
// for a local generic, or the defining module's registry collection for an
// imported one. The requesting module's generic table still supplies the
// parameter frame, recursion guards, and binding ids, so one specialization
// runs entirely in the requesting module's type environment.
func specializeFunctionIn(open *openGenericFunction, arguments []compilerTypes.Type, ctx checkContext, collection map[string]FunctionDeclaration) (FunctionDeclaration, *compilerTypes.Diagnostic) {
	generics := ctx.names.generics
	stem := open.templateKey()
	key := specializeKey(stem, arguments)
	if collection == nil {
		diagnostic := unknownAt(open.Declaration.Name)
		return FunctionDeclaration{}, &diagnostic
	}
	if cached, ok := collection[key]; ok {
		return cached, nil
	}
	for activeKey := range generics.active {
		if strings.HasPrefix(activeKey, stem+"|") && activeKey != key {
			return FunctionDeclaration{}, diagnosticAt(messageAt(open.Declaration.Name, diag.GenericSpecializationChangesArguments()))
		}
	}
	previousFrame := generics.frame
	// Merge rather than replace: a local generic template nested inside an
	// already-specializing enclosing generic function or method must still
	// resolve the enclosing type parameter, exactly as a non-generic nested
	// declaration already does through the shared generics table.
	generics.frame = mergedFrame(previousFrame, parameterFrame(open.Parameters, arguments))
	parameters, parameterDiagnostics := checkParameters(open.Declaration.Parameters, ctx.typeEnvironment, generics)
	result, resultUse, resultDiagnostics := checkResultType(open.Declaration.Return, open.Declaration.Name, ctx.typeEnvironment, generics)
	if len(parameterDiagnostics) > 0 {
		generics.frame = previousFrame
		return FunctionDeclaration{}, &parameterDiagnostics[0]
	}
	if len(resultDiagnostics) > 0 {
		generics.frame = previousFrame
		return FunctionDeclaration{}, &resultDiagnostics[0]
	}
	rest := len(parameters) > 0 && parameters[len(parameters)-1].Rest
	parameterTypes := make([]compilerTypes.Type, 0, len(parameters))
	for _, parameter := range parameters {
		parameterTypes = append(parameterTypes, parameter.Type)
	}
	if rest {
		parameterTypes[len(parameterTypes)-1] = parameters[len(parameters)-1].RestElement
	}
	functionType := ctx.typeEnvironment.FunTypeRest(parameterTypes, result, rest)
	if functionType.Signature == nil {
		generics.frame = previousFrame
		diagnostic := unknownAt(open.Declaration.Name)
		return FunctionDeclaration{}, &diagnostic
	}
	specialized := FunctionDeclaration{
		Name:       specializeFunctionName(open.generatedStem(), arguments),
		Parameters: parameters,
		Result:     result,
		ResultUse:  resultUse,
		Type:       functionType,
		Span:       open.Declaration.Name.Span,
	}
	collection[key] = specialized
	generics.active[key] = true
	generics.open = false
	var body *scope
	if open.local {
		// A local template's self-recursion binding lives in an enclosing
		// block's local map, not the module frame; only a scope chained to
		// names can reach it, and the same chain is what gives its body the
		// closed-function capture rule every other local declaration has.
		body = ctx.names.closureRootScope(specialized.Name, ctx.names.isEntryModule())
	} else {
		body = &scope{
			module:     ctx.names.module,
			local:      make(map[string]binding, len(parameters)),
			methods:    ctx.names.methods,
			function:   true,
			nextID:     ctx.names.nextID,
			flow:       newFlowState(),
			generics:   generics,
			registry:   ctx.names.registry,
			moduleID:   ctx.names.moduleID,
			logicalKey: ctx.names.logicalKey,
		}
		body.owner = specialized.Name
		if ctx.names.isEntryModule() {
			body.capture = &captureState{allowed: true, bindings: make(map[string]binding)}
		}
		body.rootIndex = ctx.rootIndex
		body.envDependent = ctx.names.envDependent
		body.envCaptures = ctx.names.envCaptures
		body.initializedRoots = ctx.names.initializedRoots
	}
	body.result = result
	body.resultUse = resultUse
	for index := range parameters {
		parameters[index].Binding = ctx.names.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	statements, bodyDiagnostics := checkBody(open.Declaration.Body, checkContext{names: body, typeEnvironment: ctx.typeEnvironment})
	generics.frame = previousFrame
	delete(generics.active, key)
	generics.open = true
	if len(bodyDiagnostics) > 0 {
		return FunctionDeclaration{}, &bodyDiagnostics[0]
	}
	if result != nil && FallsThrough(statements) {
		return FunctionDeclaration{}, diagnosticAt(messageAt(open.Declaration.End, diag.GenericFunctionMayFallThrough(specialized.Name, result.Name)))
	}
	specialized.Body = statements
	specialized.Captures = capturesOf(body.capture)
	specialized.EnvDependent = len(specialized.Captures) > 0
	specialized.DirectCallees = directCallees(open.Declaration.Body)
	inheritEnvironment(&specialized.EnvDependent, &specialized.Captures, specialized.DirectCallees, ctx.names)
	collection[key] = specialized
	return specialized, nil
}

// specializeMethod creates or reuses the concrete declaration for one
// specialization of a generic method. The record is cached in collection --
// the requesting module's own table for a local generic method, or the
// defining module's registry collection (registry.methodSpecializationStore)
// for an imported one, exactly like specializeFunctionIn.
func specializeMethod(open *openGenericMethod, receiverObject *compilerTypes.ObjectType, receiverType compilerTypes.Type, receiverArguments []compilerTypes.Type, methodArguments []compilerTypes.Type, ctx checkContext, collection map[string]MethodDeclaration) (MethodDeclaration, *compilerTypes.Diagnostic) {
	generics := ctx.names.generics
	key := open.ObjectName + "|" + argumentNames(receiverArguments) + "|" + open.Name + "|" + argumentNames(methodArguments)
	if cached, ok := collection[key]; ok {
		return cached, nil
	}
	previousFrame := generics.frame
	frame := parameterFrame(open.ReceiverParameters, receiverArguments)
	for index, parameter := range open.Parameters {
		if index < len(methodArguments) {
			frame[parameter.Lexeme] = methodArguments[index]
		}
	}
	// Merged, not replaced, for the same reason as specializeFunctionIn: a
	// method cannot itself nest inside anything, but a local generic
	// function or literal declared in its body can, and that inner template
	// specializes while this frame is still active.
	generics.frame = mergedFrame(previousFrame, frame)
	if diagnostic := methodReceiverCopyDiagnostic(receiverType, open.Declaration.Keyword); diagnostic != nil {
		generics.frame = previousFrame
		return MethodDeclaration{}, diagnostic
	}
	parameters, parameterDiagnostics := checkParameters(open.Declaration.Parameters, ctx.typeEnvironment, generics)
	result, resultUse, resultDiagnostics := checkResultType(open.Declaration.Return, open.Declaration.Name, ctx.typeEnvironment, generics)
	if len(parameterDiagnostics) > 0 {
		generics.frame = previousFrame
		return MethodDeclaration{}, &parameterDiagnostics[0]
	}
	if len(resultDiagnostics) > 0 {
		generics.frame = previousFrame
		return MethodDeclaration{}, &resultDiagnostics[0]
	}
	methodName := open.Name
	if len(open.Parameters) > 0 {
		methodName = specializeFunctionName(open.Name, methodArguments)
	}
	specialized := MethodDeclaration{
		Name:       methodName,
		Object:     receiverObject,
		SelfType:   receiverType,
		Parameters: parameters,
		Result:     result,
		ResultUse:  resultUse,
		Span:       open.Declaration.Name.Span,
	}
	collection[key] = specialized
	generics.active[key] = true
	generics.open = false
	selfID := ctx.names.newBindingID()
	specialized.SelfBinding = selfID
	body := &scope{
		module:     ctx.names.module,
		local:      make(map[string]binding, len(parameters)),
		owner:      methodName,
		result:     result,
		resultUse:  resultUse,
		methods:    ctx.names.methods,
		self:       &specialized.SelfType,
		selfID:     selfID,
		function:   true,
		nextID:     ctx.names.nextID,
		flow:       newFlowState(),
		generics:   generics,
		registry:   ctx.names.registry,
		moduleID:   ctx.names.moduleID,
		logicalKey: ctx.names.logicalKey,
	}
	for index := range parameters {
		parameters[index].Binding = ctx.names.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	statements, bodyDiagnostics := checkBody(open.Declaration.Body, checkContext{names: body, typeEnvironment: ctx.typeEnvironment})
	generics.frame = previousFrame
	delete(generics.active, key)
	generics.open = true
	if len(bodyDiagnostics) > 0 {
		return MethodDeclaration{}, &bodyDiagnostics[0]
	}
	if result != nil && FallsThrough(statements) {
		return MethodDeclaration{}, diagnosticAt(messageAt(open.Declaration.End, diag.GenericFunctionMayFallThrough(methodName, result.Name)))
	}
	specialized.Body = statements
	specialized.Captures = capturesOf(body.capture)
	specialized.EnvDependent = len(specialized.Captures) > 0
	specialized.DirectCallees = directCallees(open.Declaration.Body)
	inheritEnvironment(&specialized.EnvDependent, &specialized.Captures, specialized.DirectCallees, ctx.names)
	collection[key] = specialized
	return specialized, nil
}

func argumentNames(arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.CanonicalKey
	}
	return strings.Join(names, ",")
}

// checkGenericFunctionReference infers a generic function's arguments from an
// exact Fun<...> expected type and returns the concrete reference. It returns
// nil when the name is not a generic function.
func checkGenericFunctionReference(name lexer.Token, expected compilerTypes.Type, ctx checkContext) (*checkedExpression, *compilerTypes.Diagnostic) {
	bound, status := ctx.names.lookup(name.Lexeme)
	if status != nameFound || bound.kind != genericFunctionBinding {
		return nil, nil
	}
	open := bound.genericFunction
	if open == nil {
		diagnostic := unknownAt(name)
		return nil, &diagnostic
	}
	specialized, diagnostic := specializeFromExpectedType(open, expected, name, ctx)
	if diagnostic != nil {
		return nil, diagnostic
	}
	reference := checkedExpression{
		source: Operand{
			Kind: VariableOperand,
			Type: specialized.Type,
			Name: specialized.Name,
			Node: Expression{Kind: FunctionReferenceExpression, Name: specialized.Name, ResultType: specialized.Type},
		},
		typ:      specialized.Type,
		token:    name,
		function: true,
	}
	return &reference, nil
}

// specializeFromExpectedType infers open's type arguments by unifying its
// written signature (resolved under placeholder parameters) against an exact
// expected Fun<...> type, then specializes it. It is the shared contextual-
// specialization engine behind a bare generic function reference and a
// generic anonymous literal used where an exact Fun<...> type is expected;
// fallback names the token diagnostics anchor to when open has no useful
// position of its own (an anonymous literal has no declared name).
func specializeFromExpectedType(open *openGenericFunction, expected compilerTypes.Type, fallback lexer.Token, ctx checkContext) (FunctionDeclaration, *compilerTypes.Diagnostic) {
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
	expectedTypes := make([]compilerTypes.Type, 0, len(open.Declaration.Parameters))
	for _, parameter := range open.Declaration.Parameters {
		use, diagnostic := resolveTypeUse(parameter.Type, fallback, ctx.typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return FunctionDeclaration{}, diagnostic
		}
		expectedTypes = append(expectedTypes, use.Type)
	}
	var expectedResult compilerTypes.Type
	hasResult := false
	if open.Declaration.Return != nil {
		use, diagnostic := resolveTypeUse(open.Declaration.Return, fallback, ctx.typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return FunctionDeclaration{}, diagnostic
		}
		expectedResult = use.Type
		hasResult = true
	}
	generics.frame = previousFrame
	signature := expected.Signature
	literalRest := len(open.Declaration.Parameters) > 0 && open.Declaration.Parameters[len(open.Declaration.Parameters)-1].Rest
	if signature == nil || len(signature.Parameters) != len(expectedTypes) || (signature.Result == nil) != !hasResult || signature.Rest != literalRest {
		return FunctionDeclaration{}, diagnosticAt(messageAt(fallback, diag.GenericCallCannotInferParameter(open.Name)))
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range expectedTypes {
		if !unifyTypes(expectedTypes[index], signature.Parameters[index], bindings, open.Generic) {
			return FunctionDeclaration{}, diagnosticAt(messageAt(fallback, diag.ConflictingInferredTypes(open.Parameters[index].Lexeme)))
		}
	}
	if hasResult {
		if !unifyTypes(expectedResult, *signature.Result, bindings, open.Generic) {
			conflictingIndex := 0
			if expectedResult.Generic != nil && expectedResult.Generic == open.Generic && expectedResult.GenericIndex >= 0 && expectedResult.GenericIndex < len(open.Parameters) {
				conflictingIndex = expectedResult.GenericIndex
			} else {
				for index, placeholder := range placeholders {
					if typeContainsPlaceholder(expectedResult, placeholder) {
						conflictingIndex = index
						break
					}
				}
			}
			return FunctionDeclaration{}, diagnosticAt(messageAt(fallback, diag.ConflictingInferredTypes(open.Parameters[conflictingIndex].Lexeme)))
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return FunctionDeclaration{}, diagnosticAt(messageAt(fallback, diag.GenericParameterCannotBeInferred(open.Parameters[index].Lexeme, open.Name)))
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return FunctionDeclaration{}, diagnosticAt(messageAt(fallback, diag.GenericSpecializationHasUnresolvedArguments(open.Name)))
		}
	}
	return specializeFunction(open, bindings, ctx)
}
