package types

// The libuv-adjacent terminal builtins: TerminalSize, an ordinary immutable
// struct pairing a stream's visible column and row counts. Terminal itself is
// a protected compiler-owned namespace with no values, recognized by name
// alone (see IsProtectedTypeName), like Dns and Tcp.

// TerminalSizeType is the complete, immutable result of Terminal.size: the
// visible column and row counts of an attached terminal.
var TerminalSizeType = terminalSizeType()

func terminalSizeType() Type {
	return builtinObject("TerminalSize", "hex_t_TerminalSize", []ObjectMember{
		{Name: "columns", Type: SizeType, Use: NewTypeUse(SizeType)},
		{Name: "rows", Type: SizeType, Use: NewTypeUse(SizeType)},
	})
}

// IsTerminalSize reports whether typ is the canonical TerminalSize object.
func IsTerminalSize(typ Type) bool { return typ.Object != nil && typ.Object == TerminalSizeType.Object }
