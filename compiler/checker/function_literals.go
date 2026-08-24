package checker

// Anonymous function literals checked in expression position: stored,
// passed, returned, invoked directly, or placed in any other position the
// expanded Fun<...> matrix admits. A literal that is instead the direct
// initializer of a fixed inferred binding is declaration sugar over the
// named-function form and is checked in functions.go, never here.

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkAnonymousFunctionLiteral checks a non-capturing function value. It
// shares parameter, result, and body checking with every other function
// form; the only difference from a local named function is that it carries
// no self-recursion name, because nothing here bound it to one.
func checkAnonymousFunctionLiteral(expression parser.AnonymousFunctionLiteral, context expressionContext, ctx checkContext) checkedExpression {
	if len(expression.TypeParameters) > 0 {
		return checkGenericAnonymousFunctionLiteral(expression, context, ctx)
	}

	signature, diagnostics := checkFunctionSignature(expression.Parameters, expression.Return, expression.FunKeyword, ctx.names.generics, ctx.typeEnvironment)
	if len(diagnostics) > 0 {
		return checkedExpression{token: expression.FunKeyword, diagnostics: diagnostics}
	}
	// Assigned before the body is checked, in checked-tree preorder, from
	// the same shared counter every local named function draws from - the
	// two share one hex_fun_<ordinal> stream.
	ordinal := ctx.names.newBindingID()

	body := ctx.names.closureRootScope("function literal")
	body.result = signature.result
	body.resultUse = signature.resultUse
	statements, bodyDiagnostics := bindParametersAndCheckBody(signature.parameters, expression.Body, ctx.names, body, ctx.typeEnvironment)
	if len(bodyDiagnostics) > 0 {
		return checkedExpression{token: expression.FunKeyword, diagnostics: bodyDiagnostics}
	}
	if signature.result != nil && FallsThrough(statements) {
		diagnostic := typeErrorAt(expression.End, "returning function literal may fall through without returning "+signature.result.Name)
		return checkedExpression{token: expression.FunKeyword, diagnostic: &diagnostic}
	}

	literal := &FunctionLiteral{
		Parameters:    signature.parameters,
		Result:        signature.result,
		ResultUse:     signature.resultUse,
		Type:          signature.functionType,
		Body:          statements,
		Defers:        append([]DeferredAction(nil), body.defers...),
		HelperOrdinal: ordinal,
		SourceLine:    expression.FunKeyword.Line,
		SourceColumn:  expression.FunKeyword.Column,
	}
	source := Operand{
		Kind: ExpressionOperand,
		Type: signature.functionType,
		Node: Expression{Kind: FunctionLiteralExpression, Function: literal, LocalHelperOrdinal: ordinal, ResultType: signature.functionType},
	}
	return checkedExpression{
		source: source,
		typ:    signature.functionType,
		use:    compilerTypes.NewTypeUse(signature.functionType),
		token:  expression.FunKeyword,
	}
}

// openGenericLiteral builds an ephemeral open template for a generic
// anonymous function literal, checking its type parameters against every
// name already active in an enclosing generic function or method so a
// nested redeclaration is rejected rather than silently shadowing, and
// assigning it a compiler-owned identity so two literals in disjoint scopes
// never share a generated symbol. Nothing binds the result to a name,
// because a bare literal is never itself an open-template binding - only
// one immediate specialization of it is ever checked.
func openGenericLiteral(expression parser.AnonymousFunctionLiteral, ctx checkContext) (*openGenericFunction, compilerTypes.Diagnostics) {
	diagnostics := validateGenericParameters(expression.TypeParameters)
	for _, parameter := range expression.TypeParameters {
		if _, active := ctx.names.generics.frame[parameter.Lexeme]; active {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is already declared by an enclosing function"))
		}
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	synthesized := parser.FunctionDeclaration{
		Keyword:         expression.FunKeyword,
		Name:            expression.FunKeyword,
		TypeParameters:  expression.TypeParameters,
		Parameters:      expression.Parameters,
		Return:          expression.Return,
		Body:            expression.Body,
		End:             expression.End,
		HasSyntaxErrors: expression.HasSyntaxErrors,
	}
	open := &openGenericFunction{
		Name:        "function literal",
		Parameters:  append([]lexer.Token(nil), expression.TypeParameters...),
		Declaration: synthesized,
		local:       true,
		identity:    ctx.names.newBindingID(),
	}
	generic := ctx.typeEnvironment.DeclareGeneric(open.templateKey(), len(expression.TypeParameters), parameterNamesOf(expression.TypeParameters))
	if generic == nil {
		diagnostic := unknownAt(expression.FunKeyword, "could not declare the generic template for this function literal")
		return nil, compilerTypes.Diagnostics{diagnostic}
	}
	open.Generic = generic
	return open, nil
}

// checkGenericAnonymousFunctionLiteral contextually specializes a generic
// literal from an exact expected Fun<...> type: a typed binding, or a
// parameter position expecting an exact Fun<...> type. An unspecialized
// generic literal is a compile-time template, not a runtime value, so with
// no exact expected type there is nothing to check it against.
func checkGenericAnonymousFunctionLiteral(expression parser.AnonymousFunctionLiteral, context expressionContext, ctx checkContext) checkedExpression {
	open, diagnostics := openGenericLiteral(expression, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: expression.FunKeyword, diagnostics: diagnostics}
	}
	if context.expected.Type.Signature == nil {
		diagnostic := typeErrorAt(expression.FunKeyword, "cannot infer generic parameter for function literal")
		return checkedExpression{token: expression.FunKeyword, diagnostic: &diagnostic}
	}
	specialized, diagnostic := specializeFromExpectedType(open, context.expected.Type, expression.FunKeyword, ctx)
	if diagnostic != nil {
		return checkedExpression{token: expression.FunKeyword, diagnostic: diagnostic}
	}
	return checkedExpression{
		source: Operand{
			Kind: VariableOperand,
			Type: specialized.Type,
			Name: specialized.Name,
			Node: Expression{Kind: FunctionReferenceExpression, Name: specialized.Name, ResultType: specialized.Type},
		},
		typ:      specialized.Type,
		token:    expression.FunKeyword,
		function: true,
	}
}

// checkGenericLiteralDirectCall specializes a generic literal by inferring
// its type arguments from the call's own arguments, then checks the call
// against the concrete specialization - the direct-invocation counterpart
// to checkGenericCall's named-callee inference.
func checkGenericLiteralDirectCall(call parser.CallExpression, literal parser.AnonymousFunctionLiteral, ctx checkContext) checkedExpression {
	open, diagnostics := openGenericLiteral(literal, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: literal.FunKeyword, diagnostics: diagnostics}
	}
	argumentTypes := make([]compilerTypes.Type, 0, len(call.Arguments))
	for _, argument := range call.Arguments {
		checked := checkValue(argument, ctx)
		if argumentDiagnostics := initializerDiagnostics(checked); len(argumentDiagnostics) > 0 {
			return checkedExpression{token: literal.FunKeyword, diagnostics: argumentDiagnostics}
		}
		if checked.typ == (compilerTypes.Type{}) {
			diagnostic := typeErrorAt(literal.FunKeyword, "cannot infer generic parameter for function literal")
			return checkedExpression{token: literal.FunKeyword, diagnostic: &diagnostic}
		}
		argumentTypes = append(argumentTypes, checked.typ)
	}
	arguments, diagnostic := inferTypeArguments(open, argumentTypes, ctx)
	if diagnostic != nil {
		return checkedExpression{token: literal.FunKeyword, diagnostic: diagnostic}
	}
	specialized, specializeDiagnostic := specializeFunction(open, arguments, ctx)
	if specializeDiagnostic != nil {
		return checkedExpression{token: literal.FunKeyword, diagnostic: specializeDiagnostic}
	}
	return buildConcreteCall(call, specialized, ctx, literal.FunKeyword)
}
