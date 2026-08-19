package types

import "testing"

func TestDiagnosticFormatsCategoryBeforeDescription(t *testing.T) {
	diagnostic := Diagnostic{
		Category: SyntaxError,
		Line:     2,
		Column:   3,
		Message:  "expected an identifier",
	}

	if got, want := diagnostic.Error(), "[Syntax Error] expected an identifier at 2:3"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

// A stamped diagnostic qualifies its position with the module, so two modules'
// messages are distinguishable and never read as one interleaved list. Stamping
// is idempotent: an inner stage's attribution survives an outer stage's stamp.
func TestDiagnosticQualifiesPositionWithItsModule(t *testing.T) {
	diagnostic := Diagnostic{Category: TypeError, Line: 5, Column: 3, Message: "mismatch"}
	stamped := diagnostic.InModule("graphics/shapes.hex")
	if got, want := stamped.Error(), "[Type Error] mismatch at graphics/shapes.hex:5:3"; got != want {
		t.Fatalf("stamped diagnostic = %q, want %q", got, want)
	}
	if got := stamped.InModule("other.hex").Module; got != "graphics/shapes.hex" {
		t.Fatalf("re-stamped module = %q, want the first attribution to win", got)
	}
	if got := diagnostic.Module; got != "" {
		t.Fatalf("InModule mutated its receiver: module := %q", got)
	}
	set := Diagnostics{diagnostic, {Category: NameError, Line: 1, Column: 1, Message: "unknown"}}.InModule("app.hex")
	for _, entry := range set {
		if entry.Module != "app.hex" {
			t.Fatalf("set entry %q was not stamped", entry.Message)
		}
	}
}

// An empty category renders visibly as "[]" rather than being masked as a
// compiler Unknown Error; omitting the category at a construction site must
// surface the defect, never a user error wearing the compiler's label.
func TestDiagnosticErrorNeverMasksEmptyCategory(t *testing.T) {
	diagnostic := Diagnostic{Message: "value without a category"}
	rendered := diagnostic.Error()
	if got, want := rendered, "[] value without a category"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

func TestPtrTypeIsInterned(t *testing.T) {
	environment := NewEnvironment()
	first := environment.PtrType(Int32)
	second := environment.PtrType(Int32)

	if first != second {
		t.Fatalf("PtrType(Int32) values differ: %#v != %#v", first, second)
	}
	if first.Name != "Ptr<Int32>" {
		t.Fatalf("pointer name = %q, want %q", first.Name, "Ptr<Int32>")
	}
	if first.CName != "int32_t*" {
		t.Fatalf("pointer C name = %q, want %q", first.CName, "int32_t*")
	}
	if first.Element == nil || first.Element.Name != Int32.Name || first.Element.CName != Int32.CName {
		t.Fatalf("pointer element = %#v, want canonical Int32", first.Element)
	}
}

func TestPointerInterningIsCompilationScoped(t *testing.T) {
	first := NewEnvironment().PtrType(Int32)
	second := NewEnvironment().PtrType(Int32)

	if first == second {
		t.Fatal("pointer identities leaked across compilation environments")
	}
}

func TestPtrTypesAreNotBuiltins(t *testing.T) {
	if _, ok := Lookup("Ptr<Int32>"); ok {
		t.Fatal("Lookup resolved a constructed pointer type as a builtin")
	}
}

func TestPtrAndMutPtrAreDistinctTypes(t *testing.T) {
	environment := NewEnvironment()
	readOnly := environment.PtrType(Int32)
	writable := environment.MutPtrType(Int32)
	if Equal(readOnly, writable) {
		t.Fatal("Ptr<Int32> and MutPtr<Int32> share a canonical identity")
	}
	if readOnly.Name != "Ptr<Int32>" || writable.Name != "MutPtr<Int32>" {
		t.Fatalf("pointer names = %q/%q, want Ptr<Int32>/MutPtr<Int32>", readOnly.Name, writable.Name)
	}
	if readOnly.PointeeWritable || !writable.PointeeWritable {
		t.Fatalf("PointeeWritable = %v/%v, want false/true", readOnly.PointeeWritable, writable.PointeeWritable)
	}
	if readOnly.CName != "int32_t*" || writable.CName != "int32_t*" {
		t.Fatalf("pointer C names = %q/%q, want shared int32_t*", readOnly.CName, writable.CName)
	}
}

func TestPtrAndMutPtrAreEachInterned(t *testing.T) {
	environment := NewEnvironment()
	if environment.PtrType(Int32) != environment.PtrType(Int32) {
		t.Fatal("PtrType(Int32) was not interned")
	}
	if environment.MutPtrType(Int32) != environment.MutPtrType(Int32) {
		t.Fatal("MutPtrType(Int32) was not interned")
	}
}

func TestPointerConstructorsRejectForgedElementsWithoutPoisoningCache(t *testing.T) {
	environment := NewEnvironment()
	forgedInt32 := Int32
	forgedInt32.CName = "forged_int_t"

	if got := environment.PtrType(forgedInt32); got != (Type{}) {
		t.Fatalf("PtrType(forgedInt32) = %#v, want zero Type", got)
	}
	if got := environment.PtrType(Int32); got != environment.PtrType(Int32) || got.CName != "int32_t*" {
		t.Fatalf("PtrType(Int32) = %#v, want canonical interned pointer", got)
	}
	if got := environment.MutPtrType(forgedInt32); got != (Type{}) {
		t.Fatalf("MutPtrType(forgedInt32) = %#v, want zero Type", got)
	}
	if got := environment.MutPtrType(Int32); got != environment.MutPtrType(Int32) || got.CName != "int32_t*" {
		t.Fatalf("MutPtrType(Int32) = %#v, want canonical interned pointer", got)
	}
}

func TestPtrAndMutPtrInterningIsCompilationScoped(t *testing.T) {
	first := NewEnvironment().MutPtrType(Int32)
	second := NewEnvironment().MutPtrType(Int32)
	if first == second {
		t.Fatal("writable pointer identities leaked across compilation environments")
	}
}

// Fun<…> identity is the ordered parameter types plus the presence and type
// of a result.
func TestFunTypeIsInterned(t *testing.T) {
	environment := NewEnvironment()
	result := Int32
	first := environment.FunType([]Type{Int32}, &result)
	second := environment.FunType([]Type{Int32}, &result)

	if !Equal(first, second) {
		t.Fatalf("FunType([Int32], Int32) values differ: %#v != %#v", first, second)
	}
	if first.CName != "" {
		t.Fatalf("fun C name = %q, want empty; the generator builds the declarator", first.CName)
	}
	if first.Signature == nil || len(first.Signature.Parameters) != 1 || first.Signature.Result == nil {
		t.Fatalf("fun signature = %#v, want one parameter and a result", first.Signature)
	}
}

func TestFunTypeNamesMatchSourceSpelling(t *testing.T) {
	environment := NewEnvironment()
	result := Int32
	counter := environment.MutPtrType(environment.BeginObject("Counter", 1, 1))

	for _, testCase := range []struct {
		parameters []Type
		result     *Type
		want       string
	}{
		{parameters: []Type{Int32, Int32}, result: &result, want: "Fun<(Int32, Int32) : Int32>"},
		{parameters: nil, result: &result, want: "Fun<() : Int32>"},
		{parameters: []Type{counter}, want: "Fun<(MutPtr<Counter>)>"},
		{parameters: []Type{Int32}, want: "Fun<(Int32)>"},
		{want: "Fun<()>"},
	} {
		t.Run(testCase.want, func(t *testing.T) {
			if got := environment.FunType(testCase.parameters, testCase.result); got.Name != testCase.want {
				t.Fatalf("fun name = %q, want %q", got.Name, testCase.want)
			}
		})
	}
}

func TestFunTypeIdentityDistinguishesSignatures(t *testing.T) {
	environment := NewEnvironment()
	result := Int32

	if Equal(environment.FunType([]Type{Int32}, nil), environment.FunType([]Type{Int32}, &result)) {
		t.Fatal("Fun<(Int32)> and Fun<(Int32) : Int32> share a canonical identity")
	}
	if Equal(environment.FunType([]Type{Int32, Int64}, nil), environment.FunType([]Type{Int64, Int32}, nil)) {
		t.Fatal("Fun<…> identity ignores parameter order")
	}
	if Equal(environment.FunType(nil, nil), environment.FunType([]Type{Int32}, nil)) {
		t.Fatal("Fun<()> and Fun<(Int32)> share a canonical identity")
	}
	if !Equal(environment.FunType(nil, nil), environment.FunType([]Type{}, nil)) {
		t.Fatal("Fun<()> was not interned across nil and empty parameter slices")
	}
}

func TestFunInterningIsCompilationScoped(t *testing.T) {
	result := Int32
	first := NewEnvironment().FunType([]Type{Int32}, &result)
	second := NewEnvironment().FunType([]Type{Int32}, &result)

	if Equal(first, second) {
		t.Fatal("fun identities leaked across compilation environments")
	}
}

func TestNestedFunTypeIsInterned(t *testing.T) {
	environment := NewEnvironment()
	result := Int32
	inner := environment.FunType([]Type{Int32}, &result)
	outer := environment.FunType([]Type{inner}, &result)

	if !Equal(outer, environment.FunType([]Type{environment.FunType([]Type{Int32}, &result)}, &result)) {
		t.Fatal("Fun<(Fun<(Int32) : Int32>) : Int32> was not interned")
	}
	if outer.Name != "Fun<(Fun<(Int32) : Int32>) : Int32>" {
		t.Fatalf("nested fun name = %q", outer.Name)
	}
	if !IsCanonical(outer) {
		t.Fatal("nested fun type is not canonical")
	}
}

func TestFunTypeRejectsForgedParameters(t *testing.T) {
	environment := NewEnvironment()
	forgedInt32 := Int32
	forgedInt32.CName = "forged_int_t"

	if got := environment.FunType([]Type{forgedInt32}, nil); got != (Type{}) {
		t.Fatalf("FunType([forgedInt32]) = %#v, want zero Type", got)
	}
	if got := environment.FunType([]Type{Int32}, &forgedInt32); got != (Type{}) {
		t.Fatalf("FunType([Int32], forgedInt32) = %#v, want zero Type", got)
	}
	if got := environment.FunType([]Type{Int32}, nil); !Equal(got, environment.FunType([]Type{Int32}, nil)) {
		t.Fatalf("FunType([Int32]) = %#v, want canonical interned fun", got)
	}
}

func TestFunIsProtectedTypeName(t *testing.T) {
	if !IsProtectedTypeName("Fun") {
		t.Fatal("Fun is not a protected type name")
	}
}

func TestIsCanonicalTypes(t *testing.T) {
	environment := NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []ObjectMember{{Name: "x", Type: Int32}})
	environment.DeclareAlias("Coordinate", point)
	alias, _ := environment.Lookup("Coordinate")

	node := environment.BeginObject("Node", 1, 1)
	nodePointer := environment.MutPtrType(node)
	node = environment.CompleteObject("Node", []ObjectMember{{Name: "next", Type: nodePointer}})

	pointer := environment.PtrType(Int32)
	nestedPointer := environment.PtrType(environment.MutPtrType(pointer))
	forgedPointer := pointer
	forgedElement := *pointer.Element
	forgedElement.CName = "forged_int_t"
	forgedPointer.Element = &forgedElement

	forgedObject := point
	forgedObjectData := *point.Object
	forgedObjectData.Members = append([]ObjectMember(nil), point.Object.Members...)
	forgedObjectData.Members[0].Type.CName = "forged_int_t"
	forgedObject.Object = &forgedObjectData

	for _, testCase := range []struct {
		name string
		typ  Type
		want bool
	}{
		{name: "scalar", typ: Int32, want: true},
		{name: "alias", typ: alias, want: true},
		{name: "object", typ: point, want: true},
		{name: "recursive object", typ: node, want: true},
		{name: "pointer", typ: pointer, want: true},
		{name: "nested pointer", typ: nestedPointer, want: true},
		{name: "forged object", typ: forgedObject, want: false},
		{name: "forged pointer", typ: forgedPointer, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsCanonical(testCase.typ); got != testCase.want {
				t.Fatalf("IsCanonical(%s) = %v, want %v", testCase.name, got, testCase.want)
			}
		})
	}
}

// Truthiness is decided by the type's category: false and nil are falsey;
// every other complete value, including zero, is truthy.
func TestTruthiness(t *testing.T) {
	environment := NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []ObjectMember{{Name: "x", Type: Int32}})
	for _, testCase := range []struct {
		name     string
		typ      Type
		expected TruthinessKind
	}{
		{name: "bool", typ: Bool, expected: TruthinessBool},
		{name: "nil", typ: Nil, expected: TruthinessNil},
		{name: "integer", typ: Int32, expected: TruthinessAlwaysTrue},
		{name: "float", typ: Float64, expected: TruthinessAlwaysTrue},
		{name: "pointer", typ: environment.PtrType(Int32), expected: TruthinessAlwaysTrue},
		{name: "object", typ: point, expected: TruthinessAlwaysTrue},
		{name: "nullable pointer", typ: environment.NullableType(environment.PtrType(Int32)), expected: TruthinessNullable},
		{name: "unknown", typ: Unknown, expected: TruthinessInvalid},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := Truthiness(testCase.typ); actual != testCase.expected {
				t.Fatalf("Truthiness(%v) = %v, want %v", testCase.typ, actual, testCase.expected)
			}
		})
	}
}

func TestHeapBuiltinType(t *testing.T) {
	if Heap.Name != "Heap" || Heap.CName != "hex_heap" || Heap.Incomplete {
		t.Fatalf("Heap = %#v, want complete builtin", Heap)
	}
	if !IsCompleteValue(Heap) || !IsCanonical(Heap) {
		t.Fatal("Heap must be a canonical complete value type")
	}
	if !IsHeap(Heap) || IsHeap(Int32) {
		t.Fatal("IsHeap misclassifies Heap")
	}
}
