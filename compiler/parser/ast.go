// Package parser turns tokens into a syntax tree using recursive descent.
package parser

import "hexal/compiler/lexer"

// Program is the syntax tree for a complete Hexal source file. Import and
// Export are structurally singular and positional (the file's first and last
// top-level constructs respectively), so they are dedicated optional fields
// rather than ordinary TopLevelItems.
type Program struct {
	Import *ImportBlock
	// Externs are the optional leading foreign blocks, after the import block
	// and before every ordinary top-level item. They are a dedicated field
	// rather than ordinary TopLevelItems because their position is part of the
	// grammar.
	Externs    []ExternBlock
	Items      []TopLevelItem
	Statements []Statement
	Export     *ExportBlock
}

// ExternBlock is one `extern c from <header> do ... end` foreign block: a
// handwritten binding for the requested header. Declarations are private
// unless the module's final export block names them.
type ExternBlock struct {
	Keyword      lexer.Token
	C            lexer.Token
	From         lexer.Token
	Header       ImportReference
	Do           lexer.Token
	Declarations []ExternDeclaration
	End          lexer.Token
}

// ExternDeclaration is one declaration inside a foreign block: a foreign type,
// function, constant, or global.
type ExternDeclaration interface{ externDeclarationNode() }

// ExternMember is one field of a complete foreign struct.
type ExternMember struct {
	Mutable bool
	Name    lexer.Token
	CName   *lexer.Token // optional `as "field_name"`
	Type    TypeExpression
}

// ExternType declares one foreign type: a transparent Hexal alias, an opaque
// incomplete C type, or a complete foreign struct. CName optionally carries
// the exact C typedef or tag spelling.
type ExternType struct {
	Keyword lexer.Token
	Name    lexer.Token
	CName   *lexer.Token
	// Exactly one of Alias, Opaque, or Members is set.
	Alias   TypeExpression
	Opaque  bool
	Members []ExternMember
	End     lexer.Token
}

func (ExternType) externDeclarationNode() {}

// ExternParameter is one foreign function parameter, with an optional exact C
// type spelling.
type ExternParameter struct {
	Name  lexer.Token
	Type  TypeExpression
	CType *lexer.Token
}

// ExternFunction declares one foreign function. CName optionally carries the
// exact C symbol, and ResultCType the result's exact C spelling.
type ExternFunction struct {
	Keyword     lexer.Token
	Name        lexer.Token
	CName       *lexer.Token
	Parameters  []ExternParameter
	Result      TypeExpression
	ResultCType *lexer.Token
}

func (ExternFunction) externDeclarationNode() {}

// ExternConstant declares one foreign constant: a typed, non-addressable C
// expression whose spelling is an enumerator or object-like macro. Its type is
// a scalar, a data pointer, or a complete foreign record.
type ExternConstant struct {
	Keyword lexer.Token
	Name    lexer.Token
	CName   *lexer.Token
	Type    TypeExpression
}

func (ExternConstant) externDeclarationNode() {}

// ExternGlobal declares one foreign global. Mutable renders reads and writes;
// a fixed global permits reads only.
type ExternGlobal struct {
	Keyword lexer.Token
	Mutable bool
	Name    lexer.Token
	CName   *lexer.Token
	Type    TypeExpression
}

func (ExternGlobal) externDeclarationNode() {}

// ImportBlock is the file's one leading import list, when present.
type ImportBlock struct {
	Keyword lexer.Token
	Entries []ImportEntry
	End     lexer.Token
}

// ImportReferenceKind separates a supplied source-map path from a
// compiler-owned standard-library reference. The resolver, not a string
// prefix, decides which tables to consult.
type ImportReferenceKind uint8

const (
	// RelativeImportReference is a quoted "./" or "../" source-map path.
	RelativeImportReference ImportReferenceKind = iota
	// StandardLibraryImportReference is a dotted std.<component> reference.
	StandardLibraryImportReference
	// CHeaderImportReference is a `c <header>` or `c "header"` reference to an
	// automatically prepared C binding module.
	CHeaderImportReference
)

// ImportReference is one tagged module reference after `from`. Token is the
// opening quote for a relative reference and the `std` token for a dotted one;
// both anchor diagnostics. DisplaySpelling is the normalized source form the
// user wrote (the quoted payload, or the dotted spelling). Components carries
// the dotted components after `std.` and is empty for a relative reference.
// RelativePath is the raw quoted token and is the zero token for a dotted one.
type ImportReference struct {
	Kind            ImportReferenceKind
	Token           lexer.Token
	DisplaySpelling string
	Components      []lexer.Token
	RelativePath    lexer.Token
	// CHeader is the requested header's payload without delimiters; System is
	// true for the `<...>` form and false for the quoted form. Both name the
	// same prepared-binding identity only when the form and payload agree.
	CHeader string
	System  bool
}

// ImportEntry binds one local alias to one module reference:
// `alias from "./path"` or `alias from std.<component>...`. From is the
// contextual `from` token itself, retained for diagnostics.
type ImportEntry struct {
	Alias     lexer.Token
	From      lexer.Token
	Reference ImportReference
}

// ExportBlock is the file's one trailing export list, when present.
type ExportBlock struct {
	Keyword lexer.Token
	Entries []ExportEntry
	End     lexer.Token
}

// ExportEntry names one exported declaration or module value. Method is
// non-nil for a qualified `Type.method` entry, which names one method of the
// locally declared type Name; it is never an imported-name path.
type ExportEntry struct {
	Name   lexer.Token
	Method *lexer.Token
}

// ModuleValueDeclaration is a top-level `static [mut] name [: type] := expr`
// module value: program-lifetime storage initialized directly to C static
// storage, never an executable local. It is distinct from Declaration, which
// remains the entrypoint's ordinary executable-local spelling.
type ModuleValueDeclaration struct {
	Keyword     lexer.Token
	Mutable     bool
	Name        lexer.Token
	Type        TypeExpression // nil for the inferred form
	Initializer Expression
	Operator    lexer.Token
}

func (ModuleValueDeclaration) topLevelItemNode() {}

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
// Export visibility is resolved separately against the file's trailing
// export block; it is a Hexal-visibility marker only and never changes the
// lowering.
type TypeDeclaration struct {
	Keyword    lexer.Token
	Name       lexer.Token
	Parameters []lexer.Token // generic type parameters; empty when absent
	Target     TypeExpression
	Exported   bool
}

func (TypeDeclaration) topLevelItemNode() {}

// ObjectTypeExpression declares an ordered set of named members. It is only
// produced for a struct definition's body or an ADT payload's body; ordinary
// annotations and pointer elements continue to use TypeExpression's existing
// forms. Keyword is the introducing token (`struct` or `as`); End is the
// closing `end`.
type ObjectTypeExpression struct {
	Keyword lexer.Token
	Members []ObjectMemberDeclaration
	End     lexer.Token
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

// MethodDeclaration is a method attached to a receiver type. SelfType keeps
// the written receiver form unresolved; the checker decides whether it names a
// local nominal struct, which is the only valid receiver. Exported records an
// `export` prefix.
type MethodDeclaration struct {
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

func (MethodDeclaration) topLevelItemNode() {}

// Parameter is one annotated function or method parameter. Annotations are
// mandatory, so there is no inferred form. Rest marks a final `T...` rest
// parameter; Ellipsis carries its token for diagnostics.
type Parameter struct {
	Name     lexer.Token
	Type     TypeExpression
	Rest     bool
	Ellipsis lexer.Token
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

// UnsafeStatement is one lexical region granting permission for operations
// whose preconditions the compiler cannot prove. It is a plain block: parsing,
// typing, and every ordinary check still apply to its contents.
type UnsafeStatement struct {
	Keyword lexer.Token
	Body    []Statement
	End     lexer.Token
}

func (UnsafeStatement) topLevelItemNode() {}
func (UnsafeStatement) statementNode()    {}

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

// RawStringLiteral is an r"..." / r#"..."# literal before semantic
// resolution. The token lexeme includes the 'r', its hash delimiters, and
// the surrounding quotes; the checker strips them and validates UTF-8. Raw
// content performs no escape or interpolation processing.
type RawStringLiteral struct {
	Token lexer.Token
}

func (RawStringLiteral) expressionNode() {}

// InterpolationSegment is one ordered piece of an interpolation template.
// Exactly one of Text or Expression is set: a literal-text segment carries
// its raw (still-escaped) source token in Text, and an embedded-expression
// segment carries its parsed expression plus the "{{"/"}}" tokens that
// delimit it.
type InterpolationSegment struct {
	Text       *lexer.Token
	Expression Expression
	Open       lexer.Token
	Close      lexer.Token
}

// InterpolationTemplateExpression is the parsed contents of an interpreted
// string the lexer found to contain interpolation: alternating literal-text
// segments and embedded expressions, in source order. This is contextual
// syntax, valid only as the second argument of String.interpolate; the
// checker rejects it in every other expression or call position.
type InterpolationTemplateExpression struct {
	Start    lexer.Token
	Segments []InterpolationSegment
	End      lexer.Token
}

func (InterpolationTemplateExpression) expressionNode() {}

// VariableExpression names a declared variable. The checker resolves its
// type and binding mode.
type VariableExpression struct {
	Name lexer.Token
}

func (VariableExpression) expressionNode() {}

// PropertyExpression is a dotted member selection. Keeping the receiver as a
// tree preserves left-to-right postfix evaluation for chains such as
// holder.value.count and point.x.y; the checker resolves each name.
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
//
// ArgumentLabels carries each argument's optional `identifier =` label,
// parallel to Arguments by index; an entry is nil for a plain positional
// argument. Labels are meaningful only once the callee resolves: a struct or
// ADT-variant constructor requires one on every argument, while every other
// callee rejects one on any argument. Keeping Arguments unlabeled-shaped
// leaves every existing positional consumer unchanged.
type CallExpression struct {
	Callee         Expression
	OpenParen      lexer.Token
	Arguments      []Expression
	ArgumentLabels []*lexer.Token
	TypeArguments  []TypeExpression // explicit generic arguments; empty when absent
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
// OwnerArguments are explicit generic arguments for a generic owner; only
// the explicit generic spelling preclassifies as a variant, since a module
// alias never takes type arguments.
type VariantPattern struct {
	Owner          lexer.Token
	OwnerArguments []TypeExpression
	Variant        lexer.Token
}

func (VariantPattern) matchPatternNode() {}

// DottedPattern is a syntactically neutral `Owner.Name` or
// `Owner.Adt.Name` match arm. The checker classifies it as an ADT variant or a
// qualified type from the scrutinee domain and module context; the parser
// assigns no meaning. Member is the zero token for the two-part form and the
// variant name for the three-part `Alias.Adt.Variant` form.
type DottedPattern struct {
	Owner  lexer.Token
	Name   lexer.Token
	Member lexer.Token
}

func (DottedPattern) matchPatternNode() {}

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

// AddressExpression takes the address of a syntactic place with the prefix
// `@` operator. It maps directly to C's address-of operator; the pointer
// type is chosen by the checker.
type AddressExpression struct {
	Operator lexer.Token
	Place    Expression
}

func (AddressExpression) expressionNode() {}

// DereferenceExpression reads or writes a pointer's pointee with the prefix
// `^` operator. The checker decides writability from the operand's pointer
// type; the same token stays binary XOR in infix position.
type DereferenceExpression struct {
	Operator lexer.Token
	Operand  Expression
}

func (DereferenceExpression) expressionNode() {}

// NegatedNumericLiteral preserves the exact literal path required for signed
// minima. General unary minus uses UnaryExpression.
type NegatedNumericLiteral struct {
	Minus   lexer.Token
	Literal Expression
}

func (NegatedNumericLiteral) expressionNode() {}
