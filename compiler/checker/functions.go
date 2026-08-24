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

// collectFunctionSignature validates a function declaration's name and
// resolves its signature, binding it into names before any module-level
// body is checked: this is what lets a forward or mutually recursive call
// resolve, since every signature this pass binds is visible to every body
// checkFunctionBody later checks, in either source-order direction. A
// generic declaration is registered as an open template by
// registerGenericFunction and returns a zero signature; its body is never
// checked here, only lazily at specialization time, which needs no separate
// "check body later" step of its own.
//
// rootValueNamesSoFar names every root value declared earlier in source but
// not yet bound in names, because root values are checked in the module's
// later pass while signature collection runs first: this set is what lets a
// function collide correctly against a root value declared earlier in
// source but not yet processed, keeping diagnostic ownership on whichever
// declaration is actually later regardless of which pass reaches it first.
func collectFunctionSignature(declaration parser.FunctionDeclaration, ctx checkContext, rootValueNamesSoFar map[string]bool) (functionSignature, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
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
	if compilerTypes.IsProtectedTypeName(name) || ctx.typeEnvironment.Contains(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "value "+name+" is already declared as a type"))
	} else if ctx.names.declaredHere(name) || rootValueNamesSoFar[name] {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, name+" is already declared"))
	} else if method, taken := ctx.names.methods.cNames[name]; taken {
		// hex_f_ is not injective: Point_translate and impl Point.translate
		// share one private C spelling, so one of them has to go.
		diagnostics = append(diagnostics, collisionDiagnostic(name, method, declaration.Name))
	}

	if len(diagnostics) == 0 && len(declaration.TypeParameters) > 0 {
		return functionSignature{}, registerGenericFunction(declaration, ctx)
	}
	// An incomplete signature cannot be bound, so the body is not checked
	// either: every name in it would resolve against a fiction.
	if len(diagnostics) > 0 {
		return functionSignature{}, diagnostics
	}

	signature, signatureDiagnostics := checkFunctionSignature(declaration.Parameters, declaration.Return, declaration.Name, ctx.names.generics, ctx.typeEnvironment)
	if len(signatureDiagnostics) > 0 {
		return functionSignature{}, signatureDiagnostics
	}

	parameterUses := make([]compilerTypes.TypeUse, 0, len(signature.parameters))
	for _, parameter := range signature.parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	functionUse := compilerTypes.FunctionTypeUse(signature.functionType, parameterUses, signature.resultUse)
	ctx.names.module[name] = binding{typ: signature.functionType, use: functionUse, kind: functionBinding}
	return signature, nil
}

// checkFunctionBody checks a module function's body against its
// already-collected signature (collectFunctionSignature). It runs in the
// module's second pass, after every module-level signature is bound, so a
// call to any other module function or method - earlier, later, or mutually
// recursive - resolves.
func checkFunctionBody(declaration parser.FunctionDeclaration, signature functionSignature, ctx checkContext, analyzeReturns bool) (FunctionDeclaration, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	checked := FunctionDeclaration{
		Name:         name,
		Parameters:   signature.parameters,
		Result:       signature.result,
		ResultUse:    signature.resultUse,
		Type:         signature.functionType,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
		Exported:     declaration.Exported,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	body := ctx.names.closureRootScope(name)
	body.result = signature.result
	body.resultUse = signature.resultUse
	statements, bodyDiagnostics := bindParametersAndCheckBody(signature.parameters, declaration.Body, ctx.names, body, ctx.typeEnvironment)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = statements
	checked.Defers = append(checked.Defers, body.defers...)

	if analyzeReturns && signature.result != nil && len(bodyDiagnostics) == 0 && FallsThrough(checked.Body) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.End,
			fmt.Sprintf("returning %s may fall through without returning %s", name, signature.result.Name)))
	}
	return checked, diagnostics
}

// functionSignature is the resolved, not-yet-bound parameter list, result,
// and Fun<...> type shared by every function form.
type functionSignature struct {
	parameters   []FunctionParameter
	result       *compilerTypes.Type
	resultUse    *compilerTypes.TypeUse
	functionType compilerTypes.Type
}

// checkFunctionSignature resolves one written parameter list and optional
// result against typeEnvironment and generics. It is the one shared
// implementation behind a module FunctionDeclaration and a
// FunctionLiteralExpression: both resolve their signature identically and
// differ only in how their own name, if any, is bound around the call.
func checkFunctionSignature(written []parser.Parameter, resultExpr parser.TypeExpression, fallback lexer.Token, generics *genericTable, typeEnvironment *compilerTypes.Environment) (functionSignature, compilerTypes.Diagnostics) {
	parameters, diagnostics := checkParameters(written, typeEnvironment, generics)
	parameterTypes := make([]compilerTypes.Type, 0, len(parameters))
	for _, parameter := range parameters {
		parameterTypes = append(parameterTypes, parameter.Type)
	}
	result, resultUse, resultDiagnostics := checkResultType(resultExpr, fallback, typeEnvironment, generics)
	diagnostics = append(diagnostics, resultDiagnostics...)
	if len(diagnostics) > 0 {
		return functionSignature{}, diagnostics
	}
	functionType := typeEnvironment.FunType(parameterTypes, result)
	if functionType.Signature == nil {
		return functionSignature{}, compilerTypes.Diagnostics{unknownAt(fallback, "could not construct the function type for "+fallback.Lexeme)}
	}
	return functionSignature{parameters: parameters, result: result, resultUse: resultUse, functionType: functionType}, nil
}

// bindParametersAndCheckBody binds each resolved parameter into body (already
// constructed by the caller with the visibility rules appropriate to a
// module function, local function, or anonymous literal) and checks the
// statements. enclosing supplies the import-alias check, which is a property
// of the declaring scope regardless of which scope the parameters are bound
// into.
func bindParametersAndCheckBody(parameters []FunctionParameter, statements []parser.Statement, enclosing, body *scope, typeEnvironment *compilerTypes.Environment) ([]Statement, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for index := range parameters {
		if enclosing.importAlias(parameters[index].Name) {
			token := lexer.Token{Line: parameters[index].SourceLine, Column: parameters[index].SourceColumn, Lexeme: parameters[index].Name}
			diagnostics = append(diagnostics, nameErrorAt(token, "import alias "+parameters[index].Name+" conflicts with an existing name"))
			continue
		}
		parameters[index].Binding = body.newBindingID()
		body.local[parameters[index].Name] = binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
	}
	checkedStatements, bodyDiagnostics := checkBody(statements, checkContext{names: body, typeEnvironment: typeEnvironment})
	diagnostics = append(diagnostics, bodyDiagnostics...)
	return checkedStatements, diagnostics
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
	// A result is returned by value, so it goes through the shared position
	// model like every other copy-requiring position. Fun is
	// now valid as a function result via the expanded Storable matrix.
	if !compilerTypes.Eligible(resolved, compilerTypes.PositionFunctionResult) {
		return nil, nil, compilerTypes.Diagnostics{typeErrorAt(fallback, "function result "+resolved.Name+" is not shallow-copyable")}
	}
	return &resolved, &resolvedUse, nil
}

// directFunctionLiteralSugar reports whether declaration is declaration
// sugar over a function form rather than ordinary runtime data: an inferred
// fixed binding (`name := ...`) whose initializer is directly an anonymous
// function literal. A written type or `mut` makes it ordinary data; so does
// a call or other suffix on the initializer, which the parser represents as
// a CallExpression wrapping the literal, not this kind at all. The parser
// never wraps a redundant grouping, so no stripping is needed here.
func directFunctionLiteralSugar(declaration parser.Declaration) (parser.AnonymousFunctionLiteral, bool) {
	if declaration.Mutable || declaration.Type != nil {
		return parser.AnonymousFunctionLiteral{}, false
	}
	literal, ok := declaration.Initializer.(parser.AnonymousFunctionLiteral)
	return literal, ok
}

// asFunctionDeclaration synthesizes the module-level named-function form
// that a direct function-literal declaration is sugar for: name is the
// source binding name, and the signature and body are the literal's own.
func asFunctionDeclaration(name lexer.Token, literal parser.AnonymousFunctionLiteral) parser.FunctionDeclaration {
	return parser.FunctionDeclaration{
		Keyword:         literal.FunKeyword,
		Name:            name,
		TypeParameters:  literal.TypeParameters,
		Parameters:      literal.Parameters,
		Return:          literal.Return,
		Body:            literal.Body,
		End:             literal.End,
		HasSyntaxErrors: literal.HasSyntaxErrors,
	}
}
