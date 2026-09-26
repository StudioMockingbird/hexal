package diagnostics

import "strconv"

func UnterminatedMultilineComment() Message {
	return message("syntax.unterminated-multiline-comment", CategorySyntax, StageLexer, "unterminated multiline comment")
}

func UnterminatedByteLiteral() Message {
	return message("syntax.unterminated-byte-literal", CategorySyntax, StageLexer, "unterminated Byte literal")
}

func UnterminatedRuneLiteral() Message {
	return message("syntax.unterminated-rune-literal", CategorySyntax, StageLexer, "unterminated Rune literal")
}

func IdentifierMustBeginWithLetter() Message {
	return message("syntax.identifier-must-begin-with-letter", CategorySyntax, StageLexer, "identifiers must begin with a letter")
}

func MalformedFloatingLiteral() Message {
	return message("syntax.malformed-floating-literal", CategorySyntax, StageLexer, "malformed floating literal")
}

func UnterminatedCHeaderLiteral() Message {
	return message("syntax.unterminated-c-header-literal", CategorySyntax, StageLexer, "unterminated C header literal")
}

func InvalidCHeaderLiteral() Message {
	return message("syntax.invalid-c-header-literal", CategorySyntax, StageLexer, "invalid C header literal")
}

func UnterminatedModulePathLiteral() Message {
	return message("syntax.unterminated-module-path-literal", CategorySyntax, StageLexer, "unterminated module path literal")
}

func InvalidModulePathLiteral() Message {
	return message("syntax.invalid-module-path-literal", CategorySyntax, StageLexer, "invalid module-path literal")
}

func UnexpectedCharacter(character rune) Message {
	return message("syntax.unexpected-character", CategorySyntax, StageLexer, "unexpected character "+strconv.QuoteRune(character))
}

func NestingLimit() Message {
	return message("syntax.lexer-nesting-limit", CategorySyntax, StageLexer, "nesting exceeds the maximum depth of 128")
}

func RawNewlineInString() Message {
	return message("syntax.raw-newline-in-string", CategorySyntax, StageLexer, `String literal cannot contain a raw newline; use \n`)
}

func UnterminatedStringLiteral() Message {
	return message("syntax.unterminated-string-literal", CategorySyntax, StageLexer, "unterminated string literal")
}

func UnterminatedStringInterpolation() Message {
	return message("syntax.unterminated-string-interpolation", CategorySyntax, StageLexer, "unterminated string interpolation")
}

func UnterminatedRawStringLiteral() Message {
	return message("syntax.unterminated-raw-string-literal", CategorySyntax, StageLexer, "unterminated raw string literal")
}

func UppercaseIntegerBasePrefix() Message {
	return message("syntax.integer-base-prefix-lowercase", CategorySyntax, StageLexer, "integer base prefixes must be lowercase")
}

func MalformedDecimalIntegerLiteral() Message {
	return message("syntax.malformed-decimal-integer-literal", CategorySyntax, StageLexer, "malformed decimal integer literal")
}

func DecimalIntegerLeadingZero() Message {
	return message("syntax.decimal-integer-leading-zero", CategorySyntax, StageLexer, "decimal integer literals cannot have leading zeros")
}

func MalformedDecimalFloatingLiteral() Message {
	return message("syntax.malformed-decimal-floating-literal", CategorySyntax, StageLexer, "malformed decimal floating literal")
}

func MalformedBinaryIntegerLiteral() Message {
	return message("syntax.malformed-binary-integer-literal", CategorySyntax, StageLexer, "malformed binary integer literal")
}

func MalformedOctalIntegerLiteral() Message {
	return message("syntax.malformed-octal-integer-literal", CategorySyntax, StageLexer, "malformed octal integer literal")
}

func MalformedHexadecimalLiteral() Message {
	return message("syntax.malformed-hexadecimal-literal", CategorySyntax, StageLexer, "malformed hexadecimal literal")
}

func ByteLiteralPrintableCardinality() Message {
	return message("syntax.byte-literal-printable-cardinality", CategorySyntax, StageLexer, "Byte literal must contain exactly one printable ASCII byte")
}

func IncompleteEscapeSequence() Message {
	return message("syntax.incomplete-escape-sequence", CategorySyntax, StageLexer, "literal ends with an incomplete escape sequence")
}

func UnsupportedByteQuoteEscape() Message {
	return message("syntax.unsupported-byte-quote-escape", CategorySyntax, StageLexer, `unsupported escape \" in Byte literal`)
}

func UnsupportedEscape(escape rune) Message {
	return message("syntax.unsupported-escape", CategorySyntax, StageLexer, `unsupported escape \`+string(escape))
}

func UnsupportedHexEscape() Message {
	return message("syntax.unsupported-hex-escape", CategorySyntax, StageLexer, `unsupported escape \x; Byte literals use \xHH`)
}

func ByteHexEscapeNeedsTwoDigits() Message {
	return message("syntax.byte-hex-escape-needs-two-digits", CategorySyntax, StageLexer, `Byte literal escape \x requires exactly two hex digits`)
}

func UnicodeEscapeIsNotByteEscape() Message {
	return message("syntax.unicode-escape-not-byte-escape", CategorySyntax, StageLexer, "Unicode escapes are not Byte escapes")
}

func UnicodeEscapeRequiresBracedHex() Message {
	return message("syntax.unicode-escape-requires-braced-hex", CategorySyntax, StageLexer, `Unicode escape requires \u{HEX}`)
}

func InvalidUnicodeScalarEscape() Message {
	return message("syntax.invalid-unicode-scalar-escape", CategorySyntax, StageLexer, "invalid Unicode scalar value in escape")
}

func ByteLiteralCardinality() Message {
	return message("syntax.byte-literal-cardinality", CategorySyntax, StageLexer, "Byte literal must contain exactly one byte")
}

func RuneLiteralCardinality() Message {
	return message("syntax.rune-literal-cardinality", CategorySyntax, StageLexer, "Rune literal must contain exactly one Unicode scalar")
}

func InvalidUTF8StringLiteral() Message {
	return message("syntax.invalid-utf8-string-literal", CategorySyntax, StageLexer, "string literal contains invalid UTF-8")
}
