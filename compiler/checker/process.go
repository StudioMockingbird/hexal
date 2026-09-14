package checker

// The libuv process/IPC builtins: Process.start and the Process/Pipe
// instance methods. Environment, EnvironmentVariable, ProcessStream,
// ProcessOptions, ExitStatus, and StartedProcess are ordinary builtin
// struct/ADT types constructed and matched through the general object/ADT
// machinery; only the operations below need dedicated checking. Close
// tracking reuses the same provenance machinery File and networking share.

import (
	"fmt"

	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkProcessTypeCall resolves Process.start(options).
func checkProcessTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "start" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Process has no such operation; use Process.start(options)"))}
	}
	if len(call.Arguments) != 1 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("start expects 1 argument (options: ProcessOptions); got %d", len(call.Arguments))))}
	}
	options := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.ProcessOptionsType), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(options); len(diagnostics) > 0 {
		return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if !assignable(compilerTypes.ProcessOptionsType, options.typ) {
		return checkedExpression{token: options.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.ProcessOptionsType, options.typ, options.token))}
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.StartedProcessType, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the StartedProcess | Error result union"))}
	}
	return networkNode("process_start", nil, []Operand{options.source}, compilerTypes.Type{}, resultUnion, property)
}

// checkProcessMethodCall resolves wait, terminate, and close on a Process.
func checkProcessMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "wait", "terminate", "close":
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Process has no method "+name+"; use wait, terminate, or close"))}
	}
	if len(call.TypeArguments) != 0 || len(call.Arguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, name+" expects no arguments"))}
	}
	if ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "only Process.close() may be deferred"))}
	}
	receiver = valueFromPlace(receiver)
	if diagnostic := streamClosedDiagnostic(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	var resultUnion compilerTypes.Type
	if name == "wait" {
		resultUnion = ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.ExitStatusType, compilerTypes.ErrorType})
	} else {
		resultUnion = ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(unknownAt(callee.Property, "could not construct the "+name+" result union"))}
	}
	checked := networkMethodNode("process_"+name, receiver.source.Node, nil, compilerTypes.ProcessType, resultUnion, callee.Property)
	if name == "close" && ctx.names.cleanupDepth == 0 {
		markStreamClosed(receiver.source, callee.Property, ctx.names.flow)
	}
	return checked
}

// checkPipeMethodCall resolves read, write, shutdown, and close on a Pipe.
func checkPipeMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "read", "write", "shutdown", "close":
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Pipe has no method "+name+"; use read, write, shutdown, or close"))}
	}
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, name+" takes no type arguments"))}
	}
	if ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "only Pipe.close() may be deferred"))}
	}
	receiver = valueFromPlace(receiver)
	if diagnostic := streamClosedDiagnostic(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	var arguments []Operand
	var diagnostics compilerTypes.Diagnostics
	switch name {
	case "read":
		arguments, diagnostics = checkStreamReadArguments(call, callee, ctx)
	case "write":
		arguments, diagnostics = checkStreamWriteArguments(call, callee, ctx)
	default:
		if len(call.Arguments) != 0 {
			diagnostics = compilerTypes.Diagnostics{typeErrorAt(callee.Property, name+" expects no arguments")}
		}
	}
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultUnion compilerTypes.Type
	if name == "read" {
		resultUnion = streamResultUnion("read", ctx.typeEnvironment)
	} else {
		resultUnion = ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(unknownAt(callee.Property, "could not construct the "+name+" result union"))}
	}
	checked := networkMethodNode("pipe_"+name, receiver.source.Node, arguments, compilerTypes.PipeType, resultUnion, callee.Property)
	if name == "close" && ctx.names.cleanupDepth == 0 {
		markStreamClosed(receiver.source, callee.Property, ctx.names.flow)
	}
	return checked
}
