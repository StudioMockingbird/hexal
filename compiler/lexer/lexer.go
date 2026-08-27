// Package lexer converts Hexal source text into tokens.
package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	compilerTypes "hexal/compiler/types"
)

// literalEscapeSet selects the escape grammar of one quoted literal form.
// String and Rune share the Unicode escape set; Byte adds \xHH and excludes
// \" and \u{...}.
type literalEscapeSet int

// The concrete literalEscapeSet values, named for the literal form whose
// escape grammar they select.
const (
	ByteEscapes literalEscapeSet = iota
	RuneEscapes
	StringEscapes
)

// DecodeLiteralBody decodes the inner body of a Byte, Rune, or String
// literal (without the surrounding quotes) into payload bytes. It validates
// every escape, Byte cardinality (exactly one byte), Rune cardinality
// (exactly one Unicode scalar), and UTF-8 validity of the whole payload.
// The returned message is empty on success.
func DecodeLiteralBody(body string, set literalEscapeSet) ([]byte, string) {
	payload := make([]byte, 0, len(body))
	for index := 0; index < len(body); index++ {
		character := body[index]
		if character != '\\' {
			if set == ByteEscapes && (character >= 0x80 || character < 0x20) {
				return nil, "Byte literal must contain exactly one printable ASCII byte"
			}
			payload = append(payload, character)
			continue
		}
		index++
		if index >= len(body) {
			return nil, "literal ends with an incomplete escape sequence"
		}
		escaped := body[index]
		switch escaped {
		case '\\', '\'':
			payload = append(payload, escaped)
		case '"':
			if set == ByteEscapes {
				return nil, "unsupported escape \\\" in Byte literal"
			}
			payload = append(payload, '"')
		case 'n':
			payload = append(payload, '\n')
		case 'r':
			payload = append(payload, '\r')
		case 't':
			payload = append(payload, '\t')
		case '0':
			payload = append(payload, 0)
		case 'x':
			if set != ByteEscapes {
				return nil, "unsupported escape \\x; Byte literals use \\xHH"
			}
			if index+3 > len(body) {
				return nil, "Byte literal escape \\x requires exactly two hex digits"
			}
			value, err := strconv.ParseUint(body[index+1:index+3], 16, 8)
			if err != nil {
				return nil, "Byte literal escape \\x requires exactly two hex digits"
			}
			payload = append(payload, byte(value))
			index += 2
		case 'u':
			if set == ByteEscapes {
				return nil, "Unicode escapes are not Byte escapes"
			}
			if index+1 >= len(body) || body[index+1] != '{' {
				return nil, "Unicode escape requires \\u{HEX}"
			}
			closeIndex := index + 2
			for closeIndex < len(body) && body[closeIndex] != '}' {
				closeIndex++
			}
			if closeIndex >= len(body) {
				return nil, "Unicode escape requires \\u{HEX}"
			}
			digits := body[index+2 : closeIndex]
			if len(digits) == 0 {
				return nil, "Unicode escape requires \\u{HEX}"
			}
			value, err := strconv.ParseUint(digits, 16, 32)
			if err != nil || value > 0x10FFFF || value >= 0xD800 && value <= 0xDFFF {
				return nil, "invalid Unicode scalar value in escape"
			}
			payload = append(payload, []byte(string(rune(value)))...)
			index = closeIndex
		default:
			return nil, "unsupported escape \\" + string(escaped)
		}
	}
	if set == ByteEscapes && len(payload) != 1 {
		return nil, "Byte literal must contain exactly one byte"
	}
	if set == RuneEscapes {
		if !utf8.Valid(payload) {
			return nil, "Rune literal must contain exactly one Unicode scalar"
		}
		decoded, width := utf8.DecodeRune(payload)
		if decoded == utf8.RuneError && width <= 1 {
			return nil, "Rune literal must contain exactly one Unicode scalar"
		}
		if len(payload) != width {
			return nil, "Rune literal must contain exactly one Unicode scalar"
		}
	}
	if set == StringEscapes && !utf8.Valid(payload) {
		return nil, "string literal contains invalid UTF-8"
	}
	return payload, ""
}

// TokenKind identifies the syntactic role of a token.
type TokenKind uint8

// The concrete TokenKind values: punctuation and operators first, then
// literal forms, then keywords, in the source's own lexical groupings. Each
// name spells the token it identifies and needs no further comment; EOF is
// the sentinel the lexer emits once after the last real token.
const (
	Identifier TokenKind = iota
	Colon
	Equal
	Less
	Greater
	Minus
	Integer
	HexInteger
	BinaryInteger
	OctalInteger
	DecimalFloat
	True
	False
	NilLiteral
	Eos
	Mut
	Ref
	Type
	Dot
	LeftBrace
	RightBrace
	Comma
	Bang
	BangEqual
	EqualEqual
	LessEqual
	GreaterEqual
	Plus
	Star
	Slash
	Percent
	LeftParen
	RightParen
	LeftBracket
	RightBracket
	Amp
	Caret
	Tilde
	ShiftLeft
	ShiftRight
	And
	Or
	Pipe
	Is
	StringLiteral
	Fun
	Impl
	End
	Return
	If
	ElseIf
	Else
	While
	Break
	Continue
	Defer
	Try
	Errdefer
	Spawn
	As
	Match
	Then
	Self
	For
	In
	Do
	ByteLiteral
	RuneLiteral
	Module
	Import
	Export
	ModulePathLiteral
	// ColonEqual is one token, not Colon followed by Equal, so `x : = 5`
	// stays a syntax error.
	ColonEqual
	EOF
)

var keywords = map[string]TokenKind{
	"true":  True,
	"false": False,
	"nil":   NilLiteral,
	"eos":   Eos,
	"mut":   Mut,
	"ref":   Ref,
	"type":  Type,
	"and":   And,
	"or":    Or,
	"is":    Is,
	// `Fun` the type name stays an ordinary identifier; only lowercase `fun`
	// is a keyword.
	"fun":      Fun,
	"impl":     Impl,
	"end":      End,
	"return":   Return,
	"if":       If,
	"elseif":   ElseIf,
	"else":     Else,
	"while":    While,
	"break":    Break,
	"continue": Continue,
	"defer":    Defer,
	"try":      Try,
	"spawn":    Spawn,
	"errdefer": Errdefer,
	"as":       As,
	"match":    Match,
	"then":     Then,
	"self":     Self,
	"for":      For,
	"in":       In,
	"do":       Do,
	"module":   Module,
	"import":   Import,
	"export":   Export,
}

// String returns the readable name used in parser diagnostics.
func (kind TokenKind) String() string {
	switch kind {
	case Identifier:
		return "identifier"
	case Colon:
		return ":"
	case ColonEqual:
		return ":="
	case Equal:
		return "="
	case Less:
		return "<"
	case Greater:
		return ">"
	case Minus:
		return "-"
	case Integer:
		return "integer"
	case HexInteger:
		return "hexadecimal integer"
	case BinaryInteger:
		return "binary integer"
	case OctalInteger:
		return "octal integer"
	case DecimalFloat:
		return "decimal float"
	case True, False:
		return "boolean"
	case NilLiteral:
		return "nil"
	case Eos:
		return "eos"
	case StringLiteral:
		return "string literal"
	case ByteLiteral:
		return "byte literal"
	case RuneLiteral:
		return "rune literal"
	case Mut:
		return "mut"
	case Ref:
		return "ref"
	case Type:
		return "type"
	case Dot:
		return "."
	case LeftBrace:
		return "{"
	case RightBrace:
		return "}"
	case Comma:
		return ","
	case Bang:
		return "!"
	case BangEqual:
		return "!="
	case EqualEqual:
		return "=="
	case LessEqual:
		return "<="
	case GreaterEqual:
		return ">="
	case Plus:
		return "+"
	case Star:
		return "*"
	case Slash:
		return "/"
	case Percent:
		return "%"
	case LeftParen:
		return "("
	case RightParen:
		return ")"
	case And:
		return "and"
	case Or:
		return "or"
	case Pipe:
		return "|"
	case Amp:
		return "&"
	case Caret:
		return "^"
	case Tilde:
		return "~"
	case ShiftLeft:
		return "<<"
	case ShiftRight:
		return ">>"
	case Is:
		return "is"
	case Fun:
		return "fun"
	case Impl:
		return "impl"
	case End:
		return "end"
	case Return:
		return "return"
	case If:
		return "if"
	case ElseIf:
		return "elseif"
	case Else:
		return "else"
	case While:
		return "while"
	case Break:
		return "break"
	case Continue:
		return "continue"
	case Defer:
		return "defer"
	case Try:
		return "try"
	case Spawn:
		return "spawn"
	case Errdefer:
		return "errdefer"
	case As:
		return "as"
	case Match:
		return "match"
	case Then:
		return "then"
	case Self:
		return "self"
	case For:
		return "for"
	case In:
		return "in"
	case Do:
		return "do"
	case Module:
		return "module"
	case Import:
		return "import"
	case Export:
		return "export"
	case ModulePathLiteral:
		return "module path literal"
	case EOF:
		return "end of input"
	default:
		return "unknown token"
	}
}

// Token is one lexical unit and its 1-based source location.
type Token struct {
	Kind   TokenKind
	Lexeme string
	Line   int
	Column int
}

// Lex tokenizes source. Numeric spelling remains in tokens; exact semantic
// decoding belongs to the checker so no later phase trusts unchecked text.
func Lex(source string) ([]Token, error) {
	tokens := make([]Token, 0)
	diagnostics := make(compilerTypes.Diagnostics, 0)
	line, column := 1, 1

	for index := 0; index < len(source); {
		ch := source[index]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r':
			index++
			column++
		case ch == '\n':
			index++
			line++
			column = 1
		case ch == '-' && index+1 < len(source) && source[index+1] == '-':
			commentLine, commentColumn := line, column
			if index+2 < len(source) && source[index+2] == '[' {
				index += 3
				column += 3
				closed := false
				for index < len(source) {
					if index+2 < len(source) && source[index] == ']' && source[index+1] == '-' && source[index+2] == '-' {
						index += 3
						column += 3
						closed = true
						break
					}
					if source[index] == '\n' {
						index++
						line++
						column = 1
						continue
					}
					index++
					column++
				}
				if !closed {
					diagnostics = append(diagnostics, *literalDiagnostic(commentLine, commentColumn, "unterminated multiline comment"))
				}
				continue
			}
			if index+2 < len(source) && source[index+2] == '-' {
				index += 3
				column += 3
			} else {
				index += 2
				column += 2
			}
			for index < len(source) && source[index] != '\n' {
				index++
				column++
			}
		case ch == 'b' && index+1 < len(source) && source[index+1] == '\'':
			start, startColumn := index, column
			index += 2
			column += 2
			end, closed := scanQuotedBody(source, index, line, column)
			if !closed {
				diagnostics = append(diagnostics, *literalDiagnostic(line, startColumn, "unterminated Byte literal"))
			}
			bodyEnd := end
			if closed {
				bodyEnd = end - 1
			}
			if _, message := DecodeLiteralBody(source[index:bodyEnd], ByteEscapes); message != "" {
				diagnostics = append(diagnostics, *literalDiagnostic(line, startColumn, message))
				tokens = append(tokens, Token{Kind: EOF, Line: line, Column: startColumn})
			} else {
				tokens = append(tokens, Token{Kind: ByteLiteral, Lexeme: source[start:end], Line: line, Column: startColumn})
			}
			column += end - index
			index = end
		case ch == '\'':
			start, startColumn := index, column
			index++
			column++
			end, closed := scanQuotedBody(source, index, line, column)
			if !closed {
				diagnostics = append(diagnostics, *literalDiagnostic(line, startColumn, "unterminated Rune literal"))
			}
			bodyEnd := end
			if closed {
				bodyEnd = end - 1
			}
			if _, message := DecodeLiteralBody(source[index:bodyEnd], RuneEscapes); message != "" {
				diagnostics = append(diagnostics, *literalDiagnostic(line, startColumn, message))
				tokens = append(tokens, Token{Kind: EOF, Line: line, Column: startColumn})
			} else {
				tokens = append(tokens, Token{Kind: RuneLiteral, Lexeme: source[start:end], Line: line, Column: startColumn})
			}
			column += end - index
			index = end
		case ch == '_':
			end := consumeIdentifierTail(source, index+1)
			diagnostics = append(diagnostics, *literalDiagnostic(line, column, "identifiers must begin with a letter"))
			column += end - index
			index = end
		case isIdentifierStart(ch):
			start, startColumn := index, column
			for index < len(source) && isIdentifierPart(source[index]) {
				index++
				column++
			}
			lexeme := source[start:index]
			kind, ok := keywords[lexeme]
			if !ok {
				kind = Identifier
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: startColumn})
		case ch >= '0' && ch <= '9':
			token, end, diagnostic := scanNumber(source, index, line, column)
			if diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
			}
			if token.Kind != EOF {
				tokens = append(tokens, token)
			}
			column += end - index
			index = end
		case ch == '.' && index+1 < len(source) && isDecimalDigit(source[index+1]):
			end := consumeNumericTail(source, index+1)
			diagnostics = append(diagnostics, *literalDiagnostic(line, column, "malformed floating literal"))
			column += end - index
			index = end
		case ch == ':':
			kind, lexeme := Colon, ":"
			if index+1 < len(source) && source[index+1] == '=' {
				kind, lexeme = ColonEqual, ":="
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: column})
			index += len(lexeme)
			column += len(lexeme)
		case ch == '!':
			kind, lexeme := Bang, "!"
			if index+1 < len(source) && source[index+1] == '=' {
				kind, lexeme = BangEqual, "!="
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: column})
			index += len(lexeme)
			column += len(lexeme)
		case ch == '=':
			kind, lexeme := Equal, "="
			if index+1 < len(source) && source[index+1] == '=' {
				kind, lexeme = EqualEqual, "=="
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: column})
			index += len(lexeme)
			column += len(lexeme)
		case ch == '<':
			kind, lexeme := Less, "<"
			if index+1 < len(source) && source[index+1] == '=' {
				kind, lexeme = LessEqual, "<="
			} else if index+1 < len(source) && source[index+1] == '<' {
				// Shift-left is one maximal-munch token.
				kind, lexeme = ShiftLeft, "<<"
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: column})
			index += len(lexeme)
			column += len(lexeme)
		case ch == '>':
			kind, lexeme := Greater, ">"
			if index+1 < len(source) && source[index+1] == '=' {
				kind, lexeme = GreaterEqual, ">="
			} else if index+1 < len(source) && source[index+1] == '>' {
				// Shift-right is one maximal-munch token; the parser
				// splits it into two generic closers when needed.
				kind, lexeme = ShiftRight, ">>"
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: column})
			index += len(lexeme)
			column += len(lexeme)
		case ch == '&':
			tokens = append(tokens, Token{Kind: Amp, Lexeme: "&", Line: line, Column: column})
			index++
			column++
		case ch == '^':
			tokens = append(tokens, Token{Kind: Caret, Lexeme: "^", Line: line, Column: column})
			index++
			column++
		case ch == '~':
			tokens = append(tokens, Token{Kind: Tilde, Lexeme: "~", Line: line, Column: column})
			index++
			column++
		case ch == '-':
			tokens = append(tokens, Token{Kind: Minus, Lexeme: "-", Line: line, Column: column})
			index++
			column++
		case ch == '+':
			tokens = append(tokens, Token{Kind: Plus, Lexeme: "+", Line: line, Column: column})
			index++
			column++
		case ch == '*':
			tokens = append(tokens, Token{Kind: Star, Lexeme: "*", Line: line, Column: column})
			index++
			column++
		case ch == '/':
			tokens = append(tokens, Token{Kind: Slash, Lexeme: "/", Line: line, Column: column})
			index++
			column++
		case ch == '%':
			tokens = append(tokens, Token{Kind: Percent, Lexeme: "%", Line: line, Column: column})
			index++
			column++
		case ch == '|':
			tokens = append(tokens, Token{Kind: Pipe, Lexeme: "|", Line: line, Column: column})
			index++
			column++
		case ch == '"':
			start := index
			startColumn := column
			index++
			column++
			// The literal immediately after `import` on the same line is a
			// module path: a raw quoted payload with no escape decoding. A
			// backslash is rejected outright; module paths are plain
			// relative path spellings.
			isModulePath := len(tokens) > 0 && tokens[len(tokens)-1].Kind == Import && tokens[len(tokens)-1].Line == line
			if isModulePath {
				pathPayloadStart := index
				terminated := false
				for index < len(source) {
					character := source[index]
					index++
					column++
					if character == '"' {
						terminated = true
						break
					}
					if character == '\n' || character == '\r' {
						break
					}
				}
				if !terminated {
					diagnostics = append(diagnostics, *literalDiagnostic(line, column, "unterminated module path literal"))
				}
				// index-1 excludes the closing quote or line terminator that
				// stopped the scan above. An opening quote at the very end of
				// source leaves nothing to scan (index == pathPayloadStart),
				// which would make index-1 precede pathPayloadStart; clamp to
				// an empty payload rather than slicing out of range.
				payloadEnd := index - 1
				if payloadEnd < pathPayloadStart {
					payloadEnd = pathPayloadStart
				}
				if strings.ContainsRune(source[pathPayloadStart:payloadEnd], '\\') {
					diagnostics = append(diagnostics, *literalDiagnostic(line, startColumn, "invalid module-path literal"))
				}
				// Keep a recovery token even when the path is malformed so
				// the parser can synchronize on a real token sequence.
				tokens = append(tokens, Token{Kind: ModulePathLiteral, Lexeme: source[start:index], Line: line, Column: startColumn})
				continue
			}
			terminated := false
			var newlineLine, newlineColumn int
			hasRawNewline := false
			for index < len(source) {
				character := source[index]
				if character == '\\' {
					if index+1 >= len(source) {
						break
					}
					index += 2
					column += 2
					// A backslash immediately followed by a physical newline is
					// not a line-continuation escape.
					escaped := source[index-1]
					if escaped == '\n' || escaped == '\r' {
						if !hasRawNewline {
							hasRawNewline = true
							newlineLine, newlineColumn = line, column-1
						}
						line++
						column = 1
						if escaped == '\r' && index < len(source) && source[index] == '\n' {
							index++
						}
					}
					continue
				}
				index++
				column++
				if character == '"' {
					terminated = true
					break
				}
				if character == '\n' || character == '\r' {
					if !hasRawNewline {
						hasRawNewline = true
						newlineLine, newlineColumn = line, column-1
					}
					line++
					column = 1
					if character == '\r' && index < len(source) && source[index] == '\n' {
						index++
					}
				}
			}
			if hasRawNewline {
				// A raw newline is a Syntax Error and produces no
				// StringLiteral token; consume through the closing quote or
				// EOF for recovery and never emit a second diagnostic.
				diagnostics = append(diagnostics, *literalDiagnostic(newlineLine, newlineColumn, `String literal cannot contain a raw newline; use \n`))
			} else {
				if !terminated {
					diagnostics = append(diagnostics, *literalDiagnostic(line, column, "unterminated string literal"))
				}
				tokens = append(tokens, Token{Kind: StringLiteral, Lexeme: source[start:index], Line: line, Column: startColumn})
			}
		case ch == '(':
			tokens = append(tokens, Token{Kind: LeftParen, Lexeme: "(", Line: line, Column: column})
			index++
			column++
		case ch == ')':
			tokens = append(tokens, Token{Kind: RightParen, Lexeme: ")", Line: line, Column: column})
			index++
			column++
		case ch == '[':
			tokens = append(tokens, Token{Kind: LeftBracket, Lexeme: "[", Line: line, Column: column})
			index++
			column++
		case ch == ']':
			tokens = append(tokens, Token{Kind: RightBracket, Lexeme: "]", Line: line, Column: column})
			index++
			column++
		case ch == '.':
			tokens = append(tokens, Token{Kind: Dot, Lexeme: ".", Line: line, Column: column})
			index++
			column++
		case ch == '{':
			tokens = append(tokens, Token{Kind: LeftBrace, Lexeme: "{", Line: line, Column: column})
			index++
			column++
		case ch == '}':
			tokens = append(tokens, Token{Kind: RightBrace, Lexeme: "}", Line: line, Column: column})
			index++
			column++
		case ch == ',':
			tokens = append(tokens, Token{Kind: Comma, Lexeme: ",", Line: line, Column: column})
			index++
			column++
		default:
			diagnostics = append(diagnostics, *literalDiagnostic(line, column, fmt.Sprintf("unexpected character %q", ch)))
			index++
			column++
		}
	}

	tokens = append(tokens, Token{Kind: EOF, Line: line, Column: column})
	if len(diagnostics) > 0 {
		return tokens, diagnostics
	}
	return tokens, nil
}

// scanQuotedBody scans one single-quoted literal body (after the opening
// quote) up to the closing quote, treating backslash escapes as part of the
// body. It returns the index just past the closing quote (or past the body
// when unterminated) and whether the quote closed.
func scanQuotedBody(source string, start, line, column int) (int, bool) {
	index := start
	closed := false
	for index < len(source) {
		character := source[index]
		if character == '\\' {
			if index+1 >= len(source) {
				index++
				break
			}
			index += 2
			continue
		}
		index++
		if character == '\'' {
			closed = true
			break
		}
		if character == '\n' {
			break
		}
	}
	return index, closed
}

func scanNumber(source string, start, line, column int) (Token, int, *compilerTypes.Diagnostic) {
	if source[start] == '0' && start+1 < len(source) {
		prefix := source[start+1]
		if prefix == 'X' || prefix == 'B' || prefix == 'O' {
			end := consumeNumericTail(source, start+2)
			return Token{Kind: EOF}, end, literalDiagnostic(line, column, "integer base prefixes must be lowercase")
		}
		if prefix == 'x' || prefix == 'b' || prefix == 'o' {
			kind, label, digit := Integer, "integer", isDecimalDigit
			switch prefix {
			case 'x':
				kind, label, digit = HexInteger, "hexadecimal", isHexDigit
			case 'b':
				kind, label, digit = BinaryInteger, "binary", isBinaryDigit
			case 'o':
				kind, label, digit = OctalInteger, "octal", isOctalDigit
			}
			end, malformed := scanDigitRun(source, start+2, digit)
			if end == start+2 || malformed || isIdentifierPartAt(source, end) || (end < len(source) && source[end] == '.') {
				end = consumeNumericTail(source, end)
				message := "malformed " + label + " integer literal"
				if prefix == 'x' {
					message = "malformed hexadecimal literal"
				}
				return Token{Kind: EOF}, end, literalDiagnostic(line, column, message)
			}
			return Token{Kind: kind, Lexeme: source[start:end], Line: line, Column: column}, end, nil
		}
	}

	end, malformed, leadingZero := scanDecimalWhole(source, start)
	if malformed {
		end = consumeNumericTail(source, end)
		return Token{Kind: EOF}, end, literalDiagnostic(line, column, "malformed decimal integer literal")
	}
	if leadingZero {
		end = consumeNumericTail(source, end)
		return Token{Kind: EOF}, end, literalDiagnostic(line, column, "decimal integer literals cannot have leading zeros")
	}

	isFloat := false
	if end < len(source) && source[end] == '.' {
		if end+1 < len(source) && isDecimalDigit(source[end+1]) {
			isFloat = true
			var fractionMalformed bool
			end, fractionMalformed = scanDigitRun(source, end+1, isDecimalDigit)
			if fractionMalformed {
				end = consumeNumericTail(source, end)
				return Token{Kind: EOF}, end, literalDiagnostic(line, column, "malformed decimal floating literal")
			}
		} else {
			return Token{Kind: EOF}, end + 1, literalDiagnostic(line, column, "malformed decimal floating literal")
		}
	}
	if end < len(source) && (source[end] == 'e' || source[end] == 'E') {
		isFloat = true
		exponentStart := end
		end++
		if end < len(source) && (source[end] == '+' || source[end] == '-') {
			end++
		}
		var exponentMalformed bool
		end, exponentMalformed = scanDigitRun(source, end, isDecimalDigit)
		if exponentStart == end || exponentMalformed || end == exponentStart+1 || (end > 0 && !isDecimalDigit(source[end-1])) {
			end = consumeNumericTail(source, end)
			return Token{Kind: EOF}, end, literalDiagnostic(line, column, "malformed decimal floating literal")
		}
	}
	if isFloat {
		if end < len(source) && isIdentifierPart(source[end]) {
			end = consumeNumericTail(source, end)
			return Token{Kind: EOF}, end, literalDiagnostic(line, column, "identifiers must begin with a letter")
		}
		return Token{Kind: DecimalFloat, Lexeme: source[start:end], Line: line, Column: column}, end, nil
	}
	if end < len(source) && isIdentifierPart(source[end]) {
		end = consumeNumericTail(source, end)
		return Token{Kind: EOF}, end, literalDiagnostic(line, column, "identifiers must begin with a letter")
	}
	return Token{Kind: Integer, Lexeme: source[start:end], Line: line, Column: column}, end, nil
}

func scanDecimalWhole(source string, start int) (int, bool, bool) {
	if source[start] == '0' {
		if start+1 < len(source) && source[start+1] == '_' {
			end := consumeNumericTail(source, start+1)
			return end, false, true
		}
		if start+1 < len(source) && isDecimalDigit(source[start+1]) {
			end := start + 1
			for end < len(source) && (isDecimalDigit(source[end]) || source[end] == '_') {
				end++
			}
			return end, false, true
		}
		return start + 1, false, false
	}
	end, malformed := scanDigitRun(source, start, isDecimalDigit)
	return end, malformed, false
}

func scanDigitRun(source string, start int, isDigit func(byte) bool) (int, bool) {
	if start >= len(source) || !isDigit(source[start]) {
		return start, true
	}
	index := start + 1
	malformed := false
	for index < len(source) {
		if isDigit(source[index]) {
			index++
			continue
		}
		if source[index] != '_' {
			break
		}
		if index+1 >= len(source) || !isDigit(source[index+1]) {
			malformed = true
			index++
			break
		}
		index += 2
	}
	return index, malformed
}

func consumeNumericTail(source string, index int) int {
	for index < len(source) && (isIdentifierPart(source[index]) || source[index] == '_' || source[index] == '.') {
		index++
	}
	return index
}

func consumeIdentifierTail(source string, index int) int {
	for index < len(source) && isIdentifierPart(source[index]) {
		index++
	}
	return index
}

func isIdentifierPartAt(source string, index int) bool {
	return index < len(source) && isIdentifierPart(source[index])
}

func literalDiagnostic(line, column int, message string) *compilerTypes.Diagnostic {
	return &compilerTypes.Diagnostic{Category: compilerTypes.SyntaxError, Stage: "lexer", Line: line, Column: column, Message: message}
}

func isIdentifierStart(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func isIdentifierPart(ch byte) bool {
	return isIdentifierStart(ch) || ch == '_' || ch >= '0' && ch <= '9'
}

// IsIdentifierStart reports whether ch can begin a Hexal identifier.
func IsIdentifierStart(ch byte) bool { return isIdentifierStart(ch) }

// IsIdentifierPart reports whether ch can continue a Hexal identifier.
func IsIdentifierPart(ch byte) bool { return isIdentifierPart(ch) }

func isDecimalDigit(ch byte) bool { return ch >= '0' && ch <= '9' }

func isBinaryDigit(ch byte) bool { return ch == '0' || ch == '1' }

func isOctalDigit(ch byte) bool { return ch >= '0' && ch <= '7' }

func isHexDigit(ch byte) bool {
	return isDecimalDigit(ch) || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F'
}
