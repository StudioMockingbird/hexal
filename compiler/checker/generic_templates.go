// generic_templates.go owns generic type-use resolution and template
// registration: qualified and written type arguments, object and ADT
// specialization, and function and method template registration.
package checker

import (
	"fmt"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// specializeTypeUse resolves a written generic type use to its concrete
// specialization. Object targets create fresh nominal objects; plain type
// targets resolve under the parameter frame.
// resolveQualifiedGenericTypeUse resolves Alias.Name<Arguments>: an exported
// generic type of an imported module, specialized for the concrete request.
// Concrete arguments are resolved in the requesting module's own environment
// (typeEnvironment, generics), exactly like a qualified generic call's
// arguments; the open declaration itself is then specialized against its
// defining module's own retained scope and type environment, never the
// requester's, so its layout closes over the defining module's own private
// and exported names.
func resolveQualifiedGenericTypeUse(expression parser.QualifiedGenericTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if generics == nil || generics.registry == nil {
		diagnostic := unknownAt(expression.Module, "qualified generic type use outside a generic table")
		return compilerTypes.TypeUse{}, &diagnostic
	}
	target, ok := generics.registry.importTarget(generics.moduleID, expression.Module.Lexeme)
	if !ok {
		message := "unknown module alias " + expression.Module.Lexeme
		return compilerTypes.TypeUse{}, diagnosticAt(moduleErrorAt(expression.Module, message))
	}
	open, ok := generics.registry.genericType(target, expression.Name.Lexeme)
	if !ok {
		diagnostic := privateToModuleDiagnostic(expression.Name, expression.Name.Lexeme, target)
		return compilerTypes.TypeUse{}, &diagnostic
	}
	if len(expression.Arguments) != open.Declaration.Arity {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, fmt.Sprintf("generic type %s expects %d type arguments; got %d", open.Name, open.Declaration.Arity, len(expression.Arguments))))
	}
	arguments := make([]compilerTypes.Type, 0, len(expression.Arguments))
	for _, argumentExpression := range expression.Arguments {
		argumentUse, diagnostic := resolveTypeUse(argumentExpression, expression.Name, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		arguments = append(arguments, argumentUse.Type)
	}
	definingCtx, ok := generics.registry.definingContext(target)
	if !ok {
		diagnostic := unknownAt(expression.Name, "defining module specialization environment is unavailable for "+target)
		return compilerTypes.TypeUse{}, &diagnostic
	}
	use, diagnostic := specializeTypeUseArguments(open, arguments, expression.Name, definingCtx.typeEnvironment, definingCtx.names.generics)
	return use, diagnosticInDefiningModule(diagnostic, definingCtx.names.logicalKey)
}

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
		Name:    compilerTypes.SanitizeIdentifier(specializedName),
		Type:    completed,
		TypeUse: compilerTypes.NewTypeUse(completed),
		Span:    token.Span,
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

// isGenericReceiver reports whether a method receiver is a generic type use
// naming the owner's parameters.
func isGenericReceiver(expression parser.TypeExpression) bool {
	_, ok := expression.(parser.GenericTypeExpression)
	return ok
}

// registerGenericMethod validates and stores one generic method declaration as
// an open template. The receiver must be the owner's bare generic parameters
// in declaration order.
func registerGenericMethod(declaration parser.MethodDeclaration, ctx checkContext) compilerTypes.Diagnostics {
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
		Name:    compilerTypes.SanitizeIdentifier(specializedName),
		Type:    completed,
		TypeUse: compilerTypes.NewTypeUse(completed),
		Span:    token.Span,
	})
	return completed, nil
}
