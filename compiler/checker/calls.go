package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func checkCallStatement(call parser.CallExpression, names *scope, typeEnvironment *compilerTypes.Environment) (CallStatement, compilerTypes.Diagnostics) {
	checked := checkCall(call, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return CallStatement{}, diagnostics
	}
	return CallStatement{
		Call:         checked.source,
		SourceLine:   checked.token.Line,
		SourceColumn: checked.token.Column,
	}, nil
}

// checkCall resolves a callee, checks arity, and checks each argument in its
// parameter's expected-type position so RFC 0003/0009 contextual literals and
// RFC 0007's MutPtr-to-Ptr weakening both apply. The returned type is the zero
// Type for a no-return callee; only a call statement accepts that.
func checkCall(call parser.CallExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if property, isMethod := call.Callee.(parser.PropertyExpression); isMethod {
		return checkMethodCall(call, property, names, typeEnvironment)
	}
	callee, ok := call.Callee.(parser.VariableExpression)
	if !ok {
		return checkedExpression{token: call.OpenParen, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     call.OpenParen.Line,
			Column:   call.OpenParen.Column,
			Message:  "a call's callee must be a function name or a method selection",
		}}
	}
	if callee.Name.Kind == lexer.Self {
		return checkedExpression{token: callee.Name, diagnostic: selfNotBoundDiagnostic(callee.Name)}
	}
	// RFC 0030: the protected builtin `print` resolves before ordinary
	// free-function lookup and cannot be redeclared or referenced as a
	// value.
	if callee.Name.Lexeme == "print" {
		return checkPrintCall(call, callee.Name, names, typeEnvironment)
	}
	// RFC 0042: the protected layout queries resolve before ordinary
	// free-function lookup.
	if layoutBuiltins[callee.Name.Lexeme] {
		return checkLayoutCall(call, callee.Name, names, typeEnvironment)
	}

	name := callee.Name.Lexeme
	bound, status := names.lookup(name)
	switch status {
	case nameMissing:
		diagnostic := typeErrorAt(callee.Name, "unknown function "+name+"; functions must be declared before use")
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	case nameModuleData:
		diagnostic := moduleDataDiagnostic(names.owner, name, callee.Name)
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if bound.kind == genericFunctionBinding {
		return checkGenericCall(call, bound, name, callee.Name, names, typeEnvironment)
	}
	// RFC 0010: a call resolves the callee's effective type from the
	// branch-local flow facts, so a null test can narrow a nullable Fun<...>
	// binding to its callable member. The declared binding itself still holds
	// the nullable storage type.
	calleeType := bound.typ
	if bound.kind != functionBinding {
		if narrowed, ok := names.flow.narrowedType(bound.id); ok {
			calleeType = narrowed
		}
	}
	signature := calleeType.Signature
	if signature == nil {
		diagnostic := typeErrorAt(callee.Name, name+" is not callable")
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if compilerTypes.IsNullable(calleeType) {
		// A nullable function pointer holds a Fun member only after a null
		// test proved it; calling the union itself could jump through nil.
		diagnostic := typeErrorAt(callee.Name, calleeType.Name+" may be Nil; narrow it before calling it")
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if len(call.Arguments) != len(signature.Parameters) {
		diagnostic := typeErrorAt(callee.Name,
			fmt.Sprintf("%s expects %d arguments, got %d", name, len(signature.Parameters), len(call.Arguments)))
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}

	parameterUses := bound.use.Parameters
	if len(parameterUses) != len(signature.Parameters) {
		parameterUses = make([]compilerTypes.TypeUse, len(signature.Parameters))
		for index, parameter := range signature.Parameters {
			parameterUses[index] = compilerTypes.NewTypeUse(parameter)
		}
	}
	arguments, diagnostics := checkArguments(name, parameterUses, call.Arguments, callee.Name, names, typeEnvironment)
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Name, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}

	calleeNode := variableNodeWithBinding(name, bound.id)
	if bound.kind == functionBinding {
		calleeNode = Expression{Kind: FunctionReferenceExpression, Name: name, ResultType: bound.typ}
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
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node},
		typ:    resultType,
		token:  callee.Name,
	}
}

// checkQualifiedFunctionCall resolves Alias.name(args) where Alias is an
// import alias: the callee is the target module's exported function. The call
// is checked against the recorded signature exactly like a local call; the
// node carries the target module id for the module phase. A name that is not
// an exported concrete function may be an exported generic template, which
// the call specializes against the defining module's collection (RFC 0034
// Task 6); only then is it the visibility failure.
func checkQualifiedFunctionCall(call parser.CallExpression, property lexer.Token, target string, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	function, ok := names.registry.exportedFunction(target, property.Lexeme)
	if !ok {
		if open, generic := names.registry.genericFunction(target, property.Lexeme); generic {
			return checkQualifiedGenericCall(call, open, property, target, names, typeEnvironment)
		}
		diagnostic := privateToModuleDiagnostic(property, property.Lexeme, target)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	signature := function.Type.Signature
	if signature == nil {
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "exported function record without a signature",
		}}
	}
	if len(call.Arguments) != len(signature.Parameters) {
		diagnostic := typeErrorAt(property,
			fmt.Sprintf("%s expects %d arguments, got %d", function.Name, len(signature.Parameters), len(call.Arguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(function.Parameters))
	for _, parameter := range function.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	arguments, diagnostics := checkArguments(function.Name, parameterUses, call.Arguments, property, names, typeEnvironment)
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
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: function.Name, Node: node},
		typ:    resultType,
		token:  property,
	}
}

// checkQualifiedGenericCall specializes an imported module's exported generic
// function for the concrete request at hand and checks the call against the
// specialized signature. The specialization is resolved and its body re-checked
// in the requesting module's environment, exactly like a local generic call,
// but the record is stored into the defining module's registry collection, so
// its checked output carries the request (RFC 0034 Task 6). Repeated requests
// of one (declaration, argument) pair reuse the one recorded specialization.
// The requested body was not part of the defining module's starvation scan,
// which ran before this request arrived; re-scanning imported generic bodies
// is a codegen-phase follow-up.
func checkQualifiedGenericCall(call parser.CallExpression, open *openGenericFunction, property lexer.Token, target string, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	var arguments []compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		arguments = make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, property, typeEnvironment, names.generics)
			if diagnostic != nil {
				return checkedExpression{token: property, diagnostic: diagnostic}
			}
			arguments = append(arguments, argumentUse.Type)
		}
		if len(arguments) != open.Generic.Arity {
			diagnostic := typeErrorAt(property, "explicit generic argument count does not match declaration")
			return checkedExpression{token: property, diagnostic: &diagnostic}
		}
	} else {
		argumentTypes := make([]compilerTypes.Type, 0, len(call.Arguments))
		for _, argument := range call.Arguments {
			checked := checkValue(argument, names, typeEnvironment)
			if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
				return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			argumentTypes = append(argumentTypes, checked.typ)
		}
		inferred, diagnostic := inferTypeArguments(open, argumentTypes, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		arguments = inferred
	}
	specialized, diagnostic := specializeFunctionIn(open, arguments, names, typeEnvironment, names.registry.specializationStore(target))
	if diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	signature := specialized.Type.Signature
	if signature == nil {
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "specialized function record without a signature",
		}}
	}
	if len(call.Arguments) != len(signature.Parameters) {
		diagnostic := typeErrorAt(property,
			fmt.Sprintf("%s expects %d arguments, got %d", specialized.Name, len(signature.Parameters), len(call.Arguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, 0, len(specialized.Parameters))
	for _, parameter := range specialized.Parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	argumentsOperands, diagnostics := checkArguments(specialized.Name, parameterUses, call.Arguments, property, names, typeEnvironment)
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
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: specialized.Name, Node: node},
		typ:    resultType,
		token:  property,
	}
}

// checkArguments checks each written argument in its parameter's expected-type
// position, so contextual literals and RFC 0007 weakening both apply. Callee
// is only used to spell diagnostics.
func checkArguments(callee string, expected []compilerTypes.TypeUse, written []parser.Expression, token lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) ([]Operand, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	arguments := make([]Operand, 0, len(written))
	for index, argument := range written {
		want := expected[index]
		checked := checkInitializer(argument, want, token, names, typeEnvironment)
		if argumentDiagnostics := initializerDiagnostics(checked); len(argumentDiagnostics) > 0 {
			diagnostics = append(diagnostics, argumentDiagnostics...)
			continue
		}
		if checked.typ != (compilerTypes.Type{}) && !assignable(want.Type, checked.typ) {
			diagnostics = append(diagnostics, typeErrorAt(checked.token,
				fmt.Sprintf("%s argument %d requires %s, got %s", callee, index+1, want.Type.Name, checked.typ.Name)))
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

// checkCallValue is the value-position wrapper: a callee that returns nothing
// has no value to bind, so it is rejected here rather than reaching an
// initializer with a zero type.
func checkCallValue(call parser.CallExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	checked := checkCall(call, names, typeEnvironment)
	if len(initializerDiagnostics(checked)) > 0 {
		return checked
	}
	if checked.typ.Name == "" {
		diagnostic := typeErrorAt(checked.token, checked.token.Lexeme+" produces no value")
		return checkedExpression{token: checked.token, diagnostic: &diagnostic}
	}
	return checked
}
