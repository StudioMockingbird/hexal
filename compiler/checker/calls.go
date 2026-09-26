package checker

import (
	"maps"
	"slices"

	"hexal/compiler/corelib"
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// builtinConstructible resolves the bare-constructor set from the registry. It
// is a variable so a test can prove the dispatch is registry-driven: clearing
// it must drop a canonical constructor out of the accepted programs.
var builtinConstructible = specdata.BareConstructible

func checkCallStatement(call parser.CallExpression, ctx checkContext) (CallStatement, compilerTypes.Diagnostics) {
	checked := checkCall(call, compilerTypes.Type{}, ctx)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return CallStatement{}, diagnostics
	}
	return CallStatement{
		Call: checked.source,
		Span: checked.token.Span,
	}, nil
}

// checkCall resolves a callee, checks arity, and checks each argument in its
// parameter's expected-type position so contextual literals and Ptr<mut T>-to-Ptr
// weakening both apply. The returned type is the zero Type for a no-return
// callee; only a call statement accepts that. expectedType is the enclosing
// expression's contextual type, used only to infer a generic ADT owner's
// arguments when a qualified variant constructor omits them explicitly; it is
// the zero Type where no such context exists (statement position, spawn,
// deferred actions).
func checkCall(call parser.CallExpression, expectedType compilerTypes.Type, ctx checkContext) checkedExpression {
	if property, isMethod := call.Callee.(parser.PropertyExpression); isMethod {
		return checkMethodCall(call, property, expectedType, ctx)
	}
	callee, ok := call.Callee.(parser.VariableExpression)
	if !ok {
		// A callee that is neither a name nor a method selection is an
		// ordinary expression: an anonymous function literal invoked
		// directly, or any other expression whose checked type is Fun<...>.
		return checkIndirectCall(call, ctx)
	}
	if callee.Name.Kind == lexer.Self {
		return checkedExpression{token: callee.Name, diagnostic: selfNotBoundDiagnostic(callee.Name)}
	}
	if constructed, ok := checkBareConstructorCall(call, callee, expectedType, ctx); ok {
		return constructed
	}
	if diagnostic := rejectNamedArguments(call); diagnostic != nil {
		return checkedExpression{token: callee.Name, diagnostic: diagnostic}
	}
	// The protected builtin `print` resolves before ordinary
	// free-function lookup and cannot be redeclared or referenced as a
	// value.
	if callee.Name.Lexeme == "print" {
		return checkPrintCall(call, callee.Name, ctx)
	}
	// The protected layout queries resolve before ordinary
	// free-function lookup.
	if layoutBuiltins[callee.Name.Lexeme] {
		return checkLayoutCall(call, callee.Name, ctx)
	}

	name := callee.Name.Lexeme
	bound, status := ctx.names.lookup(name)
	switch status {
	case nameMissing:
		if hint, moved := corelib.ConstructorHint(name); moved {
			diagnostic := coreConstructorMigrationDiagnostic(callee.Name, hint)
			return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
		}
		diagnostic := messageAt(callee.Name, diag.UnknownFunction(name))
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	case nameModuleData:
		diagnostic := moduleDataDiagnostic(ctx.names.owner, name, callee.Name)
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	// A root direct call may enter an environment-dependent call graph only
	// after every binding that graph captures is initialized. Captures are
	// visited in lexical order so the reported name is stable across
	// compilations of the same source.
	if !ctx.names.inFunction() && bound.kind == functionBinding && ctx.names.envDependent[name] {
		for _, capture := range slices.Sorted(maps.Keys(ctx.names.envCaptures[name])) {
			if !ctx.names.initializedRoots[capture] {
				diagnostic := messageAt(callee.Name, diag.FunctionCapturesUninitializedBinding(name, capture))
				return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
			}
		}
	}
	if bound.kind == genericFunctionBinding {
		return checkGenericCall(call, bound, name, callee.Name, ctx)
	}
	// A call resolves the callee's effective type from the branch-local
	// flow facts, so a null test can narrow a nullable Fun<...> binding to
	// its callable member. The declared binding itself still holds the
	// nullable storage type.
	calleeType := bound.typ
	if bound.kind != functionBinding {
		if narrowed, ok := ctx.names.flow.narrowedType(bound.id); ok {
			calleeType = narrowed
		}
	}
	// A binding whose type is an open type parameter has no known signature:
	// callability and arity are substitution-dependent, so defer them while
	// still checking each argument for an independent error.
	if genericOpen(ctx) && compilerTypes.ContainsTypeParameter(calleeType) {
		if diagnostics := checkDeferredArguments(call.Arguments, ctx); len(diagnostics) > 0 {
			return checkedExpression{token: callee.Name, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		return checkedExpression{typ: calleeType, token: callee.Name}
	}
	signature := calleeType.Signature
	if signature == nil {
		diagnostic := messageAt(callee.Name, diag.ValueNotCallable(name))
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if compilerTypes.IsNullable(calleeType) {
		// A nullable function pointer holds a Fun member only after a null
		// test proved it; calling the union itself could jump through nil.
		diagnostic := messageAt(callee.Name, diag.NullableFunctionNeedsNarrowing(calleeType.Name))
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if !aritySatisfied(signature, len(call.Arguments)) {
		diagnostic := messageAt(callee.Name, arityDiagnostic(name, signature, len(call.Arguments)))
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}

	parameterUses := bound.use.Parameters
	if len(parameterUses) != len(signature.Parameters) {
		parameterUses = make([]compilerTypes.TypeUse, len(signature.Parameters))
		for index, parameter := range signature.Parameters {
			parameterUses[index] = compilerTypes.NewTypeUse(parameter)
		}
	}
	arguments, diagnostics := checkArgumentsWithRest(name, parameterUses, call.Arguments, signature.Rest, callee.Name, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Name, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}

	// A direct foreign call requires lexical permission from unsafe do ... end.
	// The gate runs after ordinary resolution so an invalid argument, result,
	// or name keeps its earlier diagnostic.
	if bound.kind == foreignFunctionBinding {
		if diagnostic := requireUnsafe(ctx, callee.Name, unsafeForeignCall, name); diagnostic != nil {
			return checkedExpression{token: callee.Name, diagnostic: diagnostic}
		}
	}

	calleeNode := variableNodeWithBinding(name, bound.id)
	if bound.kind == functionBinding {
		calleeNode = Expression{Kind: FunctionReferenceExpression, Name: name, LocalHelperOrdinal: bound.localHelperOrdinal, ResultType: bound.typ}
	} else if bound.kind == foreignFunctionBinding {
		calleeNode = Expression{Kind: ForeignFunctionReferenceExpression, Name: name, ForeignCName: bound.foreignCName, ForeignParameters: bound.foreignParameters, ForeignResult: bound.foreignResult, ResultType: bound.typ}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	node := Expression{
		Kind:        CallExpression,
		Operand:     &calleeNode,
		Arguments:   arguments,
		OperandType: calleeType,
		ResultType:  resultType,
	}
	applyRestMetadata(&node, signature, ctx.typeEnvironment)
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node},
		typ:    resultType,
		token:  callee.Name,
	}
}

// checkQualifiedFunctionCall resolves Alias.name(args) where Alias is an
// import alias: the callee is the target module's exported function. The call
// is checked against the recorded signature exactly like a local call; the
// node carries the target module id for the downstream stage. A name that is
// not
// an exported concrete function may be an exported generic template, which
// the call specializes against the defining module's collection; only then is
// it the visibility failure.
func checkQualifiedFunctionCall(call parser.CallExpression, property lexer.Token, target string, ctx checkContext) checkedExpression {
	if foreign, ok := ctx.names.registry.exportedForeignFunction(target, property.Lexeme); ok {
		return checkQualifiedForeignCall(call, foreign, property, target, ctx)
	}
	function, ok := ctx.names.registry.exportedFunction(target, property.Lexeme)
	if !ok {
		if open, generic := ctx.names.registry.genericFunction(target, property.Lexeme); generic {
			return checkQualifiedGenericCall(call, open, property, target, ctx)
		}
		if display, isCImport := ctx.names.registry.cImportHeader(target); isCImport {
			diagnostic := missingCImportDeclarationDiagnostic(property, display, property.Lexeme)
			if mapped, mappedOK := ctx.names.registry.mappedCName(target, property.Lexeme); mappedOK {
				diagnostic = mappedCDeclarationDiagnostic(property, property.Lexeme, mapped)
			}
			return checkedExpression{token: property, diagnostic: &diagnostic}
		}
		diagnostic := privateToModuleDiagnostic(property, property.Lexeme, target)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	signature := function.Type.Signature
	if signature == nil {
		diagnostic := unknownAt(property)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	if !aritySatisfied(signature, len(call.Arguments)) {
		diagnostic := messageAt(property, arityDiagnostic(function.Name, signature, len(call.Arguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	arguments, diagnostics := checkArgumentsWithRest(function.Name, parameterUses, call.Arguments, signature.Rest, property, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	calleeNode := Expression{Kind: FunctionReferenceExpression, Name: function.Name, ResultType: function.Type, Module: target}
	node := Expression{
		Kind:        CallExpression,
		Operand:     &calleeNode,
		Arguments:   arguments,
		OperandType: function.Type,
		ResultType:  resultType,
	}
	applyRestMetadata(&node, signature, ctx.typeEnvironment)
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: function.Name, Node: node},
		typ:    resultType,
		token:  property,
	}
}

// checkQualifiedForeignCall checks Alias.name(args) where the target module
// exports a handwritten foreign function. The call lowers to the recorded C
// symbol in the importer's own translation unit, which therefore also records
// the defining header as a dependency.
func checkQualifiedForeignCall(call parser.CallExpression, function ForeignFunctionDeclaration, property lexer.Token, target string, ctx checkContext) checkedExpression {
	signature := function.Type.Signature
	if signature == nil {
		diagnostic := unknownAt(property)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	if diagnostic := rejectNamedArguments(call); diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	if len(call.Arguments) != len(signature.Parameters) {
		diagnostic := messageAt(property, diag.FunctionArity(function.Name, len(signature.Parameters), len(call.Arguments), false))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	arguments, diagnostics := checkArguments(function.Name, parameterUses, call.Arguments, property, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if diagnostic := requireUnsafe(ctx, property, unsafeForeignCall, function.Name); diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	calleeNode := Expression{Kind: ForeignFunctionReferenceExpression, Name: function.Name, ForeignCName: function.CName, ForeignParameters: foreignParameterSpellings(function.Parameters), ForeignResult: function.ResultCName, ResultType: function.Type, Module: target}
	node := Expression{Kind: CallExpression, Operand: &calleeNode, Arguments: arguments, OperandType: function.Type, ResultType: resultType}
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: function.Name, Node: node},
		typ:    resultType,
		token:  property,
	}
}

// checkQualifiedGenericCall specializes an imported module's exported generic
// function for the concrete request at hand and checks the call against the
// specialized signature. Concrete arguments are resolved in the requesting
// module's environment, exactly like a local generic call; the open
// declaration's signature and body are then resolved only in the defining
// module's own retained scope and type environment (registry.definingContext),
// so a private name in that body sees the defining module's own imports and
// declarations, never the requester's, and the record is stored into the
// defining module's registry collection so its checked output carries the
// request. Repeated requests of one (declaration, argument) pair reuse the
// one recorded specialization. The requested body was not part of the
// defining module's starvation scan, which ran before this request arrived.
func checkQualifiedGenericCall(call parser.CallExpression, open *openGenericFunction, property lexer.Token, target string, ctx checkContext) checkedExpression {
	var arguments []compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		arguments = make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, property, ctx.typeEnvironment, ctx.names.generics)
			if diagnostic != nil {
				return checkedExpression{token: property, diagnostic: diagnostic}
			}
			arguments = append(arguments, argumentUse.Type)
		}
		if len(arguments) != open.Generic.Arity {
			diagnostic := messageAt(property, diag.ExplicitGenericArgumentCountMismatch())
			return checkedExpression{token: property, diagnostic: &diagnostic}
		}
	} else {
		argumentTypes := make([]compilerTypes.Type, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			checked := checkValue(argument, ctx)
			if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
				return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			argumentTypes = append(argumentTypes, checked.typ)
		}
		inferred, diagnostic := inferTypeArguments(open, argumentTypes, ctx)
		if diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		arguments = inferred
	}
	definingCtx, ok := ctx.names.registry.definingContext(target)
	if !ok {
		diagnostic := unknownAt(property)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	specialized, diagnostic := specializeFunctionIn(open, arguments, definingCtx, ctx.names.registry.specializationStore(target))
	diagnostic = diagnosticInDefiningModule(diagnostic, definingCtx.names.logicalKey)
	if diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	signature := specialized.Type.Signature
	if signature == nil {
		diagnostic := unknownAt(property)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	if !aritySatisfied(signature, len(call.Arguments)) {
		diagnostic := messageAt(property, arityDiagnostic(specialized.Name, signature, len(call.Arguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	argumentsOperands, diagnostics := checkArgumentsWithRest(specialized.Name, parameterUses, call.Arguments, signature.Rest, property, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	calleeNode := Expression{Kind: FunctionReferenceExpression, Name: specialized.Name, ResultType: specialized.Type, Module: target}
	node := Expression{
		Kind:        CallExpression,
		Operand:     &calleeNode,
		Arguments:   argumentsOperands,
		OperandType: specialized.Type,
		ResultType:  resultType,
	}
	applyRestMetadata(&node, signature, ctx.typeEnvironment)
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: specialized.Name, Node: node},
		typ:    resultType,
		token:  property,
	}
}

// checkIndirectCall checks a call whose callee is neither a bare name nor a
// method selection: an anonymous function literal invoked directly, or any
// other expression whose checked type is Fun<...>. It shares argument
// checking with the named-callee and dispatch-table-member paths; the
// checked node differs only in what its Operand holds, which here is the
// callee's own checked expression.
func checkIndirectCall(call parser.CallExpression, ctx checkContext) checkedExpression {
	if literal, isLiteral := call.Callee.(parser.AnonymousFunctionLiteral); isLiteral && len(literal.TypeParameters) > 0 {
		// An unspecialized generic literal has no exact type to check as an
		// ordinary expression; its own call arguments infer it instead,
		// exactly like a bare generic function name called without explicit
		// type arguments.
		return checkGenericLiteralDirectCall(call, literal, ctx)
	}
	callee := checkExpression(call.Callee, expressionContext{}, ctx)
	if diagnostics := initializerDiagnostics(callee); len(diagnostics) > 0 {
		return checkedExpression{token: callee.token, diagnostics: diagnostics}
	}
	funType := callee.typ
	// A callee whose type is an open type parameter has no known signature;
	// callability and arity are substitution-dependent, so defer them while
	// still checking each argument for an independent error.
	if genericOpen(ctx) && compilerTypes.ContainsTypeParameter(funType) {
		if diagnostics := checkDeferredArguments(call.Arguments, ctx); len(diagnostics) > 0 {
			return checkedExpression{token: call.OpenParen, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		return checkedExpression{typ: funType, token: call.OpenParen}
	}
	if isNullableFun(funType) {
		diagnostic := messageAt(call.OpenParen, diag.NullableFunctionNeedsNarrowing(funType.Name))
		return checkedExpression{token: call.OpenParen, diagnostic: &diagnostic}
	}
	signature := funType.Signature
	if signature == nil {
		diagnostic := messageAt(call.OpenParen, diag.IndirectCalleeInvalid())
		return checkedExpression{token: call.OpenParen, diagnostic: &diagnostic}
	}
	if !aritySatisfied(signature, len(call.Arguments)) {
		diagnostic := messageAt(call.OpenParen, arityDiagnostic("the called function", signature, len(call.Arguments)))
		return checkedExpression{token: call.OpenParen, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, len(signature.Parameters))
	for index, parameter := range signature.Parameters {
		parameterUses[index] = compilerTypes.NewTypeUse(parameter)
	}
	arguments, diagnostics := checkArgumentsWithRest("the called function", parameterUses, call.Arguments, signature.Rest, call.OpenParen, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: call.OpenParen, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	calleeNode := expressionNode(callee.source)
	node := Expression{
		Kind:        CallExpression,
		Operand:     &calleeNode,
		Arguments:   arguments,
		OperandType: funType,
		ResultType:  resultType,
	}
	applyRestMetadata(&node, signature, ctx.typeEnvironment)
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Node: node},
		typ:    resultType,
		token:  call.OpenParen,
	}
}

// checkArguments checks each written argument in its parameter's expected-type
// position, so contextual literals and Ptr<mut T>-to-Ptr weakening both apply.
// Callee is only used to spell diagnostics.
func checkArguments(callee string, expected []compilerTypes.TypeUse, written []parser.Expression, token lexer.Token, ctx checkContext) ([]Operand, compilerTypes.Diagnostics) {
	return checkArgumentsWithRest(callee, expected, written, false, token, ctx)
}

// checkArgumentsWithRest checks a written argument list against an expected
// parameter-use list. When rest is true the final expected entry is the rest
// element type T and every argument at or past the fixed boundary is checked
// in expected T context; otherwise arity is exact and every argument maps to
// its own parameter.
func checkArgumentsWithRest(callee string, expected []compilerTypes.TypeUse, written []parser.Expression, rest bool, token lexer.Token, ctx checkContext) ([]Operand, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	arguments := make([]Operand, 0, len(written))
	for index, argument := range written {
		expectedIndex := index
		if rest && index >= len(expected) {
			expectedIndex = len(expected) - 1
		}
		want := expected[expectedIndex]
		checked := checkInitializer(argument, want, token, ctx)
		if argumentDiagnostics := initializerDiagnostics(checked); len(argumentDiagnostics) > 0 {
			diagnostics = append(diagnostics, argumentDiagnostics...)
			continue
		}
		if checked.typ != (compilerTypes.Type{}) && !assignable(want.Type, checked.typ) {
			diagnostics = append(diagnostics, messageAt(checked.token,
				diag.FunctionArgumentTypeMismatch(callee, index+1, want.Type.Name, checked.typ.Name, textMismatchDetails(want.Type, checked.typ))))
			continue
		}
		if diagnostic := restEscapeDiagnostic(checked.source, checked.token); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if diagnostic := atomicCopyDiagnostic(checked.source, token); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		arguments = append(arguments, checked.source)
	}
	return arguments, diagnostics
}

// restBoundary returns the rest signature's fixed-parameter count. It is only
// meaningful when signature.Rest is true.
func restFixedCount(signature *compilerTypes.FunSignature) int {
	return len(signature.Parameters) - 1
}

// applyRestMetadata records a call's rest boundary on its checked node when its
// callee signature is rest. environment constructs the read-only Slice<T> the
// call boundary passes.
func applyRestMetadata(node *Expression, signature *compilerTypes.FunSignature, environment *compilerTypes.Environment) {
	if signature == nil || !signature.Rest {
		return
	}
	node.Rest = true
	node.RestStart = restFixedCount(signature)
	node.RestElement = signature.Parameters[node.RestStart]
	node.RestSlice = environment.SliceType(node.RestElement, false)
}

// applyParameterRestMetadata records a method call's rest boundary from its
// resolved parameter list, whose final entry already carries the Slice<T> the
// C signature uses.
func applyParameterRestMetadata(node *Expression, parameters []FunctionParameter) {
	fixed, rest := parameterRestFixed(parameters)
	if !rest {
		return
	}
	node.Rest = true
	node.RestStart = fixed
	node.RestElement = parameters[fixed].RestElement
	node.RestSlice = parameters[fixed].Type
}

// aritySatisfied reports whether got arguments satisfy signature's arity: an
// exact match for an ordinary signature, or at least the fixed count for a
// rest signature.
func aritySatisfied(signature *compilerTypes.FunSignature, got int) bool {
	if signature.Rest {
		return got >= restFixedCount(signature)
	}
	return got == len(signature.Parameters)
}

// arityDiagnostic renders the one arity error for a failed call.
func arityDiagnostic(name string, signature *compilerTypes.FunSignature, got int) diag.Message {
	expected := len(signature.Parameters)
	if signature.Rest {
		expected = restFixedCount(signature)
	}
	return diag.FunctionArity(name, expected, got, signature.Rest)
}

// parameterRestFixed returns a resolved parameter list's fixed count and
// whether its final parameter is a rest parameter.
func parameterRestFixed(parameters []FunctionParameter) (int, bool) {
	if len(parameters) > 0 && parameters[len(parameters)-1].Rest {
		return len(parameters) - 1, true
	}
	return len(parameters), false
}

// parameterAritySatisfied reports whether got arguments satisfy a resolved
// parameter list's arity.
func parameterAritySatisfied(parameters []FunctionParameter, got int) bool {
	fixed, rest := parameterRestFixed(parameters)
	if rest {
		return got >= fixed
	}
	return got == fixed
}

// parameterArityDiagnostic renders the arity error for a resolved parameter
// list.
func parameterArityDiagnostic(name string, parameters []FunctionParameter, got int) diag.Message {
	fixed, rest := parameterRestFixed(parameters)
	return diag.FunctionArity(name, fixed, got, rest)
}

// checkBareConstructorCall recognizes and checks a bare Type(...) call: one of
// the nine compiler-owned canonical constructors, or a declared struct (or a
// transparent alias/generic template resolving to one). The second result is
// false when the callee names no type at all, so the caller falls through to
// ordinary free-function lookup. Compiler-owned canonical constructors take
// only positional arguments, exactly like an ordinary call.
func checkBareConstructorCall(call parser.CallExpression, callee parser.VariableExpression, expectedType compilerTypes.Type, ctx checkContext) (checkedExpression, bool) {
	if builtinConstructible(callee.Name.Lexeme) {
		if diagnostic := rejectNamedArguments(call); diagnostic != nil {
			return checkedExpression{token: callee.Name, diagnostic: diagnostic}, true
		}
		switch callee.Name.Lexeme {
		case "Heap":
			return checkHeapTypeCall(call, callee, ctx), true
		case "Stash":
			return checkStashTypeCall(call, callee.Name, ctx), true
		case "Pool":
			return checkPoolTypeCall(call, callee.Name, ctx), true
		case "List":
			return checkListTypeCall(call, callee.Name, ctx), true
		case "Dict":
			return checkDictTypeCall(call, callee.Name, ctx), true
		case "Channel":
			return checkChannelTypeCall(call, callee.Name, ctx), true
		case "Mutex":
			return checkMutexTypeCall(call, callee.Name, ctx), true
		case "Atomic":
			return checkAtomicTypeCall(call, callee.Name, ctx), true
		case "Error":
			return checkErrorNewCall(call, callee.Name, ctx), true
		default:
			// A name the registry marks constructible but this dispatch has
			// no case for cannot lower; it is a compiler defect, not a user
			// error, so it fails closed.
			diagnostic := unknownAt(callee.Name)
			return checkedExpression{token: callee.Name, diagnostic: &diagnostic}, true
		}
	}
	if _, ok := ctx.typeEnvironment.Lookup(callee.Name.Lexeme); ok {
		return checkStructConstructorCall(call, callee.Name, expectedType, ctx), true
	}
	if ctx.names.generics != nil {
		if _, generic := ctx.names.generics.types[callee.Name.Lexeme]; generic {
			return checkStructConstructorCall(call, callee.Name, expectedType, ctx), true
		}
	}
	return checkedExpression{}, false
}

// rejectNamedArguments reports the shared diagnostic for a named argument
// passed to a callee that is not a struct or ADT-variant constructor: an
// ordinary function, method, function value, descriptive compiler-owned
// operation, or compiler-owned canonical constructor.
func rejectNamedArguments(call parser.CallExpression) *compilerTypes.Diagnostic {
	for _, label := range call.ArgumentLabels {
		if label != nil {
			diagnostic := messageAt(*label, diag.NamedArgumentsRequireConstructor())
			return &diagnostic
		}
	}
	return nil
}

// checkCallValue is the value-position wrapper: a callee that returns nothing
// has no value to bind, so it is rejected here rather than reaching an
// initializer with a zero type. expectedType is the enclosing expression's
// contextual type; see checkCall.
func checkCallValue(call parser.CallExpression, expectedType compilerTypes.Type, ctx checkContext) checkedExpression {
	checked := checkCall(call, expectedType, ctx)
	if len(initializerDiagnostics(checked)) > 0 {
		return checked
	}
	if checked.typ.Name == "" {
		diagnostic := messageAt(checked.token, diag.CallProducesNoValue(checked.token.Lexeme))
		return checkedExpression{token: checked.token, diagnostic: &diagnostic}
	}
	return checked
}
