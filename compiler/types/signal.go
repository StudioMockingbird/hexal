package types

// The libuv signal-observation builtins: the inline Signal ADT and the
// generation-checked owned Signals subscription handle.

var (
	// SignalType is the protected three-variant ordinary-signal ADT.
	SignalType = signalType()
	// SignalsType is the generation-checked owned subscription handle: up to
	// three uv_signal_t watchers behind the shared copied-handle registry.
	SignalsType = Type{
		Name:         "Signals",
		CName:        "hex_signals",
		CanonicalKey: "Signals",
		identity:     newTypeIdentity(),
	}
)

func signalType() Type {
	adt := &AdtType{
		Name:  "Signal",
		CName: "hex_t_Signal",
		Variants: []AdtVariant{
			{Name: "Interrupt"},
			{Name: "Hangup"},
			{Name: "Terminate"},
		},
		identity: newTypeIdentity(),
	}
	return Type{Name: "Signal", CName: adt.CName, CanonicalKey: canonicalNominalKey("Signal", ""), Adt: adt, identity: adt.identity}
}

func init() {
	builtinTypes["Signal"] = SignalType
	builtinTypes["Signals"] = SignalsType
}

// IsSignal reports whether typ is the canonical Signal ADT.
func IsSignal(typ Type) bool { return typ.Adt != nil && typ.Adt == SignalType.Adt }

// IsSignals reports whether typ is the canonical Signals handle.
func IsSignals(typ Type) bool { return typ.identity != nil && typ.identity == SignalsType.identity }
