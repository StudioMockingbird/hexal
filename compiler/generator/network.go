package generator

// The libuv networking family's Go side: Address, Dns, Tcp, TcpConnection,
// and TcpListener. Every operation lowers through one module-owned inline
// adapter, following the same structural-union wrapping File and IO use.

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

const (
	networkMessageAddressParse = "address parse failed"
	networkMessageDnsResolve   = "dns resolve failed"
	networkMessageTcpConnect   = "tcp connect failed"
	networkMessageTcpListen    = "tcp listen failed"
	networkMessageTcpAccept    = "tcp accept failed"
	networkMessageTcpRead      = "tcp read failed"
	networkMessageTcpWrite     = "tcp write failed"
	networkMessageTcpShutdown  = "tcp shutdown failed"
	networkMessageTcpNoDelay   = "tcp no_delay failed"
	networkMessageTcpClose     = "tcp close failed"
)

// generatedNetworkState records one module's (or the merged program's)
// networking operations and the result unions each produces. dns and tcp
// gate the additional handle/event/scheduler selection those parking
// operations require; Address parse/format alone select neither.
type generatedNetworkState struct {
	used bool
	dns  bool
	tcp  bool

	parseUnions   []compilerTypes.Type
	resolveUnions []compilerTypes.Type
	connectUnions []compilerTypes.Type
	listenUnions  []compilerTypes.Type
	acceptUnions  []compilerTypes.Type
	readUnions    []compilerTypes.Type
	// closeUnions covers every Nil | Error operation: write, shutdown,
	// no_delay, and both TcpConnection.close and TcpListener.close.
	closeUnions []compilerTypes.Type

	nameLiteral literalHandle
}

// discoverGeneratedNetwork walks one module for networking operations.
func discoverGeneratedNetwork(program checker.Program, logicalKey string, literals *literalRegistry) *generatedNetworkState {
	state := &generatedNetworkState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.NetworkExpression || isProcessOperation(node.Name) || isSignalOperation(node.Name) {
				// Process/Pipe and Signals operations share
				// checker.NetworkExpression's Kind but are discovered
				// separately by discoverGeneratedProcess/discoverGeneratedSignal;
				// they select neither Address nor Dns/Tcp.
				return nil
			}
			state.used = true
			switch node.Name {
			case "address_parse":
				state.parseUnions = appendUnionOnce(state.parseUnions, node.ResultType)
			case "address_format":
				// String, not a union: nothing to record.
			case "dns_resolve":
				state.dns = true
				state.resolveUnions = appendUnionOnce(state.resolveUnions, node.ResultType)
			case "tcp_connect":
				state.tcp = true
				state.connectUnions = appendUnionOnce(state.connectUnions, node.ResultType)
			case "tcp_listen":
				state.tcp = true
				state.listenUnions = appendUnionOnce(state.listenUnions, node.ResultType)
			case "tcp_accept":
				state.tcp = true
				state.acceptUnions = appendUnionOnce(state.acceptUnions, node.ResultType)
			case "tcp_read":
				state.tcp = true
				state.readUnions = appendUnionOnce(state.readUnions, node.ResultType)
			case "tcp_write", "tcp_shutdown", "tcp_no_delay", "tcp_close", "tcp_accept_close":
				state.tcp = true
				state.closeUnions = appendUnionOnce(state.closeUnions, node.ResultType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used {
		state.nameLiteral = literals.Intern(logicalKey)
		for _, message := range []string{
			networkMessageAddressParse, networkMessageDnsResolve, networkMessageTcpConnect,
			networkMessageTcpListen, networkMessageTcpAccept, networkMessageTcpRead,
			networkMessageTcpWrite, networkMessageTcpShutdown, networkMessageTcpNoDelay, networkMessageTcpClose,
		} {
			literals.Intern(message)
		}
	}
	return state
}

// elementNeedsNetwork reports whether a collection specialization's element
// spells a networking type, whose typedef hexal/network.h owns; a component
// header that includes this element type directly cannot rely on a
// consuming module's own include order to have supplied it first.
func elementNeedsNetwork(element compilerTypes.Type) bool {
	return compilerTypes.IsAddress(element) || compilerTypes.IsTcpConnection(element) || compilerTypes.IsTcpListener(element)
}

// mergeNetworkInto unions one module's networking demand into the program
// state.
func mergeNetworkInto(merged, state *generatedNetworkState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.dns = merged.dns || state.dns
	merged.tcp = merged.tcp || state.tcp
}

// networkComponents returns hexal/network.h and hexal/network.c when any
// networking builtin is reachable.
func networkComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged.networkState == nil || !merged.networkState.used {
		return nil, nil
	}
	model := networkSourceModel{Dns: merged.networkState.dns, Tcp: merged.networkState.tcp}
	return []componentArtifact{
		{key: "hexal/network.h", template: "network.h", model: model},
		{key: "hexal/network.c", template: "network.c", model: model},
	}, nil
}

// networkSourceModel gates the DNS and TCP declarations network.h/.c emit.
type networkSourceModel struct {
	Dns bool
	Tcp bool
}

// moduleNetworkComponent selects hexal/network.h for a module naming any
// networking builtin.
func moduleNetworkComponent(emission *moduleEmission) []string {
	if emission == nil || emission.networkState == nil || !emission.networkState.used {
		return nil
	}
	return []string{"hexal/network.h"}
}

// nonErrorUnionMember returns the one member of a two-member `T | Error`
// result union that is not Error, for a success case whose T (List<Address>,
// here) has no fixed global identity to name directly.
func nonErrorUnionMember(union compilerTypes.Type) (compilerTypes.Type, error) {
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		member, _ := members.At(index)
		if !compilerTypes.Equal(member, compilerTypes.ErrorType) {
			return member, nil
		}
	}
	return compilerTypes.Type{}, unknownExpressionDiagnostic("result union has no non-Error member")
}

// networkErrorArm spells one Error payload construction for a network
// adapter whose failure carries a hex_network_error-classified status.
func networkErrorArm(tags *tagRegistry, literals *literalRegistry, name string, union compilerTypes.Type, status, payload string) (string, error) {
	handle, ok := literals.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic("network failure message is missing from the literal registry: " + payload)
	}
	_ = name
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_network_error(line, column, %s, &%s) }",
		union.CName, tag, field, status, literals.CName(handle)), nil
}

// writeNetworkInlineHelpers emits the module-owned networking adapters: each
// wraps the network.c core in one structural result union, built with this
// module's static operation message.
func writeNetworkInlineHelpers(result *strings.Builder, state *generatedNetworkState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.used {
		return nil
	}

	for _, union := range state.parseUnions {
		addrTag, addrField := streamMemberRef(tags, union, compilerTypes.AddressType)
		failure, err := networkErrorArm(tags, literals, "parse", union, "parsed.status", networkMessageAddressParse)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_address_parse_%s(const hex_string *text, uint16_t port, size_t line, size_t column) {\n"+
				"    hex_address_parsed parsed = hex_address_parse(text, port);\n"+
				"    if (parsed.status == 0) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = parsed.address };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, addrTag, addrField, failure)
	}

	for _, union := range state.resolveUnions {
		listMember, err := nonErrorUnionMember(union)
		if err != nil {
			return err
		}
		listTag, listField := streamMemberRef(tags, union, listMember)
		failure, err := networkErrorArm(tags, literals, "resolve", union, "resolved.status", networkMessageDnsResolve)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_dns_resolve_%s(const hex_string *host, const hex_string *service, hex_heap heap, size_t line, size_t column) {\n"+
				"    hex_dns_result resolved = hex_dns_resolve(host, service, heap);\n"+
				"    if (resolved.status == 0) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = resolved.list };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, listTag, listField, failure)
	}

	for _, union := range state.connectUnions {
		connTag, connField := streamMemberRef(tags, union, compilerTypes.TcpConnectionType)
		failure, err := networkErrorArm(tags, literals, "connect", union, "connected.status", networkMessageTcpConnect)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_tcp_connect_%s(hex_t_Address address, size_t line, size_t column) {\n"+
				"    hex_tcp_connect_result connected = hex_tcp_connect(address);\n"+
				"    if (connected.status == 0) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = connected.connection };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, connTag, connField, failure)
	}

	for _, union := range state.listenUnions {
		listenerTag, listenerField := streamMemberRef(tags, union, compilerTypes.TcpListenerType)
		failure, err := networkErrorArm(tags, literals, "listen", union, "listened.status", networkMessageTcpListen)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_tcp_listen_%s(hex_t_Address address, size_t backlog, size_t line, size_t column) {\n"+
				"    hex_tcp_listen_result listened = hex_tcp_listen(address, backlog);\n"+
				"    if (listened.status == 0) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = listened.listener };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, listenerTag, listenerField, failure)
	}

	for _, union := range state.acceptUnions {
		connTag, connField := streamMemberRef(tags, union, compilerTypes.TcpConnectionType)
		failure, err := networkErrorArm(tags, literals, "accept", union, "accepted.status", networkMessageTcpAccept)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_tcp_accept_%s(hex_tcp_listener listener, size_t line, size_t column) {\n"+
				"    hex_tcp_accept_result accepted = hex_tcp_accept(listener);\n"+
				"    if (accepted.status == 0) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = accepted.connection };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, connTag, connField, failure)
	}

	for _, union := range state.readUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		eosTag, _ := streamMemberRef(tags, union, compilerTypes.EoS)
		failure, err := networkErrorArm(tags, literals, "read", union, "transfer.status", networkMessageTcpRead)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_tcp_read_%s(hex_tcp_connection connection, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {\n"+
				"    hex_tcp_transfer transfer = hex_tcp_read(connection, into, max);\n"+
				"    switch (transfer.status) {\n"+
				"    case 0:\n"+
				"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
				"    case HEX_NETWORK_EOS:\n"+
				"        return (%s){ .tag = %s };\n"+
				"    default:\n"+
				"        return %s;\n"+
				"    }\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, sizeTag, sizeField,
			union.CName, eosTag, failure)
	}

	statusAdapter := func(unions []compilerTypes.Type, operation, call, payload string) error {
		for _, union := range unions {
			nilTag, _ := streamMemberRef(tags, union, compilerTypes.Nil)
			failure, err := networkErrorArm(tags, literals, operation, union, "status", payload)
			if err != nil {
				return err
			}
			fmt.Fprintf(result,
				"\nstatic inline %s hex_tcp_%s_%s(%s, size_t line, size_t column) {\n"+
					"    int status = %s;\n"+
					"    if (status == 0) {\n"+
					"        return (%s){ .tag = %s };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				union.CName, operation, streamAdapterSuffix(union), networkStatusAdapterParams(operation),
				call,
				union.CName, nilTag, failure)
		}
		return nil
	}
	if err := statusAdapter(state.closeUnions, "write", "hex_tcp_write(connection, from)", networkMessageTcpWrite); err != nil {
		return err
	}
	if err := statusAdapter(state.closeUnions, "shutdown", "hex_tcp_shutdown(connection)", networkMessageTcpShutdown); err != nil {
		return err
	}
	if err := statusAdapter(state.closeUnions, "no_delay", "hex_tcp_no_delay(connection, enabled)", networkMessageTcpNoDelay); err != nil {
		return err
	}
	if err := statusAdapter(state.closeUnions, "close", "hex_tcp_close(connection)", networkMessageTcpClose); err != nil {
		return err
	}
	if err := statusAdapter(state.closeUnions, "listener_close", "hex_tcp_listener_close(listener)", networkMessageTcpClose); err != nil {
		return err
	}
	return nil
}

// networkStatusAdapterParams spells the one receiver parameter each
// close-shaped adapter takes, distinguished by operation.
func networkStatusAdapterParams(operation string) string {
	switch operation {
	case "write":
		return "hex_tcp_connection connection, hex_slice_UInt8 from"
	case "shutdown", "close":
		return "hex_tcp_connection connection"
	case "no_delay":
		return "hex_tcp_connection connection, bool enabled"
	default:
		return "hex_tcp_listener listener"
	}
}
