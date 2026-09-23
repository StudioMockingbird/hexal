package specdata

import (
	"strings"
	"testing"
)

// validMethod is a registry entry every field of which validateMethods accepts.
// A malformed case copies it and changes one field, so a failure names the
// field under test rather than an unrelated missing one.
var validMethod = MethodSpec{
	Owner:         ConstructorOwner(TypeList),
	Name:          "push",
	Parameters:    []ParameterSpec{{Name: "value", Type: Param(0)}},
	RuntimeSymbol: "hex_list_push_%s",
	Component:     ComponentList,
}

// TestValidateMethodsAcceptsTheRegistry pins the delivered registry against the
// validator directly, so a regression names the method rather than only
// Validate's first failure.
func TestValidateMethodsAcceptsTheRegistry(t *testing.T) {
	if err := validateMethods(methods); err != nil {
		t.Fatalf("validateMethods(registry) = %v, want nil", err)
	}
}

// TestValidateMethodsRejectsMalformedRecords covers each rejection the extended
// model adds: an unknown owner, an unresolvable type reference, a malformed
// applied constructor or structural form, a fallible method with no runtime
// operation, an unknown receiver or allocation mode, and an unknown component.
func TestValidateMethodsRejectsMalformedRecords(t *testing.T) {
	const (
		wantNoOwner        = "has no owner"
		wantBothOwners     = "names both a constructor and a concrete owner"
		wantUnknownOwner   = "names an unknown"
		wantEmptyName      = "has an empty name"
		wantDuplicate      = "is declared twice"
		wantUnknownType    = "references unknown concrete type"
		wantVoidParam      = "parameter has no type"
		wantParamRange     = "references parameter"
		wantParamOnExact   = "constructor parameter on a concrete owner"
		wantUnknownCtor    = "references unknown constructor"
		wantCtorArity      = "takes 1 argument(s); got 0"
		wantEmptyUnion     = "union has no members"
		wantPointerArity   = "pointer takes 1 element; got 0"
		wantPointerAccess  = "pointer has unknown access mode"
		wantAccessMode     = "has unknown access mode"
		wantMethodArg      = "references method type argument -1"
		wantFallibleSymbol = "can fail but names no runtime symbol"
		wantAllocation     = "has unknown allocation mode"
		wantReceiver       = "has unknown receiver mode"
		wantComponent      = "demands unknown component"
		wantSymbolSpace    = "runtime symbol"
	)
	cases := []struct {
		name   string
		mutate func(*MethodSpec)
		two    bool
		want   string
	}{
		{"no owner", func(m *MethodSpec) { m.Owner = TypePattern{} }, false, wantNoOwner},
		{"two owners", func(m *MethodSpec) { m.Owner = TypePattern{Constructor: TypeList, Exact: TypeString} }, false, wantBothOwners},
		{"unknown constructor owner", func(m *MethodSpec) { m.Owner = ConstructorOwner("Nope") }, false, wantUnknownOwner},
		{"unknown concrete owner", func(m *MethodSpec) { m.Owner = ExactOwner("Nope") }, false, wantUnknownOwner},
		{"empty name", func(m *MethodSpec) { m.Name = "" }, false, wantEmptyName},
		{"duplicate", func(m *MethodSpec) {}, true, wantDuplicate},
		{"unknown concrete type", func(m *MethodSpec) { m.Result.Type = ConcreteType("Nope") }, false, wantUnknownType},
		{"void parameter", func(m *MethodSpec) { m.Parameters = []ParameterSpec{{Name: "value", Type: ConcreteType("")}} }, false, wantVoidParam},
		{"parameter out of range", func(m *MethodSpec) { m.Parameters = []ParameterSpec{{Name: "value", Type: Param(5)}} }, false, wantParamRange},
		{"parameter on exact owner", func(m *MethodSpec) {
			m.Owner = ExactOwner(TypeString)
			m.Parameters = []ParameterSpec{{Name: "value", Type: Param(0)}}
		}, false, wantParamOnExact},
		{"unknown applied constructor", func(m *MethodSpec) {
			m.Result.Type = AppliedType("Nope", AccessReadOnly, Param(0))
		}, false, wantUnknownCtor},
		{"applied constructor arity", func(m *MethodSpec) {
			m.Result.Type = AppliedType(TypeList, AccessReadOnly)
		}, false, wantCtorArity},
		{"empty union", func(m *MethodSpec) { m.Result.Type = UnionType() }, false, wantEmptyUnion},
		{"pointer arity", func(m *MethodSpec) {
			m.Result.Type = TypeRef{Kind: RefPointer, Access: AccessReadOnly}
		}, false, wantPointerArity},
		{"pointer access", func(m *MethodSpec) {
			m.Result.Type = TypeRef{Kind: RefPointer, Arguments: []TypeRef{Param(0)}, Access: AccessMode(9)}
		}, false, wantPointerAccess},
		{"constructor access", func(m *MethodSpec) {
			m.Result.Type = TypeRef{Kind: RefApply, Constructor: TypeList, Arguments: []TypeRef{Param(0)}, Access: AccessMode(9)}
		}, false, wantAccessMode},
		{"negative method type argument", func(m *MethodSpec) {
			m.Result.Type = MethodTypeArg(-1)
		}, false, wantMethodArg},
		{"fallible without symbol", func(m *MethodSpec) {
			m.Failure = FailureChecked
			m.RuntimeSymbol = ""
		}, false, wantFallibleSymbol},
		{"unknown allocation", func(m *MethodSpec) { m.Allocation = AllocationMode(9) }, false, wantAllocation},
		{"unknown receiver", func(m *MethodSpec) { m.Receiver = ReceiverMode(9) }, false, wantReceiver},
		{"unknown component", func(m *MethodSpec) { m.Component = "nope" }, false, wantComponent},
		{"whitespace symbol", func(m *MethodSpec) { m.RuntimeSymbol = "hex list push" }, false, wantSymbolSpace},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			method := validMethod
			testCase.mutate(&method)
			registry := []MethodSpec{method}
			if testCase.two {
				// The duplicate-key check needs two records; every other case
				// rejects on the crafted record alone.
				registry = append(registry, method)
			}
			err := validateMethods(registry)
			if err == nil {
				t.Fatalf("validateMethods accepted a malformed record")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validateMethods error = %q, want %q", err, testCase.want)
			}
		})
	}
}

// TestValidateMethodsAcceptsTheExtendedTypeRefs pins the forms the model adds:
// an applied constructor, a structural union, a structural pointer, a
// method-level type argument, and the abstract integer class.
func TestValidateMethodsAcceptsTheExtendedTypeRefs(t *testing.T) {
	extended := []MethodSpec{
		{
			Owner:      ConstructorOwner(TypeList),
			Name:       "slice",
			Parameters: []ParameterSpec{{Name: "start", Type: IntegerType()}},
			Result:     ResultSpec{Type: AppliedType(TypeSlice, AccessReceiver, Param(0))},
			Component:  ComponentList,
		},
		{
			Owner:     ConstructorOwner(TypeChannel),
			Name:      "receive",
			Result:    ResultSpec{Type: UnionType(Param(0), ConcreteType(TypeEoS))},
			Component: ComponentConcurrency,
		},
		{
			Owner:     ConstructorOwner(TypeSlice),
			Name:      "pointer",
			Result:    ResultSpec{Type: UnionType(PointerType(AccessReceiver, Param(0)), ConcreteType(TypeNil))},
			Component: ComponentSlice,
		},
		{
			Owner:         ConstructorOwner(TypeInlineString),
			Name:          "widen",
			Result:        ResultSpec{Type: AppliedType(TypeInlineString, AccessReadOnly, MethodTypeArg(0))},
			RuntimeSymbol: "hex_text_fill",
			Component:     ComponentString,
		},
	}
	if err := validateMethods(extended); err != nil {
		t.Fatalf("validateMethods(extended) = %v, want nil", err)
	}
}

// TestMethodQueriesReturnDefensiveCopies mutates a returned record's nested
// type-reference slice and requires the registry to be unaffected.
func TestMethodQueriesReturnDefensiveCopies(t *testing.T) {
	got, ok := Method(ConstructorOwner(TypeChannel), "receive")
	if !ok {
		t.Fatal("Method(Channel, receive) not found")
	}
	got.Result.Type.Arguments[0] = ConcreteType(TypeBool)
	again, _ := Method(ConstructorOwner(TypeChannel), "receive")
	if again.Result.Type.Arguments[0].TypeID == TypeBool {
		t.Fatal("Method returned a type reference aliasing the registry")
	}
	all := Methods()
	for index := range all {
		if all[index].Name == "receive" && all[index].Owner.Constructor == TypeChannel {
			all[index].Result.Type.Arguments[0] = ConcreteType(TypeBool)
		}
	}
	third, _ := Method(ConstructorOwner(TypeChannel), "receive")
	if third.Result.Type.Arguments[0].TypeID == TypeBool {
		t.Fatal("Methods returned type references aliasing the registry")
	}
}
