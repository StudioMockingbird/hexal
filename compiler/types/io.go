package types

// The byte-stream builtins: IO over an open operating-system handle, Bytes
// over a borrowed List<Byte>, and the Seek ADT naming stream positions. All
// three are compiler-owned canonical identities beside Heap, String, and
// Error; no source declaration can create or shadow them.

var (
	// IOType is the descriptor-backed byte stream: one native descriptor,
	// a read/write access mask, and an ownership bit. Copies alias one
	// external resource.
	IOType = Type{
		Name:         "IO",
		CName:        "hex_io",
		CanonicalKey: "IO",
		identity:     newTypeIdentity(nil),
	}
	// BytesType is the memory-backed byte stream: one borrowed
	// List<Byte> header pointer and an inline cursor. Copying copies the
	// cursor; state-changing methods take MutPtr<Bytes>.
	BytesType = Type{
		Name:         "Bytes",
		CName:        "hex_bytes",
		CanonicalKey: "Bytes",
		identity:     newTypeIdentity(nil),
	}
)

// seekType constructs the canonical Seek ADT once. Its variants carry named
// payload fields because record-variant construction is exhaustive by field
// name; Start names an absolute position and Current and End signed offsets.
func seekType() Type {
	variants := []AdtVariant{
		{Name: "Start", Payload: []ObjectMember{{Name: "position", Type: SizeType}}},
		{Name: "Current", Payload: []ObjectMember{{Name: "offset", Type: Int64}}},
		{Name: "End", Payload: []ObjectMember{{Name: "offset", Type: Int64}}},
	}
	for index := range variants {
		for field := range variants[index].Payload {
			variants[index].Payload[field].Use = NewTypeUse(variants[index].Payload[field].Type)
		}
	}
	adt := &AdtType{
		Name:     "Seek",
		CName:    "hex_t_Seek",
		Variants: variants,
		identity: newTypeIdentity(nil),
	}
	return Type{
		Name:         "Seek",
		CName:        adt.CName,
		CanonicalKey: canonicalNominalKey("Seek", ""),
		Adt:          adt,
		identity:     adt.identity,
	}
}

// SeekType is the stream-position ADT: Start(Size), Current(Int64), and
// End(Int64). It is registered as a builtin so `Seek.Start { ... }`
// construction resolves through the ordinary qualified-variant path.
var SeekType = seekType()

func init() {
	builtinTypes["IO"] = IOType
	builtinTypes["Bytes"] = BytesType
	builtinTypes["Seek"] = SeekType
}

// IsIO reports whether typ is the canonical IO stream type.
func IsIO(typ Type) bool { return typ.identity != nil && typ.identity == IOType.identity }

// IsBytes reports whether typ is the canonical Bytes stream type.
func IsBytes(typ Type) bool {
	return typ.identity != nil && typ.identity == BytesType.identity
}

// IsSeek reports whether typ is the canonical Seek ADT.
func IsSeek(typ Type) bool { return typ.Adt != nil && typ.Adt.Name == "Seek" }

// StreamCapability records what a local flow proves an IO binding may do.
// Unknown selects the runtime access-mask check.
type StreamCapability uint8

const (
	StreamUnknown StreamCapability = iota
	StreamReadable
	StreamWritable
	StreamReadWrite
)

// CapabilityFromConstructor maps one standard-handle constructor to the
// capability its result provably carries.
func CapabilityFromConstructor(name string) (StreamCapability, bool) {
	switch name {
	case "stdin":
		return StreamReadable, true
	case "stdout", "stderr":
		return StreamWritable, true
	}
	return StreamUnknown, false
}

func init() {
	builtinTypes["IO"] = IOType
	builtinTypes["Bytes"] = BytesType
	builtinTypes["Seek"] = SeekType
}
