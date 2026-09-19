package integration

import (
	"strings"
	"testing"
)

const sharedType = "type Shared is struct mut counter: Atomic<Int32>, end\n"

// A method receiver is a fixed value copy, so a struct that cannot be
// shallow-copied cannot own one. The compiler never turns such a receiver
// into a hidden alias merely because copying it is invalid.
func TestNonCopyableReceiverIsRejected(t *testing.T) {
	assertRejectsAnyDiagnostic(t, sharedType+
		"method Shared.read(): Int32 do\n    return 0\nend\n",
		"method receiver must be shallow-copyable; got Shared")
}

func TestTransitivelyNonCopyableReceiverIsRejected(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Inner is struct mut counter: Atomic<Int32>, end\n"+
			"type Outer is struct mut inner: Inner, end\n"+
			"method Outer.read(): Int32 do\n    return 0\nend\n",
		"method receiver must be shallow-copyable; got Outer")
	// The declaration is rejected on its own terms, so a call that would only
	// ever go through a pointer cannot rescue it.
	assertRejectsAnyDiagnostic(t,
		"type Inner is struct mut counter: Atomic<Int32>, end\n"+
			"type Outer is struct mut inner: Inner, end\n"+
			"method Outer.read(): Int32 do\n    return 0\nend\n"+
			"fun call(target: Ptr<Outer>): Int32 do\n    return target.read()\nend\n",
		"method receiver must be shallow-copyable; got Outer")
}

// A generic receiver is rejected at the specialization that makes it
// non-copyable, not at the open template.
func TestGenericReceiverRejectsNonCopyableSpecialization(t *testing.T) {
	assertChecked(t,
		"type Holder<T> is struct value: T, end\n"+
			"method Holder<T>.get(): T do\n    return self.value\nend\n"+
			"fun demo(): Int32 do\n    let h: Holder<Int32> = Holder<Int32>(value = 3)\n    return h.get()\nend\n")
	assertRejectsAnyDiagnostic(t,
		"type Holder<T> is struct mut value: T, end\n"+
			"method Holder<T>.get(): Int32 do\n    return 0\nend\n"+
			"fun demo(h: Ptr<Holder<Atomic<Int32>>>): Int32 do\n    return h.get()\nend\n",
		"method receiver must be shallow-copyable; got Holder<Atomic<Int32>>")
}

// An explicit free function taking a pointer remains the non-copying way to
// operate on a non-copyable struct.
func TestPointerFunctionRemainsAvailableForNonCopyableStructs(t *testing.T) {
	assertChecked(t, sharedType+
		"fun bump(target: Ptr<mut Shared>) do\n    target.counter.store(1)\nend\n"+
		"fun read(target: Ptr<Shared>): Int32 do\n    return target.counter.load()\nend\n")
}

func TestMethodsOnAnEmptyStructAreAccepted(t *testing.T) {
	assertChecked(t,
		"type Marker is struct end\n"+
			"method Marker.value(): Int32 do\n    return 1\nend\n"+
			"fun demo(): Int32 do\n    let m: Marker = Marker()\n    return m.value()\nend\n")
}

// A transparent alias names the same struct; it never creates a second
// method owner or namespace.
func TestTransparentAliasShareOneMethodNamespace(t *testing.T) {
	assertChecked(t,
		"type Point is struct x: Int32, end\n"+
			"type Coord is Point\n"+
			"method Coord.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(): Int32 do\n    let p: Point = Point(x = 1)\n    return p.read()\nend\n")
	assertRejectsAnyDiagnostic(t,
		"type Point is struct x: Int32, end\n"+
			"type Coord is Point\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"method Coord.read(): Int32 do\n    return self.x\nend\n",
		"already has a method named read")
}

// Autoderef is an access rule, not a capability upgrade: exactly one pointer
// layer is removed, a nullable pointer must be narrowed first, and neither
// mode makes the receiver an alias.
func TestMethodAutoderefRules(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Point is struct x: Int32, end\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(p: Ptr<Ptr<Point>>): Int32 do\n    return p.read()\nend\n",
		"has no method named read")
	assertRejectsAnyDiagnostic(t,
		"type Point is struct x: Int32, end\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(p: Ptr<Point> | Nil): Int32 do\n    return p.read()\nend\n",
		"may be Nil; narrow it before dereferencing")
	assertChecked(t,
		"type Point is struct x: Int32, end\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(p: Ptr<Point> | Nil): Int32 do\n    if p != nil then\n        return p.read()\n    end\n    return 0\nend\n")
}

// A writable pointer never enables mutation through a value-receiver method.
func TestWritablePointerDoesNotMakeAReceiverAnAlias(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Point is struct mut x: Int32, end\n"+
			"method Point.bump() do\n    self.x = self.x + 1\nend\n",
		"cannot assign to read-only member self.x")
}

// Methods are not first-class values.
func TestMethodIsNotAFunValue(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Point is struct x: Int32, end\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(): Fun<(Point) : Int32> do\n    return Point.read\nend\n",
		"Point")
}

// No method definition or prototype is emitted with a pointer receiver; a
// call through a pointer emits the pointee copy instead.
func TestGeneratedMethodsNeverTakeAPointerReceiver(t *testing.T) {
	result := assertCompiles(t,
		"type Point is struct mut x: Int32, mut y: Int32, end\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(reader: Ptr<Point>, writer: Ptr<mut Point>): Int32 do\n    return reader.read() + writer.read()\nend\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Point_read(const hex_t_m3_app_Point hex_v_self)",
		"hex_f_m3_app_Point_read(*hex_v_reader)",
		"hex_f_m3_app_Point_read(*hex_v_writer)",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
	for _, unwanted := range []string{
		"hex_t_m3_app_Point *const hex_v_self",
		"hex_t_m3_app_Point *hex_v_self",
		"const hex_t_m3_app_Point *hex_v_self",
	} {
		if strings.Contains(generated, unwanted) {
			t.Fatalf("modules/app.c = %q, want no pointer-receiver lowering %q", generated, unwanted)
		}
	}
}

// A generic method specializes on the pointee struct, never on the pointer a
// call happened to arrive through: the specialization is keyed by the owner's
// type arguments alone, so a pointer receiver there would fix the receiver
// form for every other call site of the same specialization too.
func TestGenericMethodThroughPointerKeepsAValueReceiver(t *testing.T) {
	result := assertCompiles(t,
		"type Holder<T> is struct mut value: T, end\n"+
			"method Holder<T>.get(): Int32 do\n    return 0\nend\n"+
			"fun through_pointer(h: Ptr<Holder<Int32>>): Int32 do\n    return h.get()\nend\n"+
			"fun by_value(h: Holder<Int32>): Int32 do\n    return h.get()\nend\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Holder_Int32__get(hex_t_m3_app_Holder_Int32_);",
		"static int32_t hex_f_m3_app_Holder_Int32__get(const hex_t_m3_app_Holder_Int32_ hex_v_self)",
		"hex_f_m3_app_Holder_Int32__get(*hex_v_h)",
		"hex_f_m3_app_Holder_Int32__get(hex_v_h)",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
	if strings.Contains(generated, "hex_t_m3_app_Holder_Int32_ *const hex_v_self") {
		t.Fatalf("modules/app.c = %q, want no pointer-receiver specialization", generated)
	}
}
