package checker

// Declaration-time structural checking of open generic bodies. The checker
// otherwise checks a generic body only when it is specialized, so a
// never-specialized template could ship an error that holds for every
// substitution. After a module's complete signature-collection pass, each
// reachable open template is checked once with its parameters bound to their
// existing placeholders, in the defining module's environment, with
// substitution-dependent operations deferred to specialization.
//
// The check is diagnostic-only: a successful check commits nothing, and a
// failing check reports diagnostics and leaves the template unavailable for
// specialization. Every mutable checker collection a body check can touch is
// snapshotted and restored, so inspecting an unused template cannot change
// generated output or consume a binding ordinal.

import (
	"fmt"
	"maps"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// genericOpen reports whether the checker is structurally checking an open
// generic body at declaration. A substitution-dependent operation consults
// this to defer to specialization instead of rejecting the open body.
func genericOpen(ctx checkContext) bool {
	return ctx.names.generics != nil && ctx.names.generics.open
}

// deferDependentMemberCall defers a member or method lookup whose receiver is
// an open type parameter: the concrete argument decides whether the member
// exists. Every argument is still checked, so an independent error such as an
// unknown name inside a deferred operation is reported at declaration.
func deferDependentMemberCall(call methodCall) checkedExpression {
	if diagnostics := checkDeferredArguments(call.call.Arguments, call.ctx); len(diagnostics) > 0 {
		return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	return checkedExpression{typ: call.receiver.typ, token: call.callee.Property}
}

// checkDeferredArguments checks each written argument as a value so a name
// resolution failure independent of the deferred operation is still reported.
func checkDeferredArguments(arguments []parser.Expression, ctx checkContext) compilerTypes.Diagnostics {
	for _, argument := range arguments {
		checked := checkValue(argument, ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return diagnostics
		}
	}
	return nil
}

// dependentArgument reports whether any argument type contains an open type
// parameter, making a nested specialization substitution-dependent.
func dependentArgument(arguments []compilerTypes.Type) bool {
	for _, argument := range arguments {
		if compilerTypes.ContainsTypeParameter(argument) {
			return true
		}
	}
	return false
}

// genericCheckSnapshot is the diagnostic-only transaction boundary. Each map
// is cloned so additions and overwrites during a body check are discarded on
// restore, and nextID reverts every binding ordinal the check consumed.
type genericCheckSnapshot struct {
	nextID           BindingID
	open             bool
	frame            map[string]compilerTypes.Type
	functions        map[string]*openGenericFunction
	aliasSpecs       map[string]compilerTypes.Type
	objectSpecs      map[string]compilerTypes.Type
	adtSpecs         map[string]compilerTypes.Type
	objectOpen       map[*compilerTypes.ObjectType]*openGenericType
	objectArguments  map[*compilerTypes.ObjectType][]compilerTypes.Type
	adtOpen          map[*compilerTypes.AdtType]*openGenericType
	adtArguments     map[*compilerTypes.AdtType][]compilerTypes.Type
	typeDeclarations []TypeDeclaration
	functionSpecs    map[string]FunctionDeclaration
	methodSpecs      map[string]MethodDeclaration
	active           map[string]bool
}

func beginGenericCheck(ctx checkContext) genericCheckSnapshot {
	generics := ctx.names.generics
	return genericCheckSnapshot{
		nextID:           *ctx.names.nextID,
		open:             generics.open,
		frame:            generics.frame,
		functions:        maps.Clone(generics.functions),
		aliasSpecs:       maps.Clone(generics.aliasSpecializations),
		objectSpecs:      maps.Clone(generics.objectSpecializations),
		adtSpecs:         maps.Clone(generics.adtSpecializations),
		objectOpen:       maps.Clone(generics.objectOpen),
		objectArguments:  maps.Clone(generics.objectArguments),
		adtOpen:          maps.Clone(generics.adtOpen),
		adtArguments:     maps.Clone(generics.adtArguments),
		typeDeclarations: append([]TypeDeclaration(nil), generics.typeDeclarations...),
		functionSpecs:    maps.Clone(generics.functionSpecializations),
		methodSpecs:      maps.Clone(generics.methodSpecializations),
		active:           maps.Clone(generics.active),
	}
}

func (snapshot genericCheckSnapshot) restore(ctx checkContext) {
	generics := ctx.names.generics
	*ctx.names.nextID = snapshot.nextID
	generics.open = snapshot.open
	generics.frame = snapshot.frame
	generics.functions = snapshot.functions
	generics.aliasSpecializations = snapshot.aliasSpecs
	generics.objectSpecializations = snapshot.objectSpecs
	generics.adtSpecializations = snapshot.adtSpecs
	generics.objectOpen = snapshot.objectOpen
	generics.objectArguments = snapshot.objectArguments
	generics.adtOpen = snapshot.adtOpen
	generics.adtArguments = snapshot.adtArguments
	generics.typeDeclarations = snapshot.typeDeclarations
	generics.functionSpecializations = snapshot.functionSpecs
	generics.methodSpecializations = snapshot.methodSpecs
	generics.active = snapshot.active
}

// checkOpenGenericDeclarations structurally checks every open generic template
// declared by this module, in source order, after pass 2 collected every
// module-level signature. It returns every independent diagnostic; a non-empty
// result leaves the offending template unavailable for specialization.
func checkOpenGenericDeclarations(items []parser.TopLevelItem, ctx checkContext) compilerTypes.Diagnostics {
	generics := ctx.names.generics
	if generics == nil {
		return nil
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, item := range items {
		switch declaration := item.(type) {
		case parser.Declaration:
			// A module-level inferred fixed generic literal is declaration
			// sugar for a named generic function; pass 2 registered it under
			// the binding's name.
			literal, isSugar := directFunctionLiteralSugar(declaration)
			if !isSugar || len(literal.TypeParameters) == 0 {
				continue
			}
			open, ok := generics.functions[declaration.Name.Lexeme]
			if !ok || open.local {
				continue
			}
			diagnostics = append(diagnostics, checkOpenGenericFunction(open, ctx)...)
		case parser.FunctionDeclaration:
			open, ok := generics.functions[declaration.Name.Lexeme]
			if !ok || open.local || len(declaration.TypeParameters) == 0 {
				continue
			}
			diagnostics = append(diagnostics, checkOpenGenericFunction(open, ctx)...)
		case parser.MethodDeclaration:
			objectName := genericMethodObjectName(declaration)
			if objectName == "" {
				continue
			}
			open, ok := generics.methods[objectName+"."+declaration.Name.Lexeme]
			if !ok {
				continue
			}
			diagnostics = append(diagnostics, checkOpenGenericMethod(open, ctx)...)
		case parser.TypeDeclaration:
			open, ok := generics.types[declaration.Name.Lexeme]
			if !ok || len(declaration.Parameters) == 0 {
				continue
			}
			diagnostics = append(diagnostics, checkOpenGenericType(open, ctx)...)
		}
	}
	return diagnostics
}

// genericMethodObjectName returns the owner type name of a generic method
// declaration, or "" when the declaration is not a generic method.
func genericMethodObjectName(declaration parser.MethodDeclaration) string {
	if len(declaration.TypeParameters) == 0 && !isGenericReceiver(declaration.SelfType) {
		return ""
	}
	if receiver, ok := declaration.SelfType.(parser.GenericTypeExpression); ok {
		return receiver.Name.Lexeme
	}
	return ""
}

// placeholderTypes binds every declared parameter to its existing
// type-parameter placeholder.
func placeholderTypes(open *openGenericType, environment *compilerTypes.Environment) ([]compilerTypes.Type, map[string]compilerTypes.Type) {
	placeholders := make([]compilerTypes.Type, len(open.Parameters))
	frame := make(map[string]compilerTypes.Type, len(open.Parameters))
	for index, parameter := range open.Parameters {
		placeholder := environment.TypeParameter(open.Declaration, index)
		placeholders[index] = placeholder
		frame[parameter.Lexeme] = placeholder
	}
	return placeholders, frame
}

// functionPlaceholderFrame is placeholderTypes for a function or method
// template, whose arity lives on the Generic declaration.
func functionPlaceholderFrame(parameters []lexer.Token, generic *compilerTypes.GenericDeclaration, environment *compilerTypes.Environment) ([]compilerTypes.Type, map[string]compilerTypes.Type) {
	placeholders := make([]compilerTypes.Type, len(parameters))
	frame := make(map[string]compilerTypes.Type, len(parameters))
	for index, parameter := range parameters {
		placeholder := environment.TypeParameter(generic, index)
		placeholders[index] = placeholder
		frame[parameter.Lexeme] = placeholder
	}
	return placeholders, frame
}

// checkOpenGenericFunction structurally checks one open generic function body.
func checkOpenGenericFunction(open *openGenericFunction, ctx checkContext) compilerTypes.Diagnostics {
	snapshot := beginGenericCheck(ctx)
	generics := ctx.names.generics
	defer snapshot.restore(ctx)
	generics.open = true

	_, frame := functionPlaceholderFrame(open.Parameters, open.Generic, ctx.typeEnvironment)
	generics.frame = mergedFrame(generics.frame, frame)
	parameters, parameterDiagnostics := checkParameters(open.Declaration.Parameters, ctx.typeEnvironment, generics)
	result, resultUse, resultDiagnostics := checkResultType(open.Declaration.Return, open.Declaration.Name, ctx.typeEnvironment, generics)
	if len(parameterDiagnostics) > 0 {
		return parameterDiagnostics
	}
	if len(resultDiagnostics) > 0 {
		return resultDiagnostics
	}
	body := &scope{
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
	body.owner = open.Name
	body.result = result
	body.resultUse = resultUse
	if ctx.names.isEntryModule() {
		body.capture = &captureState{allowed: true, bindings: make(map[string]binding)}
	}
	body.envDependent = ctx.names.envDependent
	body.envCaptures = ctx.names.envCaptures
	body.initializedRoots = ctx.names.initializedRoots
	body.rootIndex = 1 << 30
	for index := range parameters {
		parameters[index].Binding = ctx.names.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	statements, bodyDiagnostics := checkBody(open.Declaration.Body, checkContext{names: body, typeEnvironment: ctx.typeEnvironment})
	if len(bodyDiagnostics) > 0 {
		return bodyDiagnostics
	}
	if result != nil && FallsThrough(statements) {
		diagnostic := typeErrorAt(open.Declaration.End, fmt.Sprintf("returning %s may fall through without returning %s", open.Name, result.Name))
		return compilerTypes.Diagnostics{diagnostic}
	}
	return nil
}

// checkOpenGenericMethod structurally checks one open generic method body.
func checkOpenGenericMethod(open *openGenericMethod, ctx checkContext) compilerTypes.Diagnostics {
	snapshot := beginGenericCheck(ctx)
	generics := ctx.names.generics
	defer snapshot.restore(ctx)
	generics.open = true

	receiverArguments, receiverFrame := placeholderTypes(open.Object, ctx.typeEnvironment)
	receiverUse, receiverDiagnostic := specializeTypeUseArguments(open.Object, receiverArguments, open.Declaration.Name, ctx.typeEnvironment, generics)
	if receiverDiagnostic != nil {
		return compilerTypes.Diagnostics{*receiverDiagnostic}
	}
	receiverType := receiverUse.Type
	if receiverType.Object == nil {
		diagnostic := unknownAt(open.Declaration.Name, "generic method receiver is not an object")
		return compilerTypes.Diagnostics{diagnostic}
	}
	generics.frame = mergedFrame(generics.frame, receiverFrame)
	_, methodFrame := functionPlaceholderFrame(open.Parameters, open.Generic, ctx.typeEnvironment)
	generics.frame = mergedFrame(generics.frame, methodFrame)
	if diagnostic := methodReceiverCopyDiagnostic(receiverType, open.Declaration.Keyword); diagnostic != nil {
		return compilerTypes.Diagnostics{*diagnostic}
	}
	parameters, parameterDiagnostics := checkParameters(open.Declaration.Parameters, ctx.typeEnvironment, generics)
	result, resultUse, resultDiagnostics := checkResultType(open.Declaration.Return, open.Declaration.Name, ctx.typeEnvironment, generics)
	if len(parameterDiagnostics) > 0 {
		return parameterDiagnostics
	}
	if len(resultDiagnostics) > 0 {
		return resultDiagnostics
	}
	selfID := ctx.names.newBindingID()
	self := receiverType
	body := &scope{
		module:     ctx.names.module,
		local:      make(map[string]binding, len(parameters)),
		owner:      open.Name,
		result:     result,
		resultUse:  resultUse,
		methods:    ctx.names.methods,
		self:       &self,
		selfID:     selfID,
		function:   true,
		nextID:     ctx.names.nextID,
		flow:       newFlowState(),
		generics:   generics,
		registry:   ctx.names.registry,
		moduleID:   ctx.names.moduleID,
		logicalKey: ctx.names.logicalKey,
	}
	if ctx.names.isEntryModule() {
		body.capture = &captureState{allowed: true, bindings: make(map[string]binding)}
	}
	body.envDependent = ctx.names.envDependent
	body.envCaptures = ctx.names.envCaptures
	body.initializedRoots = ctx.names.initializedRoots
	body.rootIndex = 1 << 30
	for index := range parameters {
		parameters[index].Binding = ctx.names.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	statements, bodyDiagnostics := checkBody(open.Declaration.Body, checkContext{names: body, typeEnvironment: ctx.typeEnvironment})
	if len(bodyDiagnostics) > 0 {
		return bodyDiagnostics
	}
	if result != nil && FallsThrough(statements) {
		diagnostic := typeErrorAt(open.Declaration.End, fmt.Sprintf("returning %s may fall through without returning %s", open.Name, result.Name))
		return compilerTypes.Diagnostics{diagnostic}
	}
	return nil
}

// checkOpenGenericType structurally checks one generic type layout. Alias,
// object-member, and ADT-payload types resolve under the parameter frame, so
// an unknown independent name is rejected at declaration; a layout that uses a
// parameter in a position valid for some substitution is deferred. The
// provisional open layout is rolled back and never emitted.
func checkOpenGenericType(open *openGenericType, ctx checkContext) compilerTypes.Diagnostics {
	snapshot := beginGenericCheck(ctx)
	generics := ctx.names.generics
	defer snapshot.restore(ctx)

	placeholders, frame := placeholderTypes(open, ctx.typeEnvironment)
	generics.frame = mergedFrame(generics.frame, frame)
	_, diagnostic := specializeTypeUseArguments(open, placeholders, openTargetToken(open), ctx.typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.Diagnostics{*diagnostic}
	}
	return nil
}

// openTargetToken names a generic type template's declaration position for a
// layout diagnostic; the template keeps no token of its own.
func openTargetToken(open *openGenericType) lexer.Token {
	if len(open.Parameters) > 0 {
		return open.Parameters[0]
	}
	return lexer.Token{}
}

// resolveGenericCallArguments checks a nested generic call's written arguments
// for independent errors without performing the specialization.
func resolveGenericCallArguments(call parser.CallExpression, ctx checkContext) compilerTypes.Diagnostics {
	return checkDeferredArguments(call.Arguments, ctx)
}

// deferGenericCall defers a nested generic call whose own arguments are open
// type parameters. Every written argument is still checked for an independent
// error, and the result is a placeholder so an enclosing dependent check also
// defers.
func deferGenericCall(open *openGenericFunction, call parser.CallExpression, ctx checkContext, token lexer.Token) checkedExpression {
	if diagnostics := resolveGenericCallArguments(call, ctx); len(diagnostics) > 0 {
		return checkedExpression{token: token, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultType compilerTypes.Type
	if open.Generic != nil && open.Generic.Arity > 0 {
		resultType = ctx.typeEnvironment.TypeParameter(open.Generic, 0)
	}
	return checkedExpression{typ: resultType, token: token}
}
