package checker

import (
	"fmt"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// genericTable is the compilation-scoped registry of open generic templates
// and concrete specializations. It lives on the module scope and is shared by
// every child scope, so one compilation observes one set of specializations.
type genericTable struct {
	types     map[string]*openGenericType
	functions map[string]*openGenericFunction
	methods   map[string]*openGenericMethod

	active map[string]bool               // specialization keys being resolved
	frame  map[string]compilerTypes.Type // current parameter frame; nil outside generic resolution
	open   bool                          // true while open-checking a generic body

	aliasSpecializations    map[string]compilerTypes.Type
	objectSpecializations   map[string]compilerTypes.Type
	adtSpecializations      map[string]compilerTypes.Type
	objectOpen              map[*compilerTypes.ObjectType]*openGenericType
	objectArguments         map[*compilerTypes.ObjectType][]compilerTypes.Type
	adtOpen                 map[*compilerTypes.AdtType]*openGenericType
	adtArguments            map[*compilerTypes.AdtType][]compilerTypes.Type
	typeDeclarations        []TypeDeclaration
	functionSpecializations map[string]FunctionDeclaration
	methodSpecializations   map[string]MethodDeclaration

	// registry and moduleID name the enclosing module's import graph: a
	// QualifiedTypeExpression resolves through them. moduleScope installs
	// both; the table is per module, so the pair is stable for the whole
	// compilation.
	registry *ModuleRegistry
	moduleID string
}

func newGenericTable() *genericTable {
	return &genericTable{
		types:                   make(map[string]*openGenericType),
		functions:               make(map[string]*openGenericFunction),
		methods:                 make(map[string]*openGenericMethod),
		active:                  make(map[string]bool),
		aliasSpecializations:    make(map[string]compilerTypes.Type),
		objectSpecializations:   make(map[string]compilerTypes.Type),
		adtSpecializations:      make(map[string]compilerTypes.Type),
		objectOpen:              make(map[*compilerTypes.ObjectType]*openGenericType),
		objectArguments:         make(map[*compilerTypes.ObjectType][]compilerTypes.Type),
		adtOpen:                 make(map[*compilerTypes.AdtType]*openGenericType),
		adtArguments:            make(map[*compilerTypes.AdtType][]compilerTypes.Type),
		functionSpecializations: make(map[string]FunctionDeclaration),
		methodSpecializations:   make(map[string]MethodDeclaration),
	}
}

// openGenericType is one generic type or alias declaration kept as an open
// template. Object targets are specialized into fresh nominal objects; plain
// type targets are resolved under a parameter frame and cached per argument
// list.
type openGenericType struct {
	Name        string
	Parameters  []lexer.Token
	Target      parser.TypeExpression
	Declaration *compilerTypes.GenericDeclaration
}

// openGenericFunction is one generic function declaration kept as an open
// template. Its body is re-checked under a concrete frame at specialization.
// A module-level template is identified by Name alone: one module has one
// name per declaration, so no collision is possible. Local is true for a
// generic anonymous function literal (openGenericLiteral), where identity
// instead carries the compiler-owned template identity: two literals can
// legitimately reuse the same synthesized name in disjoint scopes, and the
// module-wide type environment and specialization tables key on strings, so
// templateKey is what actually distinguishes them.
type openGenericFunction struct {
	Name        string
	Parameters  []lexer.Token
	Declaration parser.FunctionDeclaration
	Generic     *compilerTypes.GenericDeclaration
	identity    BindingID
	local       bool
}

// templateKey is the string that keys this template's type-environment
// registration, specialization cache, recursion guard, and generated C name
// stem. A module template keeps its bare source name, preserving today's
// generated names and cache keys exactly. A local template's key is
// qualified by its identity so two same-named local templates in disjoint
// scopes never share a cache entry, a type-parameter placeholder, or a
// generated symbol.
func (open *openGenericFunction) templateKey() string {
	if !open.local {
		return open.Name
	}
	return fmt.Sprintf("%s#%d", open.Name, open.identity)
}

// generatedStem is templateKey's counterpart for a generated C symbol
// fragment, which must be a valid C identifier and therefore cannot use
// templateKey's '#' separator. A module template keeps its bare name; a
// local template's stem is disambiguated with its identity so two local
// templates that reuse a name in disjoint scopes never share a symbol.
func (open *openGenericFunction) generatedStem() string {
	if !open.local {
		return open.Name
	}
	return fmt.Sprintf("%s_local%d", open.Name, open.identity)
}

// openGenericMethod is one generic method declaration. ReceiverParameters are
// the generic owner's parameters inherited from the receiver type.
type openGenericMethod struct {
	ObjectName         string
	Name               string
	ReceiverParameters []lexer.Token
	Parameters         []lexer.Token
	Declaration        parser.ImplDeclaration
	Object             *openGenericType
	Generic            *compilerTypes.GenericDeclaration
}

// specializeKey builds the deterministic in-compilation key for one
// specialization from the declaration name and the recursive module-qualified
// canonical keys of the arguments. Display names never participate: two
// same-named types from different modules produce different keys.
func specializeKey(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.CanonicalKey
	}
	return name + "|" + strings.Join(names, ",")
}

// specializeTypeName renders the deterministic source name of a specialized
// type, such as "Box<Int32>".
func specializeTypeName(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.Name
	}
	return name + "<" + strings.Join(names, ", ") + ">"
}

// specializeFunctionName renders the deterministic C-name stem of a
// specialized function, such as "identity_Int32".
func specializeFunctionName(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = compilerTypes.SanitizeIdentifier(argument.Name)
	}
	return name + "_" + strings.Join(names, "_")
}

func parameterFrame(parameters []lexer.Token, arguments []compilerTypes.Type) map[string]compilerTypes.Type {
	frame := make(map[string]compilerTypes.Type, len(parameters))
	for index, parameter := range parameters {
		if index < len(arguments) {
			frame[parameter.Lexeme] = arguments[index]
		}
	}
	return frame
}

// mergedFrame combines an enclosing generic frame with a nested template's
// own, so a generic literal specializing inside an already-specializing
// generic function or method can still resolve the enclosing type
// parameter. The active-name check at registration already rejects a
// nested declaration that redeclares an enclosing name, so no entry in
// inner ever legitimately overwrites one in outer.
func mergedFrame(outer, inner map[string]compilerTypes.Type) map[string]compilerTypes.Type {
	if len(outer) == 0 {
		return inner
	}
	merged := make(map[string]compilerTypes.Type, len(outer)+len(inner))
	for name, typ := range outer {
		merged[name] = typ
	}
	for name, typ := range inner {
		merged[name] = typ
	}
	return merged
}

// specializedFunctionList returns the cached concrete function specializations
// in deterministic specialization-key order for the generator. The final
// registry fold replaces this list with the defining module's full collection
// (own requests plus importers'), sorted identically.
func specializedFunctionList(generics *genericTable) []FunctionDeclaration {
	if generics == nil {
		return nil
	}
	return sortedFunctionSpecializations(generics.functionSpecializations)
}

// specializedMethodList returns the cached concrete method specializations in
// deterministic specialization-key order for the generator.
func specializedMethodList(generics *genericTable) []MethodDeclaration {
	if generics == nil {
		return nil
	}
	return sortedMethodSpecializations(generics.methodSpecializations)
}

// specializeTypeUse resolves a written generic type use to its concrete
// specialization. Object targets create fresh nominal objects; plain type
// targets resolve under the parameter frame.
func specializeTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if generics == nil {
		diagnostic := unknownAt(fallback, "generic type use outside a generic table")
		return compilerTypes.TypeUse{}, &diagnostic
	}
	open, ok := generics.types[expression.Name.Lexeme]
	if !ok {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "unknown generic type "+expression.Name.Lexeme))
	}
	if len(expression.Arguments) != open.Declaration.Arity {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, fmt.Sprintf("generic type %s expects %d type arguments; got %d", open.Name, open.Declaration.Arity, len(expression.Arguments))))
	}
	arguments := make([]compilerTypes.Type, 0, len(expression.Arguments))
	for _, argumentExpression := range expression.Arguments {
		argumentUse, diagnostic := resolveTypeUse(argumentExpression, fallback, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		arguments = append(arguments, argumentUse.Type)
	}
	return specializeTypeUseArguments(open, arguments, expression.Name, typeEnvironment, generics)
}

// specializeTypeUseArguments specializes an open template with already
// resolved canonical argument types.
func specializeTypeUseArguments(open *openGenericType, arguments []compilerTypes.Type, token lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(arguments) != open.Declaration.Arity {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(token, fmt.Sprintf("generic type %s expects %d type arguments; got %d", open.Name, open.Declaration.Arity, len(arguments))))
	}
	if _, objectTarget := open.Target.(parser.ObjectTypeExpression); objectTarget {
		specialized, diagnostic := specializeObjectType(open, arguments, token, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		return compilerTypes.NewTypeUse(specialized), nil
	}
	if _, adtTarget := open.Target.(parser.AdtDefinitionExpression); adtTarget {
		specialized, diagnostic := specializeADTType(open, arguments, token, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		return compilerTypes.NewTypeUse(specialized), nil
	}
	key := specializeKey(open.Name, arguments)
	if cached, ok := generics.aliasSpecializations[key]; ok {
		return compilerTypes.NewTypeUse(cached), nil
	}
	previousFrame := generics.frame
	generics.frame = parameterFrame(open.Parameters, arguments)
	resolved, diagnostic := resolveTypeUse(open.Target, token, typeEnvironment, generics)
	generics.frame = previousFrame
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	generics.aliasSpecializations[key] = resolved.Type
	return resolved, nil
}

// specializeObjectType creates or reuses the nominal object for one concrete
// specialization of a generic object template.
func specializeObjectType(open *openGenericType, arguments []compilerTypes.Type, token lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.Type, *compilerTypes.Diagnostic) {
	key := specializeKey(open.Name, arguments)
	if cached, ok := generics.objectSpecializations[key]; ok {
		return cached, nil
	}
	for activeKey := range generics.active {
		if strings.HasPrefix(activeKey, open.Name+"|") && activeKey != key {
			return compilerTypes.Type{}, diagnosticAt(typeErrorAt(token, "recursive type specialization changes generic arguments"))
		}
	}
	object, ok := open.Target.(parser.ObjectTypeExpression)
	if !ok {
		diagnostic := unknownAt(token, "generic object specialization without an object template")
		return compilerTypes.Type{}, &diagnostic
	}
	specializedName := specializeTypeName(open.Name, arguments)
	provisional := typeEnvironment.BeginObject(specializedName, token.Line, token.Column)
	// The specialized object is a nominal type of the module whose table
	// created it, so it is stamped with that module's identity like any
	// locally declared object, and its canonical key follows the stamped
	// module, not the requesting environment's.
	provisional.Object.SetModuleOwner(generics.moduleID)
	provisional.CanonicalKey = compilerTypes.CanonicalNominalKey(specializedName, generics.moduleID)
	generics.objectSpecializations[key] = provisional
	generics.objectOpen[provisional.Object] = open
	generics.objectArguments[provisional.Object] = append([]compilerTypes.Type(nil), arguments...)
	generics.active[key] = true
	previousFrame := generics.frame
	generics.frame = parameterFrame(open.Parameters, arguments)
	members, memberDiagnostics := resolveObjectMembers(specializedName, object, typeEnvironment, generics)
	generics.frame = previousFrame
	delete(generics.active, key)
	if len(memberDiagnostics) > 0 {
		delete(generics.objectSpecializations, key)
		return compilerTypes.Type{}, &memberDiagnostics[0]
	}
	completed := typeEnvironment.CompleteObject(specializedName, members)
	generics.objectSpecializations[key] = completed
	generics.typeDeclarations = append(generics.typeDeclarations, TypeDeclaration{
		Name:         compilerTypes.SanitizeIdentifier(specializedName),
		Type:         completed,
		TypeUse:      compilerTypes.NewTypeUse(completed),
		SourceLine:   token.Line,
		SourceColumn: token.Column,
	})
	return completed, nil
}

// registerGenericFunction validates and stores one generic function
// declaration as an open template, then binds its name so calls resolve.
func registerGenericFunction(declaration parser.FunctionDeclaration, ctx checkContext) compilerTypes.Diagnostics {
	name := declaration.Name.Lexeme
	diagnostics := validateGenericParameters(declaration.TypeParameters)
	if len(diagnostics) > 0 {
		return diagnostics
	}
	parameterNames := parameterNamesOf(declaration.TypeParameters)
	generic := ctx.typeEnvironment.DeclareGeneric(name, len(declaration.TypeParameters), parameterNames)
	if generic == nil {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, name+" is already declared")}
	}
	open := &openGenericFunction{
		Name:        name,
		Parameters:  append([]lexer.Token(nil), declaration.TypeParameters...),
		Declaration: declaration,
		Generic:     generic,
		identity:    ctx.names.newBindingID(),
	}
	// A module template is additionally published under its bare name so a
	// clean module can export it; registerGenerics reads exactly this map.
	// A local template never reaches this path.
	ctx.names.generics.functions[name] = open
	ctx.names.module[name] = binding{typ: compilerTypes.Type{}, use: compilerTypes.NewTypeUse(compilerTypes.Type{}), kind: genericFunctionBinding, genericFunction: open}
	return nil
}

// isGenericReceiver reports whether an impl receiver is a generic type use
// naming the owner's parameters.
func isGenericReceiver(expression parser.TypeExpression) bool {
	_, ok := expression.(parser.GenericTypeExpression)
	return ok
}

// registerGenericMethod validates and stores one generic method declaration as
// an open template. The receiver must be the owner's bare generic parameters
// in declaration order.
func registerGenericMethod(declaration parser.ImplDeclaration, ctx checkContext) compilerTypes.Diagnostics {
	receiver, ok := declaration.SelfType.(parser.GenericTypeExpression)
	if !ok {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Keyword, "a generic method requires a generic receiver")}
	}
	open, generic := ctx.names.generics.types[receiver.Name.Lexeme]
	if !generic {
		return compilerTypes.Diagnostics{typeErrorAt(receiver.Name, "unknown generic type "+receiver.Name.Lexeme)}
	}
	if len(receiver.Arguments) != len(open.Parameters) {
		return compilerTypes.Diagnostics{typeErrorAt(receiver.Name, "generic receiver pattern overlaps another implementation")}
	}
	for index, argument := range receiver.Arguments {
		named, ok := argument.(parser.NamedTypeExpression)
		if !ok || named.Name.Lexeme != open.Parameters[index].Lexeme {
			return compilerTypes.Diagnostics{typeErrorAt(receiver.Name, "generic receiver pattern overlaps another implementation")}
		}
	}
	diagnostics := validateGenericParameters(declaration.TypeParameters)
	if len(diagnostics) > 0 {
		return diagnostics
	}
	parameterNames := parameterNamesOf(declaration.TypeParameters)
	methodGeneric := ctx.typeEnvironment.DeclareGeneric(declaration.Name.Lexeme+"<method>", len(declaration.TypeParameters), parameterNames)
	if methodGeneric == nil {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, declaration.Name.Lexeme+" is already declared")}
	}
	ctx.names.generics.methods[open.Name+"."+declaration.Name.Lexeme] = &openGenericMethod{
		ObjectName:         open.Name,
		Name:               declaration.Name.Lexeme,
		ReceiverParameters: append([]lexer.Token(nil), open.Parameters...),
		Parameters:         append([]lexer.Token(nil), declaration.TypeParameters...),
		Declaration:        declaration,
		Object:             open,
		Generic:            methodGeneric,
	}
	return nil
}

func validateGenericParameters(parameters []lexer.Token) compilerTypes.Diagnostics {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(parameters))
	for _, parameter := range parameters {
		if seen[parameter.Lexeme] {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is declared more than once"))
			continue
		}
		seen[parameter.Lexeme] = true
		if compilerTypes.IsProtectedTypeName(parameter.Lexeme) {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is a protected type name"))
		}
	}
	return diagnostics
}

func parameterNamesOf(parameters []lexer.Token) []string {
	names := make([]string, len(parameters))
	for index, parameter := range parameters {
		names[index] = parameter.Lexeme
	}
	return names
}

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
		diagnostic := unknownAt(open.Declaration.Name, "generic function specialization outside a specialization collection")
		return FunctionDeclaration{}, &diagnostic
	}
	if cached, ok := collection[key]; ok {
		return cached, nil
	}
	for activeKey := range generics.active {
		if strings.HasPrefix(activeKey, stem+"|") && activeKey != key {
			return FunctionDeclaration{}, diagnosticAt(typeErrorAt(open.Declaration.Name, "recursive specialization changes generic arguments"))
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
	parameterTypes := make([]compilerTypes.Type, 0, len(parameters))
	for _, parameter := range parameters {
		parameterTypes = append(parameterTypes, parameter.Type)
	}
	functionType := ctx.typeEnvironment.FunType(parameterTypes, result)
	if functionType.Signature == nil {
		generics.frame = previousFrame
		diagnostic := unknownAt(open.Declaration.Name, "could not construct the function type for "+open.Name)
		return FunctionDeclaration{}, &diagnostic
	}
	specialized := FunctionDeclaration{
		Name:         specializeFunctionName(open.generatedStem(), arguments),
		Parameters:   parameters,
		Result:       result,
		ResultUse:    resultUse,
		Type:         functionType,
		SourceLine:   open.Declaration.Name.Line,
		SourceColumn: open.Declaration.Name.Column,
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
		body = ctx.names.closureRootScope(specialized.Name)
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
		return FunctionDeclaration{}, diagnosticAt(typeErrorAt(open.Declaration.End, fmt.Sprintf("returning %s may fall through without returning %s", specialized.Name, result.Name)))
	}
	specialized.Body = statements
	collection[key] = specialized
	return specialized, nil
}

// specializeMethod creates or reuses the concrete declaration for one
// specialization of a generic method.
func specializeMethod(open *openGenericMethod, receiverObject *compilerTypes.ObjectType, receiverType compilerTypes.Type, receiverArguments []compilerTypes.Type, methodArguments []compilerTypes.Type, ctx checkContext) (MethodDeclaration, *compilerTypes.Diagnostic) {
	generics := ctx.names.generics
	key := open.ObjectName + "|" + argumentNames(receiverArguments) + "|" + open.Name + "|" + argumentNames(methodArguments)
	if cached, ok := generics.methodSpecializations[key]; ok {
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
		Name:         methodName,
		Object:       receiverObject,
		SelfType:     receiverType,
		Parameters:   parameters,
		Result:       result,
		ResultUse:    resultUse,
		SourceLine:   open.Declaration.Name.Line,
		SourceColumn: open.Declaration.Name.Column,
	}
	generics.methodSpecializations[key] = specialized
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
		return MethodDeclaration{}, diagnosticAt(typeErrorAt(open.Declaration.End, fmt.Sprintf("returning %s may fall through without returning %s", methodName, result.Name)))
	}
	specialized.Body = statements
	generics.methodSpecializations[key] = specialized
	return specialized, nil
}

func argumentNames(arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.CanonicalKey
	}
	return strings.Join(names, ",")
}

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
	if len(expected) != len(actual) {
		return nil, diagnosticAt(typeErrorAt(open.Declaration.Name, fmt.Sprintf("%s expects %d arguments; got %d", open.Name, len(expected), len(actual))))
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range expected {
		if !unifyTypes(expected[index], actual[index], bindings, open.Generic) {
			conflictingLexeme := open.Parameters[0].Lexeme
			if expected[index].Generic != nil && expected[index].Generic == open.Generic && expected[index].GenericIndex >= 0 && expected[index].GenericIndex < len(open.Parameters) {
				conflictingLexeme = open.Parameters[expected[index].GenericIndex].Lexeme
			} else {
				for paramIndex, placeholder := range placeholders {
					if typeContainsPlaceholder(expected[index], placeholder) {
						conflictingLexeme = open.Parameters[paramIndex].Lexeme
						break
					}
				}
			}
			return nil, diagnosticAt(typeErrorAt(open.Declaration.Name, fmt.Sprintf("conflicting inferred types for generic parameter %s", conflictingLexeme)))
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, diagnosticAt(typeErrorAt(open.Declaration.Name, fmt.Sprintf("cannot infer generic parameter %s for %s", open.Parameters[index].Lexeme, open.Name)))
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, diagnosticAt(typeErrorAt(open.Declaration.Name, fmt.Sprintf("cannot specialize %s with unresolved type arguments", open.Name)))
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
	if typ.View != nil && typeContainsPlaceholder(typ.View.Element, placeholder) {
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
		diagnostic := unknownAt(token, "generic function binding without an open template")
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
			return checkedExpression{token: token, diagnostic: diagnosticAt(typeErrorAt(token, "explicit generic argument count does not match declaration"))}
		}
	} else {
		argumentTypes := make([]compilerTypes.Type, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			checked := checkValue(argument, ctx)
			if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
				return checkedExpression{token: token, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if checked.typ == (compilerTypes.Type{}) {
				return checkedExpression{token: token, diagnostic: diagnosticAt(typeErrorAt(token, fmt.Sprintf("cannot infer generic parameter for %s", name)))}
			}
			argumentTypes = append(argumentTypes, checked.typ)
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
	if len(call.Arguments) != len(signature.Parameters) {
		return checkedExpression{token: token, diagnostic: diagnosticAt(typeErrorAt(token, fmt.Sprintf("%s expects %d arguments; got %d", specialized.Name, len(signature.Parameters), len(call.Arguments))))}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	arguments, diagnostics := checkArguments(specialized.Name, parameterUses, call.Arguments, token, ctx)
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
		diagnostic := unknownAt(callee.Property, "generic method call without receiver arguments")
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
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "explicit generic argument count does not match declaration"))}
		}
	} else {
		inferred, diagnostic := inferMethodArguments(open, receiverArguments, call.Arguments, callee.Property, ctx)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		methodArguments = inferred
	}
	specialized, diagnostic := specializeMethod(open, object, receiver.typ, receiverArguments, methodArguments, ctx)
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
	if len(expected) != len(written) {
		return nil, diagnosticAt(typeErrorAt(token, fmt.Sprintf("%s expects %d arguments; got %d", open.Name, len(expected), len(written))))
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
	for index := range expected {
		if !unifyTypes(expected[index], actual[index], bindings, open.Generic) {
			return nil, diagnosticAt(typeErrorAt(token, fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[index].Lexeme)))
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, diagnosticAt(typeErrorAt(token, fmt.Sprintf("cannot infer generic parameter %s for method %s", open.Parameters[index].Lexeme, open.Name)))
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, diagnosticAt(typeErrorAt(token, fmt.Sprintf("cannot specialize %s with unresolved type arguments", open.Name)))
		}
	}
	return bindings, nil
}

// buildConcreteMethodCall checks a call against a specialized method and
// builds the checked method-call node.
func buildConcreteMethodCall(call parser.CallExpression, callee parser.PropertyExpression, specialized MethodDeclaration, receiver checkedExpression, ctx checkContext) checkedExpression {
	if len(call.Arguments) != len(specialized.Parameters) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, fmt.Sprintf("%s expects %d arguments; got %d", specialized.Name, len(specialized.Parameters), len(call.Arguments))))}
	}
	adapted, diagnostic := adaptReceiver(receiver, specialized, callee, ctx.typeEnvironment, ctx.names.flow)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	expected := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		expected = append(expected, parameter.TypeUse)
	}
	arguments, diagnostics := checkArguments(specialized.Name, expected, call.Arguments, callee.Property, ctx)
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
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: specialized.Name, Node: node},
		typ:    resultType,
		token:  callee.Property,
	}
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
		diagnostic := unknownAt(name, "generic function binding without an open template")
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
	if signature == nil || len(signature.Parameters) != len(expectedTypes) || (signature.Result == nil) != !hasResult {
		return FunctionDeclaration{}, diagnosticAt(typeErrorAt(fallback, fmt.Sprintf("cannot infer generic parameter for %s", open.Name)))
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range expectedTypes {
		if !unifyTypes(expectedTypes[index], signature.Parameters[index], bindings, open.Generic) {
			return FunctionDeclaration{}, diagnosticAt(typeErrorAt(fallback, fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[index].Lexeme)))
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
			return FunctionDeclaration{}, diagnosticAt(typeErrorAt(fallback, fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[conflictingIndex].Lexeme)))
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return FunctionDeclaration{}, diagnosticAt(typeErrorAt(fallback, fmt.Sprintf("cannot infer generic parameter %s for %s", open.Parameters[index].Lexeme, open.Name)))
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return FunctionDeclaration{}, diagnosticAt(typeErrorAt(fallback, fmt.Sprintf("cannot specialize %s with unresolved type arguments", open.Name)))
		}
	}
	return specializeFunction(open, bindings, ctx)
}

// specializeADTType creates or reuses the nominal ADT for one concrete
// specialization of a generic ADT template.
func specializeADTType(open *openGenericType, arguments []compilerTypes.Type, token lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.Type, *compilerTypes.Diagnostic) {
	key := specializeKey(open.Name, arguments)
	if cached, ok := generics.adtSpecializations[key]; ok {
		return cached, nil
	}
	for activeKey := range generics.active {
		if strings.HasPrefix(activeKey, open.Name+"|") && activeKey != key {
			return compilerTypes.Type{}, diagnosticAt(typeErrorAt(token, "recursive type specialization changes generic arguments"))
		}
	}
	target, ok := open.Target.(parser.AdtDefinitionExpression)
	if !ok {
		diagnostic := unknownAt(token, "generic ADT specialization without an ADT template")
		return compilerTypes.Type{}, &diagnostic
	}
	specializedName := specializeTypeName(open.Name, arguments)
	provisional := typeEnvironment.BeginADT(specializedName, token.Line, token.Column)
	provisional.Adt.SetModuleOwner(generics.moduleID)
	provisional.CanonicalKey = compilerTypes.CanonicalNominalKey(specializedName, generics.moduleID)
	variants := make([]compilerTypes.AdtVariant, 0, len(target.Variants))
	for _, variant := range target.Variants {
		resolved := compilerTypes.AdtVariant{Name: variant.Name.Lexeme}
		if variant.Payload != nil {
			previousFrame := generics.frame
			generics.frame = parameterFrame(open.Parameters, arguments)
			payload, memberDiagnostics := resolveADTPayload(specializedName, *variant.Payload, typeEnvironment, generics)
			generics.frame = previousFrame
			if len(memberDiagnostics) > 0 {
				typeEnvironment.AbandonADT(specializedName)
				return compilerTypes.Type{}, &memberDiagnostics[0]
			}
			resolved.Payload = payload
		}
		variants = append(variants, resolved)
	}
	completed := typeEnvironment.CompleteADT(specializedName, variants)
	generics.adtSpecializations[key] = completed
	generics.adtOpen[completed.Adt] = open
	generics.adtArguments[completed.Adt] = append([]compilerTypes.Type(nil), arguments...)
	generics.typeDeclarations = append(generics.typeDeclarations, TypeDeclaration{
		Name:         compilerTypes.SanitizeIdentifier(specializedName),
		Type:         completed,
		TypeUse:      compilerTypes.NewTypeUse(completed),
		SourceLine:   token.Line,
		SourceColumn: token.Column,
	})
	return completed, nil
}
