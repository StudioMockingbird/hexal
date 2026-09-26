package diagnostics

import "fmt"

func EqualityUnavailableBecause(reason string) Message {
	return message("type.equality-unavailable-because", CategoryType, StageChecker,
		"equality is unavailable because "+reason+" does not support ==")
}

func FunctionValuesNotComparable() Message {
	return message("type.function-values-not-comparable", CategoryType, StageChecker, "function values are not equality-comparable")
}

func AllocatorHandlesNotComparable() Message {
	return message("type.allocator-handles-not-comparable", CategoryType, StageChecker, "allocator handles are not equality-comparable")
}

func DictionaryEqualityUnavailable() Message {
	return message("type.dictionary-equality-unavailable", CategoryType, StageChecker, "dictionary equality is not available in v1")
}

func EqualityUnavailableFor(name string) Message {
	return message("type.equality-unavailable-for", CategoryType, StageChecker, "equality is unavailable for "+name)
}

func NoLosslessCommonNumericComparisonType() Message {
	return message("type.no-lossless-common-numeric-comparison-type", CategoryType, StageChecker,
		"comparison has no lossless common numeric type")
}

func OperatorRequiresIdenticalOperands(operator, left, right string) Message {
	return message("type.operator-requires-identical-operands", CategoryType, StageChecker,
		fmt.Sprintf("operator %s requires identical operand types; got %s and %s", operator, left, right))
}

func PointerEqualityRequiresIdenticalTypes() Message {
	return message("type.pointer-equality-identical-types", CategoryType, StageChecker,
		"pointer equality requires identical pointer types")
}

func EqualityRequiresIdenticalCanonicalTypes() Message {
	return message("type.equality-identical-canonical-types", CategoryType, StageChecker,
		"equality requires identical canonical non-numeric operand types")
}

func OrderingUnavailableFor(name string) Message {
	return message("type.ordering-unavailable-for", CategoryType, StageChecker,
		"ordering is unavailable for "+name+" values")
}

func ForeignRecordEqualityUnavailable(name string) Message {
	return message("type.foreign-record-equality-unavailable", CategoryType, StageChecker,
		"equality is unavailable for foreign record "+name)
}
