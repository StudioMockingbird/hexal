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
	"slices"

	"hexal/compiler/corelib"
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// corelibParamType resolves one declared parameter shape in the calling
// module's own type environment, exactly like a qualified generic's
// arguments: a core library has no environment of its own to resolve a
// compound type against. app is the call's application type for the generic
// std/http shapes and the zero Type elsewhere.
func corelibParamType(param corelib.Param, app compilerTypes.Type, ctx checkContext) compilerTypes.Type {
	switch param {
	case corelib.ParamHeap:
		return compilerTypes.Heap
	case corelib.ParamMutByteSlice:
		return ctx.typeEnvironment.SliceType(compilerTypes.UInt8, true)
	case corelib.ParamString:
		return compilerTypes.StringType
	case corelib.ParamUInt16:
		return compilerTypes.UInt16
	case corelib.ParamSize:
		return compilerTypes.SizeType
	case corelib.ParamBytes:
		return compilerTypes.HttpByteSliceType()
	case corelib.ParamConfig:
		return compilerTypes.HttpServerConfigType()
	case corelib.ParamRequest:
		return compilerTypes.HttpRequestType()
	case corelib.ParamWriter:
		return compilerTypes.HttpWriterType()
	case corelib.ParamRouter:
		return ctx.typeEnvironment.HttpRouterType(app)
	case corelib.ParamServer:
		return ctx.typeEnvironment.HttpServerType(app)
	case corelib.ParamAppPtr:
		return ctx.typeEnvironment.PtrType(app)
	case corelib.ParamHandler:
		return httpHandlerType(app, ctx)
	default:
		return compilerTypes.Type{}
	}
}

// httpHandlerType is Fun<(Ptr<App>, Request, Writer): Nil | Error>, the one
// handler shape every route and server of an application shares.
func httpHandlerType(app compilerTypes.Type, ctx checkContext) compilerTypes.Type {
	pointer := ctx.typeEnvironment.PtrType(app)
	result := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	if pointer == (compilerTypes.Type{}) || result == (compilerTypes.Type{}) {
		return compilerTypes.Type{}
	}
	return ctx.typeEnvironment.FunType([]compilerTypes.Type{pointer, compilerTypes.HttpRequestType(), compilerTypes.HttpWriterType()}, &result)
}

// corelibResultType resolves one declared result shape, unioning with Error
// where the function can fail.
func corelibResultType(result corelib.Result, app compilerTypes.Type, ctx checkContext) compilerTypes.Type {
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
	case corelib.ResultValue:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.JsonValueType(), compilerTypes.ErrorType})
	case corelib.ResultPattern:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.RegexPatternType(), compilerTypes.ErrorType})
	case corelib.ResultBool:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Bool, compilerTypes.ErrorType})
	case corelib.ResultSpanNil:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{
			compilerTypes.RegexSpanType(), compilerTypes.Nil, compilerTypes.ErrorType,
		})
	case corelib.ResultMatchNil:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{
			compilerTypes.RegexMatchType(), compilerTypes.Nil, compilerTypes.ErrorType,
		})
	case corelib.ResultRouter:
		return ctx.typeEnvironment.HttpRouterType(app)
	case corelib.ResultServer:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{ctx.typeEnvironment.HttpServerType(app), compilerTypes.ErrorType})
	case corelib.ResultConfig:
		return compilerTypes.HttpServerConfigType()
	case corelib.ResultReadBody:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.SizeType, compilerTypes.EoS, compilerTypes.ErrorType})
	case corelib.ResultBytes:
		return compilerTypes.HttpByteSliceType()
	case corelib.ResultBytesNil:
		return ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.HttpByteSliceType(), compilerTypes.Nil})
	case corelib.ResultHeaders:
		return compilerTypes.HttpHeaderSliceType()
	case corelib.ResultNoValue:
		return compilerTypes.Type{}
	default:
		return compilerTypes.Type{}
	}
}

// corelibApplication resolves the explicit type arguments of one core-library
// call: none for a non-generic function, exactly one for a generic std/http
// function. The returned type is the application type, or the zero Type when
// the function takes none.
func corelibApplication(function corelib.Function, call parser.CallExpression, property lexer.Token, ctx checkContext) (compilerTypes.Type, *checkedExpression) {
	if len(call.TypeArguments) != function.TypeParams {
		diagnostic := messageAt(property, diag.CorelibTypeArgumentCount(property.Lexeme, function.TypeParams, len(call.TypeArguments)))
		return compilerTypes.Type{}, &checkedExpression{token: property, diagnostic: &diagnostic}
	}
	if function.TypeParams == 0 {
		return compilerTypes.Type{}, nil
	}
	use, diagnostic := resolveTypeUse(call.TypeArguments[0], property, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return compilerTypes.Type{}, &checkedExpression{token: property, diagnostic: diagnostic}
	}
	return use.Type, nil
}

// checkCorelibCall resolves Alias.name(args) where Alias is bound to a known
// core-library module and name is one of its exported functions.
func checkCorelibCall(target string, function corelib.Function, call parser.CallExpression, alias parser.VariableExpression, property lexer.Token, ctx checkContext) checkedExpression {
	if function.Runtime == "" {
		return checkCorelibBuiltin(function.Builtin, call, alias, property, ctx)
	}
	app, failure := corelibApplication(function, call, property, ctx)
	if failure != nil {
		return *failure
	}
	return checkCorelibRuntimeCall(function, function.Params, nil, 0, app, call, property, ctx)
}

// checkCorelibMethodCall resolves receiver.name(args) for a registry method.
// The call site writes the method's CallParams; the receiver operand is
// spliced into the runtime argument list at the slot the row reserves for it,
// so the checked node is the one a module function with the receiver as an
// ordinary argument would have produced. A generic receiver supplies the
// application type its parameter and result shapes name.
func checkCorelibMethodCall(method corelib.Method, receiver checkedExpression, call parser.CallExpression, property lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 0 {
		diagnostic := messageAt(property, diag.CorelibTypeArgumentCount(property.Lexeme, 0, len(call.TypeArguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	app, _ := compilerTypes.HttpGenericApp(receiver.typ)
	operand := receiver.source
	return checkCorelibRuntimeCall(method.CoreFunction, method.CallParams(), &operand, method.ReceiverIndex, app, call, property, ctx)
}

// checkCorelibRuntimeCall checks the arguments a call site writes against
// params and builds the CorelibCallExpression for function's runtime entry
// point; a method's receiver, when present, is spliced in at receiverIndex.
func checkCorelibRuntimeCall(function corelib.Function, params []corelib.Param, receiver *Operand, receiverIndex int, app compilerTypes.Type, call parser.CallExpression, property lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != len(params) {
		diagnostic := messageAt(property, diag.CorelibFunctionArity(property.Lexeme, len(params), len(call.Arguments)))
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	arguments := make([]Operand, 0, len(params)+1)
	for index, param := range params {
		expected := corelibParamType(param, app, ctx)
		if expected == (compilerTypes.Type{}) {
			diagnostic := unknownAt(property)
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
	if receiver != nil {
		arguments = slices.Insert(arguments, receiverIndex, *receiver)
	}
	resultType := corelibResultType(function.Result, app, ctx)
	if function.Result != corelib.ResultNoValue && resultType == (compilerTypes.Type{}) {
		// Only the no-value cleanup contracts carry no result; every other
		// declared shape must resolve.
		diagnostic := unknownAt(property)
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
		// The rebased property keeps the caller's span: the core-library
		// builtin is the same source construct under its former namespace, so
		// the checked node's location is the call site's, not a zero span.
		callee.Property = lexer.Token{Kind: property.Kind, Lexeme: operation, Line: property.Line, Column: property.Column, Span: property.Span}
		call.Callee = callee
		return call
	}
	namespace := func(name string) parser.VariableExpression {
		return parser.VariableExpression{Name: lexer.Token{Kind: alias.Name.Kind, Lexeme: name, Line: alias.Name.Line, Column: alias.Name.Column, Span: alias.Name.Span}}
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
		diagnostic := unknownAt(property)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
}
