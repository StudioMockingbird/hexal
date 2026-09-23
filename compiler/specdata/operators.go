package specdata

import "fmt"

// The operator, numeric-widening, and conversion matrices. A record says which
// operand or conversion pairs the language permits; it does not say what
// happens once a pair is permitted. Target widths and signed ranges,
// wraparound, floating-point rounding, contextual typing and inference
// rejection, and every diagnostic and its position stay with the phase that
// owns them. Identifiers cross this boundary; compiler Type values do not.

// OperatorID identifies one operator in the operand-admissibility matrix. It is
// a registry key, not the checker's Operator: the checker maps its resolved
// operator to one of these before querying.
type OperatorID string

// The operator identities the matrix covers.
const (
	OperatorNegate       OperatorID = "negate"
	OperatorLogicalNot   OperatorID = "logical_not"
	OperatorBitwiseNot   OperatorID = "bitwise_not"
	OperatorAdd          OperatorID = "add"
	OperatorSubtract     OperatorID = "subtract"
	OperatorMultiply     OperatorID = "multiply"
	OperatorDivide       OperatorID = "divide"
	OperatorRemainder    OperatorID = "remainder"
	OperatorBitwiseAnd   OperatorID = "bitwise_and"
	OperatorBitwiseXor   OperatorID = "bitwise_xor"
	OperatorBitwiseOr    OperatorID = "bitwise_or"
	OperatorShiftLeft    OperatorID = "shift_left"
	OperatorShiftRight   OperatorID = "shift_right"
	OperatorEqual        OperatorID = "equal"
	OperatorNotEqual     OperatorID = "not_equal"
	OperatorLess         OperatorID = "less"
	OperatorLessEqual    OperatorID = "less_equal"
	OperatorGreater      OperatorID = "greater"
	OperatorGreaterEqual OperatorID = "greater_equal"
	OperatorLogicalAnd   OperatorID = "logical_and"
	OperatorLogicalOr    OperatorID = "logical_or"
)

// The scalar identity sets operator rows share. The operator records are still
// the authority on which set each operator admits; these names keep one family
// from being retyped once per operator.
var (
	// numericIdentities is every scalar identity the numeric matrices range
	// over: the fixed-width integers, the floating-point widths, the
	// target-sized Size, and the Unicode-scalar Rune.
	numericIdentities = []TypeID{
		TypeInt8, TypeInt16, TypeInt32, TypeInt64,
		TypeUInt8, TypeUInt16, TypeUInt32, TypeUInt64,
		TypeFloat32, TypeFloat64, TypeRune, TypeSize,
	}
	// integerIdentities are the numeric identities the checker classifies as
	// integers; Size and Rune are integers at the language level.
	integerIdentities = []TypeID{
		TypeInt8, TypeInt16, TypeInt32, TypeInt64,
		TypeUInt8, TypeUInt16, TypeUInt32, TypeUInt64,
		TypeRune, TypeSize,
	}
	// signedNumericIdentities are the types unary negation accepts: the signed
	// fixed-width integers and the floating-point widths.
	signedNumericIdentities = []TypeID{
		TypeInt8, TypeInt16, TypeInt32, TypeInt64, TypeFloat32, TypeFloat64,
	}
	// scalarIdentities are every scalar identity equality accepts: the numeric
	// identities plus Bool, which is scalar but has no ordering.
	scalarIdentities = []TypeID{
		TypeBool,
		TypeInt8, TypeInt16, TypeInt32, TypeInt64,
		TypeUInt8, TypeUInt16, TypeUInt32, TypeUInt64,
		TypeFloat32, TypeFloat64, TypeRune, TypeSize,
	}
)

// OperatorSpec is one operator's operand matrix. Operands lists every concrete
// type identity the operator admits as an operand. AnyOperand marks an operator
// whose operand domain is the open set of every value-producing type -- the
// logical connectives test truthiness, not a scalar family -- and so has no
// finite identity list. Exactly one of Operands and AnyOperand is set.
type OperatorSpec struct {
	ID         OperatorID
	Operands   []TypeID
	AnyOperand bool
}

// operators is the matrix. It is unexported so no importer can rewrite a
// record, and every query clones the operand slice it returns.
var operators = []OperatorSpec{
	{ID: OperatorNegate, Operands: signedNumericIdentities},
	{ID: OperatorLogicalNot, AnyOperand: true},
	{ID: OperatorBitwiseNot, Operands: integerIdentities},
	{ID: OperatorAdd, Operands: numericIdentities},
	{ID: OperatorSubtract, Operands: numericIdentities},
	{ID: OperatorMultiply, Operands: numericIdentities},
	{ID: OperatorDivide, Operands: numericIdentities},
	{ID: OperatorRemainder, Operands: integerIdentities},
	{ID: OperatorBitwiseAnd, Operands: integerIdentities},
	{ID: OperatorBitwiseXor, Operands: integerIdentities},
	{ID: OperatorBitwiseOr, Operands: integerIdentities},
	{ID: OperatorShiftLeft, Operands: integerIdentities},
	{ID: OperatorShiftRight, Operands: integerIdentities},
	{ID: OperatorEqual, Operands: scalarIdentities},
	{ID: OperatorNotEqual, Operands: scalarIdentities},
	{ID: OperatorLess, Operands: numericIdentities},
	{ID: OperatorLessEqual, Operands: numericIdentities},
	{ID: OperatorGreater, Operands: numericIdentities},
	{ID: OperatorGreaterEqual, Operands: numericIdentities},
	{ID: OperatorLogicalAnd, AnyOperand: true},
	{ID: OperatorLogicalOr, AnyOperand: true},
}

// NumericRankSpec is one fixed-width numeric identity's position in the
// least-common-type order: when two operands share more than one common type,
// the least ranked candidate is their result. Size and Rune are numeric but
// appear in no widening edge and carry no rank, so every widening endpoint has
// a rank by construction.
type NumericRankSpec struct {
	ID   TypeID
	Rank int
}

// numericRanks orders the fixed-width numeric identities. The rank is the
// language's preference order, not a width.
var numericRanks = []NumericRankSpec{
	{ID: TypeInt8, Rank: 0},
	{ID: TypeUInt8, Rank: 1},
	{ID: TypeInt16, Rank: 2},
	{ID: TypeUInt16, Rank: 3},
	{ID: TypeInt32, Rank: 4},
	{ID: TypeUInt32, Rank: 5},
	{ID: TypeInt64, Rank: 6},
	{ID: TypeUInt64, Rank: 7},
	{ID: TypeFloat32, Rank: 8},
	{ID: TypeFloat64, Rank: 9},
}

// WideningSpec is one lossless numeric widening pair: every value of From is
// exactly representable by To. Identity is not a pair; a caller treats a type
// as reachable from itself without consulting the registry.
type WideningSpec struct {
	From TypeID
	To   TypeID
}

// wideningPairs is the matrix. Order within a source is the order a caller
// receives its targets in; the least common type is selected by rank, not by
// this order.
var wideningPairs = []WideningSpec{
	{From: TypeInt8, To: TypeInt16},
	{From: TypeInt8, To: TypeInt32},
	{From: TypeInt8, To: TypeInt64},
	{From: TypeInt8, To: TypeFloat32},
	{From: TypeInt8, To: TypeFloat64},
	{From: TypeInt16, To: TypeInt32},
	{From: TypeInt16, To: TypeInt64},
	{From: TypeInt16, To: TypeFloat32},
	{From: TypeInt16, To: TypeFloat64},
	{From: TypeInt32, To: TypeInt64},
	{From: TypeInt32, To: TypeFloat64},
	{From: TypeUInt8, To: TypeUInt16},
	{From: TypeUInt8, To: TypeUInt32},
	{From: TypeUInt8, To: TypeUInt64},
	{From: TypeUInt8, To: TypeInt16},
	{From: TypeUInt8, To: TypeInt32},
	{From: TypeUInt8, To: TypeInt64},
	{From: TypeUInt8, To: TypeFloat32},
	{From: TypeUInt8, To: TypeFloat64},
	{From: TypeUInt16, To: TypeUInt32},
	{From: TypeUInt16, To: TypeUInt64},
	{From: TypeUInt16, To: TypeInt32},
	{From: TypeUInt16, To: TypeInt64},
	{From: TypeUInt16, To: TypeFloat32},
	{From: TypeUInt16, To: TypeFloat64},
	{From: TypeUInt32, To: TypeUInt64},
	{From: TypeUInt32, To: TypeInt64},
	{From: TypeUInt32, To: TypeFloat64},
	{From: TypeFloat32, To: TypeFloat64},
}

// ConversionSpec is one explicit conversion direction: the checker admits
// `source.to<destination>()` for exactly the recorded pairs. Every numeric
// identity converts to every other, identity included, because an explicit
// conversion is a checked value change rather than an implicit widening.
type ConversionSpec struct {
	From TypeID
	To   TypeID
}

// conversions is the matrix. It is built from the numeric identity domain so
// the domain has one owner; Validate still checks every pair and the domain's
// full coverage.
var conversions = conversionMatrix(numericIdentities)

// conversionMatrix builds the ordered pairs of one identity domain.
func conversionMatrix(ids []TypeID) []ConversionSpec {
	matrix := make([]ConversionSpec, 0, len(ids)*len(ids))
	for _, from := range ids {
		for _, to := range ids {
			matrix = append(matrix, ConversionSpec{From: from, To: to})
		}
	}
	return matrix
}

// Operator resolves one operator's operand matrix.
func Operator(id OperatorID) (OperatorSpec, bool) {
	for _, spec := range operators {
		if spec.ID == id {
			return cloneOperator(spec), true
		}
	}
	return OperatorSpec{}, false
}

// Operators returns every operator record in registration order as a copy.
func Operators() []OperatorSpec {
	specs := make([]OperatorSpec, len(operators))
	for index, spec := range operators {
		specs[index] = cloneOperator(spec)
	}
	return specs
}

// cloneOperator deep-copies the operand slice a record owns, so a query result
// shares no backing array with the registry.
func cloneOperator(spec OperatorSpec) OperatorSpec {
	spec.Operands = append([]TypeID(nil), spec.Operands...)
	return spec
}

// WideningTargets returns the identities From widens to directly, in
// registration order, as a copy.
func WideningTargets(from TypeID) []TypeID {
	var targets []TypeID
	for _, pair := range wideningPairs {
		if pair.From == from {
			targets = append(targets, pair.To)
		}
	}
	return targets
}

// Widening reports whether From widens directly to To.
func Widening(from, to TypeID) bool {
	for _, pair := range wideningPairs {
		if pair.From == from && pair.To == to {
			return true
		}
	}
	return false
}

// WideningPairs returns every widening pair in registration order as a copy.
func WideningPairs() []WideningSpec {
	return append([]WideningSpec(nil), wideningPairs...)
}

// NumericRank returns id's least-common-type order position. The bool is false
// for a numeric identity outside the fixed-width order and for every
// non-numeric identity; the checker's common-type selection can only reach a
// ranked identity, so a false result is a lookup no consumer performs.
func NumericRank(id TypeID) (int, bool) {
	for _, entry := range numericRanks {
		if entry.ID == id {
			return entry.Rank, true
		}
	}
	return 0, false
}

// Conversion reports whether the explicit conversion matrix permits a value of
// from to be converted to to.
func Conversion(from, to TypeID) bool {
	for _, row := range conversions {
		if row.From == from && row.To == to {
			return true
		}
	}
	return false
}

// Conversions returns every conversion record in registration order as a copy.
func Conversions() []ConversionSpec {
	return append([]ConversionSpec(nil), conversions...)
}

// validateOperators checks the operator, widening, and conversion matrices. A
// record must name declared identities, must not repeat a key, and must be a
// legal member of its domain: an operator may not admit an undeclared operand
// type, a widening pair must raise the least-common-type order, and a
// conversion must stay inside the numeric identity domain and cover it fully.
func validateOperators() error {
	if err := validateOperatorMatrix(operators); err != nil {
		return err
	}
	if err := validateNumericRanks(numericRanks); err != nil {
		return err
	}
	if err := validateWideningPairs(wideningPairs, numericRanks); err != nil {
		return err
	}
	return validateConversions(conversions, numericIdentities)
}

// validateOperatorMatrix rejects an operator row that cannot be trusted: an
// empty or repeated operator, a row that declares both an operand list and an
// open domain or neither, an operand identity the constructor registry does not
// declare, and a repeated operand.
func validateOperatorMatrix(rows []OperatorSpec) error {
	seen := make(map[OperatorID]bool, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			return fmt.Errorf("specdata/operators: operator has an empty id")
		}
		if seen[row.ID] {
			return fmt.Errorf("specdata/operators: operator %q is declared twice", row.ID)
		}
		seen[row.ID] = true
		if (len(row.Operands) == 0) == !row.AnyOperand {
			return fmt.Errorf("specdata/operators: operator %q must declare either operand identities or an open operand domain, not both and not neither", row.ID)
		}
		operands := make(map[TypeID]bool, len(row.Operands))
		for _, operand := range row.Operands {
			if !isConcreteTypeID(operand) {
				return fmt.Errorf("specdata/operators: operator %q names undeclared operand type %q", row.ID, operand)
			}
			if operands[operand] {
				return fmt.Errorf("specdata/operators: operator %q lists operand %q twice", row.ID, operand)
			}
			operands[operand] = true
		}
	}
	return nil
}

// validateNumericRanks rejects a rank row that cannot be trusted: an undeclared
// type, a repeated type, a negative rank, and a rank another type already
// holds. Least-common-type selection is order-independent only while ranks are
// unique.
func validateNumericRanks(ranks []NumericRankSpec) error {
	seenTypes := make(map[TypeID]bool, len(ranks))
	seenRanks := make(map[int]TypeID, len(ranks))
	for _, entry := range ranks {
		if !isConcreteTypeID(entry.ID) {
			return fmt.Errorf("specdata/operators: rank names undeclared type %q", entry.ID)
		}
		if seenTypes[entry.ID] {
			return fmt.Errorf("specdata/operators: rank for %q is declared twice", entry.ID)
		}
		seenTypes[entry.ID] = true
		if entry.Rank < 0 {
			return fmt.Errorf("specdata/operators: rank for %q is negative", entry.ID)
		}
		if owner, ok := seenRanks[entry.Rank]; ok {
			return fmt.Errorf("specdata/operators: rank %d is held by both %q and %q", entry.Rank, owner, entry.ID)
		}
		seenRanks[entry.Rank] = entry.ID
	}
	return nil
}

// validateWideningPairs rejects a widening pair that cannot be trusted: an
// endpoint outside the ranked numeric identities, identity, a repeated pair,
// and a pair whose destination does not raise the least-common-type order.
func validateWideningPairs(pairs []WideningSpec, ranks []NumericRankSpec) error {
	rank := make(map[TypeID]int, len(ranks))
	for _, entry := range ranks {
		rank[entry.ID] = entry.Rank
	}
	seen := make(map[[2]TypeID]bool, len(pairs))
	for _, pair := range pairs {
		fromRank, fromOK := rank[pair.From]
		toRank, toOK := rank[pair.To]
		if !fromOK || !toOK {
			return fmt.Errorf("specdata/operators: widening %q to %q names a type outside the ranked numeric identities", pair.From, pair.To)
		}
		if pair.From == pair.To {
			return fmt.Errorf("specdata/operators: widening %q to %q is identity, not a widening step", pair.From, pair.To)
		}
		key := [2]TypeID{pair.From, pair.To}
		if seen[key] {
			return fmt.Errorf("specdata/operators: widening %q to %q is declared twice", pair.From, pair.To)
		}
		seen[key] = true
		if toRank <= fromRank {
			return fmt.Errorf("specdata/operators: widening %q to %q does not raise the least-common-type order", pair.From, pair.To)
		}
	}
	return nil
}

// validateConversions rejects a conversion record that cannot be trusted: a
// direction outside the numeric identity domain, a repeated direction, and an
// incomplete matrix. Missing data is a compiler-development failure, never a
// silently disabled conversion, so every ordered pair of the domain must be
// present.
func validateConversions(rows []ConversionSpec, numeric []TypeID) error {
	domain := make(map[TypeID]bool, len(numeric))
	for _, id := range numeric {
		domain[id] = true
	}
	seen := make(map[[2]TypeID]bool, len(rows))
	for _, row := range rows {
		if !domain[row.From] || !domain[row.To] {
			return fmt.Errorf("specdata/operators: conversion %q to %q names a direction outside the numeric identities", row.From, row.To)
		}
		key := [2]TypeID{row.From, row.To}
		if seen[key] {
			return fmt.Errorf("specdata/operators: conversion %q to %q is declared twice", row.From, row.To)
		}
		seen[key] = true
	}
	for _, from := range numeric {
		for _, to := range numeric {
			if !seen[[2]TypeID{from, to}] {
				return fmt.Errorf("specdata/operators: conversion %q to %q is missing", from, to)
			}
		}
	}
	return nil
}
