package checker

// Core-library module functions: calls reached through an import alias bound
// to a "std/..." module (Prog.arguments(), Ent.fill(into), ...). A core
// library publishes no ModuleRegistry entry -- it has no Hexal source and no
// defining scope -- so its calls resolve directly against the compiler-owned
// corelib.Modules table instead of the ordinary exported-interface path.

import (
	"fmt"

	"hexal/compiler/corelib"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// corelibParamType resolves one declared parameter shape in the calling
// module's own type environment, exactly like a qualified generic's
// arguments: a core library has no environment of its own to resolve a
// compound type against.
func corelibParamType(param corelib.Param, ctx checkContext) compilerTypes.Type {
	switch param {
	case corelib.ParamHeap:
		return compilerTypes.Heap
	case corelib.ParamMutByteSlice:
		return ctx.typeEnvironment.SliceType(compilerTypes.UInt8, true)
	default:
		return compilerTypes.Type{}
	}
}

// corelibResultType resolves one declared result shape, unioning with Error
// where the function can fail.
func corelibResultType(result corelib.Result, ctx checkContext) compilerTypes.Type {
	switch result {
	case corelib.ResultString:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.StringType, compilerTypes.ErrorType})
	case corelib.ResultStringSlice:
		slice := ctx.typeEnvironment.SliceType(compilerTypes.StringType, false)
		if slice == (compilerTypes.Type{}) {
			return compilerTypes.Type{}
		}
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{slice, compilerTypes.ErrorType})
	case corelib.ResultNil:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	case corelib.ResultSize:
		return compilerTypes.SizeType
	default:
		return compilerTypes.Type{}
	}
}

// checkCorelibCall resolves Alias.name(args) where Alias is bound to a known
// core-library module. ok is false only when name is not one of that
// module's exported functions, so the caller can fall through to its other
// alias-qualified diagnostics; every other failure is a checkedExpression
// diagnostic.
func checkCorelibCall(target string, call parser.CallExpression, property lexer.Token, ctx checkContext) checkedExpression {
	function, ok := corelib.Lookup(target, property.Lexeme)
	if !ok {
		diagnostic := privateToModuleDiagnostic(property, property.Lexeme, target)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	if len(call.Arguments) != len(function.Params) {
		diagnostic := typeErrorAt(property, fmt.Sprintf("%s expects %d argument(s); got %d", property.Lexeme, len(function.Params), len(call.Arguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	arguments := make([]Operand, 0, len(function.Params))
	for index, param := range function.Params {
		expected := corelibParamType(param, ctx)
		if expected == (compilerTypes.Type{}) {
			diagnostic := unknownAt(property, "could not resolve a core-library parameter type")
			return checkedExpression{token: property, diagnostic: &diagnostic}
		}
		checked := checkInitializer(call.Arguments[index], compilerTypes.NewTypeUse(expected), tokenOf(call.Arguments[index]), ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(expected, checked.typ) {
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(expected, checked.typ, checked.token))}
		}
		arguments = append(arguments, checked.source)
	}
	resultType := corelibResultType(function.Result, ctx)
	if resultType == (compilerTypes.Type{}) {
		diagnostic := unknownAt(property, "could not construct a core-library result type")
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	node := Expression{
		Kind:         CorelibCallExpression,
		Name:         function.Runtime,
		Arguments:    arguments,
		ResultType:   resultType,
		SourceLine:   property.Line,
		SourceColumn: property.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: function.Runtime, Node: node}
	return checkedExpression{source: source, typ: resultType, token: property}
}
