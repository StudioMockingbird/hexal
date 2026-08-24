package checker

import (
	"go/constant"

	compilerTypes "hexal/compiler/types"
)

// BindingID is a compilation-scoped identity for one value declaration or
// parameter. Source names are insufficient once sibling blocks shadow one
// another, so checked references carry this identity into generation.
type BindingID uint64

// OperandKind distinguishes checked constants, variables, objects, and
// structured expressions. Keeping the tag explicit prevents generation from
// guessing from spelling.
type OperandKind uint8

// The concrete OperandKind values. Each name is self-evident given
// OperandKind's own contract; InvalidOperand is the unset zero value, never a
// checked operand's real kind.
const (
	InvalidOperand OperandKind = iota
	ConstantOperand
	VariableOperand
	ObjectOperand
	ExpressionOperand
)

// ExpressionKind is the small checked expression language understood by the
// generator. Source identifiers stay as source spellings here; C lowering is
// deliberately owned by the generator.
type ExpressionKind uint8

// The concrete ExpressionKind values. InvalidExpression is the unset zero
// value; the generator's render and validation dispatchers explicitly own
// every other kind but never this one. A kind whose carried fields are
// self-evident from its name and Expression's own field docs carries no
// further comment; every kind with a non-obvious contract, invariant, or
// field convention documents it individually below.
const (
	InvalidExpression ExpressionKind = iota
	VariableExpression
	AddressOfExpression
	DereferenceExpression
	MemberExpression
	ObjectExpression
	ConstantExpression
	UnaryOperationExpression
	BinaryOperationExpression
	// FunctionReferenceExpression is a declared function's name used as a
	// Fun<...> value. It is not a variable read: no storage exists for it.
	FunctionReferenceExpression
	// CallExpression applies Operand to Arguments. ResultType is the zero Type
	// when the callee returns no value, which only a call statement accepts.
	CallExpression
	// MethodCallExpression calls the method Name declared on Owner. Operand is
	// the receiver already adapted to the method's target form, so the checked
	// tree carries any dereference or address-of the adaptation inserted and
	// the generator only spells the call.
	MethodCallExpression
	// NilExpression is the singleton Nil literal. It carries no go/constant
	// value: Nil has exactly one value, which the generator lowers directly.
	NilExpression
	// EosExpression is the end-of-stream singleton literal. Like
	// NilExpression it carries no go/constant: EoS has exactly one value.
	EosExpression
	// NullTestExpression tests a nullable operand against Nil with == or !=
	// and yields Bool. The checked node is normalized so Operand always holds
	// the nullable side, which makes nil == maybe share the shape of
	// maybe == nil. OperandType is the nullable union type.
	NullTestExpression
	// UnionInjectionExpression constructs one member of a tagged or nullable
	// union without source-level constructor syntax.
	UnionInjectionExpression
	// UnionWidenExpression converts a source union to a wider destination union.
	UnionWidenExpression
	// UnionTestExpression asks whether one exact union member is active.
	UnionTestExpression
	// UnionPayloadExpression reads a member after flow narrowing proved its tag.
	UnionPayloadExpression
	// UnionEqualityExpression compares two identical canonical union values.
	UnionEqualityExpression
	// HeapAllocateExpression allocates one T from a Heap and initializes it.
	HeapAllocateExpression
	// HeapFreeExpression releases a Heap allocation identified by a pointer.
	HeapFreeExpression
	// AdtConstructExpression constructs one variant of a nominal ADT.
	AdtConstructExpression
	// AdtPayloadExpression reads one payload field after a tag proof.
	AdtPayloadExpression
	// MatchExpression evaluates a scrutinee once and selects one arm.
	MatchExpression
	// ArrayLiteralExpression constructs one fixed inline Array<T, N> value.
	// OperandType is the element type; Arguments holds the elements.
	ArrayLiteralExpression
	// IndexExpression reads or writes one element of an Array<T, N>. Operand
	// is the array place, Arguments holds the single index operand, and
	// OperandType is the array type.
	IndexExpression
	// CollectionMethodCallExpression is one built-in Array or View method:
	// length or slice. Name selects the operation; Element is the element
	// type.
	CollectionMethodCallExpression
	// CollectionSliceExpression builds a View<T> from an Array or View
	// receiver. OperandType is the receiver type, Arguments holds the two
	// index operands, and ViewRoots records the receiver's root chain for
	// lexical lifetime checking.
	CollectionSliceExpression
	// StringLiteralExpression is a static-provenance String literal; Name
	// carries the decoded payload bytes.
	StringLiteralExpression
	// StringMethodCallExpression is one built-in String method: length,
	// bytes, slice, rune_cursor, to_string, concat, or free. Name
	// selects the operation; Element
	// is the byte view element type for bytes and slice.
	StringMethodCallExpression
	// StringFromBytesExpression constructs a fresh owning String by copying
	// a View<Byte> payload through a Heap.
	StringFromBytesExpression
	// StringFromRunesExpression constructs a fresh owning String by
	// validating and encoding a View<Rune> payload through a Heap.
	StringFromRunesExpression
	// ListNewExpression constructs a fresh owning List<T> header through a
	// Heap; Element is T.
	ListNewExpression
	// DictNewExpression constructs a fresh owning Dict<K, V> header through
	// a Heap; Element is V.
	DictNewExpression
	// BitCastExpression reinterprets the receiver's exact representation
	// bits as the same-width destination scalar.
	BitCastExpression
	// EndianConversionExpression converts a fixed-width integer to or from
	// its explicit-endian byte sequence. Name is "to" or "from";
	// MemberIndex is 0 for little endian and 1 for big endian; Element is
	// the integer type; Operand is the receiver (or the type marker for
	// from) and Arguments carries the bytes for from.
	EndianConversionExpression
	// TryExpression propagates an Error from the enclosing function.
	// OperandType is the source union; MemberIndex is the Error
	// member's index; ResultType is the normalized success value or union;
	// Element is the enclosing function's declared result type.
	TryExpression
	// PrintExpression is one checked print call; it produces no value and
	// carries the ordered argument operands.
	PrintExpression
	// DeepEqualityExpression compares two non-scalar values through the
	// per-type equality helper. OperandType is the compared type; Left and
	// Right hold the operands; Operator is Equal or NotEqual.
	DeepEqualityExpression
	// StringCompareExpression applies an ordering operator to two String or
	// Strand values through the per-type bytewise compare helper.
	StringCompareExpression
	// WideningExpression casts an operand to a proven lossless numeric
	// common type; OperandType is the source type and ResultType the
	// destination.
	WideningExpression
	// ConversionExpression applies a built-in destination-named numeric
	// conversion method; OperandType is the source type, ResultType the
	// destination, and MemberIndex the conversion mode.
	ConversionExpression
	// SpawnExpression starts one new Task<R> running a named function.
	// Operand is the checked call node; OperandType is the Task
	// handle type; ResultType is Task<R> | Error; Element is R.
	SpawnExpression
	// TaskYieldExpression is the Task.yield() intrinsic: a hint that parks
	// the current task so other tasks may run.
	TaskYieldExpression
	// TaskMethodCallExpression is one Task handle method: join (yields R) or
	// detach (yields Nil). Operand is the handle; OperandType is the Task
	// type; Element is R.
	TaskMethodCallExpression
	// ChannelConstructorExpression is Channel<T>.new(heap, capacity), which
	// yields Channel<T> | Error. Arguments holds heap and capacity;
	// OperandType is the Channel type; Element is T.
	ChannelConstructorExpression
	// ChannelMethodCallExpression is one Channel handle method: send,
	// receive, close, length, capacity, is_closed, or free. Operand is the
	// handle; OperandType is the Channel type; Element is T.
	ChannelMethodCallExpression
	// MutexConstructorExpression is Mutex.new(heap), which yields Mutex |
	// Error. Arguments holds the heap.
	MutexConstructorExpression
	// MutexMethodCallExpression is one Mutex handle method: lock, unlock, or
	// free. Operand is the handle.
	MutexMethodCallExpression
	// AtomicConstructorExpression is Atomic<T>.new(initial), which yields an
	// inline Atomic<T>. Arguments holds the initial value; OperandType is the
	// Atomic type; Element is T.
	AtomicConstructorExpression
	// AtomicMethodCallExpression is one Atomic method: load, store, exchange,
	// fetch_add, fetch_sub, or compare_exchange. Operand is the Atomic
	// lvalue; OperandType is the Atomic type; Element is T.
	AtomicMethodCallExpression
	// LayoutExpression is size_of<T>() or align_of<T>(). Name
	// selects the query; OperandType is the measured type; ResultType is
	// Size.
	LayoutExpression
	// VolatileReadExpression reads one integer through a volatile-qualified
	// pointer. Operand is the Ptr or MutPtr receiver; OperandType
	// is the pointer type; Element is the integer element.
	VolatileReadExpression
	// VolatileWriteExpression writes one integer through a
	// volatile-qualified MutPtr. Operand is the receiver;
	// Arguments holds the written value; OperandType is the pointer type;
	// Element is the integer element.
	VolatileWriteExpression
	// ViewBridgeExpression is View<T>.from_pointer(pointer, length) or
	// View<T>.empty(). Name selects the form; Arguments holds the
	// pointer and length for from_pointer; OperandType is the View type;
	// Element is T.
	ViewBridgeExpression
	// RuneCursorMethodCallExpression is one RuneCursor method: has_next or
	// next. Operand is the cursor descriptor; OperandType is the
	// RuneCursor type.
	RuneCursorMethodCallExpression
	// StreamConstructorExpression is one standard-handle constructor:
	// IO.stdin(), IO.stdout(), or IO.stderr(). Name selects the handle;
	// OperandType is IO; ResultType is IO | Error. The capability the
	// result provably carries is recovered from Name at fact-seeding time.
	StreamConstructorExpression
	// BytesOverExpression is Bytes.over(buffer), which borrows a
	// List<Byte>. Arguments holds the list; OperandType is the List type;
	// ResultType is Bytes.
	BytesOverExpression
	// StreamMethodCallExpression is one byte-stream operation: read, write,
	// seek, or close. Operand is the receiver already adapted to its C form:
	// an IO value for IO methods, a MutPtr<Bytes> value for Bytes methods.
	// OperandType is that adapted receiver type; ResultType is the
	// operation's structural result union.
	StreamMethodCallExpression
	// FunctionLiteralExpression is a non-capturing anonymous function value
	// checked in expression position: stored, passed, returned, or invoked
	// directly. Function carries its checked signature and body; ResultType
	// is its exact Fun<...> type. It never carries a recursion name: a
	// literal that is the direct initializer of a fixed inferred binding is
	// declaration sugar and is checked as the equivalent named function
	// declaration instead, never as this expression kind.
	FunctionLiteralExpression
)

// Operator is the resolved semantic operator carried by a checked operation.
// It deliberately contains no lexer token or generated C spelling.
type Operator uint8

// The concrete Operator values: unary forms first, then binary. Grouping is
// documentation convenience only; declaration order carries no semantic
// weight. InvalidOperator is the unset zero value.
const (
	InvalidOperator Operator = iota
	NegateOperator
	LogicalNotOperator
	BitwiseNotOperator
	AddOperator
	SubtractOperator
	MultiplyOperator
	DivideOperator
	RemainderOperator
	BitwiseAndOperator
	BitwiseXorOperator
	BitwiseOrOperator
	ShiftLeftOperator
	ShiftRightOperator
	EqualOperator
	NotEqualOperator
	LessOperator
	LessEqualOperator
	GreaterOperator
	GreaterEqualOperator
	LogicalAndOperator
	LogicalOrOperator
)

// String returns the Hexal spelling of a resolved operator.
func (operator Operator) String() string {
	switch operator {
	case NegateOperator:
		return "-"
	case LogicalNotOperator:
		return "!"
	case AddOperator:
		return "+"
	case SubtractOperator:
		return "-"
	case MultiplyOperator:
		return "*"
	case BitwiseAndOperator:
		return "&"
	case BitwiseXorOperator:
		return "^"
	case BitwiseOrOperator:
		return "|"
	case ShiftLeftOperator:
		return "<<"
	case ShiftRightOperator:
		return ">>"
	case BitwiseNotOperator:
		return "~"
	case DivideOperator:
		return "/"
	case RemainderOperator:
		return "%"
	case EqualOperator:
		return "=="
	case NotEqualOperator:
		return "!="
	case LessOperator:
		return "<"
	case LessEqualOperator:
		return "<="
	case GreaterOperator:
		return ">"
	case GreaterEqualOperator:
		return ">="
	case LogicalAndOperator:
		return "and"
	case LogicalOrOperator:
		return "or"
	default:
		return "invalid operator"
	}
}

// Expression is a structured checked expression. Composite C fragments are
// never stored here, which keeps naming and C syntax in one backend.
type Expression struct {
	Kind    ExpressionKind
	Name    string
	Binding BindingID
	// CollectionRoot identifies the copied List or Dict state represented by
	// this expression. Zero means the expression is not a tracked collection
	// place or its identity cannot be established statically.
	CollectionRoot BindingID
	Member         *compilerTypes.ObjectMember
	Operand        *Expression
	Left           *Expression
	Right          *Expression
	Object         *ObjectValue
	Constant       *Operand
	// Owner is the nominal object a MethodCallExpression's method belongs to.
	Owner *compilerTypes.ObjectType
	// Arguments is in written order for every kind except
	// AdtConstructExpression, whose payload fields may be written out of
	// declaration order: there it is declaration order, matching the
	// generated struct's field layout, and EvaluationOrder separately
	// carries the written order for sequencing.
	Arguments    []Operand
	Operator     Operator
	OperandType  compilerTypes.Type
	ResultType   compilerTypes.Type
	MemberIndex  int
	VariantIndex int
	TestType     compilerTypes.Type
	MemberMap    []int
	// EvaluationOrder is non-nil only for AdtConstructExpression: the
	// indices into Arguments (declaration order) in the order the payload
	// fields were actually written, for evaluation sequencing.
	EvaluationOrder []int
	Element         compilerTypes.Type
	// ViewRoots is the ordered binding chain a View-producing expression is
	// borrowed from, outermost root first.
	ViewRoots []BindingID
	// RootKind classifies a View's root at its return site: no root (empty),
	// a foreign from_pointer region, or the bindings in ViewRoots.
	RootKind ViewRootKind
	// SourceLine and SourceColumn name the source site of compiler-built
	// runtime failures: the Error constructed when a spawn, Channel, or
	// Mutex operation fails. Zero for all other kinds.
	SourceLine   int
	SourceColumn int
	// Module is the canonical module id of a FunctionReferenceExpression
	// resolved through an import alias; empty for every
	// local reference.
	Module string
	// MethodParameters carries the parameter types of an imported
	// MethodCallExpression: the call node lacks a Fun
	// signature, so the generator needs these to declare the foreign
	// prototype in the importer's header. Nil for local calls.
	MethodParameters []compilerTypes.Type
	// Function is the checked signature and body of a
	// FunctionLiteralExpression. Nil for every other kind.
	Function *FunctionLiteral
	// LocalHelperOrdinal is nonzero exactly when Kind is
	// FunctionReferenceExpression or FunctionLiteralExpression and the
	// referenced function is a local named function or anonymous literal:
	// its generated symbol is hex_fun_<LocalHelperOrdinal>, not an
	// owner-qualified name built from Name. BindingID allocation never
	// returns zero, so zero unambiguously means "not a local helper
	// reference".
	LocalHelperOrdinal BindingID
}

// FunctionLiteral is the checked signature and body shared by every
// function form: a module FunctionDeclaration and a FunctionLiteralExpression
// both check their parameters, result, and body through the same path and
// store the result in this shape.
type FunctionLiteral struct {
	Parameters []FunctionParameter
	Result     *compilerTypes.Type
	ResultUse  *compilerTypes.TypeUse
	Type       compilerTypes.Type
	Body       []Statement
	Defers     []DeferredAction
	// HelperOrdinal is this literal's compiler-owned identity, assigned once
	// at check time from the shared BindingID counter, in the hex_fun_<ordinal>
	// stream.
	HelperOrdinal BindingID
	SourceLine    int
	SourceColumn  int
}

// ViewRootKind classifies the root of a View-producing expression for the
// return check.
type ViewRootKind uint8

// The concrete ViewRootKind values, each documented at its own line.
const (
	ViewRootNone     ViewRootKind = iota // empty(): no root; any return is safe
	ViewRootForeign                      // from_pointer: opaque foreign region
	ViewRootBindings                     // roots listed in ViewRoots
)

// ObjectValue is a complete checked object literal. Initializers are kept in
// source-written order so future effectful expressions can preserve their
// evaluation order; the generator chooses declaration order for designators.
type ObjectValue struct {
	Type         compilerTypes.Type
	Initializers []ObjectMemberValue
}

// ObjectMemberValue is one checked object-literal initializer: the member it
// fills and the operand it was initialized from.
type ObjectMemberValue struct {
	Member *compilerTypes.ObjectMember
	Source Operand
}

// LiteralRadix records the source radix retained for readable integer
// lowering. It is metadata, not a source-trust shortcut: the exact value is
// still authoritative in Constant.
type LiteralRadix uint8

// The concrete LiteralRadix values, named for the source spelling they
// record.
const (
	DecimalRadix LiteralRadix = iota
	HexadecimalRadix
	BinaryRadix
	OctalRadix
)

// Operand is the shared checked representation used by declarations and
// assignments. Constant values use go/constant so all widths share one exact
// representation until the generator selects the target C spelling.
type Operand struct {
	Kind        OperandKind
	Type        compilerTypes.Type
	Constant    constant.Value
	Literal     string
	Name        string // source spelling retained for diagnostics
	Binding     BindingID
	Node        Expression
	Addressable bool
	Writable    bool
	Radix       LiteralRadix
	Negative    bool
	FloatBits   uint64
	Object      *ObjectValue
}

func constantOperand(typ compilerTypes.Type, value constant.Value, literal string) Operand {
	negative := value != nil && value.Kind() == constant.Int && constant.Sign(value) < 0
	return Operand{Kind: ConstantOperand, Type: typ, Constant: value, Literal: literal, Negative: negative}
}

// nilOperand builds the checked Nil singleton literal. The kind stays
// ConstantOperand so immutable Nil bindings retain a known value, while the
// node kind marks the literal for null-test normalization and nullptr
// lowering. Nil has one value, so no go/constant is carried.
func nilOperand(literal string) Operand {
	return Operand{
		Kind:    ConstantOperand,
		Type:    compilerTypes.Nil,
		Literal: literal,
		Node:    Expression{Kind: NilExpression, ResultType: compilerTypes.Nil},
	}
}

// eosOperand builds the checked end-of-stream singleton. The kind
// stays ConstantOperand so immutable eos bindings retain a known value, and
// the node kind marks the literal for EoS equality folding and union
// narrowing. EoS has one value, so no go/constant is carried.
func eosOperand(literal string) Operand {
	return Operand{
		Kind:    ConstantOperand,
		Type:    compilerTypes.EoS,
		Literal: literal,
		Node:    Expression{Kind: EosExpression, ResultType: compilerTypes.EoS},
	}
}

func constantNode(source Operand) Expression {
	return Expression{Kind: ConstantExpression, Constant: &source, ResultType: source.Type}
}

func operationUnaryNode(operator Operator, operand Expression, operandType, resultType compilerTypes.Type) Expression {
	return Expression{
		Kind:        UnaryOperationExpression,
		Operand:     &operand,
		Operator:    operator,
		OperandType: operandType,
		ResultType:  resultType,
	}
}

func operationBinaryNode(operator Operator, left, right Expression, operandType, resultType compilerTypes.Type) Expression {
	return Expression{
		Kind:        BinaryOperationExpression,
		Left:        &left,
		Right:       &right,
		Operator:    operator,
		OperandType: operandType,
		ResultType:  resultType,
	}
}
