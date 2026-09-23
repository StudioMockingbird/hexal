package specdata

import (
	"fmt"
	"strings"
)

// Built-in method records. A method is declared once per constructor, with its
// parameters and result referring to the constructor's own parameters by
// position, so one record covers every specialization of its receiver. The
// checker still owns source expressions, argument evaluation, mutability, flow
// facts, and diagnostic locations; the generator still owns lowering and
// evaluation order.
//
// RuntimeSymbol names the emitted C operation. A method whose lowering reads a
// field or computes a constant emits no callable and records the empty string,
// with a comment at the record why. A parameterized symbol keeps the
// generator's own fmt format: %s stands for the per-specialization suffix, and
// it appears exactly where the generator writes one.
//
// Component names the method's runtime component demand when the operation
// lowers to that component's code; an operation that lowers entirely to a
// field read or a constant still names the component its owner constructor
// already demands, so every record states one component.

// TypePattern selects the receiver a method is declared on: a type constructor,
// which covers every specialization, or one concrete compiler-owned type.
// Exactly one field is set.
type TypePattern struct {
	Constructor TypeID
	Exact       TypeID
}

// ConstructorOwner names a receiver by its type constructor.
func ConstructorOwner(id TypeID) TypePattern { return TypePattern{Constructor: id} }

// ExactOwner names a receiver by one concrete compiler-owned type.
func ExactOwner(id TypeID) TypePattern { return TypePattern{Exact: id} }

// RefKind classifies one type reference: a concrete compiler-owned type, a
// position among the owner constructor's parameters, an applied constructor, a
// structural union, a structural pointer, a method-level type argument, or the
// abstract integer class.
type RefKind uint8

const (
	// RefConcrete names a concrete compiler-owned type. An empty TypeID is
	// the void result.
	RefConcrete RefKind = iota
	// RefParam names the owner constructor's parameter at Param.
	RefParam
	// RefApply applies the type constructor named by Constructor to Arguments.
	// Access selects the mutability of the result where the constructor is a
	// collection with read and writable forms.
	RefApply
	// RefUnion is the structural union of Arguments, in written order.
	RefUnion
	// RefPointer is the structural pointer Ptr<T> or Ptr<mut T> over
	// Arguments[0], selected by Access.
	RefPointer
	// RefMethodArg names the method's own type argument at Param; an operation
	// parameterized per call, such as the capacity of an inline text result,
	// states that argument here because no owner parameter names it.
	RefMethodArg
	// RefInteger is the abstract integer class accepted wherever any signed or
	// unsigned integer scalar is, such as a collection bound.
	RefInteger
)

// AccessMode classifies the mutability carried by an applied collection or a
// structural pointer. It is meaningful only for RefApply and RefPointer.
type AccessMode uint8

const (
	// AccessReadOnly is a read-only element or pointer: Slice<T>, Ptr<T>.
	AccessReadOnly AccessMode = iota
	// AccessMutable is a writable element or pointer: Slice<mut T>,
	// Ptr<mut T>.
	AccessMutable
	// AccessReceiver follows the receiver specialization's own access mode,
	// which a constructor-level record cannot otherwise name: re-slicing a
	// Slice preserves what it was sliced from.
	AccessReceiver
)

// TypeRef names a method parameter's or result's type. A parameter reference
// keeps one method record valid across every specialization of its owner.
type TypeRef struct {
	Kind        RefKind
	TypeID      TypeID
	Param       int
	Constructor TypeID
	Arguments   []TypeRef
	Access      AccessMode
}

// ConcreteType references one concrete compiler-owned type.
func ConcreteType(id TypeID) TypeRef { return TypeRef{Kind: RefConcrete, TypeID: id} }

// Param references the owner constructor's parameter at index, counted from
// zero in written order.
func Param(index int) TypeRef { return TypeRef{Kind: RefParam, Param: index} }

// AppliedType references the type constructor id applied to arguments, with the
// given result access for a collection constructor.
func AppliedType(id TypeID, access AccessMode, arguments ...TypeRef) TypeRef {
	return TypeRef{Kind: RefApply, Constructor: id, Arguments: arguments, Access: access}
}

// UnionType references the structural union of members, in written order.
func UnionType(members ...TypeRef) TypeRef {
	return TypeRef{Kind: RefUnion, Arguments: members}
}

// PointerType references the structural pointer over element with the given
// access.
func PointerType(access AccessMode, element TypeRef) TypeRef {
	return TypeRef{Kind: RefPointer, Arguments: []TypeRef{element}, Access: access}
}

// MethodTypeArg references the method's own type argument at index.
func MethodTypeArg(index int) TypeRef { return TypeRef{Kind: RefMethodArg, Param: index} }

// IntegerType references the abstract integer class.
func IntegerType() TypeRef { return TypeRef{Kind: RefInteger} }

// ParameterSpec is one method parameter. Name carries the source spelling when
// the declaration fixes one; Type is the parameter's type reference.
type ParameterSpec struct {
	Name string
	Type TypeRef
}

// ResultSpec is one method's success shape. A zero Type is the void result; a
// FailureChecked method additionally unions the built-in Error type at the
// call site.
type ResultSpec struct {
	Type TypeRef
}

// FailureMode classifies a method's failure behavior.
type FailureMode uint8

const (
	// FailureInfallible yields its result or traps, never an Error.
	FailureInfallible FailureMode = iota
	// FailureChecked returns its result unioned with the built-in Error type.
	FailureChecked
)

// AllocationMode classifies a method's allocation behavior.
type AllocationMode uint8

const (
	// AllocationNone allocates nothing.
	AllocationNone AllocationMode = iota
	// AllocationHeap allocates through a Heap the receiver or an argument
	// supplies.
	AllocationHeap
)

// ReceiverMode classifies how a method's receiver reaches the operation.
type ReceiverMode uint8

const (
	// ReceiverValue passes the receiver of the owner's concrete type.
	ReceiverValue ReceiverMode = iota
	// ReceiverAddress passes the receiver by address, as the atomic read and
	// update operations do when the value is an inline Atomic<T>.
	ReceiverAddress
)

// MethodSpec is one compiler-owned built-in method. Parameters and Result refer
// to the owner constructor's parameters by position.
type MethodSpec struct {
	Owner         TypePattern
	Name          string
	Parameters    []ParameterSpec
	Result        ResultSpec
	Failure       FailureMode
	RuntimeSymbol string
	Component     ComponentID
	Allocation    AllocationMode
	Receiver      ReceiverMode
}

// methods is the registry. It is unexported so no importer can rewrite a
// record, and every query clones the parameter slice it returns.
var methods = []MethodSpec{
	// Array and Slice share a read-only length. Array's is the compile-time
	// extent and Slice's is a descriptor field, so neither emits a callable.
	{
		Owner:     ConstructorOwner(TypeArray),
		Name:      "length",
		Result:    ResultSpec{Type: ConcreteType(TypeSize)},
		Component: ComponentArray,
	},
	{
		Owner:     ConstructorOwner(TypeSlice),
		Name:      "length",
		Result:    ResultSpec{Type: ConcreteType(TypeSize)},
		Component: ComponentSlice,
	},
	{
		Owner: ConstructorOwner(TypeArray),
		Name:  "slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessReadOnly, Param(0))},
		RuntimeSymbol: "hex_array_slice_%s",
		Component:     ComponentArray,
	},
	{
		Owner: ConstructorOwner(TypeArray),
		Name:  "mut_slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessMutable, Param(0))},
		RuntimeSymbol: "hex_array_mut_slice_%s",
		Component:     ComponentArray,
	},
	// Re-slicing a Slice preserves its access mode, which is why the result
	// names the receiver rather than one fixed Slice form.
	{
		Owner: ConstructorOwner(TypeSlice),
		Name:  "slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result: ResultSpec{Type: AppliedType(TypeSlice, AccessReceiver, Param(0))},
		// The symbol follows the receiver's access mode, so one fixed field
		// cannot name it: read-only emits hex_slice_slice_<suffix> and
		// writable emits hex_mut_slice_slice_<suffix>.
		Component: ComponentSlice,
	},
	{
		Owner:  ConstructorOwner(TypeSlice),
		Name:   "pointer",
		Result: ResultSpec{Type: UnionType(PointerType(AccessReceiver, Param(0)), ConcreteType(TypeNil))},
		// The result is the descriptor's own data address, so no callable is
		// emitted; the field read is (receiver).data.
		Component: ComponentSlice,
	},

	{
		Owner:      ConstructorOwner(TypeList),
		Name:       "length",
		Result:     ResultSpec{Type: ConcreteType(TypeSize)},
		Component:  ComponentList,
		Allocation: AllocationNone,
		// The handle's own length field is read directly, so no callable is
		// emitted.
	},
	{
		Owner:         ConstructorOwner(TypeList),
		Name:          "push",
		Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
		RuntimeSymbol: "hex_list_push_%s",
		Component:     ComponentList,
		Allocation:    AllocationHeap,
	},
	{
		Owner:         ConstructorOwner(TypeList),
		Name:          "pop",
		Result:        ResultSpec{Type: Param(0)},
		RuntimeSymbol: "hex_list_pop_%s",
		Component:     ComponentList,
	},
	{
		Owner:         ConstructorOwner(TypeList),
		Name:          "clear",
		RuntimeSymbol: "hex_list_clear_%s",
		Component:     ComponentList,
	},
	{
		Owner:         ConstructorOwner(TypeList),
		Name:          "free",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		RuntimeSymbol: "hex_list_free_%s",
		Component:     ComponentList,
	},
	{
		Owner: ConstructorOwner(TypeList),
		Name:  "slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessReadOnly, Param(0))},
		RuntimeSymbol: "hex_list_slice_%s",
		Component:     ComponentList,
	},
	{
		Owner: ConstructorOwner(TypeList),
		Name:  "mut_slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessMutable, Param(0))},
		RuntimeSymbol: "hex_list_mut_slice_%s",
		Component:     ComponentList,
	},

	{
		Owner:     ConstructorOwner(TypeDict),
		Name:      "length",
		Result:    ResultSpec{Type: ConcreteType(TypeSize)},
		Component: ComponentDict,
		// The handle's own length field is read directly, so no callable is
		// emitted.
	},
	{
		Owner: ConstructorOwner(TypeDict),
		Name:  "insert",
		Parameters: []ParameterSpec{
			{Name: "key", Type: Param(0)},
			{Name: "value", Type: Param(1)},
		},
		RuntimeSymbol: "hex_dict_insert_%s",
		Component:     ComponentDict,
		Allocation:    AllocationHeap,
	},
	{
		Owner:         ConstructorOwner(TypeDict),
		Name:          "get",
		Parameters:    []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:        ResultSpec{Type: Param(1)},
		RuntimeSymbol: "hex_dict_get_%s",
		Component:     ComponentDict,
	},
	{
		Owner:         ConstructorOwner(TypeDict),
		Name:          "find",
		Parameters:    []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:        ResultSpec{Type: UnionType(Param(1), ConcreteType(TypeNil))},
		RuntimeSymbol: "hex_dict_find_%s",
		Component:     ComponentDict,
	},
	{
		Owner:         ConstructorOwner(TypeDict),
		Name:          "remove",
		Parameters:    []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:        ResultSpec{Type: Param(1)},
		RuntimeSymbol: "hex_dict_remove_%s",
		Component:     ComponentDict,
	},
	{
		Owner:         ConstructorOwner(TypeDict),
		Name:          "contains",
		Parameters:    []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:        ResultSpec{Type: ConcreteType(TypeBool)},
		RuntimeSymbol: "hex_dict_contains_%s",
		Component:     ComponentDict,
	},
	{
		Owner:         ConstructorOwner(TypeDict),
		Name:          "free",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		RuntimeSymbol: "hex_dict_free_%s",
		Component:     ComponentDict,
	},

	{
		Owner:         ConstructorOwner(TypeTask),
		Name:          "join",
		Result:        ResultSpec{Type: Param(0)},
		RuntimeSymbol: "hex_task_join_%s",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeTask),
		Name:          "detach",
		RuntimeSymbol: "hex_task_detach",
		Component:     ComponentConcurrency,
	},

	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "send",
		Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:        ResultSpec{Type: ConcreteType(TypeNil)},
		Failure:       FailureChecked,
		RuntimeSymbol: "hex_chan_send_%s",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "receive",
		Result:        ResultSpec{Type: UnionType(Param(0), ConcreteType(TypeEoS))},
		RuntimeSymbol: "hex_chan_recv_%s",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "close",
		Result:        ResultSpec{},
		RuntimeSymbol: "hex_chan_close",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "free",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		RuntimeSymbol: "hex_chan_free_%s",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "length",
		Result:        ResultSpec{Type: ConcreteType(TypeSize)},
		RuntimeSymbol: "hex_chan_length",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "capacity",
		Result:        ResultSpec{Type: ConcreteType(TypeSize)},
		RuntimeSymbol: "hex_chan_capacity",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ConstructorOwner(TypeChannel),
		Name:          "is_closed",
		Result:        ResultSpec{Type: ConcreteType(TypeBool)},
		RuntimeSymbol: "hex_chan_is_closed",
		Component:     ComponentConcurrency,
	},

	{
		Owner:         ConstructorOwner(TypeAtomic),
		Name:          "load",
		Result:        ResultSpec{Type: Param(0)},
		RuntimeSymbol: "hex_atomic_%s_load",
		Component:     ComponentConcurrency,
		Receiver:      ReceiverAddress,
	},
	{
		Owner:         ConstructorOwner(TypeAtomic),
		Name:          "store",
		Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
		RuntimeSymbol: "hex_atomic_%s_store",
		Component:     ComponentConcurrency,
		Receiver:      ReceiverAddress,
	},
	{
		Owner:         ConstructorOwner(TypeAtomic),
		Name:          "exchange",
		Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:        ResultSpec{Type: Param(0)},
		RuntimeSymbol: "hex_atomic_%s_exchange",
		Component:     ComponentConcurrency,
		Receiver:      ReceiverAddress,
	},
	{
		Owner:         ConstructorOwner(TypeAtomic),
		Name:          "fetch_add",
		Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:        ResultSpec{Type: Param(0)},
		RuntimeSymbol: "hex_atomic_%s_fetch_add",
		Component:     ComponentConcurrency,
		Receiver:      ReceiverAddress,
	},
	{
		Owner:         ConstructorOwner(TypeAtomic),
		Name:          "fetch_sub",
		Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:        ResultSpec{Type: Param(0)},
		RuntimeSymbol: "hex_atomic_%s_fetch_sub",
		Component:     ComponentConcurrency,
		Receiver:      ReceiverAddress,
	},
	{
		Owner: ConstructorOwner(TypeAtomic),
		Name:  "compare_exchange",
		Parameters: []ParameterSpec{
			{Name: "expected", Type: Param(0)},
			{Name: "desired", Type: Param(0)},
		},
		Result:        ResultSpec{Type: ConcreteType(TypeBool)},
		RuntimeSymbol: "hex_atomic_%s_compare_exchange",
		Component:     ComponentConcurrency,
		Receiver:      ReceiverAddress,
	},

	{
		Owner:         ConstructorOwner(TypeStash),
		Name:          "allocate",
		Parameters:    []ParameterSpec{{Name: "initial", Type: Param(0)}},
		Result:        ResultSpec{Type: PointerType(AccessMutable, Param(0))},
		RuntimeSymbol: "hex_stash_alloc_%s",
		Component:     ComponentStash,
	},
	{
		Owner:         ConstructorOwner(TypeStash),
		Name:          "reset",
		RuntimeSymbol: "hex_stash_reset",
		Component:     ComponentStash,
	},
	{
		Owner:         ConstructorOwner(TypeStash),
		Name:          "destroy",
		RuntimeSymbol: "hex_stash_destroy",
		Component:     ComponentStash,
	},

	{
		Owner:         ConstructorOwner(TypePool),
		Name:          "allocate",
		Parameters:    []ParameterSpec{{Name: "initial", Type: Param(0)}},
		Result:        ResultSpec{Type: PointerType(AccessMutable, Param(0))},
		RuntimeSymbol: "hex_pool_alloc_%s",
		Component:     ComponentPool,
	},
	{
		Owner:         ConstructorOwner(TypePool),
		Name:          "free",
		Parameters:    []ParameterSpec{{Name: "pointer", Type: PointerType(AccessReadOnly, Param(0))}},
		RuntimeSymbol: "hex_pool_free_%s",
		Component:     ComponentPool,
	},
	{
		Owner:         ConstructorOwner(TypePool),
		Name:          "destroy",
		RuntimeSymbol: "hex_pool_destroy_%s",
		Component:     ComponentPool,
	},

	// String, the heap handle, exposes the operations that own or read its
	// bytes; String<N> shares the read-only and constructing operations.
	{
		Owner:     ExactOwner(TypeString),
		Name:      "length",
		Result:    ResultSpec{Type: ConcreteType(TypeSize)},
		Component: ComponentString,
		// The byte-view descriptor's own length field is read directly, so no
		// callable is emitted.
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "rune_length",
		Result:        ResultSpec{Type: ConcreteType(TypeSize)},
		RuntimeSymbol: "hex_text_rune_length",
		Component:     ComponentString,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "grapheme_length",
		Result:        ResultSpec{Type: ConcreteType(TypeSize)},
		RuntimeSymbol: "hex_text_grapheme_length",
		Component:     ComponentString,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "byte_cursor",
		Result:        ResultSpec{Type: ConcreteType(TypeByteCursor)},
		RuntimeSymbol: "hex_text_byte_cursor",
		Component:     ComponentString,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "rune_cursor",
		Result:        ResultSpec{Type: ConcreteType(TypeRuneCursor)},
		RuntimeSymbol: "hex_text_rune_cursor",
		Component:     ComponentString,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "grapheme_cursor",
		Result:        ResultSpec{Type: ConcreteType(TypeGraphemeCursor)},
		RuntimeSymbol: "hex_text_grapheme_cursor",
		Component:     ComponentString,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "bytes",
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessReadOnly, ConcreteType(TypeUInt8))},
		Component:     ComponentString,
		RuntimeSymbol: "hex_text_bytes",
	},
	{
		Owner: ExactOwner(TypeString),
		Name:  "slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessReadOnly, ConcreteType(TypeUInt8))},
		RuntimeSymbol: "hex_text_slice",
		Component:     ComponentString,
	},
	{
		Owner:      ExactOwner(TypeString),
		Name:       "casefold",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		Result:     ResultSpec{Type: ConcreteType(TypeString)},
		Failure:    FailureChecked,
		// The adapter suffix is the result union's canonical C name.
		RuntimeSymbol: "hex_string_casefold_%s",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner: ExactOwner(TypeString),
		Name:  "normalize",
		Parameters: []ParameterSpec{
			{Name: "heap", Type: ConcreteType(TypeHeap)},
			{Name: "form", Type: ConcreteType(TypeNormalization)},
		},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		Failure:       FailureChecked,
		RuntimeSymbol: "hex_string_normalize_%s",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "copy",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		RuntimeSymbol: "hex_string_make",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner: ExactOwner(TypeString),
		Name:  "concat",
		Parameters: []ParameterSpec{
			{Name: "heap", Type: ConcreteType(TypeHeap)},
			{Name: "other", Type: AppliedType(TypeSlice, AccessReadOnly, ConcreteType(TypeUInt8))},
		},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		Failure:       FailureChecked,
		RuntimeSymbol: "hex_string_concat_%s",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner:         ExactOwner(TypeString),
		Name:          "free",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		RuntimeSymbol: "hex_string_free",
		Component:     ComponentString,
	},
	{
		Owner:  ExactOwner(TypeString),
		Name:   "c_pointer",
		Result: ResultSpec{Type: PointerType(AccessReadOnly, ConcreteType(TypeUInt8))},
		// The result is the header's own byte address, so no callable is
		// emitted; the field read is (receiver)->data.
		Component: ComponentString,
	},

	// String<N> shares the heap form's read-only and constructing operations,
	// because both lower through the same byte-view helpers; each shared record
	// therefore names the heap record's symbol. The operations unique to one
	// form -- free and c_pointer on the heap form, widen on the inline form --
	// stay single-owner.
	{
		Owner:     ConstructorOwner(TypeInlineString),
		Name:      "length",
		Result:    ResultSpec{Type: ConcreteType(TypeSize)},
		Component: ComponentString,
		// The byte-view descriptor's own length field is read directly, so no
		// callable is emitted.
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "rune_length",
		Result:        ResultSpec{Type: ConcreteType(TypeSize)},
		RuntimeSymbol: "hex_text_rune_length",
		Component:     ComponentString,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "grapheme_length",
		Result:        ResultSpec{Type: ConcreteType(TypeSize)},
		RuntimeSymbol: "hex_text_grapheme_length",
		Component:     ComponentString,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "byte_cursor",
		Result:        ResultSpec{Type: ConcreteType(TypeByteCursor)},
		RuntimeSymbol: "hex_text_byte_cursor",
		Component:     ComponentString,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "rune_cursor",
		Result:        ResultSpec{Type: ConcreteType(TypeRuneCursor)},
		RuntimeSymbol: "hex_text_rune_cursor",
		Component:     ComponentString,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "grapheme_cursor",
		Result:        ResultSpec{Type: ConcreteType(TypeGraphemeCursor)},
		RuntimeSymbol: "hex_text_grapheme_cursor",
		Component:     ComponentString,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "bytes",
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessReadOnly, ConcreteType(TypeUInt8))},
		RuntimeSymbol: "hex_text_bytes",
		Component:     ComponentString,
	},
	{
		Owner: ConstructorOwner(TypeInlineString),
		Name:  "slice",
		Parameters: []ParameterSpec{
			{Name: "start", Type: IntegerType()},
			{Name: "end", Type: IntegerType()},
		},
		Result:        ResultSpec{Type: AppliedType(TypeSlice, AccessReadOnly, ConcreteType(TypeUInt8))},
		RuntimeSymbol: "hex_text_slice",
		Component:     ComponentString,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "copy",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		RuntimeSymbol: "hex_string_make",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "casefold",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		Failure:       FailureChecked,
		RuntimeSymbol: "hex_string_casefold_%s",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner: ConstructorOwner(TypeInlineString),
		Name:  "normalize",
		Parameters: []ParameterSpec{
			{Name: "heap", Type: ConcreteType(TypeHeap)},
			{Name: "form", Type: ConcreteType(TypeNormalization)},
		},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		Failure:       FailureChecked,
		RuntimeSymbol: "hex_string_normalize_%s",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner: ConstructorOwner(TypeInlineString),
		Name:  "concat",
		Parameters: []ParameterSpec{
			{Name: "heap", Type: ConcreteType(TypeHeap)},
			{Name: "other", Type: AppliedType(TypeSlice, AccessReadOnly, ConcreteType(TypeUInt8))},
		},
		Result:        ResultSpec{Type: ConcreteType(TypeString)},
		Failure:       FailureChecked,
		RuntimeSymbol: "hex_string_concat_%s",
		Component:     ComponentString,
		Allocation:    AllocationHeap,
	},
	{
		Owner:         ConstructorOwner(TypeInlineString),
		Name:          "widen",
		Result:        ResultSpec{Type: AppliedType(TypeInlineString, AccessReadOnly, MethodTypeArg(0))},
		RuntimeSymbol: "hex_text_fill",
		Component:     ComponentString,
	},

	{
		Owner:         ExactOwner(TypeMutex),
		Name:          "lock",
		RuntimeSymbol: "hex_mutex_lock",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ExactOwner(TypeMutex),
		Name:          "unlock",
		RuntimeSymbol: "hex_mutex_unlock",
		Component:     ComponentConcurrency,
	},
	{
		Owner:         ExactOwner(TypeMutex),
		Name:          "free",
		Parameters:    []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		RuntimeSymbol: "hex_mutex_free_hex_mutex",
		Component:     ComponentConcurrency,
	},
}

// Method resolves one built-in method by its owner pattern and name.
func Method(owner TypePattern, name string) (MethodSpec, bool) {
	for _, method := range methods {
		if method.Owner == owner && method.Name == name {
			return cloneMethod(method), true
		}
	}
	return MethodSpec{}, false
}

// Methods returns every method record in registration order as a copy.
func Methods() []MethodSpec {
	specs := make([]MethodSpec, len(methods))
	for index, method := range methods {
		specs[index] = cloneMethod(method)
	}
	return specs
}

// cloneMethod deep-copies the slices a record owns recursively, so a query
// result shares no backing array with the registry.
func cloneMethod(method MethodSpec) MethodSpec {
	method.Parameters = make([]ParameterSpec, len(method.Parameters))
	for index, parameter := range method.Parameters {
		parameter.Type = cloneTypeRef(parameter.Type)
		method.Parameters[index] = parameter
	}
	method.Result.Type = cloneTypeRef(method.Result.Type)
	return method
}

// cloneTypeRef deep-copies one type reference's argument slice.
func cloneTypeRef(ref TypeRef) TypeRef {
	if ref.Arguments == nil {
		return ref
	}
	arguments := make([]TypeRef, len(ref.Arguments))
	for index, argument := range ref.Arguments {
		arguments[index] = cloneTypeRef(argument)
	}
	ref.Arguments = arguments
	return ref
}

// validateMethods checks one method registry against the constructor registry:
// every owner and referenced identifier must resolve, every parameter reference
// must fall inside its owner's parameter list, every named component must
// exist, a fallible method must name the runtime operation that reports its
// failure, and an owner may not declare one method name twice. It takes the
// slice so a test can validate a crafted registry without mutating the
// package's own.
func validateMethods(registry []MethodSpec) error {
	seen := make(map[methodKey]bool, len(registry))
	for _, method := range registry {
		if method.Owner.Constructor == "" && method.Owner.Exact == "" {
			return fmt.Errorf("specdata/methods: method %q has no owner", method.Name)
		}
		if method.Owner.Constructor != "" && method.Owner.Exact != "" {
			return fmt.Errorf("specdata/methods: method %q names both a constructor and a concrete owner", method.Name)
		}
		if method.Name == "" {
			return fmt.Errorf("specdata/methods: a method on %s has an empty name", ownerName(method.Owner))
		}
		count, constructor := 0, false
		if method.Owner.Constructor != "" {
			spec, ok := TypeConstructor(method.Owner.Constructor)
			if !ok {
				return fmt.Errorf("specdata/methods: method %s.%s names an unknown constructor", method.Owner.Constructor, method.Name)
			}
			count, constructor = len(spec.Params), true
		} else if !isConcreteTypeID(method.Owner.Exact) {
			return fmt.Errorf("specdata/methods: method %s.%s names an unknown concrete type", method.Owner.Exact, method.Name)
		}
		key := methodKey{Owner: method.Owner, Name: method.Name}
		if seen[key] {
			return fmt.Errorf("specdata/methods: %s.%s is declared twice", ownerName(method.Owner), method.Name)
		}
		seen[key] = true
		for index, parameter := range method.Parameters {
			if err := validateTypeRef(parameter.Type, count, constructor, false); err != nil {
				return fmt.Errorf("specdata/methods: %s.%s parameter %d: %w", ownerName(method.Owner), method.Name, index, err)
			}
		}
		if err := validateTypeRef(method.Result.Type, count, constructor, true); err != nil {
			return fmt.Errorf("specdata/methods: %s.%s result: %w", ownerName(method.Owner), method.Name, err)
		}
		if method.Failure != FailureInfallible && method.Failure != FailureChecked {
			return fmt.Errorf("specdata/methods: %s.%s has unknown failure mode", ownerName(method.Owner), method.Name)
		}
		// A fallible operation unions Error at the call site, so it must name
		// the runtime operation that reports the failure.
		if method.Failure == FailureChecked && method.RuntimeSymbol == "" {
			return fmt.Errorf("specdata/methods: %s.%s can fail but names no runtime symbol", ownerName(method.Owner), method.Name)
		}
		if method.Allocation != AllocationNone && method.Allocation != AllocationHeap {
			return fmt.Errorf("specdata/methods: %s.%s has unknown allocation mode", ownerName(method.Owner), method.Name)
		}
		if method.Receiver != ReceiverValue && method.Receiver != ReceiverAddress {
			return fmt.Errorf("specdata/methods: %s.%s has unknown receiver mode", ownerName(method.Owner), method.Name)
		}
		if _, known := Component(method.Component); !known {
			return fmt.Errorf("specdata/methods: %s.%s demands unknown component %q", ownerName(method.Owner), method.Name, method.Component)
		}
		// A symbol is a C identifier or a generator format; whitespace means a
		// maintainer pasted prose into the field. Two records may share one
		// symbol when one helper serves both owners, so identity is not an
		// error.
		if strings.ContainsAny(method.RuntimeSymbol, " \t\n") {
			return fmt.Errorf("specdata/methods: %s.%s runtime symbol %q contains whitespace", ownerName(method.Owner), method.Name, method.RuntimeSymbol)
		}
	}
	return nil
}

// methodKey is the registry's uniqueness key. A MethodSpec cannot be one,
// because its parameter slice makes it incomparable.
type methodKey struct {
	Owner TypePattern
	Name  string
}

// ownerName renders a pattern for a validation message.
func ownerName(owner TypePattern) string {
	if owner.Constructor != "" {
		return string(owner.Constructor)
	}
	return string(owner.Exact)
}

// validateTypeRef rejects a reference that names nothing this registry knows.
// A parameter reference is valid only against a constructor owner and only
// inside its parameter list; an applied constructor must resolve and take the
// number of arguments the registry declares; a concrete reference must resolve,
// unless it is a permitted void result. Every nested reference is checked with
// the same owner context, so a parameter of an applied argument still names the
// owner's parameter list.
func validateTypeRef(ref TypeRef, count int, constructor, allowVoid bool) error {
	switch ref.Kind {
	case RefConcrete:
		if ref.TypeID == "" {
			if allowVoid {
				return nil
			}
			return fmt.Errorf("specdata/methods: parameter has no type")
		}
		if !isConcreteTypeID(ref.TypeID) {
			return fmt.Errorf("specdata/methods: references unknown concrete type %q", ref.TypeID)
		}
	case RefParam:
		if !constructor {
			return fmt.Errorf("specdata/methods: references a constructor parameter on a concrete owner")
		}
		if ref.Param < 0 || ref.Param >= count {
			return fmt.Errorf("specdata/methods: references parameter %d outside the owner's %d parameters", ref.Param, count)
		}
	case RefApply:
		if !isConstructorTypeID(ref.Constructor) {
			return fmt.Errorf("specdata/methods: references unknown constructor %q", ref.Constructor)
		}
		spec, _ := TypeConstructor(ref.Constructor)
		if len(ref.Arguments) != len(spec.Params) {
			return fmt.Errorf("specdata/methods: constructor %q takes %d argument(s); got %d", ref.Constructor, len(spec.Params), len(ref.Arguments))
		}
		if ref.Access != AccessReadOnly && ref.Access != AccessMutable && ref.Access != AccessReceiver {
			return fmt.Errorf("specdata/methods: constructor %q has unknown access mode", ref.Constructor)
		}
		for index, argument := range ref.Arguments {
			if err := validateTypeRef(argument, count, constructor, false); err != nil {
				return fmt.Errorf("specdata/methods: constructor %q argument %d: %w", ref.Constructor, index, err)
			}
		}
	case RefUnion:
		if len(ref.Arguments) == 0 {
			return fmt.Errorf("specdata/methods: union has no members")
		}
		for index, member := range ref.Arguments {
			if err := validateTypeRef(member, count, constructor, false); err != nil {
				return fmt.Errorf("specdata/methods: union member %d: %w", index, err)
			}
		}
	case RefPointer:
		if len(ref.Arguments) != 1 {
			return fmt.Errorf("specdata/methods: pointer takes 1 element; got %d", len(ref.Arguments))
		}
		if ref.Access != AccessReadOnly && ref.Access != AccessMutable && ref.Access != AccessReceiver {
			return fmt.Errorf("specdata/methods: pointer has unknown access mode")
		}
		if err := validateTypeRef(ref.Arguments[0], count, constructor, false); err != nil {
			return fmt.Errorf("specdata/methods: pointer element: %w", err)
		}
	case RefMethodArg:
		if ref.Param < 0 {
			return fmt.Errorf("specdata/methods: references method type argument %d", ref.Param)
		}
	case RefInteger:
		// The abstract integer class names no identifier.
	default:
		return fmt.Errorf("specdata/methods: has unknown reference kind")
	}
	return nil
}
