package types

import "strconv"

// The libuv networking builtins: the inline Address ADT and the two
// generation-checked owned handles TcpConnection and TcpListener. Both
// handles are compiler-owned canonical identities beside File; no source
// declaration can create or shadow them.

var (
	// These canonical inline Lists seed each compilation arena so Address
	// payloads and source List<Byte, N> values share one type identity.
	addressIPv4Bytes = builtinInlineListType(UInt8, 4)
	addressIPv6Bytes = builtinInlineListType(UInt8, 16)

	// AddressType is the protected inline network address ADT: IPv4 stores
	// four network-order bytes and a host-order port; IPv6 stores sixteen
	// network-order bytes, a host-order port, and a numeric scope. No
	// Address value allocates.
	AddressType = addressType()

	// TcpConnectionType is the generation-checked owned TCP connection
	// handle: one native uv_tcp_t behind the shared copied-handle registry.
	TcpConnectionType = Type{
		Name:         "TcpConnection",
		CName:        "hex_tcp_connection",
		CanonicalKey: "TcpConnection",
		identity:     newTypeIdentity(),
	}
	// TcpListenerType is the generation-checked owned TCP listener handle.
	TcpListenerType = Type{
		Name:         "TcpListener",
		CName:        "hex_tcp_listener",
		CanonicalKey: "TcpListener",
		identity:     newTypeIdentity(),
	}
)

func builtinInlineListType(element Type, capacity uint64) Type {
	canonicalKey := "inline-list:" + element.CanonicalKey + "," + strconv.FormatUint(capacity, 10)
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	return Type{
		Name:         "List<" + element.Name + ", " + strconv.FormatUint(capacity, 10) + ">",
		CName:        "hex_list_inline_" + SanitizeIdentifier(element.Name) + "_" + strconv.FormatUint(capacity, 10),
		CanonicalKey: canonicalKey,
		InlineList:   &InlineListInfo{Element: element, Capacity: capacity},
		identity:     identity,
	}
}

func addressType() Type {
	variants := []AdtVariant{
		{Name: "IPv4", Payload: []ObjectMember{
			{Name: "bytes", Type: addressIPv4Bytes, Use: NewTypeUse(addressIPv4Bytes)},
			{Name: "port", Type: UInt16, Use: NewTypeUse(UInt16)},
		}},
		{Name: "IPv6", Payload: []ObjectMember{
			{Name: "bytes", Type: addressIPv6Bytes, Use: NewTypeUse(addressIPv6Bytes)},
			{Name: "port", Type: UInt16, Use: NewTypeUse(UInt16)},
			{Name: "scope", Type: UInt32, Use: NewTypeUse(UInt32)},
		}},
	}
	adt := &AdtType{
		Name:     "Address",
		CName:    "hex_t_Address",
		Variants: variants,
		identity: newTypeIdentity(),
	}
	return Type{
		Name:         "Address",
		CName:        adt.CName,
		CanonicalKey: canonicalNominalKey("Address", ""),
		Adt:          adt,
		identity:     adt.identity,
	}
}

// IsAddress reports whether typ is the canonical Address ADT.
func IsAddress(typ Type) bool { return typ.Adt != nil && typ.Adt == AddressType.Adt }

// IsTcpConnection reports whether typ is the canonical TcpConnection handle.
func IsTcpConnection(typ Type) bool {
	return typ.identity != nil && typ.identity == TcpConnectionType.identity
}

// IsTcpListener reports whether typ is the canonical TcpListener handle.
func IsTcpListener(typ Type) bool {
	return typ.identity != nil && typ.identity == TcpListenerType.identity
}
