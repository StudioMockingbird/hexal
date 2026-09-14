package types

import "strconv"

// The libuv networking builtins: the inline Address ADT and the two
// generation-checked owned handles TcpConnection and TcpListener. Both
// handles are compiler-owned canonical identities beside File; no source
// declaration can create or shadow them.

var (
	// addressIPv4Bytes and addressIPv6Bytes are fixed, globally interned
	// array shapes reserved for Address's own payload fields only. They are
	// not registered in any per-compilation array arena, so a program's own
	// independently declared Array<Byte, 4> is a distinct identity; only a
	// direct array literal, never an existing binding, may fill an Address
	// variant's bytes field.
	addressIPv4Bytes = builtinArrayType("hex_addr_ipv4_bytes", "builtin-array:address-ipv4-bytes", UInt8, 4)
	addressIPv6Bytes = builtinArrayType("hex_addr_ipv6_bytes", "builtin-array:address-ipv6-bytes", UInt8, 16)

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

func builtinArrayType(cName, canonicalKey string, element Type, length uint64) Type {
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	return Type{
		Name:         "Array<" + element.Name + ", " + strconv.FormatUint(length, 10) + ">",
		CName:        cName,
		CanonicalKey: canonicalKey,
		Array:        &ArrayInfo{Element: element, Length: length},
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

func init() {
	builtinTypes["Address"] = AddressType
	builtinTypes["TcpConnection"] = TcpConnectionType
	builtinTypes["TcpListener"] = TcpListenerType
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
