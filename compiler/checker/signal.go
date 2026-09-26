package checker

// The libuv ordinary-signal-observation builtin: Signals construction and
// its next/close instance methods. Signal itself is an ordinary inline ADT
// constructed and matched through the general ADT machinery; only Signals'
// operations below need dedicated checking. Close tracking reuses the same
// provenance machinery File, Process, and Pipe share.

import (
	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkSignalsTypeCall resolves the bare constructor call
// Signals(subscriptions: Slice<Signal>).
func checkSignalsTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.SignalsConstructorArguments()))}
	}
	signalSlice := ctx.typeEnvironment.SliceType(compilerTypes.SignalType, false)
	if signalSlice == (compilerTypes.Type{}) {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(unknownAt(callee))}
	}
	subscriptions := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(signalSlice), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(subscriptions); len(diagnostics) > 0 {
		return checkedExpression{token: callee, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if !assignable(signalSlice, subscriptions.typ) {
		return checkedExpression{token: subscriptions.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(signalSlice, subscriptions.typ, subscriptions.token))}
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.SignalsType, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(unknownAt(callee))}
	}
	return networkNode("signals_new", nil, []Operand{subscriptions.source}, compilerTypes.Type{}, resultUnion, callee)
}

// checkSignalsMethodCall resolves next() and close() on a Signals handle.
func checkSignalsMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	switch name {
	case "next", "close":
	default:
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownSignalsMethod(name)))}
	}
	if len(call.call.TypeArguments) != 0 || len(call.call.Arguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.SignalsMethodTakesNoArguments(name)))}
	}
	if call.ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.OnlySignalsCloseMayBeDeferred()))}
	}
	call.receiver = valueFromPlace(call.receiver)
	if diagnostic := streamClosedDiagnostic(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	var resultUnion compilerTypes.Type
	if name == "next" {
		resultUnion = call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.SignalType, compilerTypes.EoS, compilerTypes.ErrorType})
	} else {
		resultUnion = call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	checked := networkMethodNode("signals_"+name, call.receiver.source.Node, nil, compilerTypes.SignalsType, resultUnion, call.callee.Property)
	if name == "close" && call.ctx.names.cleanupDepth == 0 {
		markStreamClosed(call.receiver.source, call.callee.Property, call.ctx.names.flow)
	}
	return checked
}
