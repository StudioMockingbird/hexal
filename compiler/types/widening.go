package types

// Lossless numeric widening: a typed numeric value may widen implicitly
// only when every value of the source type is exactly representable by the
// destination. The relation is a single widening step; identity counts as a
// step.

var losslessWideningTargets = map[string][]string{
	"Int8":    {"Int16", "Int32", "Int64", "Float32", "Float64"},
	"Int16":   {"Int32", "Int64", "Float32", "Float64"},
	"Int32":   {"Int64", "Float64"},
	"Int64":   {},
	"UInt8":   {"UInt16", "UInt32", "UInt64", "Int16", "Int32", "Int64", "Float32", "Float64"},
	"UInt16":  {"UInt32", "UInt64", "Int32", "Int64", "Float32", "Float64"},
	"UInt32":  {"UInt64", "Int64", "Float64"},
	"UInt64":  {},
	"Float32": {"Float64"},
	"Float64": {},
	// Size is C-target-driven, so no implicit conversion is portable across
	// targets. Size appears in no widening edge and has no binary common
	// type with a distinct numeric type; identity remains.
	"Size": {},
}

// wideningRank orders the candidate types so the least common type is the
// minimum of the candidate intersection. Int8 and UInt8 never appear in an
// intersection of distinct operands, so they are unreachable in practice.
// The order is Int16 < UInt16 < Int32 < UInt32 < Int64 < UInt64 <
// Float32 < Float64.
var wideningRank = map[string]int{
	"Int8":    0,
	"UInt8":   1,
	"Int16":   2,
	"UInt16":  3,
	"Int32":   4,
	"UInt32":  5,
	"Int64":   6,
	"UInt64":  7,
	"Float32": 8,
	"Float64": 9,
}

// losslessWideningSet returns the numeric types reachable from typ by zero
// or one widening step, including typ itself.
func losslessWideningSet(typ Type) []Type {
	if !IsInteger(typ) && !IsFloat(typ) {
		return nil
	}
	set := []Type{typ}
	for _, name := range losslessWideningTargets[typ.Name] {
		if target, ok := builtinTypes[name]; ok {
			set = append(set, target)
		}
	}
	return set
}

// WidensTo reports whether every value of the source type is exactly
// representable by the destination type: identity or one direct lossless
// widening table entry. This is the one-directional relation used by
// assignment, arguments, returns, field initialization, and Array elements.
func WidensTo(source, target Type) bool {
	if Equal(source, target) {
		return true
	}
	if !IsInteger(source) && !IsFloat(source) || !IsInteger(target) && !IsFloat(target) {
		return false
	}
	for _, name := range losslessWideningTargets[source.Name] {
		if name == target.Name {
			return true
		}
	}
	return false
}

// LosslessCommonType returns the unique least numeric type to which both
// operand types widen losslessly, or false when no common type exists.
// Candidates are the types reachable from both operands by zero or one
// widening step; the least candidate is the minimum under wideningRank.
// No runtime range test is needed because validity follows from the source
// and destination type ranges. Size has no widening edges, so a Size
// operand only ever shares a common type with itself.
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
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if wideningRank[candidate.Name] < wideningRank[best.Name] {
			best = candidate
		}
	}
	return best, true
}
