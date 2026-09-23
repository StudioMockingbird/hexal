package parser

import (
	"errors"
	"strings"

	"hexal/compiler/config"
	"hexal/compiler/lexer"
	"hexal/compiler/span"
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
	// pendingGreaterToken is the `>>` token the split came from; the second
	// synthetic `>` is that token's second byte, so a split never invents a
	// location.
	pendingGreaterToken lexer.Token
	// matchBoundary suspends the `|` (and, for a scrutinee, `is`) operator at
	// match-expression depth zero so an unparenthesized `|` starts the next
	// arm and an unparenthesized `is` selects type mode. Parenthesized
	// subexpressions clear it.
	matchBoundary matchBoundaryKind
	// methodReceiver is set while parsing a method declaration's receiver type
	// and unionMemberDepth counts union-member primaries inside it. A dotted
	// name inside such a member (method Ptr<mut Node> | Nil.read()) is the
	// declaration's method delimiter, never a qualified type chain; every
	// union receiver is semantically invalid anyway, so suppressing the chain
	// there loses nothing.
	methodReceiver   bool
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
	// syntaxDepth is the one shared recursive-syntax budget covering every
	// recursively entered production: expressions, type expressions, match
	// patterns, and statement blocks. Counting only one production leaves the
	// others able to exhaust the Go call stack, which a panic recovery cannot
	// catch because a stack overflow is a fatal runtime error, not a panic.
	syntaxDepth int
}

// enterSyntax increments the shared recursive-syntax depth and returns the
// matching exit function, which the caller must defer immediately so the
// depth unwinds on every return path, including one already carrying this
// call's own depth-exceeded error and including recovery paths that resume
// parsing with the same Parser after an error. Call this at the start of
// every production named on syntaxDepth, before any recursive descent.
func (parser *Parser) enterSyntax() (func(), error) {
	parser.syntaxDepth++
	exit := func() { parser.syntaxDepth-- }
	if parser.syntaxDepth > config.MaxSyntaxDepth {
		return exit, parser.errorAtCurrent("nesting exceeds the maximum depth of 128")
	}
	return exit, nil
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

// Parse consumes an optional leading import block, every recoverable
// top-level item, and an optional trailing export block through EOF. Invalid
// items are discarded during synchronization so valid later items remain
// available to the checker.
func Parse(tokens []lexer.Token) (Program, error) {
	if len(tokens) == 0 {
		return Program{}, compilerTypes.NewDiagnostic(compilerTypes.SyntaxError, "parser", 1, 1, "expected a declaration")
	}

	parser := Parser{tokens: tokens}
	program := Program{}
	if parser.check(lexer.Import) {
		block, err := parser.importBlock()
		if err != nil {
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(err)...)
			parser.synchronize(parser.current)
		} else {
			program.Import = &block
		}
	}

	// Foreign blocks are leading: they follow the optional import block and
	// precede every ordinary top-level item.
	externs := make([]ExternBlock, 0)
	for parser.atExternBlock() && !parser.check(lexer.EOF) {
		block, err := parser.externBlock()
		if err != nil {
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(err)...)
			parser.synchronize(parser.current)
			continue
		}
		externs = append(externs, block)
	}
	program.Externs = externs

	items := make([]TopLevelItem, 0)
	statements := make([]Statement, 0)
	exportClosed := false
	for !parser.check(lexer.EOF) {
		if parser.atExternBlock() {
			token := parser.peek()
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(parser.errorAt(token, "extern blocks must precede ordinary top-level items"))...)
			parser.synchronize(parser.current)
			continue
		}
		if exportClosed {
			next := parser.peek()
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(parser.errorAt(next, "export block must be the final top-level construct"))...)
			parser.synchronize(parser.current)
			if parser.check(lexer.EOF) {
				break
			}
			parser.advance()
			continue
		}
		if parser.check(lexer.Import) {
			keyword := parser.peek()
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(parser.errorAt(keyword, "import block must be the first top-level construct"))...)
			parser.synchronize(parser.current)
			continue
		}
		if parser.check(lexer.Export) {
			block, err := parser.exportBlock()
			if err != nil {
				parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(err)...)
				parser.synchronize(parser.current)
				continue
			}
			program.Export = &block
			exportClosed = true
			continue
		}
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
		items = append(items, item)
		if statement, ok := item.(Statement); ok {
			statements = append(statements, statement)
		}
	}
	program.Items = items
	program.Statements = statements
	if len(parser.diagnostics) > 0 {
		return program, parser.diagnostics
	}
	return program, nil
}

func (parser *Parser) topLevelItem() (TopLevelItem, error) {
	switch {
	case parser.check(lexer.Type):
		return parser.typeDeclaration(false)
	case parser.check(lexer.Fun):
		return parser.functionDeclaration(false)
	case parser.check(lexer.Method):
		return parser.methodDeclaration(false)
	}
	statement, err := parser.statement()
	if err != nil {
		return nil, err
	}
	return statement, nil
}

// importBlock parses the file's one leading import list:
// `import alias from "path", alias from std.module, end`. from is contextual:
// it is recognized only here, by lexeme, never reserved.
func (parser *Parser) importBlock() (ImportBlock, error) {
	keyword := parser.advance()
	if parser.check(lexer.End) {
		parser.advance()
		return ImportBlock{}, parser.errorAt(keyword, "import block requires at least one entry")
	}
	var entries []ImportEntry
	for {
		alias, err := parser.consume(lexer.Identifier, "an import alias")
		if err != nil {
			return ImportBlock{}, err
		}
		from := parser.peek()
		if from.Kind != lexer.Identifier || from.Lexeme != "from" {
			return ImportBlock{}, parser.errorAtCurrent("expected 'from' after an import alias")
		}
		parser.advance()
		reference, err := parser.importReference(from)
		if err != nil {
			return ImportBlock{}, err
		}
		entries = append(entries, ImportEntry{Alias: alias, From: from, Reference: reference})
		if parser.check(lexer.Comma) {
			parser.advance()
			if parser.check(lexer.End) {
				break
			}
			continue
		}
		break
	}
	end, err := parser.consume(lexer.End, "'end' to close the import block")
	if err != nil {
		return ImportBlock{}, err
	}
	return ImportBlock{Keyword: keyword, Entries: entries, End: end}, nil
}

// importReference parses one module reference after a contextual `from`: a
// quoted relative source-map path, or a dotted `std.<component>...`
// standard-library reference. A quoted non-relative payload is a syntax error
// that names the dotted spelling it must have used.
func (parser *Parser) importReference(from lexer.Token) (ImportReference, error) {
	switch {
	case parser.check(lexer.ModulePathLiteral):
		path := parser.advance()
		payload := strings.TrimSuffix(strings.TrimPrefix(path.Lexeme, "\""), "\"")
		if strings.HasPrefix(payload, "./") || strings.HasPrefix(payload, "../") {
			return ImportReference{
				Kind:            RelativeImportReference,
				Token:           path,
				DisplaySpelling: payload,
				RelativePath:    path,
			}, nil
		}
		if strings.HasPrefix(payload, "std/") || payload == "std" {
			// A quoted standard-library path is a retired spelling: the
			// dotted form is the one obvious way to name a stdlib module.
			dotted := strings.ReplaceAll(payload, "/", ".")
			return ImportReference{}, parser.errorAt(path,
				"standard-library imports use dotted paths; write "+dotted)
		}
		return ImportReference{}, parser.errorAt(path, "quoted import paths must begin with ./ or ../")
	}
	if parser.check(lexer.Identifier) && parser.peek().Lexeme == "c" {
		return parser.cHeaderReference(parser.advance())
	}
	std := parser.peek()
	if std.Kind != lexer.Identifier || std.Lexeme != "std" {
		return ImportReference{}, parser.errorAtCurrent("a module path literal after 'from'")
	}
	if std.Line != from.Line {
		return ImportReference{}, parser.errorAt(std, "module reference must begin on the same line as 'from'")
	}
	parser.advance()
	if parser.check(lexer.Slash) {
		// `std/io` mixes the retired collection-path separator into the
		// dotted reference; name the exact replacement.
		return ImportReference{}, parser.errorAt(parser.peek(), "standard-library imports use dots between components")
	}
	if !parser.check(lexer.Dot) {
		return ImportReference{}, parser.errorAt(std, "standard-library import requires a component after std.")
	}
	parser.advance()
	var components []lexer.Token
	spelled := "std"
	for {
		if !parser.check(lexer.Identifier) {
			return ImportReference{}, parser.errorAtCurrent("expected a standard-library module component after '.'")
		}
		component := parser.advance()
		components = append(components, component)
		spelled += "." + component.Lexeme
		if parser.check(lexer.Dot) {
			parser.advance()
			continue
		}
		break
	}
	return ImportReference{
		Kind:            StandardLibraryImportReference,
		Token:           std,
		DisplaySpelling: spelled,
		Components:      components,
	}, nil
}

// cHeaderReference parses `c <header>` / `c "header"` after a contextual
// `from`. The literal parsing, payload extraction, and name validation live in
// cHeaderLiteral, shared with the foreign-block header.
func (parser *Parser) cHeaderReference(keyword lexer.Token) (ImportReference, error) {
	return parser.cHeaderLiteral(keyword)
}

// validCHeaderName reports whether payload is a nonempty relative header name
// that does not walk to a parent directory or name an absolute path.
func validCHeaderName(payload string) bool {
	if payload == "" || strings.HasPrefix(payload, "/") || strings.HasPrefix(payload, "\\") {
		return false
	}
	if len(payload) >= 2 && payload[1] == ':' {
		return false
	}
	for _, component := range strings.FieldsFunc(payload, func(r rune) bool { return r == '/' || r == '\\' }) {
		if component == ".." {
			return false
		}
	}
	return true
}

// exportBlock parses the file's one trailing export list:
// `export name, Type.method, end`.
func (parser *Parser) exportBlock() (ExportBlock, error) {
	keyword := parser.advance()
	if parser.check(lexer.End) {
		parser.advance()
		return ExportBlock{}, parser.errorAt(keyword, "export block requires at least one entry")
	}
	var entries []ExportEntry
	for {
		name, err := parser.consume(lexer.Identifier, "an exported name")
		if err != nil {
			return ExportBlock{}, err
		}
		entry := ExportEntry{Name: name}
		if parser.check(lexer.Dot) {
			parser.advance()
			method, err := parser.consume(lexer.Identifier, "a method name after '.'")
			if err != nil {
				return ExportBlock{}, err
			}
			entry.Method = &method
		}
		entries = append(entries, entry)
		if parser.check(lexer.Comma) {
			parser.advance()
			if parser.check(lexer.End) {
				break
			}
			continue
		}
		break
	}
	end, err := parser.consume(lexer.End, "'end' to close the export block")
	if err != nil {
		return ExportBlock{}, err
	}
	return ExportBlock{Keyword: keyword, Entries: entries, End: end}, nil
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
		start := parser.pendingGreaterToken.Span.Start + 1
		return lexer.Token{
			Kind:   lexer.Greater,
			Lexeme: ">",
			Span:   span.Span{File: parser.pendingGreaterToken.Span.File, Start: start, End: start + 1},
			Line:   parser.pendingGreaterToken.Line,
			Column: parser.pendingGreaterToken.Column + 1,
		}, nil
	}
	if parser.check(kind) {
		return parser.advance(), nil
	}
	return lexer.Token{}, parser.errorAtCurrent("expected " + expected)
}

func (parser *Parser) statement() (Statement, error) {
	switch {
	case parser.atExternBlock():
		// A foreign block is top-level only, and `extern` is reserved at
		// statement start. Reuse the ordering diagnostic so a nested block
		// reports the same error as a late one instead of parsing as an
		// expression.
		return nil, parser.errorAt(parser.peek(), "extern blocks must precede ordinary top-level items")
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
	case parser.check(lexer.Method):
		return nil, parser.errorAt(parser.peek(), "method declarations are module-level only")
	case parser.check(lexer.Type):
		return nil, parser.errorAt(parser.peek(), "type declarations are module-level only")
	case parser.check(lexer.LeftParen):
		// A parenthesized place opens a dereference-first assignment target
		// such as (^pointer).member = value. Anything else starting with
		// '(' is still the split-call error below.
		start := parser.current
		target, err := parser.expression()
		if err == nil && isPlaceExpression(target) && parser.check(lexer.Equal) {
			parser.advance()
			initializer, err := parser.expression()
			if err != nil {
				return nil, err
			}
			return Assignment{Target: target, Initializer: initializer}, nil
		}
		parser.current = start
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
	case parser.check(lexer.Unsafe):
		return parser.unsafeStatement()
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
	case parser.check(lexer.At), parser.check(lexer.Caret):
		// An assignment target may open with an address or dereference
		// operator, as in ^pointer = value. The target grammar is the
		// ordinary unary chain extended with postfix suffixes; the checker
		// owns place validity.
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return parser.postfixStatement(operand)
	case parser.check(lexer.Try):
		// `try <unary-expression>` is a statement as well as an expression,
		// with the same unary boundary as prefix try.
		keyword := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return TryStatement{Keyword: keyword, Operand: operand}, nil
	case parser.check(lexer.Let):
		return parser.letDeclaration(parser.advance())
	}

	if parser.check(lexer.Mut) {
		// `mut` is only valid immediately after `let` in a declaration.
		return nil, parser.errorAt(parser.peek(), "'mut' appears only immediately after 'let' in a declaration")
	}

	name, err := parser.consume(lexer.Identifier, "an identifier")
	if err != nil {
		return nil, err
	}
	if parser.check(lexer.Colon) {
		return parser.deprecatedDeclaration(name)
	}
	return parser.postfixStatement(VariableExpression{Name: name})
}

// postfixStatement completes the two statement forms that start with a postfix
// chain. An assignment target may be a variable or a place such as ^pointer.
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
	return nil, parser.errorAtCurrent("expected '=' for an assignment")
}

// isPlaceExpression reports whether a parsed expression has a place shape
// usable as an assignment target: the checker owns validity, so calls and
// other value-only shapes stay with the split-call error.
func isPlaceExpression(expression Expression) bool {
	switch expression.(type) {
	case VariableExpression, PropertyExpression, IndexExpression,
		AddressExpression, DereferenceExpression:
		return true
	default:
		return false
	}
}

// returnStatement parses `return` with its optional value. The value's first
// token must sit on the return's own line; otherwise the return is bare. When
// the next line begins with a token that can only start an expression, the
// source cannot be read as bare-return-then-statement, so it is reported here
// rather than as a confusing statement-form error. A return parses at module
// scope too: whether it is legal depends on whether the module is the
// entrypoint, which only the checker knows, so the parser represents it and
// leaves the entry-versus-import decision to checking.
func (parser *Parser) returnStatement() (Statement, error) {
	keyword := parser.advance()
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
		lexer.Minus, lexer.Bang, lexer.At, lexer.Caret, lexer.LeftBracket, lexer.StringLiteral,
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
	return compilerTypes.Diagnostic{
		Category: compilerTypes.SyntaxError,
		Stage:    "parser",
		Span:     token.Span,
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
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
	if parser.check(lexer.Type) || parser.check(lexer.Fun) || parser.check(lexer.Method) ||
		parser.check(lexer.Import) || parser.check(lexer.Export) ||
		parser.check(lexer.Let) ||
		parser.check(lexer.If) || parser.check(lexer.While) || parser.check(lexer.For) ||
		parser.check(lexer.Unsafe) ||
		parser.check(lexer.Break) || parser.check(lexer.Continue) || parser.check(lexer.Return) ||
		parser.check(lexer.Self) {
		return true
	}
	if parser.check(lexer.Mut) {
		// A statement-leading `mut` is always the malformed old spelling and
		// is never a valid statement start, so recovery skips it.
		return false
	}
	if !parser.check(lexer.Identifier) {
		return false
	}
	if parser.current+1 >= len(parser.tokens) {
		return false
	}
	next := parser.tokens[parser.current+1].Kind
	if next == lexer.Colon || next == lexer.Equal {
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
