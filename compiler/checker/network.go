package checker

// The libuv networking builtins: the inline Address ADT (constructed and
// matched through the ordinary ADT machinery), Address.parse/format,
// Dns.resolve, Tcp.connect/listen, and the TcpConnection/TcpListener
// generation-checked handles. Close tracking reuses the same provenance
// machinery File and IO share.

import (
	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkAddressTypeCall resolves Address.parse(text, port).
func checkAddressTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "parse" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(messageAt(variable.Name, diagnosticsPkg.UnknownAddressOperation()))}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diagnosticsPkg.AddressParseArity(len(call.Arguments))))}
	}
	var arguments []Operand
	for index, expected := range []compilerTypes.Type{compilerTypes.StringType, compilerTypes.UInt16} {
		checked := checkInitializer(call.Arguments[index], compilerTypes.NewTypeUse(expected), tokenOf(call.Arguments[index]), ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(expected, checked.typ) {
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(expected, checked.typ, checked.token))}
		}
		arguments = append(arguments, checked.source)
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.AddressType, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
	}
	return networkNode("address_parse", nil, arguments, compilerTypes.Type{}, resultUnion, property)
}

// checkDnsTypeCall resolves Dns.resolve(heap, host, service).
func checkDnsTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "resolve" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(messageAt(variable.Name, diagnosticsPkg.UnknownDNSOperation()))}
	}
	if len(call.Arguments) != 3 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diagnosticsPkg.DNSResolveArity(len(call.Arguments))))}
	}
	var arguments []Operand
	for index, expected := range []compilerTypes.Type{compilerTypes.Heap, compilerTypes.StringType, compilerTypes.StringType} {
		checked := checkInitializer(call.Arguments[index], compilerTypes.NewTypeUse(expected), tokenOf(call.Arguments[index]), ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(expected, checked.typ) {
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(expected, checked.typ, checked.token))}
		}
		arguments = append(arguments, checked.source)
	}
	addressList := ctx.typeEnvironment.ListType(compilerTypes.AddressType)
	if addressList == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{addressList, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
	}
	return networkNode("dns_resolve", nil, arguments, compilerTypes.Type{}, resultUnion, property)
}

// checkTcpTypeCall resolves Tcp.connect(address) and Tcp.listen(address, backlog).
func checkTcpTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(messageAt(variable.Name, diagnosticsPkg.TCPHasNoTypeArguments()))}
	}
	switch property.Lexeme {
	case "connect":
		if len(call.Arguments) != 1 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diagnosticsPkg.TCPConnectArity(len(call.Arguments))))}
		}
		address := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.AddressType), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(address); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(compilerTypes.AddressType, address.typ) {
			return checkedExpression{token: address.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.AddressType, address.typ, address.token))}
		}
		resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.TcpConnectionType, compilerTypes.ErrorType})
		if resultUnion == (compilerTypes.Type{}) {
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
		}
		return networkNode("tcp_connect", nil, []Operand{address.source}, compilerTypes.Type{}, resultUnion, property)
	case "listen":
		if len(call.Arguments) != 2 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diagnosticsPkg.TCPListenArity(len(call.Arguments))))}
		}
		address := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.AddressType), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(address); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(compilerTypes.AddressType, address.typ) {
			return checkedExpression{token: address.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.AddressType, address.typ, address.token))}
		}
		backlog := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), ctx)
		if diagnostics := initializerDiagnostics(backlog); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(compilerTypes.SizeType, backlog.typ) {
			return checkedExpression{token: backlog.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.SizeType, backlog.typ, backlog.token))}
		}
		resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.TcpListenerType, compilerTypes.ErrorType})
		if resultUnion == (compilerTypes.Type{}) {
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
		}
		return networkNode("tcp_listen", nil, []Operand{address.source, backlog.source}, compilerTypes.Type{}, resultUnion, property)
	default:
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(messageAt(variable.Name, diagnosticsPkg.UnknownTCPOperation()))}
	}
}

// checkAddressMethodCall resolves Address.format(heap), the one instance
// operation on an Address value. Formatting is infallible: every valid
// Address has a bounded representation.
func checkAddressMethodCall(call methodCall) checkedExpression {
	if call.callee.Property.Lexeme != "format" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownAddressMethod(call.callee.Property.Lexeme)))}
	}
	if len(call.call.TypeArguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.MethodTakesNoTypeArguments("format")))}
	}
	if len(call.call.Arguments) != 1 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AddressFormatArity(len(call.call.Arguments))))}
	}
	heap := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Heap), tokenOf(call.call.Arguments[0]), call.ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if !assignable(compilerTypes.Heap, heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.Heap, heap.typ, heap.token))}
	}
	call.receiver = valueFromPlace(call.receiver)
	node := networkMethodNode("address_format", call.receiver.source.Node, []Operand{heap.source}, compilerTypes.AddressType, compilerTypes.StringType, call.callee.Property)
	return node
}

// checkTcpListenerMethodCall resolves accept() and close() on a TcpListener.
func checkTcpListenerMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	switch name {
	case "accept", "close":
	default:
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownTCPListenerMethod(name)))}
	}
	if len(call.call.TypeArguments) != 0 || len(call.call.Arguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected(name)))}
	}
	if call.ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.OnlyTCPListenerCloseMayBeDeferred()))}
	}
	call.receiver = valueFromPlace(call.receiver)
	if diagnostic := streamClosedDiagnostic(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	var resultUnion compilerTypes.Type
	if name == "accept" {
		resultUnion = call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.TcpConnectionType, compilerTypes.ErrorType})
	} else {
		resultUnion = call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	checked := networkMethodNode("tcp_"+name, call.receiver.source.Node, nil, compilerTypes.TcpListenerType, resultUnion, call.callee.Property)
	if name == "close" && call.ctx.names.cleanupDepth == 0 {
		markStreamClosed(call.receiver.source, call.callee.Property, call.ctx.names.flow)
	}
	return checked
}

// checkTcpConnectionMethodCall resolves read, write, shutdown, no_delay, and
// close on a TcpConnection.
func checkTcpConnectionMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	switch name {
	case "read", "write", "shutdown", "no_delay", "close":
	default:
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownTCPConnectionMethod(name)))}
	}
	if len(call.call.TypeArguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.MethodTakesNoTypeArguments(name)))}
	}
	if call.ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.OnlyTCPConnectionCloseMayBeDeferred()))}
	}
	call.receiver = valueFromPlace(call.receiver)
	if diagnostic := streamClosedDiagnostic(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	var arguments []Operand
	var diagnostics compilerTypes.Diagnostics
	switch name {
	case "read":
		arguments, diagnostics = checkStreamReadArguments(call)
	case "write":
		arguments, diagnostics = checkStreamWriteArguments(call)
	case "no_delay":
		if len(call.call.Arguments) != 1 {
			diagnostics = compilerTypes.Diagnostics{messageAt(call.callee.Property, diagnosticsPkg.TCPNoDelayArity(len(call.call.Arguments)))}
			break
		}
		enabled := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Bool), tokenOf(call.call.Arguments[0]), call.ctx)
		if nested := initializerDiagnostics(enabled); len(nested) > 0 {
			diagnostics = nested
			break
		}
		if !assignable(compilerTypes.Bool, enabled.typ) {
			diagnostics = compilerTypes.Diagnostics{typeMismatchDiagnostic(compilerTypes.Bool, enabled.typ, enabled.token)}
			break
		}
		arguments = []Operand{enabled.source}
	default:
		if len(call.call.Arguments) != 0 {
			diagnostics = compilerTypes.Diagnostics{messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected(name))}
		}
	}
	if len(diagnostics) > 0 {
		return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	var resultUnion compilerTypes.Type
	if name == "read" {
		resultUnion = streamResultUnion("read", call.ctx.typeEnvironment)
	} else {
		resultUnion = call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	checked := networkMethodNode("tcp_"+name, call.receiver.source.Node, arguments, compilerTypes.TcpConnectionType, resultUnion, call.callee.Property)
	if name == "close" && call.ctx.names.cleanupDepth == 0 {
		markStreamClosed(call.receiver.source, call.callee.Property, call.ctx.names.flow)
	}
	return checked
}

func networkNode(name string, operand *Expression, arguments []Operand, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := Expression{
		Kind:        NetworkExpression,
		Name:        name,
		Operand:     operand,
		Arguments:   arguments,
		OperandType: operandType,
		ResultType:  resultType,
		Span:        token.Span,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func networkMethodNode(name string, receiver Expression, arguments []Operand, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	return networkNode(name, &receiver, arguments, operandType, resultType, token)
}
