package checker

// Core-library module functions: calls reached through an import alias bound
// to a "std/..." module (Prog.arguments(), Ent.fill(into), Fs.open(...), ...).
// A core library publishes no ModuleRegistry entry -- it has no Hexal source
// and no defining scope -- so its calls resolve directly against the
// compiler-owned core-library registry in compiler/specdata instead of the
// ordinary exported-interface path. A moved capability's module function reuses the
// same checker operation as its former protected namespace, so the checked
// tree and generated C are unchanged.

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
// core-library module and name is one of its exported functions.
func checkCorelibCall(target string, function corelib.Function, call parser.CallExpression, alias parser.VariableExpression, property lexer.Token, ctx checkContext) checkedExpression {
	if function.Runtime == "" {
		return checkCorelibBuiltin(function.Builtin, call, alias, property, ctx)
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
		Kind:       CorelibCallExpression,
		Name:       function.Runtime,
		Arguments:  arguments,
		ResultType: resultType,
		Span:       property.Span,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: function.Runtime, Node: node}
	return checkedExpression{source: source, typ: resultType, token: property}
}

// checkCorelibBuiltin routes one moved capability's module function to the
// existing checker operation. The type-call checkers recognize their former
// namespace and operation names, so this presents each with exactly those
// names; the resulting checked node is the one the old spelling produced.
func checkCorelibBuiltin(builtin string, call parser.CallExpression, alias parser.VariableExpression, property lexer.Token, ctx checkContext) checkedExpression {
	rebased := func(operation string) parser.CallExpression {
		callee, _ := call.Callee.(parser.PropertyExpression)
		callee.Property = lexer.Token{Kind: property.Kind, Lexeme: operation, Line: property.Line, Column: property.Column}
		call.Callee = callee
		return call
	}
	namespace := func(name string) parser.VariableExpression {
		return parser.VariableExpression{Name: lexer.Token{Kind: alias.Name.Kind, Lexeme: name, Line: alias.Name.Line, Column: alias.Name.Column}}
	}
	switch builtin {
	case "io_stdin", "io_stdout", "io_stderr":
		return checkIOTypeCall(rebased(property.Lexeme), namespace("IO"), ctx)
	case "io_bytes_over":
		return checkBytesTypeCall(rebased("over"), namespace("Bytes"), ctx)
	case "file_open":
		return checkFileTypeCall(rebased("open"), namespace("File"), ctx)
	case "time_nanoseconds", "time_microseconds", "time_milliseconds", "time_seconds":
		return checkTimeTypeCall(rebased(property.Lexeme), namespace("Duration"), ctx)
	case "time_now":
		return checkTimeTypeCall(rebased("now"), namespace("Instant"), ctx)
	case "time_wall_time":
		return checkTimeTypeCall(rebased("now"), namespace("WallTime"), ctx)
	case "time_sleep":
		return checkTaskSleepCall(call, property, ctx)
	case "address_parse":
		return checkAddressTypeCall(rebased("parse"), namespace("Address"), ctx)
	case "dns_resolve":
		return checkDnsTypeCall(rebased("resolve"), namespace("Dns"), ctx)
	case "tcp_connect", "tcp_listen":
		return checkTcpTypeCall(rebased(property.Lexeme), namespace("Tcp"), ctx)
	case "process_start":
		return checkProcessTypeCall(rebased("start"), namespace("Process"), ctx)
	case "signals_subscribe":
		return checkSignalsTypeCall(call, property, ctx)
	case "terminal_is_attached", "terminal_size":
		return checkTerminalTypeCall(rebased(property.Lexeme), namespace("Terminal"), ctx)
	default:
		diagnostic := unknownAt(property, "unknown core-library builtin "+builtin)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
}
