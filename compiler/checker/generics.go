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
	// QualifiedTypeExpression resolves through them (RFC 0034 Task 5).
	// moduleScope installs both; the table is per module, so the pair is
	// stable for the whole compilation.
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
type openGenericFunction struct {
	Name        string
	Parameters  []lexer.Token
	Declaration parser.FunctionDeclaration
	Generic     *compilerTypes.GenericDeclaration
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
// specialization from the declaration name and the canonical argument display
// names. Display names are unique within one compilation, so the key is
// injective for distinct canonical argument lists.
func specializeKey(name string, arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.Name
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
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic type use outside a generic table",
		}
	}
	open, ok := generics.types[expression.Name.Lexeme]
	if !ok {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  "unknown generic type " + expression.Name.Lexeme,
		}
	}
	if len(expression.Arguments) != open.Declaration.Arity {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  fmt.Sprintf("generic type %s expects %d type arguments, got %d", open.Name, open.Declaration.Arity, len(expression.Arguments)),
		}
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
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     token.Line,
			Column:   token.Column,
			Message:  fmt.Sprintf("generic type %s expects %d type arguments, got %d", open.Name, open.Declaration.Arity, len(arguments)),
		}
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
			return compilerTypes.Type{}, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     token.Line,
				Column:   token.Column,
				Message:  "recursive type specialization changes generic arguments",
			}
		}
	}
	object, ok := open.Target.(parser.ObjectTypeExpression)
	if !ok {
		return compilerTypes.Type{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic object specialization without an object template",
		}
	}
	specializedName := specializeTypeName(open.Name, arguments)
	provisional := typeEnvironment.BeginObject(specializedName, token.Line, token.Column)
	// The specialized object is a nominal type of the module whose table
	// created it, so it is stamped with that module's identity like any
	// locally declared object (RFC 0034 Task 6).
	provisional.Object.ModuleID = generics.moduleID
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
func registerGenericFunction(declaration parser.FunctionDeclaration, names *scope, typeEnvironment *compilerTypes.Environment) compilerTypes.Diagnostics {
	name := declaration.Name.Lexeme
	diagnostics := validateGenericParameters(declaration.TypeParameters)
	if len(diagnostics) > 0 {
		return diagnostics
	}
	parameterNames := parameterNamesOf(declaration.TypeParameters)
	generic := typeEnvironment.DeclareGeneric(name, len(declaration.TypeParameters), parameterNames)
	if generic == nil {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, name+" is already declared")}
	}
	names.generics.functions[name] = &openGenericFunction{
		Name:        name,
		Parameters:  append([]lexer.Token(nil), declaration.TypeParameters...),
		Declaration: declaration,
		Generic:     generic,
	}
	names.module[name] = binding{typ: compilerTypes.Type{}, use: compilerTypes.NewTypeUse(compilerTypes.Type{}), kind: genericFunctionBinding}
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
func registerGenericMethod(declaration parser.ImplDeclaration, names *scope, typeEnvironment *compilerTypes.Environment) compilerTypes.Diagnostics {
	receiver, ok := declaration.SelfType.(parser.GenericTypeExpression)
	if !ok {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Keyword, "a generic method requires a generic receiver")}
	}
	open, generic := names.generics.types[receiver.Name.Lexeme]
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
	methodGeneric := typeEnvironment.DeclareGeneric(declaration.Name.Lexeme+"<method>", len(declaration.TypeParameters), parameterNames)
	if methodGeneric == nil {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, declaration.Name.Lexeme+" is already declared")}
	}
	names.generics.methods[open.Name+"."+declaration.Name.Lexeme] = &openGenericMethod{
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
// through specializeFunctionIn with the defining module's collection instead
// (RFC 0034 Task 6).
func specializeFunction(open *openGenericFunction, arguments []compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) (FunctionDeclaration, *compilerTypes.Diagnostic) {
	return specializeFunctionIn(open, arguments, names, typeEnvironment, names.generics.functionSpecializations)
}

// specializeFunctionIn is the shared specialization engine behind
// specializeFunction and the imported-generic path. It re-checks the
// template's signature and body under concrete arguments and caches the
// resulting declaration in collection -- the requesting module's own table
// for a local generic, or the defining module's registry collection for an
// imported one. The requesting module's generic table still supplies the
// parameter frame, recursion guards, and binding ids, so one specialization
// runs entirely in the requesting module's type environment.
func specializeFunctionIn(open *openGenericFunction, arguments []compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment, collection map[string]FunctionDeclaration) (FunctionDeclaration, *compilerTypes.Diagnostic) {
	generics := names.generics
	key := specializeKey(open.Name, arguments)
	if collection == nil {
		return FunctionDeclaration{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic function specialization outside a specialization collection",
		}
	}
	if cached, ok := collection[key]; ok {
		return cached, nil
	}
	for activeKey := range generics.active {
		if strings.HasPrefix(activeKey, open.Name+"|") && activeKey != key {
			return FunctionDeclaration{}, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     open.Declaration.Name.Line,
				Column:   open.Declaration.Name.Column,
				Message:  "recursive specialization changes generic arguments",
			}
		}
	}
	previousFrame := generics.frame
	generics.frame = parameterFrame(open.Parameters, arguments)
	parameters, parameterDiagnostics := checkParameters(open.Declaration.Parameters, typeEnvironment, generics)
	result, resultUse, resultDiagnostics := checkResultType(open.Declaration.Return, open.Declaration.Name, typeEnvironment, generics)
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
	functionType := typeEnvironment.FunType(parameterTypes, result)
	if functionType.Signature == nil {
		generics.frame = previousFrame
		return FunctionDeclaration{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "could not construct the function type for " + open.Name,
		}
	}
	specialized := FunctionDeclaration{
		Name:         specializeFunctionName(open.Name, arguments),
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
	body := &scope{
		module:    names.module,
		local:     make(map[string]binding, len(parameters)),
		owner:     specialized.Name,
		result:    result,
		resultUse: resultUse,
		methods:   names.methods,
		function:  true,
		nextID:    names.nextID,
		flow:      newFlowState(),
		generics:  generics,
		registry:  names.registry,
		moduleID:  names.moduleID,
	}
	for index := range parameters {
		parameters[index].Binding = names.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	statements, bodyDiagnostics := checkBody(open.Declaration.Body, body, typeEnvironment)
	generics.frame = previousFrame
	delete(generics.active, key)
	generics.open = true
	if len(bodyDiagnostics) > 0 {
		return FunctionDeclaration{}, &bodyDiagnostics[0]
	}
	if result != nil && FallsThrough(statements) {
		return FunctionDeclaration{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     open.Declaration.End.Line,
			Column:   open.Declaration.End.Column,
			Message:  fmt.Sprintf("returning %s may fall through without returning %s", specialized.Name, result.Name),
		}
	}
	specialized.Body = statements
	collection[key] = specialized
	return specialized, nil
}

// specializeMethod creates or reuses the concrete declaration for one
// specialization of a generic method.
func specializeMethod(open *openGenericMethod, receiverObject *compilerTypes.ObjectType, receiverType compilerTypes.Type, receiverArguments []compilerTypes.Type, methodArguments []compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) (MethodDeclaration, *compilerTypes.Diagnostic) {
	generics := names.generics
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
	generics.frame = frame
	parameters, parameterDiagnostics := checkParameters(open.Declaration.Parameters, typeEnvironment, generics)
	result, resultUse, resultDiagnostics := checkResultType(open.Declaration.Return, open.Declaration.Name, typeEnvironment, generics)
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
	selfID := names.newBindingID()
	specialized.SelfBinding = selfID
	body := &scope{
		module:    names.module,
		local:     make(map[string]binding, len(parameters)),
		owner:     methodName,
		result:    result,
		resultUse: resultUse,
		methods:   names.methods,
		self:      &specialized.SelfType,
		selfID:    selfID,
		function:  true,
		nextID:    names.nextID,
		flow:      newFlowState(),
		generics:  generics,
		registry:  names.registry,
		moduleID:  names.moduleID,
	}
	for index := range parameters {
		parameters[index].Binding = names.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	statements, bodyDiagnostics := checkBody(open.Declaration.Body, body, typeEnvironment)
	generics.frame = previousFrame
	delete(generics.active, key)
	generics.open = true
	if len(bodyDiagnostics) > 0 {
		return MethodDeclaration{}, &bodyDiagnostics[0]
	}
	if result != nil && FallsThrough(statements) {
		return MethodDeclaration{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     open.Declaration.End.Line,
			Column:   open.Declaration.End.Column,
			Message:  fmt.Sprintf("returning %s may fall through without returning %s", methodName, result.Name),
		}
	}
	specialized.Body = statements
	generics.methodSpecializations[key] = specialized
	return specialized, nil
}

func argumentNames(arguments []compilerTypes.Type) string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		names[index] = argument.Name
	}
	return strings.Join(names, ",")
}

// inferTypeArguments unifies the open declaration's signature (resolved under
// placeholder parameters) against concrete call argument types, binding each
// parameter to exactly one canonical type.
func inferTypeArguments(open *openGenericFunction, actual []compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) ([]compilerTypes.Type, *compilerTypes.Diagnostic) {
	generics := names.generics
	previousFrame := generics.frame
	placeholderFrame := make(map[string]compilerTypes.Type, len(open.Parameters))
	placeholders := make([]compilerTypes.Type, open.Generic.Arity)
	for index, parameter := range open.Parameters {
		placeholder := typeEnvironment.TypeParameter(open.Generic, index)
		placeholders[index] = placeholder
		placeholderFrame[parameter.Lexeme] = placeholder
	}
	generics.frame = placeholderFrame
	expected := make([]compilerTypes.Type, 0, len(open.Declaration.Parameters))
	expectedUses := make([]compilerTypes.TypeUse, 0, len(open.Declaration.Parameters))
	for _, parameter := range open.Declaration.Parameters {
		use, diagnostic := resolveTypeUse(parameter.Type, parameter.Name, typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return nil, diagnostic
		}
		expectedUses = append(expectedUses, use)
		expected = append(expected, use.Type)
	}
	generics.frame = previousFrame
	if len(expected) != len(actual) {
		return nil, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     open.Declaration.Name.Line,
			Column:   open.Declaration.Name.Column,
			Message:  fmt.Sprintf("%s expects %d arguments, got %d", open.Name, len(expected), len(actual)),
		}
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range expected {
		if !unifyTypes(expected[index], actual[index], bindings, open.Generic) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     open.Declaration.Name.Line,
				Column:   open.Declaration.Name.Column,
				Message:  fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[0].Lexeme),
			}
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     open.Declaration.Name.Line,
				Column:   open.Declaration.Name.Column,
				Message:  fmt.Sprintf("cannot infer generic parameter %s for %s", open.Parameters[index].Lexeme, open.Name),
			}
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     open.Declaration.Name.Line,
				Column:   open.Declaration.Name.Column,
				Message:  fmt.Sprintf("cannot specialize %s with unresolved type arguments", open.Name),
			}
		}
	}
	_ = expectedUses
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

func unionTypeMembers(typ compilerTypes.Type) ([]compilerTypes.Type, bool) {
	if compilerTypes.IsUnion(typ) {
		return compilerTypes.UnionMembers(typ), true
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
func checkGenericCall(call parser.CallExpression, bound binding, name string, token lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	open, ok := names.generics.functions[name]
	if !ok {
		return checkedExpression{token: token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic function binding without an open template",
		}}
	}
	var arguments []compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		arguments = make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, token, typeEnvironment, names.generics)
			if diagnostic != nil {
				return checkedExpression{token: token, diagnostic: diagnostic}
			}
			arguments = append(arguments, argumentUse.Type)
		}
		if len(arguments) != open.Generic.Arity {
			return checkedExpression{token: token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     token.Line,
				Column:   token.Column,
				Message:  "explicit generic argument count does not match declaration",
			}}
		}
	} else {
		argumentTypes := make([]compilerTypes.Type, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			checked := checkValue(argument, names, typeEnvironment)
			if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
				return checkedExpression{token: token, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if checked.typ == (compilerTypes.Type{}) {
				return checkedExpression{token: token, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError,
					Stage:    "checker",
					Line:     token.Line,
					Column:   token.Column,
					Message:  fmt.Sprintf("cannot infer generic parameter for %s", name),
				}}
			}
			argumentTypes = append(argumentTypes, checked.typ)
		}
		inferred, diagnostic := inferTypeArguments(open, argumentTypes, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: token, diagnostic: diagnostic}
		}
		arguments = inferred
	}
	specialized, diagnostic := specializeFunction(open, arguments, names, typeEnvironment)
	if diagnostic != nil {
		return checkedExpression{token: token, diagnostic: diagnostic}
	}
	return buildConcreteCall(call, specialized, names, typeEnvironment, token)
}

// buildConcreteCall checks a call against a concrete specialized signature and
// builds the checked call node.
func buildConcreteCall(call parser.CallExpression, specialized FunctionDeclaration, names *scope, typeEnvironment *compilerTypes.Environment, token lexer.Token) checkedExpression {
	signature := specialized.Type.Signature
	if len(call.Arguments) != len(signature.Parameters) {
		return checkedExpression{token: token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     token.Line,
			Column:   token.Column,
			Message:  fmt.Sprintf("%s expects %d arguments, got %d", specialized.Name, len(signature.Parameters), len(call.Arguments)),
		}}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	arguments, diagnostics := checkArguments(specialized.Name, parameterUses, call.Arguments, token, names, typeEnvironment)
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
func checkGenericMethodCall(call parser.CallExpression, callee parser.PropertyExpression, open *openGenericMethod, object *compilerTypes.ObjectType, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	receiverArguments := names.generics.objectArguments[object]
	if receiverArguments == nil {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic method call without receiver arguments",
		}}
	}
	var methodArguments []compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		methodArguments = make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, callee.Property, typeEnvironment, names.generics)
			if diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
			methodArguments = append(methodArguments, argumentUse.Type)
		}
		if len(methodArguments) != open.Generic.Arity {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     callee.Property.Line,
				Column:   callee.Property.Column,
				Message:  "explicit generic argument count does not match declaration",
			}}
		}
	} else {
		inferred, diagnostic := inferMethodArguments(open, receiverArguments, call.Arguments, callee.Property, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		methodArguments = inferred
	}
	specialized, diagnostic := specializeMethod(open, object, receiver.typ, receiverArguments, methodArguments, names, typeEnvironment)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	return buildConcreteMethodCall(call, callee, specialized, receiver, names, typeEnvironment)
}

// inferMethodArguments infers a method's own arguments from the call
// arguments under the combined receiver and method parameter frame.
func inferMethodArguments(open *openGenericMethod, receiverArguments []compilerTypes.Type, written []parser.Expression, token lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) ([]compilerTypes.Type, *compilerTypes.Diagnostic) {
	generics := names.generics
	previousFrame := generics.frame
	frame := parameterFrame(open.ReceiverParameters, receiverArguments)
	placeholders := make([]compilerTypes.Type, open.Generic.Arity)
	for index, parameter := range open.Parameters {
		placeholder := typeEnvironment.TypeParameter(open.Generic, index)
		placeholders[index] = placeholder
		frame[parameter.Lexeme] = placeholder
	}
	generics.frame = frame
	expected := make([]compilerTypes.Type, 0, len(open.Declaration.Parameters))
	for _, parameter := range open.Declaration.Parameters {
		use, diagnostic := resolveTypeUse(parameter.Type, token, typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return nil, diagnostic
		}
		expected = append(expected, use.Type)
	}
	generics.frame = previousFrame
	if len(expected) != len(written) {
		return nil, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     token.Line,
			Column:   token.Column,
			Message:  fmt.Sprintf("%s expects %d arguments, got %d", open.Name, len(expected), len(written)),
		}
	}
	actual := make([]compilerTypes.Type, 0, len(written))
	for _, argument := range written {
		checked := checkValue(argument, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return nil, &diagnostics[0]
		}
		actual = append(actual, checked.typ)
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range expected {
		if !unifyTypes(expected[index], actual[index], bindings, open.Generic) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     token.Line,
				Column:   token.Column,
				Message:  fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[index].Lexeme),
			}
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     token.Line,
				Column:   token.Column,
				Message:  fmt.Sprintf("cannot infer generic parameter %s for method %s", open.Parameters[index].Lexeme, open.Name),
			}
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     token.Line,
				Column:   token.Column,
				Message:  fmt.Sprintf("cannot specialize %s with unresolved type arguments", open.Name),
			}
		}
	}
	return bindings, nil
}

// buildConcreteMethodCall checks a call against a specialized method and
// builds the checked method-call node.
func buildConcreteMethodCall(call parser.CallExpression, callee parser.PropertyExpression, specialized MethodDeclaration, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != len(specialized.Parameters) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  fmt.Sprintf("%s expects %d arguments, got %d", specialized.Name, len(specialized.Parameters), len(call.Arguments)),
		}}
	}
	adapted, diagnostic := adaptReceiver(receiver, specialized, callee, typeEnvironment)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	expected := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		expected = append(expected, parameter.TypeUse)
	}
	arguments, diagnostics := checkArguments(specialized.Name, expected, call.Arguments, callee.Property, names, typeEnvironment)
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
func checkGenericFunctionReference(name lexer.Token, expected compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) (*checkedExpression, *compilerTypes.Diagnostic) {
	bound, status := names.lookup(name.Lexeme)
	if status != nameFound || bound.kind != genericFunctionBinding {
		return nil, nil
	}
	open, ok := names.generics.functions[name.Lexeme]
	if !ok {
		return nil, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic function binding without an open template",
		}
	}
	generics := names.generics
	previousFrame := generics.frame
	placeholderFrame := make(map[string]compilerTypes.Type, len(open.Parameters))
	placeholders := make([]compilerTypes.Type, open.Generic.Arity)
	for index, parameter := range open.Parameters {
		placeholder := typeEnvironment.TypeParameter(open.Generic, index)
		placeholders[index] = placeholder
		placeholderFrame[parameter.Lexeme] = placeholder
	}
	generics.frame = placeholderFrame
	expectedTypes := make([]compilerTypes.Type, 0, len(open.Declaration.Parameters))
	for _, parameter := range open.Declaration.Parameters {
		use, diagnostic := resolveTypeUse(parameter.Type, name, typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return nil, diagnostic
		}
		expectedTypes = append(expectedTypes, use.Type)
	}
	var expectedResult compilerTypes.Type
	hasResult := false
	if open.Declaration.Return != nil {
		use, diagnostic := resolveTypeUse(open.Declaration.Return, name, typeEnvironment, generics)
		if diagnostic != nil {
			generics.frame = previousFrame
			return nil, diagnostic
		}
		expectedResult = use.Type
		hasResult = true
	}
	generics.frame = previousFrame
	signature := expected.Signature
	if signature == nil || len(signature.Parameters) != len(expectedTypes) || (signature.Result == nil) != !hasResult {
		return nil, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     name.Line,
			Column:   name.Column,
			Message:  fmt.Sprintf("cannot infer generic parameter for %s", open.Name),
		}
	}
	bindings := make([]compilerTypes.Type, open.Generic.Arity)
	for index := range expectedTypes {
		if !unifyTypes(expectedTypes[index], signature.Parameters[index], bindings, open.Generic) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     name.Line,
				Column:   name.Column,
				Message:  fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[index].Lexeme),
			}
		}
	}
	if hasResult {
		if !unifyTypes(expectedResult, *signature.Result, bindings, open.Generic) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     name.Line,
				Column:   name.Column,
				Message:  fmt.Sprintf("conflicting inferred types for generic parameter %s", open.Parameters[0].Lexeme),
			}
		}
	}
	for index, binding := range bindings {
		if binding == (compilerTypes.Type{}) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     name.Line,
				Column:   name.Column,
				Message:  fmt.Sprintf("cannot infer generic parameter %s for %s", open.Parameters[index].Lexeme, open.Name),
			}
		}
		if compilerTypes.ContainsTypeParameter(binding) {
			return nil, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     name.Line,
				Column:   name.Column,
				Message:  fmt.Sprintf("cannot specialize %s with unresolved type arguments", open.Name),
			}
		}
	}
	specialized, diagnostic := specializeFunction(open, bindings, names, typeEnvironment)
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

// specializeADTType creates or reuses the nominal ADT for one concrete
// specialization of a generic ADT template.
func specializeADTType(open *openGenericType, arguments []compilerTypes.Type, token lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.Type, *compilerTypes.Diagnostic) {
	key := specializeKey(open.Name, arguments)
	if cached, ok := generics.adtSpecializations[key]; ok {
		return cached, nil
	}
	for activeKey := range generics.active {
		if strings.HasPrefix(activeKey, open.Name+"|") && activeKey != key {
			return compilerTypes.Type{}, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     token.Line,
				Column:   token.Column,
				Message:  "recursive type specialization changes generic arguments",
			}
		}
	}
	target, ok := open.Target.(parser.AdtDefinitionExpression)
	if !ok {
		return compilerTypes.Type{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "generic ADT specialization without an ADT template",
		}
	}
	specializedName := specializeTypeName(open.Name, arguments)
	typeEnvironment.BeginADT(specializedName, token.Line, token.Column)
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
