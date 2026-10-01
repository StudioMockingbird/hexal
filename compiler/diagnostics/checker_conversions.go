package diagnostics

import "fmt"

func NumericConversionTypeArgumentCount() Message {
	return message("type.numeric-conversion-type-argument-count", CategoryType, StageChecker, "to requires exactly 1 explicit type argument")
}

func ConversionValueArgumentCount() Message {
	return message("type.conversion-value-argument-count", CategoryType, StageChecker,
		"to accepts one Heap argument when converting to String, and no value arguments otherwise")
}

func UnsupportedConversion(source, target string) Message {
	return message("type.conversion-unsupported", CategoryType, StageChecker, "cannot convert "+source+" to "+target)
}

func ConversionValueNotScalar(value string) Message {
	return message("type.conversion-value-not-scalar", CategoryType, StageChecker, "value "+value+" is not a Unicode scalar value")
}

func ConversionValueUnrepresentable(target string) Message {
	return message("type.conversion-value-unrepresentable", CategoryType, StageChecker, "value cannot be represented as "+target)
}

func FloatToIntegerConversionInvalid(target string) Message {
	return message("type.float-to-integer-conversion-invalid", CategoryType, StageChecker, "floating value cannot be converted to "+target)
}

func ConversionRequiresInteger() Message {
	return message("type.conversion-requires-integer", CategoryType, StageChecker, "value is not an integer")
}

func ConversionValueOutOfRange(value, target string) Message {
	return message("type.conversion-value-out-of-range", CategoryType, StageChecker, fmt.Sprintf("value %s is outside the range of %s", value, target))
}
