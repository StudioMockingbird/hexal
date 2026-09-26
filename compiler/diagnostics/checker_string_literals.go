package diagnostics

func MalformedStringLiteral() Message {
	return message("type.string-literal-malformed", CategoryType, StageChecker, "malformed string literal")
}

func MalformedRawStringLiteral() Message {
	return message("type.raw-string-literal-malformed", CategoryType, StageChecker, "malformed raw string literal")
}

func StringLiteralInvalidUTF8() Message {
	return message("type.string-literal-invalid-utf8", CategoryType, StageChecker, "string literal contains invalid UTF-8")
}

func MalformedByteLiteral() Message {
	return message("type.byte-literal-malformed", CategoryType, StageChecker, "malformed Byte literal")
}

func InvalidByteLiteralCardinality() Message {
	return message("type.byte-literal-cardinality", CategoryType, StageChecker, "Byte literal must contain exactly one byte")
}

func MalformedRuneLiteral() Message {
	return message("type.rune-literal-malformed", CategoryType, StageChecker, "malformed Rune literal")
}
