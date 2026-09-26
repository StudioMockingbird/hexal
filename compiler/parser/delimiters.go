package parser

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
)

// requireDelimiter consumes the mandatory body opener of a function, method,
// if, or elseif header. A missing opener reports a construct-specific Syntax
// Error at the first token after the header.
func (parser *Parser) requireDelimiter(kind lexer.TokenKind, message diag.Message) error {
	if parser.check(kind) {
		parser.advance()
		return nil
	}
	return parser.errorAtCurrent(message)
}
