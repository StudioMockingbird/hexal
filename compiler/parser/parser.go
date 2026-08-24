package parser

import (
	"errors"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// Parser owns the token cursor used by the recursive-descent grammar.
type Parser struct {
	tokens      []lexer.Token
	current     int
	braceDepth  int
	bodyDepth   int
	blockStack  []string
	diagnostics compilerTypes.Diagnostics
	// pendingGreater carries the second half of a `>>` token split into two
	// generic closers while parsing nested type arguments.
	pendingGreater bool
	// matchBoundary suspends the `|` (and, for a scrutinee, `is`) operator at
	// match-expression depth zero so an unparenthesized `|` starts the next
	// arm and an unparenthesized `is` selects type mode. Parenthesized
	// subexpressions clear it.
	matchBoundary matchBoundaryKind
	// implReceiver is set while parsing an impl declaration's receiver type
	// and unionMemberDepth counts union-member primaries inside it. A dotted
	// name inside such a member (impl MutPtr<Node> | Nil.read()) is the
	// declaration's method delimiter, never a qualified type chain; every
	// union receiver is semantically invalid anyway, so suppressing the chain
	// there loses nothing.
	implReceiver     bool
	unionMemberDepth int
	// binaryOperatorRecorded, binaryOperatorKind, and binaryOperatorToken track
	// the current expression region's one allowed binary operator token kind.
	// expression and matchExpression's scrutinee and arm parses each push a
	// fresh region and restore the containing one on exit; every other
	// independently delimited nested expression reaches a fresh region for
	// free by calling expression again. recordBinaryOperator is the single
	// enforcement point every binary-operator parser path calls.
	binaryOperatorRecorded bool
	binaryOperatorKind     lexer.TokenKind
	binaryOperatorToken    lexer.Token
}

// matchBoundaryKind selects which tokens terminate a match position's top-level
// parse.
type matchBoundaryKind uint8

const (
	noMatchBoundary   matchBoundaryKind = iota
	scrutineeBoundary                   // an unparenthesized `is` or `|` ends the scrutinee
	armBoundary                         // an unparenthesized `|` ends the arm result
)

// blockRecovery is an internal signal used to return an outer delimiter, or an
// already-diagnosed EOF, to its still-active block owner. The diagnostic that
// caused the recovery is stored on Parser and must never be reported as this
// sentinel.
type blockRecovery struct{}

func (blockRecovery) Error() string { return "internal block recovery" }

// blockFailure carries a block-owned diagnostic through enclosing blocks so it
// is recorded once at the top-level recovery boundary.
type blockFailure struct {
	err error
}

func (failure blockFailure) Error() string { return failure.err.Error() }

// Unwrap exposes the carried diagnostic so errors.As and errors.Is traverse a
// block failure instead of stopping at it.
func (failure blockFailure) Unwrap() error { return failure.err }

// Parse consumes every recoverable top-level item through EOF. Invalid items
// are discarded during synchronization so valid later items remain available
// to the checker.
func Parse(tokens []lexer.Token) (Program, error) {
	if len(tokens) == 0 {
		return Program{}, compilerTypes.NewDiagnostic(compilerTypes.SyntaxError, "parser", 1, 1, "expected a declaration")
	}

	parser := Parser{tokens: tokens}
	items := make([]TopLevelItem, 0)
	statements := make([]Statement, 0)
	// hasNonImport marks the first top-level item the import prefix cannot
	// span; any import parsed after it is misplaced.
	hasNonImport := false
	for !parser.check(lexer.EOF) {
		start := parser.current
		item, err := parser.topLevelItem()
		if err != nil {
			if _, recovered := err.(blockRecovery); !recovered {
				parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(err)...)
			}
			before := parser.current
			parser.synchronize(start)
			// A propagated boundary has no owner at module level. Consume it
			// after synchronization so recovery cannot retry the same token.
			if _, recovered := err.(blockRecovery); recovered && parser.current == before && !parser.check(lexer.EOF) {
				parser.advance()
			}
			continue
		}
		// The import prefix closes at the first non-import item: an import
		// after any other top-level item is a Syntax Error at its own
		// keyword, and the misplaced item is dropped so no alias reaches the
		// checker.
		if importDecl, isImport := item.(ImportDeclaration); isImport && hasNonImport {
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(parser.errorAt(importDecl.ImportKeyword, "imports must precede all other top-level items"))...)
			continue
		}
		items = append(items, item)
		if statement, ok := item.(Statement); ok {
			statements = append(statements, statement)
		}
		if _, isImport := item.(ImportDeclaration); !isImport {
			hasNonImport = true
		}
	}
	program := Program{Items: items, Statements: statements}
	if len(parser.diagnostics) > 0 {
		return program, parser.diagnostics
	}
	return program, nil
}

func (parser *Parser) topLevelItem() (TopLevelItem, error) {
	switch {
	case parser.check(lexer.Export):
		return parser.exportedDeclaration()
	case parser.check(lexer.Module):
		return parser.importDeclaration()
	case parser.check(lexer.Import):
		// A bare `import` lacks the mandatory `module <alias> =` prefix; route
		// through importDeclaration so the diagnostic points at the keyword.
		return parser.importDeclaration()
	case parser.check(lexer.Type):
		return parser.typeDeclaration(false)
	case parser.check(lexer.Fun):
		return parser.functionDeclaration(false)
	case parser.check(lexer.Impl):
		return parser.implDeclaration(false)
	}
	statement, err := parser.statement()
	if err != nil {
		return nil, err
	}
	return statement, nil
}

// exportedDeclaration consumes a leading `export` and requires the exportable
// declaration forms: a module-level type, function, or implementation
// declaration. Anything else is a Syntax Error.
func (parser *Parser) exportedDeclaration() (TopLevelItem, error) {
	parser.advance()
	switch {
	case parser.check(lexer.Type):
		return parser.typeDeclaration(true)
	case parser.check(lexer.Fun):
		return parser.functionDeclaration(true)
	case parser.check(lexer.Impl):
		return parser.implDeclaration(true)
	}
	next := parser.peek()
	parser.synchronize(parser.current)
	return nil, parser.errorAt(next, "export may prefix only a module-level type, function, or implementation declaration")
}

// importDeclaration parses the exact form
// `module <identifier> = import <module-path-literal>` and binds the alias.
func (parser *Parser) importDeclaration() (ImportDeclaration, error) {
	moduleKeyword, err := parser.consume(lexer.Module, "'module'")
	if err != nil {
		return ImportDeclaration{}, err
	}
	alias, err := parser.consume(lexer.Identifier, "an identifier after 'module'")
	if err != nil {
		return ImportDeclaration{}, err
	}
	equal, err := parser.consume(lexer.Equal, "'='")
	if err != nil {
		return ImportDeclaration{}, err
	}
	importKeyword, err := parser.consume(lexer.Import, "'import'")
	if err != nil {
		return ImportDeclaration{}, err
	}
	path, err := parser.consume(lexer.ModulePathLiteral, "a module path literal after 'import'")
	if err != nil {
		return ImportDeclaration{}, err
	}
	return ImportDeclaration{
		ModuleKeyword: moduleKeyword,
		Alias:         alias,
		Equal:         equal,
		ImportKeyword: importKeyword,
		Path:          path,
	}, nil
}

func (parser *Parser) peek() lexer.Token {
	if parser.current >= len(parser.tokens) {
		return lexer.Token{Kind: lexer.EOF, Line: 1, Column: 1}
	}
	return parser.tokens[parser.current]
}

func (parser *Parser) advance() lexer.Token {
	token := parser.peek()
	if parser.current < len(parser.tokens) {
		parser.current++
		switch token.Kind {
		case lexer.LeftBrace:
			parser.braceDepth++
		case lexer.RightBrace:
			if parser.braceDepth > 0 {
				parser.braceDepth--
			}
		}
	}
	return token
}

func (parser *Parser) check(kind lexer.TokenKind) bool {
	return parser.peek().Kind == kind
}

func (parser *Parser) consume(kind lexer.TokenKind, expected string) (lexer.Token, error) {
	if kind == lexer.Greater && parser.pendingGreater {
		parser.pendingGreater = false
		return lexer.Token{Kind: lexer.Greater, Lexeme: ">", Line: parser.peek().Line, Column: parser.peek().Column}, nil
	}
	if parser.check(kind) {
		return parser.advance(), nil
	}
	return lexer.Token{}, parser.errorAtCurrent("expected " + expected)
}

func (parser *Parser) statement() (Statement, error) {
	switch {
	case parser.check(lexer.Fun):
		// At statement position, `fun` followed by an identifier used to
		// begin a local named function declaration; named function
		// declarations are now module-scope only. `fun (` and `fun<` begin
		// an anonymous literal which is not a statement - it must be bound
		// first.
		if parser.tokenAfterFun() == lexer.Identifier {
			return nil, parser.errorAt(parser.peek(), "named function declarations are only valid at module scope")
		}
		if parser.funBeginsAnonymousLiteral() {
			return nil, parser.errorAt(parser.peek(), "anonymous functions cannot begin statements; bind the function first")
		}
		return nil, parser.errorAt(parser.peek(), "anonymous function requires '(' or '<' after 'fun'")
	case parser.check(lexer.Impl):
		return nil, parser.errorAt(parser.peek(), "impl declarations are module-level only")
	case parser.check(lexer.Type):
		return nil, parser.errorAt(parser.peek(), "type declarations are module-level only")
	case parser.check(lexer.LeftParen):
		// postfix refuses a '(' that begins a new line, which leaves the '('
		// starting a statement. That is only ever a split call.
		return nil, parser.errorAt(parser.peek(), "a call's ( must follow its callee on the same line")
	case parser.check(lexer.Return):
		return parser.returnStatement()
	case parser.check(lexer.If):
		return parser.ifStatement()
	case parser.check(lexer.While):
		return parser.whileStatement()
	case parser.check(lexer.For):
		return parser.forStatement()
	case parser.check(lexer.Break):
		return BreakStatement{Keyword: parser.advance()}, nil
	case parser.check(lexer.Continue):
		return ContinueStatement{Keyword: parser.advance()}, nil
	case parser.check(lexer.Defer):
		keyword := parser.advance()
		expression, err := parser.expression()
		if err != nil {
			return nil, err
		}
		return DeferStatement{Keyword: keyword, Expression: expression}, nil
	case parser.check(lexer.Errdefer):
		keyword := parser.advance()
		expression, err := parser.expression()
		if err != nil {
			return nil, err
		}
		return ErrdeferStatement{Keyword: keyword, Expression: expression}, nil
	case parser.check(lexer.ElseIf):
		if len(parser.blockStack) > 0 && parser.blockStack[len(parser.blockStack)-1] == "while" {
			return nil, parser.errorAt(parser.peek(), "'elseif' cannot appear inside a while body")
		}
		if len(parser.blockStack) > 0 && parser.blockStack[len(parser.blockStack)-1] == "else" {
			return nil, parser.errorAt(parser.peek(), "'elseif' cannot appear after 'else'")
		}
		return nil, parser.errorAt(parser.peek(), "unexpected 'elseif' outside an if statement")
	case parser.check(lexer.Export):
		// export applies only to module-level declarations. At statement
		// position it can never be valid, so it is rejected here rather than
		// as a confusing identifier-form error.
		return nil, parser.errorAt(parser.peek(), "export may prefix only a module-level type, function, or implementation declaration")
	case parser.check(lexer.Else):
		if len(parser.blockStack) > 0 {
			top := parser.blockStack[len(parser.blockStack)-1]
			if top == "while" {
				return nil, parser.errorAt(parser.peek(), "'else' cannot appear inside a while body")
			}
			if top == "else" {
				return nil, parser.errorAt(parser.peek(), "'else' must be the final clause of an if statement")
			}
		}
		return nil, parser.errorAt(parser.peek(), "unexpected 'else' outside an if statement")
	case parser.check(lexer.End):
		return nil, parser.errorAt(parser.peek(), "unexpected 'end' outside a block")
	case parser.check(lexer.Self):
		return parser.postfixStatement(VariableExpression{Name: parser.advance()})
	case parser.check(lexer.Try):
		// `try <unary-expression>` is a statement as well as an expression,
		// with the same unary boundary as prefix try.
		keyword := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return TryStatement{Keyword: keyword, Operand: operand}, nil
	}

	if parser.check(lexer.Mut) {
		mutable := parser.advance()
		if parser.check(lexer.Fun) {
			return nil, parser.errorAt(mutable, "mut cannot modify a function declaration; declare a mut Fun binding")
		}
		name, err := parser.consume(lexer.Identifier, "an identifier after 'mut'")
		if err != nil {
			return nil, err
		}
		if parser.check(lexer.Equal) {
			equal := parser.advance()
			return nil, parser.errorAt(equal, "binding declarations require ':='; '=' assigns to an existing place")
		}
		if !parser.check(lexer.Colon) && !parser.check(lexer.ColonEqual) {
			return nil, parser.errorAt(mutable, "'mut' at statement start must introduce a declaration")
		}
		return parser.declaration(name, true)
	}

	name, err := parser.consume(lexer.Identifier, "an identifier")
	if err != nil {
		return nil, err
	}
	if parser.check(lexer.Colon) || parser.check(lexer.ColonEqual) {
		return parser.declaration(name, false)
	}
	return parser.postfixStatement(VariableExpression{Name: name})
}

// postfixStatement completes the two statement forms that start with a postfix
// chain. An assignment target may be a variable or a place such as p.value.
// A call statement is a chain whose final operation is a call; a chain ending
// in member selection is an expression and never a statement.
func (parser *Parser) postfixStatement(start Expression) (Statement, error) {
	target, err := parser.postfix(start)
	if err != nil {
		return nil, err
	}
	if parser.check(lexer.Equal) {
		parser.advance()
		initializer, err := parser.expression()
		if err != nil {
			return nil, err
		}
		name, _ := start.(VariableExpression)
		return Assignment{Name: name.Name, Target: target, Initializer: initializer}, nil
	}
	if call, ok := target.(CallExpression); ok {
		return call, nil
	}
	return nil, parser.errorAtCurrent("expected ':' or ':=' for a declaration, or '=' for an assignment")
}

// returnStatement parses `return` with its optional value. The value's first
// token must sit on the return's own line; otherwise the return is bare. When
// the next line begins with a token that can only start an expression, the
// source cannot be read as bare-return-then-statement, so it is reported here
// rather than as a confusing statement-form error.
func (parser *Parser) returnStatement() (Statement, error) {
	keyword := parser.advance()
	if parser.bodyDepth == 0 {
		return nil, parser.errorAt(keyword, "return is only valid inside a function or method body")
	}
	next := parser.peek()
	// `fun` is not classified by startsExpression/valueOnlyToken below: unlike
	// every other token there, whether it is a value depends on the token
	// after it. `fun (`/`fun<` is always an anonymous literal (a value);
	// `fun identifier` is a local function declaration (never a value), and
	// must fall through to the ordinary next-statement path even when it
	// shares return's line.
	if next.Kind == lexer.Fun {
		if next.Line == keyword.Line && parser.funBeginsAnonymousLiteral() {
			value, err := parser.expression()
			if err != nil {
				return nil, err
			}
			return ReturnStatement{Keyword: keyword, Value: value}, nil
		}
		return ReturnStatement{Keyword: keyword}, nil
	}
	if next.Line == keyword.Line && startsExpression(next.Kind) {
		value, err := parser.expression()
		if err != nil {
			return nil, err
		}
		return ReturnStatement{Keyword: keyword, Value: value}, nil
	}
	if valueOnlyToken(next.Kind) {
		return nil, parser.errorAt(next, "a return value must begin on the same line as return")
	}
	return ReturnStatement{Keyword: keyword}, nil
}

// tokenAfterFun returns the kind of the token immediately after the current
// `fun` token, or lexer.EOF when none exists. `fun` is the only reserved word
// whose statement-versus-expression classification depends on lookahead
// rather than its own kind, so every `fun`-dispatch site shares this helper.
func (parser *Parser) tokenAfterFun() lexer.TokenKind {
	if parser.current+1 >= len(parser.tokens) {
		return lexer.EOF
	}
	return parser.tokens[parser.current+1].Kind
}

// funBeginsAnonymousLiteral reports whether the token at the parser's current
// position is `fun` immediately followed by `(` or `<`, per the grammar rule
// that no other token may follow expression-position `fun`.
func (parser *Parser) funBeginsAnonymousLiteral() bool {
	next := parser.tokenAfterFun()
	return next == lexer.LeftParen || next == lexer.Less
}

func startsExpression(kind lexer.TokenKind) bool {
	switch kind {
	case lexer.Identifier, lexer.Self, lexer.LeftParen:
		return true
	default:
		return valueOnlyToken(kind)
	}
}

// valueOnlyToken reports whether a token can begin an expression but can never
// begin a statement. '(' is excluded: it is owned by the same-line call rule.
// Match is value-only: every match lives in expression position, so a match
// after return belongs to the return's own line rather than opening a
// statement.
func valueOnlyToken(kind lexer.TokenKind) bool {
	switch kind {
	case lexer.Integer, lexer.HexInteger, lexer.BinaryInteger, lexer.OctalInteger,
		lexer.DecimalFloat, lexer.True, lexer.False, lexer.NilLiteral, lexer.Eos,
		lexer.Minus, lexer.Bang, lexer.Ref, lexer.LeftBracket, lexer.StringLiteral,
		lexer.Match:
		return true
	default:
		return false
	}
}

func (parser *Parser) errorAtCurrent(message string) error {
	token := parser.peek()
	return parser.errorAt(token, message)
}

func (parser *Parser) errorAt(token lexer.Token, message string) error {
	return compilerTypes.NewDiagnostic(compilerTypes.SyntaxError, "parser", token.Line, token.Column, message)
}

// synchronize discards the invalid statement while preserving the next token
// sequence that can begin a statement or delimiter. A failed parse must
// consume at least one token or recovery can repeatedly retry the same input.
func (parser *Parser) synchronize(start int) {
	if parser.current == start && !parser.check(lexer.EOF) {
		parser.advance()
	}
	for !parser.check(lexer.EOF) && !parser.atStatementStart() {
		parser.advance()
	}
}

// synchronizeBlock preserves a delimiter that caused an error before any
// token was consumed. The enclosing block can then either own it or propagate
// it to its own owner instead of losing the delimiter in global recovery.
func (parser *Parser) synchronizeBlock(start int) {
	if parser.current == start && isBlockDelimiter(parser.peek().Kind) {
		return
	}
	parser.synchronize(start)
}

func isBlockDelimiter(kind lexer.TokenKind) bool {
	return kind == lexer.ElseIf || kind == lexer.Else || kind == lexer.End
}

func (parser *Parser) atStatementStart() bool {
	if parser.braceDepth != 0 {
		return false
	}
	// Block delimiters are recovery points too. Leaving them available lets
	// the next parser invocation report the delimiter instead of silently
	// consuming an outer construct's closing token.
	if parser.check(lexer.ElseIf) || parser.check(lexer.Else) || parser.check(lexer.End) {
		return true
	}
	if parser.check(lexer.Type) || parser.check(lexer.Fun) || parser.check(lexer.Impl) ||
		parser.check(lexer.Module) || parser.check(lexer.Import) || parser.check(lexer.Export) ||
		parser.check(lexer.If) || parser.check(lexer.While) || parser.check(lexer.For) ||
		parser.check(lexer.Break) || parser.check(lexer.Continue) || parser.check(lexer.Return) ||
		parser.check(lexer.Self) {
		return true
	}
	if parser.check(lexer.Mut) {
		if parser.current+2 >= len(parser.tokens) {
			return false
		}
		return parser.tokens[parser.current+1].Kind == lexer.Identifier &&
			(parser.tokens[parser.current+2].Kind == lexer.Colon ||
				parser.tokens[parser.current+2].Kind == lexer.ColonEqual)
	}
	if !parser.check(lexer.Identifier) {
		return false
	}
	if parser.current+1 >= len(parser.tokens) {
		return false
	}
	next := parser.tokens[parser.current+1].Kind
	if next == lexer.Colon || next == lexer.ColonEqual || next == lexer.Equal {
		return true
	}
	// A call statement resumes parsing only when its '(' obeys the same-line
	// rule; otherwise the '(' is not part of this identifier's chain.
	if next == lexer.LeftParen {
		return parser.tokens[parser.current+1].Line == parser.tokens[parser.current].Line
	}
	if next != lexer.Dot {
		return false
	}

	// A dotted place is a recovery point only when its complete postfix chain
	// is followed by assignment or a same-line call; otherwise the chain is an
	// expression and never a statement. Member names inside a malformed brace
	// construct are excluded by braceDepth above.
	index := parser.current + 1
	for index+1 < len(parser.tokens) &&
		parser.tokens[index].Kind == lexer.Dot &&
		parser.tokens[index+1].Kind == lexer.Identifier {
		index += 2
	}
	if index >= len(parser.tokens) {
		return false
	}
	if parser.tokens[index].Kind == lexer.Equal {
		return true
	}
	return parser.tokens[index].Kind == lexer.LeftParen &&
		parser.tokens[index].Line == parser.tokens[index-1].Line
}

// diagnosticsFrom renders any parser error as structured diagnostics. It
// traverses wrappers with errors.As rather than a hand-rolled type-assertion
// ladder, so a diagnostic stays reachable however deeply it is wrapped:
// blockFailure included, through its Unwrap.
func diagnosticsFrom(err error) compilerTypes.Diagnostics {
	var diagnostics compilerTypes.Diagnostics
	if errors.As(err, &diagnostics) {
		return diagnostics
	}
	var diagnostic compilerTypes.Diagnostic
	if errors.As(err, &diagnostic) {
		return compilerTypes.Diagnostics{diagnostic}
	}
	return compilerTypes.Diagnostics{{
		Category: compilerTypes.UnknownError,
		Stage:    "parser",
		Message:  err.Error(),
	}}
}
