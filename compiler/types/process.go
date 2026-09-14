package types

// The libuv process/IPC builtins: Environment, EnvironmentVariable,
// ProcessStream, ProcessOptions, ExitStatus, StartedProcess, and the two
// generation-checked owned handles Process and Pipe.

var (
	// EnvironmentVariableType is one typed NAME/value pair for
	// Environment.Replace; no NAME=value parsing surface exists.
	EnvironmentVariableType = environmentVariableType()
	// EnvironmentType selects inheriting the parent environment or
	// replacing it completely with an explicit typed list.
	EnvironmentType = environmentType()
	// ProcessStreamType selects Ignore, Inherit, or a newly created Pipe for
	// one child standard stream.
	ProcessStreamType = processStreamType()
	// ProcessOptionsType is the complete, validated spawn request.
	ProcessOptionsType = processOptionsType()
	// ExitStatusType distinguishes a normal exit code from signal
	// termination; Terminated is produced only when the target reports it.
	ExitStatusType = exitStatusType()
	// StartedProcessType pairs the owned Process handle with the Pipe
	// handles StartedProcess non-Nil exactly where the matching option
	// requested one.
	StartedProcessType = startedProcessType()

	// ProcessType is the generation-checked owned child-process handle.
	ProcessType = Type{
		Name:         "Process",
		CName:        "hex_process",
		CanonicalKey: "Process",
		identity:     newTypeIdentity(),
	}
	// PipeType is the generation-checked owned child standard-stream pipe
	// handle: one native uv_pipe_t behind the shared copied-handle registry.
	PipeType = Type{
		Name:         "Pipe",
		CName:        "hex_pipe",
		CanonicalKey: "Pipe",
		identity:     newTypeIdentity(),
	}
)

func builtinObject(name, cName string, members []ObjectMember) Type {
	object := &ObjectType{Name: name, CName: cName, Members: members}
	identity := newTypeIdentity()
	identity.object = object
	object.identity = identity
	return Type{Name: name, CName: cName, CanonicalKey: canonicalNominalKey(name, ""), Object: object, identity: identity}
}

func environmentVariableType() Type {
	return builtinObject("EnvironmentVariable", "hex_t_EnvironmentVariable", []ObjectMember{
		{Name: "name", Type: StringType, Use: NewTypeUse(StringType)},
		{Name: "value", Type: StringType, Use: NewTypeUse(StringType)},
	})
}

func environmentType() Type {
	variants := []AdtVariant{
		{Name: "Inherit"},
		{Name: "Replace", Payload: []ObjectMember{
			// The element type is resolved lazily below to avoid a forward
			// reference to EnvironmentVariableType during package init
			// ordering; ListType requires a live *Environment, which
			// builtins do not have, so this uses the same fixed,
			// non-arena-interned array/list convention as Address's byte
			// arrays: a stable identity reserved once for this field alone.
		}},
	}
	adt := &AdtType{Name: "Environment", CName: "hex_t_Environment", Variants: variants, identity: newTypeIdentity()}
	environmentVariableList := builtinListType("hex_list_EnvironmentVariable", "list:"+EnvironmentVariableType.CanonicalKey, EnvironmentVariableType)
	adt.Variants[1].Payload = []ObjectMember{
		{Name: "values", Type: environmentVariableList, Use: NewTypeUse(environmentVariableList)},
	}
	return Type{Name: "Environment", CName: adt.CName, CanonicalKey: canonicalNominalKey("Environment", ""), Adt: adt, identity: adt.identity}
}

// builtinListType constructs a fixed, globally interned List<T> shape
// reserved for one builtin ADT payload field. canonicalKey must be
// "list:"+element.CanonicalKey, ListType's own signature format, so
// isCanonicalList's shape check accepts it; it is still a distinct identity
// from any environment.ListType(element) call, since List's own interning
// lives on the per-compilation arena and is unreachable at package init()
// time. checkStructConstructorCall and checkVariantConstructorCall
// re-resolve a member typed this way through the live arena before matching
// it against an initializer, so an existing List<T> binding -- not just a
// direct list literal -- may fill this field.
func builtinListType(cName, canonicalKey string, element Type) Type {
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	return Type{
		Name:         "List<" + element.Name + ">",
		CName:        cName,
		CanonicalKey: canonicalKey,
		List:         &ListInfo{Element: element},
		identity:     identity,
	}
}

func processStreamType() Type {
	adt := &AdtType{
		Name:  "ProcessStream",
		CName: "hex_t_ProcessStream",
		Variants: []AdtVariant{
			{Name: "Ignore"},
			{Name: "Inherit"},
			{Name: "Pipe"},
		},
		identity: newTypeIdentity(),
	}
	return Type{Name: "ProcessStream", CName: adt.CName, CanonicalKey: canonicalNominalKey("ProcessStream", ""), Adt: adt, identity: adt.identity}
}

func processOptionsType() Type {
	argumentsList := builtinListType("hex_list_String", "list:"+StringType.CanonicalKey, StringType)
	// String is not pointer-like (IsPointerLike requires typ.Element or
	// typ.Signature, neither of which StringType sets), so a real
	// `environment.NullableType(StringType)` would refuse it too; a
	// String-or-absent field is an ordinary structural union like the Pipe
	// one below, not the pointer-null-niche optimization.
	nullableWorkingDirectory := builtinOptionalHandleUnion(StringType)
	return builtinObject("ProcessOptions", "hex_t_ProcessOptions", []ObjectMember{
		{Name: "program", Type: StringType, Use: NewTypeUse(StringType)},
		{Name: "arguments", Type: argumentsList, Use: NewTypeUse(argumentsList)},
		{Name: "environment", Type: EnvironmentType, Use: NewTypeUse(EnvironmentType)},
		{Name: "working_directory", Type: nullableWorkingDirectory, Use: NewTypeUse(nullableWorkingDirectory)},
		{Name: "input", Type: ProcessStreamType, Use: NewTypeUse(ProcessStreamType)},
		{Name: "output", Type: ProcessStreamType, Use: NewTypeUse(ProcessStreamType)},
		{Name: "error", Type: ProcessStreamType, Use: NewTypeUse(ProcessStreamType)},
	})
}

func exitStatusType() Type {
	adt := &AdtType{
		Name:  "ExitStatus",
		CName: "hex_t_ExitStatus",
		Variants: []AdtVariant{
			{Name: "Exited", Payload: []ObjectMember{
				{Name: "code", Type: Int64, Use: NewTypeUse(Int64)},
			}},
			{Name: "Terminated"},
		},
		identity: newTypeIdentity(),
	}
	return Type{Name: "ExitStatus", CName: adt.CName, CanonicalKey: canonicalNominalKey("ExitStatus", ""), Adt: adt, identity: adt.identity}
}

// builtinStructuralUnions collects every fixed, globally interned structural
// union built at package init() time, so NewArena can pre-seed each fresh
// per-compilation arena with the exact identity these builtin fields carry;
// without that seeding, isCanonicalUnion's registry-membership check would
// reject them the first time they reach a live Environment (a union, unlike
// a plain object or ADT, must be reachable by name in the arena that owns
// it).
var builtinStructuralUnions []Type

// builtinOptionalHandleUnion constructs a fixed, globally interned `T | Nil`
// structural union for one non-pointer-like handle field (Pipe is a plain
// generation-checked struct, not a raw pointer, so it cannot use the
// pointer-null-niche NullableType optimization real `T | Nil` unions over a
// pointer-like base get). Its name and canonical key follow the exact same
// derivation environment.UnionType uses, so a live `environment.UnionType([]Type{base,
// Nil})` call anywhere in the compilation resolves to this same pre-seeded
// identity instead of minting an independent one.
func builtinOptionalHandleUnion(base Type) Type {
	members := []Type{base, Nil}
	canonicalKey := unionKey(members)
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	union := Type{
		Name:         unionName(members),
		CName:        unionBaseName(members),
		CanonicalKey: canonicalKey,
		Union:        &UnionInfo{Members: members},
		identity:     identity,
	}
	builtinStructuralUnions = append(builtinStructuralUnions, union)
	return union
}

func startedProcessType() Type {
	nullablePipe := builtinOptionalHandleUnion(PipeType)
	return builtinObject("StartedProcess", "hex_t_StartedProcess", []ObjectMember{
		{Name: "process", Type: ProcessType, Use: NewTypeUse(ProcessType)},
		{Name: "input", Type: nullablePipe, Use: NewTypeUse(nullablePipe)},
		{Name: "output", Type: nullablePipe, Use: NewTypeUse(nullablePipe)},
		{Name: "error", Type: nullablePipe, Use: NewTypeUse(nullablePipe)},
	})
}

func init() {
	builtinTypes["EnvironmentVariable"] = EnvironmentVariableType
	builtinTypes["Environment"] = EnvironmentType
	builtinTypes["ProcessStream"] = ProcessStreamType
	builtinTypes["ProcessOptions"] = ProcessOptionsType
	builtinTypes["ExitStatus"] = ExitStatusType
	builtinTypes["StartedProcess"] = StartedProcessType
	builtinTypes["Process"] = ProcessType
	builtinTypes["Pipe"] = PipeType
}

// IsProcess reports whether typ is the canonical Process handle.
func IsProcess(typ Type) bool { return typ.identity != nil && typ.identity == ProcessType.identity }

// IsPipe reports whether typ is the canonical Pipe handle.
func IsPipe(typ Type) bool { return typ.identity != nil && typ.identity == PipeType.identity }

// IsEnvironment reports whether typ is the canonical Environment ADT.
func IsEnvironment(typ Type) bool { return typ.Adt != nil && typ.Adt == EnvironmentType.Adt }

// IsProcessStream reports whether typ is the canonical ProcessStream ADT.
func IsProcessStream(typ Type) bool { return typ.Adt != nil && typ.Adt == ProcessStreamType.Adt }

// IsExitStatus reports whether typ is the canonical ExitStatus ADT.
func IsExitStatus(typ Type) bool { return typ.Adt != nil && typ.Adt == ExitStatusType.Adt }

// IsProcessOptions reports whether typ is the canonical ProcessOptions object.
func IsProcessOptions(typ Type) bool { return typ.Object != nil && typ.Object == ProcessOptionsType.Object }

// IsEnvironmentVariable reports whether typ is the canonical
// EnvironmentVariable object.
func IsEnvironmentVariable(typ Type) bool {
	return typ.Object != nil && typ.Object == EnvironmentVariableType.Object
}

// IsStartedProcess reports whether typ is the canonical StartedProcess
// object.
func IsStartedProcess(typ Type) bool {
	return typ.Object != nil && typ.Object == StartedProcessType.Object
}
