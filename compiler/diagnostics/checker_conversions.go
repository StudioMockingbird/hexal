package diagnostics

import "fmt"

func NumericConversionTypeArgumentCount() Message {
	return message("type.numeric-conversion-type-argument-count", CategoryType, StageChecker, "to requires exactly 1 explicit type argument")
}

func NumericConversionValueArgumentCount() Message {
	return message("type.numeric-conversion-value-argument-count", CategoryType, StageChecker, "to accepts no value arguments")
}

func UnsupportedNumericConversion(source, target string) Message {
	return message("type.numeric-conversion-unsupported", CategoryType, StageChecker,
		"numeric conversion requires a supported scalar source and destination; got "+source+" and "+target)
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
