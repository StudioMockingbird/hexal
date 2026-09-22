package specdata

import "fmt"

// Built-in method records. A method is declared once per constructor, with its
// parameters and result referring to the constructor's own parameters by
// position, so one record covers every specialization of its receiver. The
// checker still owns source expressions, argument evaluation, mutability, flow
// facts, and diagnostic locations; the generator still owns lowering and
// evaluation order.

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

// RefKind classifies one type reference: a concrete compiler-owned type or a
// position among the owner constructor's parameters.
type RefKind uint8

const (
	// RefConcrete names a concrete compiler-owned type. An empty TypeID is
	// the void result.
	RefConcrete RefKind = iota
	// RefParam names the owner constructor's parameter at Param.
	RefParam
)

// TypeRef names a method parameter's or result's type. A parameter reference
// keeps one method record valid across every specialization of its owner.
type TypeRef struct {
	Kind   RefKind
	TypeID TypeID
	Param  int
}

// ConcreteType references one concrete compiler-owned type.
func ConcreteType(id TypeID) TypeRef { return TypeRef{Kind: RefConcrete, TypeID: id} }

// Param references the owner constructor's parameter at index, counted from
// zero in written order.
func Param(index int) TypeRef { return TypeRef{Kind: RefParam, Param: index} }

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

// MethodSpec is one compiler-owned built-in method. Parameters and Result refer
// to the owner constructor's parameters by position.
type MethodSpec struct {
	Owner      TypePattern
	Name       string
	Parameters []ParameterSpec
	Result     ResultSpec
	Failure    FailureMode
	Allocation AllocationMode
}

// methods is the registry. It is unexported so no importer can rewrite a
// record, and every query clones the parameter slice it returns.
var methods = []MethodSpec{
	// Array and Slice share a read-only length; neither exposes an applied
	// constructor result here.
	{
		Owner:  ConstructorOwner(TypeArray),
		Name:   "length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner:  ConstructorOwner(TypeSlice),
		Name:   "length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},

	{
		Owner:      ConstructorOwner(TypeList),
		Name:       "length",
		Result:     ResultSpec{Type: ConcreteType(TypeSize)},
		Allocation: AllocationNone,
	},
	{
		Owner:      ConstructorOwner(TypeList),
		Name:       "push",
		Parameters: []ParameterSpec{{Name: "value", Type: Param(0)}},
		Allocation: AllocationHeap,
	},
	{
		Owner:  ConstructorOwner(TypeList),
		Name:   "pop",
		Result: ResultSpec{Type: Param(0)},
	},
	{
		Owner: ConstructorOwner(TypeList),
		Name:  "clear",
	},
	{
		Owner:      ConstructorOwner(TypeList),
		Name:       "free",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
	},

	{
		Owner:  ConstructorOwner(TypeDict),
		Name:   "length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner: ConstructorOwner(TypeDict),
		Name:  "insert",
		Parameters: []ParameterSpec{
			{Name: "key", Type: Param(0)},
			{Name: "value", Type: Param(1)},
		},
		Allocation: AllocationHeap,
	},
	{
		Owner:      ConstructorOwner(TypeDict),
		Name:       "get",
		Parameters: []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:     ResultSpec{Type: Param(1)},
	},
	{
		Owner:      ConstructorOwner(TypeDict),
		Name:       "remove",
		Parameters: []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:     ResultSpec{Type: Param(1)},
	},
	{
		Owner:      ConstructorOwner(TypeDict),
		Name:       "contains",
		Parameters: []ParameterSpec{{Name: "key", Type: Param(0)}},
		Result:     ResultSpec{Type: ConcreteType(TypeBool)},
	},
	{
		Owner:      ConstructorOwner(TypeDict),
		Name:       "free",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
	},

	{
		Owner:  ConstructorOwner(TypeTask),
		Name:   "join",
		Result: ResultSpec{Type: Param(0)},
	},
	{
		Owner: ConstructorOwner(TypeTask),
		Name:  "detach",
	},

	{
		Owner:      ConstructorOwner(TypeChannel),
		Name:       "send",
		Parameters: []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:     ResultSpec{Type: ConcreteType(TypeNil)},
		Failure:    FailureChecked,
	},
	{
		Owner:  ConstructorOwner(TypeChannel),
		Name:   "close",
		Result: ResultSpec{},
	},
	{
		Owner:      ConstructorOwner(TypeChannel),
		Name:       "free",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
	},
	{
		Owner:  ConstructorOwner(TypeChannel),
		Name:   "length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner:  ConstructorOwner(TypeChannel),
		Name:   "capacity",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner:  ConstructorOwner(TypeChannel),
		Name:   "is_closed",
		Result: ResultSpec{Type: ConcreteType(TypeBool)},
	},

	{
		Owner:  ConstructorOwner(TypeAtomic),
		Name:   "load",
		Result: ResultSpec{Type: Param(0)},
	},
	{
		Owner:      ConstructorOwner(TypeAtomic),
		Name:       "store",
		Parameters: []ParameterSpec{{Name: "value", Type: Param(0)}},
	},
	{
		Owner:      ConstructorOwner(TypeAtomic),
		Name:       "exchange",
		Parameters: []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:     ResultSpec{Type: Param(0)},
	},
	{
		Owner:      ConstructorOwner(TypeAtomic),
		Name:       "fetch_add",
		Parameters: []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:     ResultSpec{Type: Param(0)},
	},
	{
		Owner:      ConstructorOwner(TypeAtomic),
		Name:       "fetch_sub",
		Parameters: []ParameterSpec{{Name: "value", Type: Param(0)}},
		Result:     ResultSpec{Type: Param(0)},
	},
	{
		Owner: ConstructorOwner(TypeAtomic),
		Name:  "compare_exchange",
		Parameters: []ParameterSpec{
			{Name: "expected", Type: Param(0)},
			{Name: "desired", Type: Param(0)},
		},
		Result: ResultSpec{Type: ConcreteType(TypeBool)},
	},

	{
		Owner: ConstructorOwner(TypeStash),
		Name:  "reset",
	},
	{
		Owner: ConstructorOwner(TypeStash),
		Name:  "destroy",
	},

	{
		Owner: ConstructorOwner(TypePool),
		Name:  "destroy",
	},

	// String, the heap handle, exposes the operations that own or read its
	// bytes; String<N> shares only the read-only length.
	{
		Owner:  ExactOwner(TypeString),
		Name:   "length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner:  ExactOwner(TypeString),
		Name:   "rune_length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner:  ExactOwner(TypeString),
		Name:   "grapheme_length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},
	{
		Owner:  ExactOwner(TypeString),
		Name:   "byte_cursor",
		Result: ResultSpec{Type: ConcreteType(TypeByteCursor)},
	},
	{
		Owner:  ExactOwner(TypeString),
		Name:   "rune_cursor",
		Result: ResultSpec{Type: ConcreteType(TypeRuneCursor)},
	},
	{
		Owner:  ExactOwner(TypeString),
		Name:   "grapheme_cursor",
		Result: ResultSpec{Type: ConcreteType(TypeGraphemeCursor)},
	},
	{
		Owner:      ExactOwner(TypeString),
		Name:       "casefold",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		Result:     ResultSpec{Type: ConcreteType(TypeString)},
		Failure:    FailureChecked,
		Allocation: AllocationHeap,
	},
	{
		Owner: ExactOwner(TypeString),
		Name:  "normalize",
		Parameters: []ParameterSpec{
			{Name: "heap", Type: ConcreteType(TypeHeap)},
			{Name: "form", Type: ConcreteType(TypeNormalization)},
		},
		Result:     ResultSpec{Type: ConcreteType(TypeString)},
		Failure:    FailureChecked,
		Allocation: AllocationHeap,
	},
	{
		Owner:      ExactOwner(TypeString),
		Name:       "copy",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
		Result:     ResultSpec{Type: ConcreteType(TypeString)},
		Allocation: AllocationHeap,
	},
	{
		Owner:      ExactOwner(TypeString),
		Name:       "free",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
	},
	{
		Owner:  ConstructorOwner(TypeInlineString),
		Name:   "length",
		Result: ResultSpec{Type: ConcreteType(TypeSize)},
	},

	{
		Owner: ExactOwner(TypeMutex),
		Name:  "lock",
	},
	{
		Owner: ExactOwner(TypeMutex),
		Name:  "unlock",
	},
	{
		Owner:      ExactOwner(TypeMutex),
		Name:       "free",
		Parameters: []ParameterSpec{{Name: "heap", Type: ConcreteType(TypeHeap)}},
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

// cloneMethod deep-copies the parameter slice a record owns, so a query result
// shares no backing array with the registry.
func cloneMethod(method MethodSpec) MethodSpec {
	method.Parameters = append([]ParameterSpec(nil), method.Parameters...)
	return method
}

// validateMethods checks the method registry against the constructor registry:
// every owner and referenced identifier must resolve, every parameter reference
// must fall inside its owner's parameter list, and an owner may not declare one
// method name twice.
func validateMethods() error {
	seen := make(map[methodKey]bool, len(methods))
	for _, method := range methods {
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
		if method.Allocation != AllocationNone && method.Allocation != AllocationHeap {
			return fmt.Errorf("specdata/methods: %s.%s has unknown allocation mode", ownerName(method.Owner), method.Name)
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
// inside its parameter list; a concrete reference must resolve, unless it is a
// permitted void result.
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
	default:
		return fmt.Errorf("specdata/methods: has unknown reference kind")
	}
	return nil
}
