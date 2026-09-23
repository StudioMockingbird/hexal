package types

import "hexal/compiler/specdata"

// Lossless numeric widening: a typed numeric value may widen implicitly
// only when every value of the source type is exactly representable by the
// destination. The relation is a single widening step; identity counts as a
// step. Which pairs are permitted is a registry fact in compiler/specdata;
// this file resolves registry identifiers to canonical types and applies the
// relation.

// SpecID maps one builtin scalar Type to the registry identifier that names
// its canonical identity. It is the inverse of ResolveSpecID for the scalar
// identities the operator, widening, and conversion matrices range over, and
// reports false for every other type, including type parameters and
// user-defined types, so a caller treats an unregistered identity as outside
// those matrices.
func SpecID(typ Type) (specdata.TypeID, bool) {
	switch {
	case Equal(typ, Bool):
		return specdata.TypeBool, true
	case Equal(typ, Int8):
		return specdata.TypeInt8, true
	case Equal(typ, Int16):
		return specdata.TypeInt16, true
	case Equal(typ, Int32):
		return specdata.TypeInt32, true
	case Equal(typ, Int64):
		return specdata.TypeInt64, true
	case Equal(typ, UInt8):
		return specdata.TypeUInt8, true
	case Equal(typ, UInt16):
		return specdata.TypeUInt16, true
	case Equal(typ, UInt32):
		return specdata.TypeUInt32, true
	case Equal(typ, UInt64):
		return specdata.TypeUInt64, true
	case Equal(typ, Rune):
		return specdata.TypeRune, true
	case Equal(typ, SizeType):
		return specdata.TypeSize, true
	case Equal(typ, Float32):
		return specdata.TypeFloat32, true
	case Equal(typ, Float64):
		return specdata.TypeFloat64, true
	}
	return "", false
}

// losslessWideningSet returns the numeric types reachable from typ by zero
// or one widening step, including typ itself.
func losslessWideningSet(typ Type) []Type {
	if !IsInteger(typ) && !IsFloat(typ) {
		return nil
	}
	set := []Type{typ}
	from, ok := SpecID(typ)
	if !ok {
		return set
	}
	for _, id := range specdata.WideningTargets(from) {
		if target, ok := ResolveSpecID(id); ok {
			set = append(set, target)
		}
	}
	return set
}

// WidensTo reports whether every value of the source type is exactly
// representable by the destination type: identity or one direct lossless
// widening registry entry. This is the one-directional relation used by
// assignment, arguments, returns, field initialization, and Array elements.
func WidensTo(source, target Type) bool {
	if Equal(source, target) {
		return true
	}
	if !IsInteger(source) && !IsFloat(source) || !IsInteger(target) && !IsFloat(target) {
		return false
	}
	from, fromOK := SpecID(source)
	to, toOK := SpecID(target)
	if !fromOK || !toOK {
		return false
	}
	return specdata.Widening(from, to)
}

// LosslessCommonType returns the unique least numeric type to which both
// operand types widen losslessly, or false when no common type exists.
// Candidates are the types reachable from both operands by zero or one
// widening step; the least candidate is the minimum under the registry's
// numeric rank. No runtime range test is needed because validity follows from
// the source and destination type ranges. Size has no widening edges, so a
// Size operand only ever shares a common type with itself.
func LosslessCommonType(left, right Type) (Type, bool) {
	if Equal(left, right) {
		return left, true
	}
	if !IsInteger(left) && !IsFloat(left) || !IsInteger(right) && !IsFloat(right) {
		return Type{}, false
	}
	leftSet := losslessWideningSet(left)
	rightSet := losslessWideningSet(right)
	var candidates []Type
	for _, candidate := range leftSet {
		for _, other := range rightSet {
			if candidate.Name == other.Name {
				candidates = append(candidates, candidate)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return Type{}, false
	}
	best := Type{}
	bestRank := 0
	found := false
	for _, candidate := range candidates {
		id, ok := SpecID(candidate)
		if !ok {
			continue
		}
		rank, ok := specdata.NumericRank(id)
		if !ok {
			continue
		}
		if !found || rank < bestRank {
			best, bestRank, found = candidate, rank, true
		}
	}
	if !found {
		return Type{}, false
	}
	return best, true
}
