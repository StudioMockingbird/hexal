package diagnostics

func NegatedIntegerRequiresSignedType() Message {
	return message("type.negated-integer-requires-signed", CategoryType, StageChecker, "negated integer literal requires a signed destination")
}

func UnsupportedNegatedLiteral() Message {
	return message("type.negated-literal-unsupported", CategoryType, StageChecker, "unsupported negated literal")
}

func NumericLiteralOutOfRange(name string) Message {
	return message("type.numeric-literal-out-of-range", CategoryType, StageChecker, "given value is outside the "+name+" range")
}
