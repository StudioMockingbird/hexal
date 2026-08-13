package types

import "testing"

func TestNilAndUnknownBuiltins(t *testing.T) {
	if Nil.Name != "Nil" || Nil.CName != "nullptr_t" || !IsNil(Nil) {
		t.Fatalf("Nil = %#v, want the canonical nullptr_t singleton", Nil)
	}
	// RFC 0049 item 8.1: Nil keeps its representation but is not canonical as
	// a standalone value type; it is canonical only as a union member.
	if IsCanonical(Nil) || !IsCompleteValue(Nil) {
		t.Fatal("Nil should be a complete non-canonical value type")
	}
	if !Unknown.Incomplete || !IsUnknown(Unknown) || IsCompleteValue(Unknown) {
		t.Fatalf("Unknown = %#v, want an incomplete non-value type", Unknown)
	}
	if IsCanonical(Unknown) {
		t.Fatal("bare Unknown should not be canonical as a value type")
	}

	for _, name := range []string{"Nil", "Unknown"} {
		builtin, ok := Lookup(name)
		if !ok || builtin.Name != name {
			t.Fatalf("Lookup(%q) = %#v, %v", name, builtin, ok)
		}
		if !IsProtectedTypeName(name) {
			t.Fatalf("%s is not a protected type name", name)
		}
	}

	environment := NewEnvironment()
	for _, name := range []string{"Nil", "Unknown"} {
		if _, ok := environment.Lookup(name); !ok {
			t.Fatalf("environment does not contain protected builtin %s", name)
		}
	}
}

func TestNullableTypeIsInternedAndIdempotent(t *testing.T) {
	environment := NewEnvironment()
	base := environment.PtrType(Int32)
	nullable := environment.NullableType(base)

	if nullable == (Type{}) || !IsNullable(nullable) {
		t.Fatalf("NullableType(%s) = %#v, want a nullable type", base.Name, nullable)
	}
	if got, ok := NullableBase(nullable); !ok || !Equal(got, base) {
		t.Fatalf("NullableBase(%s) = %s, %v; want %s, true", nullable.Name, got.Name, ok, base.Name)
	}
	if nullable.Name != "Ptr<Int32> | Nil" || nullable.CName != base.CName {
		t.Fatalf("nullable metadata = %#v, want canonical name and representation", nullable)
	}
	if nullable.Element == nil || !Equal(*nullable.Element, *base.Element) || nullable.PointeeWritable {
		t.Fatalf("nullable pointer metadata = %#v, want the base pointer representation", nullable)
	}
	if !IsPointerLike(nullable) || !IsCompleteValue(nullable) || !IsCanonical(nullable) {
		t.Fatal("nullable pointer should retain pointer-like, complete, canonical semantics")
	}
	if !Equal(nullable, environment.NullableType(base)) || !Equal(nullable, environment.NullableType(nullable)) {
		t.Fatal("nullable interning is not stable or idempotent")
	}

	otherEnvironment := NewEnvironment()
	other := otherEnvironment.NullableType(otherEnvironment.PtrType(Int32))
	if Equal(nullable, other) {
		t.Fatal("nullable identities leaked across compilation environments")
	}
}

func TestNullableTypeRejectsNonPointerLikeBases(t *testing.T) {
	environment := NewEnvironment()
	for _, base := range []Type{Int32, Nil, Unknown} {
		if got := environment.NullableType(base); got != (Type{}) {
			t.Fatalf("NullableType(%s) = %#v, want zero Type", base.Name, got)
		}
	}
}

func TestUnknownPointerTypesAreCanonicalCompleteAndPointerLike(t *testing.T) {
	environment := NewEnvironment()
	for _, pointer := range []Type{
		environment.PtrType(Unknown),
		environment.MutPtrType(Unknown),
	} {
		if pointer == (Type{}) || pointer.Element == nil || !IsUnknown(*pointer.Element) {
			t.Fatalf("Unknown pointer = %#v, want Unknown as its immediate pointee", pointer)
		}
		if !IsPointerLike(pointer) || !IsCompleteValue(pointer) || !IsCanonical(pointer) {
			t.Fatalf("Unknown pointer %s is not a canonical complete pointer value", pointer.Name)
		}
		nullable := environment.NullableType(pointer)
		if nullable == (Type{}) || !IsNullable(nullable) || !IsCanonical(nullable) {
			t.Fatalf("nullable Unknown pointer %s is not canonical", nullable.Name)
		}
	}
	if got := environment.PtrType(Unknown); got.Name != "Ptr<Unknown>" || got.CName != "void*" {
		t.Fatalf("Ptr<Unknown> = %#v, want void* representation", got)
	}
	if got := environment.MutPtrType(Unknown); got.Name != "MutPtr<Unknown>" || got.CName != "void*" {
		t.Fatalf("MutPtr<Unknown> = %#v, want void* representation", got)
	}
}

func TestNullableFunctionSignaturesAreCanonical(t *testing.T) {
	environment := NewEnvironment()
	base := environment.PtrType(Int32)
	nullable := environment.NullableType(base)
	result := nullable
	signature := environment.FunType([]Type{nullable}, &result)
	if signature == (Type{}) || signature.Signature == nil || !IsCanonical(signature) {
		t.Fatalf("FunType with nullable parameter/result = %#v, want canonical signature", signature)
	}

	nullableFunction := environment.NullableType(signature)
	if !IsNullable(nullableFunction) || !IsPointerLike(nullableFunction) || !IsCanonical(nullableFunction) {
		t.Fatalf("nullable function signature = %#v, want canonical nullable function pointer", nullableFunction)
	}
	if nullableFunction.Signature != signature.Signature {
		t.Fatal("nullable function did not preserve signature metadata")
	}
	if !Equal(nullableFunction, environment.NullableType(signature)) || !Equal(nullableFunction, environment.NullableType(nullableFunction)) {
		t.Fatal("nullable function interning is not stable or idempotent")
	}
}

func TestNullableCanonicalValidationPreservesRecursiveObjects(t *testing.T) {
	environment := NewEnvironment()
	node := environment.BeginObject("Node", 1, 1)
	next := environment.NullableType(environment.MutPtrType(node))
	node = environment.CompleteObject("Node", []ObjectMember{{Name: "next", Type: next}})
	if !IsCanonical(node) {
		t.Fatal("recursive object with a nullable pointer member is not canonical")
	}

	forged := next
	forged.CName = "forged_t*"
	if IsCanonical(forged) {
		t.Fatal("forged nullable metadata passed canonical validation")
	}
}

func TestAssignableHandlesIdentityWeakeningAndNullableInjection(t *testing.T) {
	environment := NewEnvironment()
	readOnly := environment.PtrType(Int32)
	writable := environment.MutPtrType(Int32)
	nullable := environment.NullableType(readOnly)

	for _, testCase := range []struct {
		name   string
		target Type
		source Type
		want   bool
	}{
		{name: "identity", target: readOnly, source: readOnly, want: true},
		{name: "pointer weakening", target: readOnly, source: writable, want: true},
		{name: "pointer injection", target: nullable, source: writable, want: true},
		{name: "nil injection", target: nullable, source: Nil, want: true},
		{name: "nullable removal", target: readOnly, source: nullable, want: false},
		{name: "read-only strengthening", target: writable, source: readOnly, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Assignable(testCase.target, testCase.source); got != testCase.want {
				t.Fatalf("Assignable(%s, %s) = %v, want %v", testCase.target.Name, testCase.source.Name, got, testCase.want)
			}
		})
	}
}

func TestAssignableHandlesOneLayerUnknownConversions(t *testing.T) {
	environment := NewEnvironment()
	readOnly := environment.PtrType(Int32)
	writable := environment.MutPtrType(Int32)
	erasedReadOnly := environment.PtrType(Unknown)
	erasedWritable := environment.MutPtrType(Unknown)
	readOnlyNullable := environment.NullableType(readOnly)
	writableNullable := environment.NullableType(writable)
	erasedNullable := environment.NullableType(erasedReadOnly)

	for _, testCase := range []struct {
		name   string
		target Type
		source Type
		want   bool
	}{
		{name: "erase read access", target: erasedReadOnly, source: readOnly, want: true},
		{name: "erase write access", target: erasedWritable, source: writable, want: true},
		{name: "erase and weaken access", target: erasedReadOnly, source: writable, want: true},
		{name: "recover read access", target: readOnly, source: erasedReadOnly, want: true},
		{name: "recover write access", target: writable, source: erasedWritable, want: true},
		{name: "recover and weaken access", target: readOnly, source: erasedWritable, want: true},
		{name: "nullable erasure", target: environment.NullableType(erasedReadOnly), source: readOnlyNullable, want: true},
		{name: "nullable recovery", target: readOnlyNullable, source: erasedNullable, want: true},
		{name: "nullable weakening", target: readOnlyNullable, source: writableNullable, want: true},
		{name: "nullable erasure from writable", target: environment.NullableType(erasedReadOnly), source: writable, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Assignable(testCase.target, testCase.source); got != testCase.want {
				t.Fatalf("Assignable(%s, %s) = %v, want %v", testCase.target.Name, testCase.source.Name, got, testCase.want)
			}
		})
	}
}

func TestAssignableRejectsUnknownCompositionAndNestedSlots(t *testing.T) {
	environment := NewEnvironment()
	readOnlyInt8 := environment.PtrType(Int8)
	readOnlyInt64 := environment.PtrType(Int64)
	writableInt8 := environment.MutPtrType(Int8)
	writableInt64 := environment.MutPtrType(Int64)
	nestedInt8 := environment.MutPtrType(writableInt8)
	nestedUnknown := environment.MutPtrType(environment.MutPtrType(Unknown))

	for _, testCase := range []struct {
		name   string
		target Type
		source Type
	}{
		{name: "read access strengthening", target: environment.MutPtrType(Int32), source: environment.PtrType(Int32)},
		{name: "recovery access strengthening", target: environment.MutPtrType(Int32), source: environment.PtrType(Unknown)},
		{name: "erasure and recovery do not compose", target: writableInt64, source: writableInt8},
		{name: "nested slot erasure", target: nestedUnknown, source: nestedInt8},
		{name: "nullable removal", target: readOnlyInt8, source: environment.NullableType(readOnlyInt8)},
		{name: "nil removal", target: readOnlyInt64, source: Nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if Assignable(testCase.target, testCase.source) {
				t.Fatalf("Assignable(%s, %s) = true, want false", testCase.target.Name, testCase.source.Name)
			}
		})
	}
}
