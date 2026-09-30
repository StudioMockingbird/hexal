package integration

import (
	"strings"
	"testing"
)

const sharedType = "type Shared is struct mut counter: Atomic<Int32>, end\n"

// A struct containing an Atomic owns methods: self is a reference, so nothing
// copies the receiver. The readonly method keeps a non-const receiver because
// atomic operations need a non-const object.
func TestAtomicStructDeclaresAndCallsMethods(t *testing.T) {
	result := assertCompiles(t, sharedType+
		"method Shared.read(): Int32 do\n    return self.counter.load()\nend\n"+
		"method mut Shared.bump() do\n    self.counter.store(1)\nend\n"+
		"let mut shared: Shared = Shared(counter = Atomic<Int32>(0))\n"+
		"shared.bump()\n"+
		"print(shared.read())\n")
	generated := withoutLineDirectives(rootC(t, result))
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Shared_read(hex_t_m3_app_Shared *const hex_v_self)",
		"static void hex_f_m3_app_Shared_bump(hex_t_m3_app_Shared *const hex_v_self)",
		"hex_f_m3_app_Shared_bump(&hex_v_shared);",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("modules/app.c = %q, want %q", generated, want)
		}
	}
}

// An explicit free function taking a pointer remains available for a
// non-copyable struct.
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
// layer is removed and a nullable pointer must be narrowed first.
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

// Methods are not first-class values.
func TestMethodIsNotAFunValue(t *testing.T) {
	assertRejectsAnyDiagnostic(t,
		"type Point is struct x: Int32, end\n"+
			"method Point.read(): Int32 do\n    return self.x\nend\n"+
			"fun demo(): Fun<(Point) : Int32> do\n    return Point.read\nend\n",
		"Point")
}
