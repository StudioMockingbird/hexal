package diagnostics

import "fmt"

func UnknownAddressOperation() Message {
	return message("type.unknown-address-operation", CategoryType, StageChecker, "Address has no such operation; use Address.parse(text, port)")
}
func AddressParseArity(actual int) Message {
	return message("type.address-parse-arity", CategoryType, StageChecker, fmt.Sprintf("parse expects 2 arguments (text: String, port: UInt16); got %d", actual))
}
func UnknownDNSOperation() Message {
	return message("type.unknown-dns-operation", CategoryType, StageChecker, "Dns has no such operation; use Dns.resolve(heap, host, service)")
}
func DNSResolveArity(actual int) Message {
	return message("type.dns-resolve-arity", CategoryType, StageChecker, fmt.Sprintf("resolve expects 3 arguments (heap: Heap, host: String, service: String); got %d", actual))
}
func TCPHasNoTypeArguments() Message {
	return message("type.tcp-no-type-arguments", CategoryType, StageChecker, "Tcp operations take no type arguments")
}
func TCPConnectArity(actual int) Message {
	return message("type.tcp-connect-arity", CategoryType, StageChecker, fmt.Sprintf("connect expects 1 argument (address: Address); got %d", actual))
}
func TCPListenArity(actual int) Message {
	return message("type.tcp-listen-arity", CategoryType, StageChecker, fmt.Sprintf("listen expects 2 arguments (address: Address, backlog: Size); got %d", actual))
}
func UnknownTCPOperation() Message {
	return message("type.unknown-tcp-operation", CategoryType, StageChecker, "Tcp has no such operation; use Tcp.connect or Tcp.listen")
}
func UnknownAddressMethod(name string) Message {
	return message("type.unknown-address-method", CategoryType, StageChecker, "Address has no method "+name+"; use format")
}
func MethodTakesNoTypeArguments(name string) Message {
	return message("type.method-no-type-arguments", CategoryType, StageChecker, name+" takes no type arguments")
}
func AddressFormatArity(actual int) Message {
	return message("type.address-format-arity", CategoryType, StageChecker, fmt.Sprintf("format expects 1 argument (heap: Heap); got %d", actual))
}
func UnknownTCPListenerMethod(name string) Message {
	return message("type.unknown-tcp-listener-method", CategoryType, StageChecker, "TcpListener has no method "+name+"; use accept or close")
}
func OnlyTCPListenerCloseMayBeDeferred() Message {
	return message("type.only-tcp-listener-close-deferred", CategoryType, StageChecker, "only TcpListener.close() may be deferred")
}
func UnknownTCPConnectionMethod(name string) Message {
	return message("type.unknown-tcp-connection-method", CategoryType, StageChecker, "TcpConnection has no method "+name+"; use read, write, shutdown, no_delay, or close")
}
func OnlyTCPConnectionCloseMayBeDeferred() Message {
	return message("type.only-tcp-connection-close-deferred", CategoryType, StageChecker, "only TcpConnection.close() may be deferred")
}
func TCPNoDelayArity(actual int) Message {
	return message("type.tcp-no-delay-arity", CategoryType, StageChecker, fmt.Sprintf("no_delay expects 1 argument (enabled: Bool); got %d", actual))
}
