package types

// The builtin type registry: package-level canonical identities, the map
// that resolves them by name, and the reserved-name guards over both.

// IsProtectedTypeName reports whether a name is reserved by the language.
func IsProtectedTypeName(name string) bool {
	if _, ok := builtinTypes[name]; ok {
		return true
	}
	switch name {
	case "Ptr", "MutPtr", "Fun", "Array", "List", "Dict", "View", "Slice", "Task", "Channel", "Atomic", "Stash", "Pool":
		return true
	}
	return false
}

// IsSize reports whether typ is the canonical target-sized Size type.
func IsSize(typ Type) bool { return typ.identity != nil && typ.identity == SizeType.identity }

func scalarType(name, cName string, kind ScalarKind, bits int) Type {
	identity := newTypeIdentity()
	identity.signature = "scalar:" + name
	return Type{
		Name:         name,
		CName:        cName,
		CanonicalKey: name,
		ScalarKind:   kind,
		Bits:         bits,
		identity:     identity,
	}
}

// Package-level canonical builtins. Scalars share one package-level identity,
// so they compare equal across compilation environments.
var (
	Int8   = scalarType("Int8", "int8_t", ScalarSignedInteger, 8)
	Int16  = scalarType("Int16", "int16_t", ScalarSignedInteger, 16)
	Int32  = scalarType("Int32", "int32_t", ScalarSignedInteger, 32)
	Int64  = scalarType("Int64", "int64_t", ScalarSignedInteger, 64)
	UInt8  = scalarType("UInt8", "uint8_t", ScalarUnsignedInteger, 8)
	UInt16 = scalarType("UInt16", "uint16_t", ScalarUnsignedInteger, 16)
	UInt32 = scalarType("UInt32", "uint32_t", ScalarUnsignedInteger, 32)
	UInt64 = scalarType("UInt64", "uint64_t", ScalarUnsignedInteger, 64)
	// Rune is a Unicode scalar value: a UInt32 excluding surrogates. It lowers
	// to the ordinary uint32_t scalar so text iteration and conversions share
	// the unsigned machinery; only its literal and validity rules differ.
	Rune    = scalarType("Rune", "uint32_t", ScalarUnsignedInteger, 32)
	Float32 = scalarType("Float32", "float", ScalarFloat, 32)
	Float64 = scalarType("Float64", "double", ScalarFloat, 64)
	Bool    = scalarType("Bool", "bool", ScalarBool, 1)

	// Int, Float, and UInt are the idiomatic aliases of the platform-native
	// widths.
	Int   = Int32
	Float = Float64
	UInt  = UInt32

	Nil = Type{
		Name:         "Nil",
		CName:        "nullptr_t",
		CanonicalKey: "Nil",
		identity:     newTypeIdentity(),
	}
	// EoS is the end-of-stream singleton. Its one-byte C value is never
	// allocated; the `T | EoS` result union carries it as a tag-only
	// alternative.
	EoS = Type{
		Name:         "EoS",
		CName:        "hex_eos",
		CanonicalKey: "EoS",
		identity:     newTypeIdentity(),
	}
	Unknown = Type{
		Name:         "Unknown",
		CName:        "void",
		CanonicalKey: "Unknown",
		Incomplete:   true,
		identity:     newTypeIdentity(),
	}
	Heap = Type{
		Name:         "Heap",
		CName:        "hex_heap",
		CanonicalKey: "Heap",
		identity:     newTypeIdentity(),
	}
	StringType = Type{
		Name:         "String",
		CName:        "hex_string",
		CanonicalKey: "String",
		identity:     newTypeIdentity(),
	}
	// SizeType is the target-sized unsigned integer corresponding to C's
	// size_t. It is a distinct canonical type even where its width matches
	// a fixed-width integer.
	SizeType = Type{
		Name:         "Size",
		CName:        "size_t",
		CanonicalKey: "Size",
		ScalarKind:   ScalarUnsignedInteger,
		Bits:         64,
		identity:     newTypeIdentity(),
	}
	// ErrorType is the built-in nominal error value: five fixed fields
	// recording the construction site and the program's category and
	// message. It is reserved: user source cannot redeclare or shadow it.
	ErrorType = errorType()
	// MutexType is the scheduler-aware mutual-exclusion handle. It is a
	// heap-backed, pointer-sized reference-like value with one canonical
	// identity, like String; its control block lives on the Heap passed to
	// Mutex.new.
	MutexType = Type{
		Name:         "Mutex",
		CName:        "hex_mutex",
		CanonicalKey: "Mutex",
		identity:     newTypeIdentity(),
	}
	// ByteCursorType and RuneCursorType are non-owning text cursors: one
	// descriptor holding the source byte pointer, the byte length, and the
	// current byte offset. Each is an inline value with one canonical
	// identity, copies by value, and is not Dict-key eligible.
	ByteCursorType = Type{
		Name:         "ByteCursor",
		CName:        "hex_byte_cursor",
		CanonicalKey: "ByteCursor",
		identity:     newTypeIdentity(),
	}
	RuneCursorType = Type{
		Name:         "RuneCursor",
		CName:        "hex_rune_cursor",
		CanonicalKey: "RuneCursor",
		identity:     newTypeIdentity(),
	}
	// GraphemeType is the borrowed byte range spanning exactly one extended
	// grapheme cluster of valid UTF-8. It is a view, like Slice<Byte>, and is
	// not Dict-key eligible.
	GraphemeType = Type{
		Name:         "Grapheme",
		CName:        "hex_grapheme",
		CanonicalKey: "Grapheme",
		identity:     newTypeIdentity(),
	}
	// GraphemeCursorType is the stateful break cursor: it carries the source
	// bytes, the current byte offset, the utf8proc break state, and the one
	// lookahead cluster peek computes.
	GraphemeCursorType = Type{
		Name:         "GraphemeCursor",
		CName:        "hex_grapheme_cursor",
		CanonicalKey: "GraphemeCursor",
		identity:     newTypeIdentity(),
	}
)

// errorType constructs the canonical built-in Error object, linking its
// type identity to the shared object record like every interner does.
func errorType() Type {
	object := &ObjectType{
		Name:  "Error",
		CName: "hex_t_Error",
		Members: []ObjectMember{
			{Name: "file", Type: StringType},
			{Name: "line", Type: SizeType},
			{Name: "column", Type: SizeType},
			{Name: "kind", Type: ErrorKindType},
			{Name: "message", Type: ErrorMessageText},
		},
	}
	identity := newTypeIdentity()
	identity.object = object
	object.identity = identity
	return Type{Name: "Error", CName: "hex_t_Error", CanonicalKey: "Error", Object: object, identity: identity}
}

// builtinTypes is the canonical registry of every builtin type name: scalars,
// Nil, Unknown, and Heap. Canonicality compares against these records.
var builtinTypes = map[string]Type{
	"Bool":    Bool,
	"Int8":    Int8,
	"Int16":   Int16,
	"Int32":   Int32,
	"Int64":   Int64,
	"UInt8":   UInt8,
	"UInt16":  UInt16,
	"UInt32":  UInt32,
	"UInt64":  UInt64,
	"Rune":    Rune,
	"Float32": Float32,
	"Float64": Float64,
	"Nil":     Nil,
	"EoS":     EoS,
	"Unknown": Unknown,
	"Heap":    Heap,
	"String":  StringType,
	"Size":    SizeType,
	"Error":   ErrorType,
	"Mutex":   MutexType,
	// Byte is the canonical transparent alias of UInt8; both spellings
	// share one identity and one C representation.
	"Byte": UInt8,
	// The text cursors are compiler-owned inline descriptors.
	"ByteCursor": ByteCursorType,
	"RuneCursor": RuneCursorType,
	// Grapheme is a borrowed byte range and GraphemeCursor its stateful
	// segmenter.
	"Grapheme":       GraphemeType,
	"GraphemeCursor": GraphemeCursorType,
}

// Lookup resolves a builtin type by name.
func Lookup(name string) (Type, bool) {
	typ, ok := builtinTypes[name]
	return typ, ok
}
