// Package parser turns tokens into a syntax tree using recursive descent.
package parser

import "hexal/compiler/lexer"

// Program is the syntax tree for a complete Hexal source file.
type Program struct {
	Items      []TopLevelItem
	Statements []Statement
}

// TopLevelItem is an ordered source construct. Type declarations remain
// outside Statements because they have no runtime emission.
type TopLevelItem interface {
	topLevelItemNode()
}

// Statement is one top-level executable source construct.
type Statement interface {
	TopLevelItem
	statementNode()
}

// TypeDeclaration names an already-resolvable type expression. The checker
// turns it into a transparent alias without adding an executable statement.
// Exported records an `export` prefix; it is a Hexal-visibility marker only
// and never changes the lowering.
type TypeDeclaration struct {
	Keyword    lexer.Token
	Name       lexer.Token
	Parameters []lexer.Token // generic type parameters; empty when absent
	Target     TypeExpression
	Exported   bool
}

func (TypeDeclaration) topLevelItemNode() {}

// ImportDeclaration binds one local alias to one module path. It is a
// top-level item but never a statement: imports are declarations-only and
// carry no runtime emission of their own.
type ImportDeclaration struct {
	ModuleKeyword lexer.Token
	Alias         lexer.Token
	Equal         lexer.Token
	ImportKeyword lexer.Token
	Path          lexer.Token // the module-path literal, raw quoted spelling
}

func (ImportDeclaration) topLevelItemNode() {}

// ObjectTypeExpression declares an ordered set of named members. It is only
// produced for the direct target of a type declaration; ordinary annotations
// and pointer elements continue to use TypeExpression's existing forms.
type ObjectTypeExpression struct {
	OpenBrace  lexer.Token
	Members    []ObjectMemberDeclaration
	CloseBrace lexer.Token
}

func (ObjectTypeExpression) typeExpressionNode() {}

// AdtVariantDeclaration is one variant of an ADT definition. Payload is nil
// for a unit variant.
type AdtVariantDeclaration struct {
	Name    lexer.Token
	Payload *ObjectTypeExpression
}

// AdtDefinitionExpression is the right-hand side of an ADT type declaration.
type AdtDefinitionExpression struct {
	Variants []AdtVariantDeclaration
}

func (AdtDefinitionExpression) typeExpressionNode() {}

// ObjectMemberDeclaration is one member in an object type declaration.
type ObjectMemberDeclaration struct {
	Name    lexer.Token
	Mutable bool
	Type    TypeExpression
}

// Declaration binds a name to an initializer and records its declaration operator.
type Declaration struct {
	Name        lexer.Token
	Mutable     bool
	Type        TypeExpression
	Initializer Expression
	// Operator is the := token for both typed and inferred declarations. Type
	// being nil is the sole marker for the inferred form.
	Operator lexer.Token
}

func (Declaration) topLevelItemNode() {}
func (Declaration) statementNode()    {}

// Assignment updates an existing variable without repeating its type.
type Assignment struct {
	Name        lexer.Token // The first identifier, retained for diagnostics.
	Target      Expression
	Initializer Expression
}

func (Assignment) topLevelItemNode() {}
func (Assignment) statementNode()    {}

// FunctionDeclaration is a named module-level function; there are no nested
// functions, so this is a top-level item and never a statement. Exported
// records an `export` prefix.
type FunctionDeclaration struct {
	Keyword         lexer.Token
	Name            lexer.Token
	TypeParameters  []lexer.Token // generic type parameters; empty when absent
	Parameters      []Parameter
	Return          TypeExpression // nil when the function returns no value.
	Body            []Statement
	End             lexer.Token
	HasSyntaxErrors bool
	Exported        bool
}

func (FunctionDeclaration) topLevelItemNode() {}

// AnonymousFunctionLiteral is a non-capturing function value.
// It has no source name and cannot use `export`.
type AnonymousFunctionLiteral struct {
	FunKeyword      lexer.Token
	TypeParameters  []lexer.Token
	Parameters      []Parameter
	Return          TypeExpression
	Body            []Statement
	End             lexer.Token
	HasSyntaxErrors bool
}

func (AnonymousFunctionLiteral) expressionNode() {}

// ImplDeclaration is a method attached to a receiver type. SelfType keeps the
// written receiver form (Point, Ptr<Point>, MutPtr<Point>) unresolved; the
// checker decides whether it names a nominal object type. Exported records an
// `export` prefix.
type ImplDeclaration struct {
	Keyword         lexer.Token
	SelfType        TypeExpression
	Name            lexer.Token
	TypeParameters  []lexer.Token // the method's own generic parameters
	Parameters      []Parameter
	Return          TypeExpression // nil when the method returns no value.
	Body            []Statement
	End             lexer.Token
	HasSyntaxErrors bool
	Exported        bool
}

func (ImplDeclaration) topLevelItemNode() {}

// Parameter is one annotated function or method parameter. Annotations are
// mandatory, so there is no inferred form.
type Parameter struct {
	Name lexer.Token
	Type TypeExpression
}

// ReturnStatement leaves a function body, with or without a value. A bare
// return has a nil Value.
type ReturnStatement struct {
	Keyword lexer.Token
	Value   Expression
}

func (ReturnStatement) topLevelItemNode() {}
func (ReturnStatement) statementNode()    {}

// IfStatement is one complete conditional chain. Each body owns a lexical
// scope; the checker preserves the branch order for short-circuit execution.
type IfStatement struct {
	Keyword     lexer.Token
	Condition   Expression
	Then        []Statement
	ElseIf      []ElseIfClause
	Else        []Statement
	ElseKeyword lexer.Token
	End         lexer.Token
}

func (IfStatement) topLevelItemNode() {}
func (IfStatement) statementNode()    {}

// ElseIfClause stores one source-ordered conditional branch.
type ElseIfClause struct {
	Keyword   lexer.Token
	Condition Expression
	Body      []Statement
}

// WhileStatement is a pre-test loop with one lexical body scope. `do` is the
// mandatory delimiter between the condition and the body.
type WhileStatement struct {
	Keyword   lexer.Token
	Condition Expression
	Body      []Statement
	End       lexer.Token
}

func (WhileStatement) topLevelItemNode() {}
func (WhileStatement) statementNode()    {}

// TryStatement discards the success value of a `try` operand: the operand
// propagates Error from the enclosing function exactly like a try expression,
// and the normalized success value is unused.
type TryStatement struct {
	Keyword lexer.Token
	Operand Expression
}

func (TryStatement) topLevelItemNode() {}
func (TryStatement) statementNode()    {}

// ForStatement iterates one built-in collection or text source. Binders are
// fresh immutable names in a fresh body scope; the first binder is always the
// optional Size index.
type ForStatement struct {
	Keyword lexer.Token
	Binders []lexer.Token // 1, 2, or 3 names in written order
	Source  Expression
	Body    []Statement
	End     lexer.Token
}

func (ForStatement) topLevelItemNode() {}
func (ForStatement) statementNode()    {}

// BreakStatement exits the nearest while loop.
type BreakStatement struct {
	Keyword lexer.Token
}

func (BreakStatement) topLevelItemNode() {}
func (BreakStatement) statementNode()    {}

// ContinueStatement skips to the nearest while condition.
type ContinueStatement struct {
	Keyword lexer.Token
}

func (ContinueStatement) topLevelItemNode() {}
func (ContinueStatement) statementNode()    {}

// DeferStatement registers an expression for evaluation when the current
// lexical scope exits. Direct calls capture their arguments at registration.
type DeferStatement struct {
	Keyword    lexer.Token
	Expression Expression
}

func (DeferStatement) topLevelItemNode() {}
func (DeferStatement) statementNode()    {}

// ErrdeferStatement registers one cleanup action that runs only when the
// current function exits by returning Error.
type ErrdeferStatement struct {
	Keyword    lexer.Token
	Expression Expression
}

func (ErrdeferStatement) topLevelItemNode() {}
func (ErrdeferStatement) statementNode()    {}

// TryExpression is the prefix `try` form: the operand must produce a union
// containing Error, which `try` returns from the enclosing function while
// yielding the active success value otherwise.
type TryExpression struct {
	Keyword lexer.Token
	Operand Expression
}

func (TryExpression) expressionNode() {}

// SpawnExpression is the prefix `spawn` form: the operand must be a direct
// call to a named function whose execution becomes a new Task.
type SpawnExpression struct {
	Keyword lexer.Token
	Operand Expression
}

func (SpawnExpression) expressionNode() {}

// Expression is a syntax-tree expression node.
type Expression interface {
	expressionNode()
}

// IntegerLiteral is an integer expression before semantic resolution. The
// token kind identifies its source radix.
type IntegerLiteral struct {
	Token lexer.Token
}

func (IntegerLiteral) expressionNode() {}

// DecimalLiteral is a decimal floating-point expression before semantic
// resolution. The checker selects Float32 or Float64 from its context.
type DecimalLiteral struct {
	Token lexer.Token
}

func (DecimalLiteral) expressionNode() {}

// BooleanLiteral is a true or false expression, carrying its value as the
// token's lexeme.
type BooleanLiteral struct {
	Token lexer.Token
}

func (BooleanLiteral) expressionNode() {}

// NilLiteral is the singleton nil value before semantic resolution.
type NilLiteral struct {
	Token lexer.Token
}

func (NilLiteral) expressionNode() {}

// EosLiteral is the end-of-stream singleton value `eos` before semantic
// resolution.
type EosLiteral struct {
	Token lexer.Token
}

func (EosLiteral) expressionNode() {}

// StringLiteral is a double-quoted string literal before semantic
// resolution. The token lexeme includes the surrounding quotes; the checker
// decodes escapes and records provenance.
type StringLiteral struct {
	Token lexer.Token
}

func (StringLiteral) expressionNode() {}

// ByteLiteral is a single-quoted b'...' literal carrying exactly one byte.
// The lexer validated the escape grammar and cardinality; the checker decodes
// the payload value.
type ByteLiteral struct {
	Token lexer.Token
}

func (ByteLiteral) expressionNode() {}

// RuneLiteral is a single-quoted '...' literal carrying exactly one Unicode
// scalar. The lexer validated the escape grammar and scalar validity; the
// checker decodes the payload value.
type RuneLiteral struct {
	Token lexer.Token
}

func (RuneLiteral) expressionNode() {}

// VariableExpression names a declared variable. The checker resolves its
// type and binding mode.
type VariableExpression struct {
	Name lexer.Token
}

func (VariableExpression) expressionNode() {}

// PropertyExpression is a dotted member selection. Keeping the receiver as a
// tree preserves left-to-right postfix evaluation for chains such as
// pp.value.value and point.x.y; the checker resolves each name.
type PropertyExpression struct {
	Receiver Expression
	Property lexer.Token
}

func (PropertyExpression) expressionNode() {}

// ArrayLiteralExpression is a bracket list of element expressions. The
// checker derives the array length from the element count and types the
// elements from the first one.
type ArrayLiteralExpression struct {
	OpenBracket  lexer.Token
	Elements     []Expression
	CloseBracket lexer.Token
}

func (ArrayLiteralExpression) expressionNode() {}

// IndexExpression selects one element of an array by an index expression.
type IndexExpression struct {
	Receiver     Expression
	OpenBracket  lexer.Token
	Index        Expression
	CloseBracket lexer.Token
}

func (IndexExpression) expressionNode() {}

// CallExpression applies a callee to an argument list. A postfix chain whose
// final operation is a call is also a statement; a chain ending in member
// selection is an expression only, so those markers live here.
type CallExpression struct {
	Callee        Expression
	OpenParen     lexer.Token
	Arguments     []Expression
	TypeArguments []TypeExpression // explicit generic arguments; empty when absent
}

func (CallExpression) expressionNode()   {}
func (CallExpression) topLevelItemNode() {}
func (CallExpression) statementNode()    {}

// UnaryExpression applies a prefix operator to one operand. The parser keeps
// the operator token so later phases can resolve its semantic meaning.
type UnaryExpression struct {
	Operator lexer.Token
	Operand  Expression
}

func (UnaryExpression) expressionNode() {}

// BinaryExpression combines two operands with an infix operator. Nested nodes
// retain the tree shape written explicit grouping and left associativity
// produce; grouping-only parentheses select this shape and are otherwise
// discarded, never appearing as a node of their own.
type BinaryExpression struct {
	Left     Expression
	Operator lexer.Token
	Right    Expression
}

func (BinaryExpression) expressionNode() {}

// TypeTestExpression asks whether one runtime union member is active. The
// checker resolves Type and enforces that it names one exact member of the
// operand's union.
type TypeTestExpression struct {
	Operand Expression
	IsToken lexer.Token
	Type    TypeExpression
}

func (TypeTestExpression) expressionNode() {}

// QualifiedVariantExpression constructs an ADT variant with a payload
// initializer. Unit variants parse as PropertyExpression and are resolved by
// the checker. OwnerArguments are explicit generic arguments for a generic
// owner; empty when absent.
type QualifiedVariantExpression struct {
	Owner          lexer.Token
	OwnerArguments []TypeExpression
	Variant        lexer.Token
	Payload        *[]MemberInitializer
}

func (QualifiedVariantExpression) expressionNode() {}

// MatchPattern is one arm pattern of a match expression.
type MatchPattern interface {
	matchPatternNode()
}

// BoolPattern matches a Boolean literal in value mode.
type BoolPattern struct {
	Token lexer.Token
}

func (BoolPattern) matchPatternNode() {}

// ElsePattern is the final default arm.
type ElsePattern struct {
	Token lexer.Token
}

func (ElsePattern) matchPatternNode() {}

// TypePattern matches one exact canonical type in type mode.
type TypePattern struct {
	Type TypeExpression
}

func (TypePattern) matchPatternNode() {}

// VariantPattern matches one qualified ADT variant in type mode.
// OwnerArguments are explicit generic arguments for a generic owner.
type VariantPattern struct {
	Owner          lexer.Token
	OwnerArguments []TypeExpression
	Variant        lexer.Token
}

func (VariantPattern) matchPatternNode() {}

// MatchArm is one `| pattern then expression` arm.
type MatchArm struct {
	Pipe       lexer.Token
	Pattern    MatchPattern
	Then       lexer.Token
	Expression Expression
}

// MatchExpression evaluates its scrutinee once and selects one arm. TypeMode
// selects exact type and variant patterns.
type MatchExpression struct {
	Keyword   lexer.Token
	Scrutinee Expression
	TypeMode  bool
	Arms      []MatchArm
	End       lexer.Token
}

func (MatchExpression) expressionNode() {}

// ObjectLiteral constructs a named object value. Initializers retain written
// order; the checker later validates names and determines declaration order.
type ObjectLiteral struct {
	TypeName      lexer.Token
	OpenBrace     lexer.Token
	Initializers  []MemberInitializer
	CloseBrace    lexer.Token
	TypeArguments []TypeExpression // explicit generic arguments; empty when absent
}

func (ObjectLiteral) expressionNode() {}

// MemberInitializer assigns one expression to a named object member.
type MemberInitializer struct {
	Name  lexer.Token
	Equal lexer.Token
	Value Expression
}

// RefExpression takes the address of a syntactic place. It maps directly to
// C's address-of operator; the pointer type is chosen by the checker.
type RefExpression struct {
	Keyword lexer.Token
	Place   Expression
}

func (RefExpression) expressionNode() {}

// NegatedNumericLiteral preserves the exact literal path required for signed
// minima. General unary minus uses UnaryExpression.
type NegatedNumericLiteral struct {
	Minus   lexer.Token
	Literal Expression
}

func (NegatedNumericLiteral) expressionNode() {}
