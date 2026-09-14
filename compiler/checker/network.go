package checker

// The libuv networking builtins: the inline Address ADT (constructed and
// matched through the ordinary ADT machinery), Address.parse/format,
// Dns.resolve, Tcp.connect/listen, and the TcpConnection/TcpListener
// generation-checked handles. Close tracking reuses the same provenance
// machinery File and IO share.

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkAddressTypeCall resolves Address.parse(text, port).
func checkAddressTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "parse" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Address has no such operation; use Address.parse(text, port)"))}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("parse expects 2 arguments (text: String, port: UInt16); got %d", len(call.Arguments))))}
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
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the Address | Error result union"))}
	}
	return networkNode("address_parse", nil, arguments, compilerTypes.Type{}, resultUnion, property)
}

// checkDnsTypeCall resolves Dns.resolve(heap, host, service).
func checkDnsTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "resolve" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Dns has no such operation; use Dns.resolve(heap, host, service)"))}
	}
	if len(call.Arguments) != 3 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("resolve expects 3 arguments (heap: Heap, host: String, service: String); got %d", len(call.Arguments))))}
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
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct List<Address>"))}
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{addressList, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the List<Address> | Error result union"))}
	}
	return networkNode("dns_resolve", nil, arguments, compilerTypes.Type{}, resultUnion, property)
}

// checkTcpTypeCall resolves Tcp.connect(address) and Tcp.listen(address, backlog).
func checkTcpTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Tcp operations take no type arguments"))}
	}
	switch property.Lexeme {
	case "connect":
		if len(call.Arguments) != 1 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("connect expects 1 argument (address: Address); got %d", len(call.Arguments))))}
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
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the TcpConnection | Error result union"))}
		}
		return networkNode("tcp_connect", nil, []Operand{address.source}, compilerTypes.Type{}, resultUnion, property)
	case "listen":
		if len(call.Arguments) != 2 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("listen expects 2 arguments (address: Address, backlog: Size); got %d", len(call.Arguments))))}
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
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the TcpListener | Error result union"))}
		}
		return networkNode("tcp_listen", nil, []Operand{address.source, backlog.source}, compilerTypes.Type{}, resultUnion, property)
	default:
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Tcp has no such operation; use Tcp.connect or Tcp.listen"))}
	}
}

// checkAddressMethodCall resolves Address.format(heap), the one instance
// operation on an Address value. Formatting is infallible: every valid
// Address has a bounded representation.
func checkAddressMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	if callee.Property.Lexeme != "format" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Address has no method "+callee.Property.Lexeme+"; use format"))}
	}
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "format takes no type arguments"))}
	}
	if len(call.Arguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, fmt.Sprintf("format expects 1 argument (heap: Heap); got %d", len(call.Arguments))))}
	}
	heap := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Heap), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if !assignable(compilerTypes.Heap, heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.Heap, heap.typ, heap.token))}
	}
	receiver = valueFromPlace(receiver)
	node := networkMethodNode("address_format", receiver.source.Node, []Operand{heap.source}, compilerTypes.AddressType, compilerTypes.StringType, callee.Property)
	return node
}

// checkTcpListenerMethodCall resolves accept() and close() on a TcpListener.
func checkTcpListenerMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "accept", "close":
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "TcpListener has no method "+name+"; use accept or close"))}
	}
	if len(call.TypeArguments) != 0 || len(call.Arguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, name+" expects no arguments"))}
	}
	if ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "only TcpListener.close() may be deferred"))}
	}
	receiver = valueFromPlace(receiver)
	if diagnostic := streamClosedDiagnostic(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	var resultUnion compilerTypes.Type
	if name == "accept" {
		resultUnion = ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.TcpConnectionType, compilerTypes.ErrorType})
	} else {
		resultUnion = ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
	}
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(unknownAt(callee.Property, "could not construct the "+name+" result union"))}
	}
	checked := networkMethodNode("tcp_"+name, receiver.source.Node, nil, compilerTypes.TcpListenerType, resultUnion, callee.Property)
	if name == "close" && ctx.names.cleanupDepth == 0 {
		markStreamClosed(receiver.source, callee.Property, ctx.names.flow)
	}
	return checked
}

// checkTcpConnectionMethodCall resolves read, write, shutdown, no_delay, and
// close on a TcpConnection.
func checkTcpConnectionMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "read", "write", "shutdown", "no_delay", "close":
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "TcpConnection has no method "+name+"; use read, write, shutdown, no_delay, or close"))}
	}
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, name+" takes no type arguments"))}
	}
	if ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "only TcpConnection.close() may be deferred"))}
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
	case "no_delay":
		if len(call.Arguments) != 1 {
			diagnostics = compilerTypes.Diagnostics{typeErrorAt(callee.Property, fmt.Sprintf("no_delay expects 1 argument (enabled: Bool); got %d", len(call.Arguments)))}
			break
		}
		enabled := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Bool), tokenOf(call.Arguments[0]), ctx)
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
	checked := networkMethodNode("tcp_"+name, receiver.source.Node, arguments, compilerTypes.TcpConnectionType, resultUnion, callee.Property)
	if name == "close" && ctx.names.cleanupDepth == 0 {
		markStreamClosed(receiver.source, callee.Property, ctx.names.flow)
	}
	return checked
}

func networkNode(name string, operand *Expression, arguments []Operand, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := Expression{
		Kind:         NetworkExpression,
		Name:         name,
		Operand:      operand,
		Arguments:    arguments,
		OperandType:  operandType,
		ResultType:   resultType,
		SourceLine:   token.Line,
		SourceColumn: token.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func networkMethodNode(name string, receiver Expression, arguments []Operand, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	return networkNode(name, &receiver, arguments, operandType, resultType, token)
}
