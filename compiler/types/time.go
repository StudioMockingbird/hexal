package types

// The time builtins: Duration is an unsigned nanosecond magnitude, Instant a
// private monotonic timestamp, and WallTime a UTC observation. All three are
// compiler-owned canonical value identities with no scalar kind, so ordinary
// numeric operators never accept them; the checker admits exactly the
// operations the time surface defines.

var (
	// DurationType lowers to a uint64_t nanosecond count.
	DurationType = Type{
		Name:         "Duration",
		CName:        "hex_duration",
		CanonicalKey: "Duration",
		identity:     newTypeIdentity(),
	}
	// InstantType lowers to a private uint64_t monotonic tick value whose
	// origin is not observable.
	InstantType = Type{
		Name:         "Instant",
		CName:        "hex_instant",
		CanonicalKey: "Instant",
		identity:     newTypeIdentity(),
	}
	// WallTimeType lowers to signed Unix seconds plus a normalized
	// nanosecond fraction.
	WallTimeType = Type{
		Name:         "WallTime",
		CName:        "hex_wall_time",
		CanonicalKey: "WallTime",
		identity:     newTypeIdentity(),
	}
)

func init() {
	builtinTypes["Duration"] = DurationType
	builtinTypes["Instant"] = InstantType
	builtinTypes["WallTime"] = WallTimeType
}

// IsDuration reports whether typ is the canonical Duration type.
func IsDuration(typ Type) bool {
	return typ.identity != nil && typ.identity == DurationType.identity
}

// IsInstant reports whether typ is the canonical Instant type.
func IsInstant(typ Type) bool {
	return typ.identity != nil && typ.identity == InstantType.identity
}

// IsWallTime reports whether typ is the canonical WallTime type.
func IsWallTime(typ Type) bool {
	return typ.identity != nil && typ.identity == WallTimeType.identity
}

// IsTime reports whether typ is one of the three time value types.
func IsTime(typ Type) bool { return IsDuration(typ) || IsInstant(typ) || IsWallTime(typ) }
