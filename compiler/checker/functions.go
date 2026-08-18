package checker

// Function declarations, parameters, and results: declaration structure and
// validation.

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// FunctionDeclaration is a checked module-level function. Type is the Fun<...>
// type its name produces in a value position; Result is nil when the function
// returns no value.
type FunctionDeclaration struct {
	Name         string
	Parameters   []FunctionParameter
	Result       *compilerTypes.Type
	ResultUse    *compilerTypes.TypeUse
	Type         compilerTypes.Type
	Body         []Statement
	Defers       []DeferredAction
	SourceLine   int
	SourceColumn int
	Exported     bool // external linkage + prototype in this module's header
}

func (FunctionDeclaration) statementNode() {}

// FunctionParameter is one resolved parameter. Parameters are fixed bindings,
// so no mutability field exists.
type FunctionParameter struct {
	Name         string
	Binding      BindingID
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	SourceLine   int
	SourceColumn int
}

// checkFunctionDeclaration follows the single-pass order exactly:
// resolve the complete signature, bind the name, then check the body with
// everything visible at this source position. Binding before the body is what
// makes direct self-recursion work; no later signature is collected.
func checkFunctionDeclaration(declaration parser.FunctionDeclaration, names *scope, typeEnvironment *compilerTypes.Environment, analyzeReturns bool) (FunctionDeclaration, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	checked := FunctionDeclaration{
		Name:         name,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
		Exported:     declaration.Exported,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	if name == "print" {
		// The protected builtin name cannot be bound by a function
		// declaration.
		diagnostics = append(diagnostics, nameErrorAt(declaration.Name, "print is a protected built-in name"))
	}
	if layoutBuiltins[name] {
		// The layout query names cannot be bound by a function
		// declaration.
		diagnostics = append(diagnostics, nameErrorAt(declaration.Name, name+" is a protected built-in name"))
	}
	if compilerTypes.IsProtectedTypeName(name) || typeEnvironment.Contains(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "value "+name+" is already declared as a type"))
	} else if names.declaredHere(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, name+" is already declared"))
	} else if method, taken := names.methods.cNames[name]; taken {
		// hex_f_ is not injective: Point_translate and impl Point.translate
		// share one private C spelling, so one of them has to go.
		diagnostics = append(diagnostics, collisionDiagnostic(name, method, declaration.Name))
	}

	if len(diagnostics) == 0 && len(declaration.TypeParameters) > 0 {
		return checked, registerGenericFunction(declaration, names, typeEnvironment)
	}

	parameters, parameterDiagnostics := checkParameters(declaration.Parameters, typeEnvironment, names.generics)
	diagnostics = append(diagnostics, parameterDiagnostics...)
	parameterTypes := make([]compilerTypes.Type, 0, len(parameters))
	for _, parameter := range parameters {
		parameterTypes = append(parameterTypes, parameter.Type)
	}

	result, resultUse, resultDiagnostics := checkResultType(declaration.Return, declaration.Name, typeEnvironment, names.generics)
	diagnostics = append(diagnostics, resultDiagnostics...)

	// An incomplete signature cannot be bound, so the body is not checked
	// either: every name in it would resolve against a fiction.
	if len(diagnostics) > 0 {
		return checked, diagnostics
	}

	functionType := typeEnvironment.FunType(parameterTypes, result)
	if functionType.Signature == nil {
		return checked, compilerTypes.Diagnostics{{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "could not construct the function type for " + name,
		}}
	}
	checked.Parameters = parameters
	checked.Result = result
	checked.ResultUse = resultUse
	checked.Type = functionType

	parameterUses := make([]compilerTypes.TypeUse, 0, len(parameters))
	for _, parameter := range parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	functionUse := compilerTypes.FunctionTypeUse(functionType, parameterUses, resultUse)
	names.module[name] = binding{typ: functionType, use: functionUse, kind: functionBinding}

	body := &scope{
		module:    names.module,
		local:     make(map[string]binding, len(parameters)),
		owner:     name,
		result:    result,
		resultUse: resultUse,
		methods:   names.methods,
		function:  true,
		nextID:    names.nextID,
		flow:      newFlowState(),
		generics:  names.generics,
		registry:  names.registry,
		moduleID:  names.moduleID,
	}
	for index := range parameters {
		// No nested scope may shadow an import alias; a conflicting
		// parameter is rejected like any other redeclaration.
		if names.importAlias(parameters[index].Name) {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.NameError,
				Stage:    "checker",
				Line:     parameters[index].SourceLine,
				Column:   parameters[index].SourceColumn,
				Message:  "import alias " + parameters[index].Name + " conflicts with an existing name",
			})
			continue
		}
		parameters[index].Binding = names.newBindingID()
		bound := binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
		body.local[parameters[index].Name] = bound
	}
	checked.Parameters = parameters

	statements, bodyDiagnostics := checkBody(declaration.Body, body, typeEnvironment)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = statements
	checked.Defers = append(checked.Defers, body.defers...)

	if analyzeReturns && result != nil && len(bodyDiagnostics) == 0 && FallsThrough(checked.Body) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.End,
			fmt.Sprintf("returning %s may fall through without returning %s", name, result.Name)))
	}
	return checked, diagnostics
}

// checkParameters resolves one written parameter list. Functions and methods
// share it; `self` is implicit and never appears here.
func checkParameters(written []parser.Parameter, typeEnvironment *compilerTypes.Environment, generics *genericTable) ([]FunctionParameter, compilerTypes.Diagnostics) {
	parameters := make([]FunctionParameter, 0, len(written))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(written))
	for _, parameter := range written {
		parameterName := parameter.Name.Lexeme
		if compilerTypes.IsProtectedTypeName(parameterName) || typeEnvironment.Contains(parameterName) {
			diagnostics = append(diagnostics, typeErrorAt(parameter.Name, "value "+parameterName+" is already declared as a type"))
			continue
		}
		if seen[parameterName] {
			diagnostics = append(diagnostics, typeErrorAt(parameter.Name, "parameter "+parameterName+" is declared more than once"))
			continue
		}
		resolvedUse, diagnostic := resolveTypeUse(parameter.Type, parameter.Name, typeEnvironment, generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		resolved := resolvedUse.Type
		if diagnostic := valueTypeDiagnostic(parameter.Type, parameter.Name, resolved); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		// Parameters copy their values at every call, so they go through
		// the shared position model like every other copy-requiring
		// position.
		if !compilerTypes.Eligible(resolved, compilerTypes.PositionFunctionParam) {
			diagnostics = append(diagnostics, typeErrorAt(parameter.Name,
				"function parameter "+resolved.Name+" is not shallow-copyable"))
			continue
		}
		seen[parameterName] = true
		parameters = append(parameters, FunctionParameter{
			Name:         parameterName,
			Type:         resolved,
			TypeUse:      resolvedUse,
			SourceLine:   parameter.Name.Line,
			SourceColumn: parameter.Name.Column,
		})
	}
	return parameters, diagnostics
}

// checkResultType resolves an optional return clause. A nil expression is a
// no-return declaration; a Fun<...> result is a deferred position.
func checkResultType(written parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (*compilerTypes.Type, *compilerTypes.TypeUse, compilerTypes.Diagnostics) {
	if written == nil {
		return nil, nil, nil
	}
	resolvedUse, diagnostic := resolveTypeUse(written, fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return nil, nil, compilerTypes.Diagnostics{*diagnostic}
	}
	resolved := resolvedUse.Type
	if unknownDiagnostic := valueTypeDiagnostic(written, fallback, resolved); unknownDiagnostic != nil {
		return nil, nil, compilerTypes.Diagnostics{*unknownDiagnostic}
	}
	if resolved.Signature != nil {
		// Supported-position whitelist: a Fun<...> result needs C declarator
		// rules that are not supported.
		return nil, nil, compilerTypes.Diagnostics{typeErrorAt(fallback, "returning "+resolved.Name+" is not supported")}
	}
	// A result is returned by value, so it goes through the shared position
	// model like every other copy-requiring position.
	if !compilerTypes.Eligible(resolved, compilerTypes.PositionFunctionResult) {
		return nil, nil, compilerTypes.Diagnostics{typeErrorAt(fallback, "function result "+resolved.Name+" is not shallow-copyable")}
	}
	return &resolved, &resolvedUse, nil
}
