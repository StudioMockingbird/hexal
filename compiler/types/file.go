package types

// The regular-file builtins: File over one owned libuv descriptor
// and the five-variant FileMode ADT. Both are compiler-owned canonical
// identities beside IO; no source declaration can create or shadow them.

var (
	// FileType is the owned regular-file handle: one uv_file descriptor and
	// an access mask. Copies alias one native cursor and close state.
	FileType = Type{
		Name:         "File",
		CName:        "hex_file",
		CanonicalKey: "File",
		identity:     newTypeIdentity(),
	}
	// FileModeType is the protected open-mode ADT: Read, Write, Append,
	// ReadWrite, and CreateNew, all unit variants.
	FileModeType = fileModeType()
)

func fileModeType() Type {
	names := []string{"Read", "Write", "Append", "ReadWrite", "CreateNew"}
	variants := make([]AdtVariant, len(names))
	for index, name := range names {
		variants[index] = AdtVariant{Name: name}
	}
	adt := &AdtType{
		Name:     "FileMode",
		CName:    "hex_t_FileMode",
		Variants: variants,
		identity: newTypeIdentity(),
	}
	return Type{
		Name:         "FileMode",
		CName:        adt.CName,
		CanonicalKey: canonicalNominalKey("FileMode", ""),
		Adt:          adt,
		identity:     adt.identity,
	}
}

func init() {
	builtinTypes["File"] = FileType
	builtinTypes["FileMode"] = FileModeType
}

// IsFile reports whether typ is the canonical File handle type.
func IsFile(typ Type) bool { return typ.identity != nil && typ.identity == FileType.identity }

// IsFileMode reports whether typ is the canonical FileMode ADT.
func IsFileMode(typ Type) bool { return typ.Adt != nil && typ.Adt == FileModeType.Adt }

// IsBuiltinAdt reports whether typ is a compiler-owned ADT whose struct lives
// in a shared component header rather than in any module header.
func IsBuiltinAdt(typ Type) bool {
	return IsSeek(typ) || IsFileMode(typ) || IsErrorKind(typ) || IsAddress(typ) ||
		IsEnvironment(typ) || IsProcessStream(typ) || IsExitStatus(typ) || IsSignal(typ)
}

// IsBuiltinObject reports whether object is a compiler-owned struct whose
// body lives in a shared component header (hexal/process.h) rather than in
// any module header, mirroring IsBuiltinAdt for the Object family.
func IsBuiltinObject(object *ObjectType) bool {
	return object != nil && (object == EnvironmentVariableType.Object || object == ProcessOptionsType.Object || object == StartedProcessType.Object || object == TerminalSizeType.Object)
}

// IsBuiltinUnion reports whether union is one of the fixed structural `T |
// Nil` unions a builtin process/IPC field carries (String | Nil for
// ProcessOptions.working_directory, Pipe | Nil for StartedProcess's stream
// fields). Its body is hand-written once in hexal/process.h alongside the
// object that embeds it, mirroring IsBuiltinAdt and IsBuiltinObject; every
// other union in a program is emitted generically per module.
func IsBuiltinUnion(union Type) bool {
	if union.Union == nil {
		return false
	}
	for _, candidate := range builtinStructuralUnions {
		if candidate.Union == union.Union {
			return true
		}
	}
	return false
}

// CapabilityFromFileMode maps one FileMode variant index to the capability a
// File opened with it carries.
func CapabilityFromFileMode(variant int) StreamCapability {
	switch variant {
	case 0:
		return StreamReadable
	case 3:
		return StreamReadWrite
	default:
		return StreamWritable
	}
}
