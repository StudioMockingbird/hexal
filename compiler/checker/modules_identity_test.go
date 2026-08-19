package checker

// Module identity in nominal types, implementation ownership,
// cross-module method calls, and generic specialization ownership.

import (
	"testing"

	compilerTypes "hexal/compiler/types"
)

// An impl of an imported type is rejected at the receiver: only the type's
// defining module may declare its methods. The diagnostic uses the qualified
// receiver spelling as written.
func TestImplRejectsImportedType(t *testing.T) {
	_, err := checkModules(t,
		"module Geometry = import \"./math\"\nimpl Geometry.Point.rotate(): Int32 do\n    return 0\nend\n",
		"export type Point = { x: Int32, y: Int32 }\n")
	requireMessage(t, err, "cannot declare methods for imported type Geometry.Point")
}

// A transparent alias of an imported type resolves to the imported identity,
// so declaring methods through the alias is the same ownership failure; the
// diagnostic uses the alias spelling.
func TestImplRejectsImportedTypeThroughAlias(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\ntype P = Math.Point\nimpl P.rotate(): Int32 do\n    return 0\nend\n",
		"export type Point = { x: Int32, y: Int32 }\n")
	requireMessage(t, err, "cannot declare methods for imported type P")
}

// The defining module keeps declaring methods for its own type, including
// through its own aliases, exactly as before.
func TestImplOwnModuleTypeStillDeclaresMethods(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\n",
		"export type Point = { x: Int32, y: Int32 }\nexport impl Point.length_squared(): Int32 do\n    return self.x * self.x\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the defining module's own impl: %v", err)
	}
	declaration := checked["math.hex"].Statements[0].(MethodDeclaration)
	if declaration.Object == nil || declaration.Object.Name != "Point" {
		t.Fatalf("statement = %#v, want length_squared on Point", checked["math.hex"].Statements[0])
	}
}

// A method call on an imported exported type routes to the defining module's
// recorded methods and checks against its resolved signature.
func TestImportedMethodCallResolvesExportedMethod(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\np: Math.Point := Math.origin()\narea: Int32 := p.length_squared()\n",
		"export type Point = { x: Int32, y: Int32 }\nexport impl Point.length_squared(): Int32 do\n    return self.x * self.x + self.y * self.y\nend\nexport fun origin(): Point do\n    return Point { x = 3, y = 4 }\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the imported method call: %v", err)
	}
	declaration := checked["app.hex"].Statements[1].(Declaration)
	if declaration.Type != compilerTypes.Int32 || declaration.Source.Node.Kind != MethodCallExpression {
		t.Fatalf("app statement = %#v, want an Int32 method call", checked["app.hex"].Statements[1])
	}
	node := declaration.Source.Node
	if node.Name != "length_squared" || node.Owner == nil || node.Owner.Name != "Point" {
		t.Fatalf("call node = %#v, want length_squared on Point", node)
	}
}

// A private method is invisible cross-module: the call is the visibility
// failure at the method, even when the receiver type is exported.
func TestImportedMethodCallRejectsPrivateMethod(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\np: Math.Point := Math.origin()\narea: Int32 := p.length_squared()\n",
		"export type Point = { x: Int32, y: Int32 }\nimpl Point.length_squared(): Int32 do\n    return self.x\nend\nexport fun origin(): Point do\n    return Point { x = 3, y = 4 }\nend\n")
	requireMessage(t, err, "declaration length_squared is private to module math")
}

// A specialization of an imported module's exported generic is recorded with
// the defining module: math's checked output carries every requested
// specialization, sorted by declaration name then canonical argument
// signature, and the requesting module's output carries none of them.
func TestImportedGenericSpecializationsLandInDefiningModule(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\na: Int32 := Math.identity<Int32>(1)\nb: Float64 := Math.identity<Float64>(2.0)\nc: Int32 := Math.identity<Int32>(3)\n",
		"export fun identity<T>(value: T): T do\n    return value\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the imported generic calls: %v", err)
	}
	specialized := checked["math.hex"].SpecializedFunctions
	if len(specialized) != 2 {
		t.Fatalf("math specializations = %d, want 2", len(specialized))
	}
	// Deterministic order: the specialization key sorts by declaration name,
	// then the canonical argument signature string.
	if specialized[0].Name != "identity_Float64" || specialized[1].Name != "identity_Int32" {
		t.Fatalf("math specializations = %#v, want identity_Float64 then identity_Int32", specialized)
	}
	if len(checked["app.hex"].SpecializedFunctions) != 0 {
		t.Fatalf("app specializations = %d, want 0 (the defining module owns them)", len(checked["app.hex"].SpecializedFunctions))
	}
	// The checked call mirrors a local generic call, with the target module
	// id stamped on the callee.
	call := checked["app.hex"].Statements[0].(Declaration).Source.Node
	if call.Kind != CallExpression || call.Operand == nil || call.Operand.Name != "identity_Int32" || call.Operand.Module != "math" {
		t.Fatalf("source node = %#v, want qualified call to math's identity_Int32", call)
	}
}

// Repeated requests of one (declaration, argument) set reuse the one
// specialization: interleaved requests in different source order still yield
// exactly one entry in the defining module's output.
func TestImportedGenericRepeatedRequestDeduplicates(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\na: Int32 := Math.identity<Int32>(1)\nb: Float64 := Math.identity<Float64>(2.0)\nc: Int32 := Math.identity<Int32>(3)\nd: Float64 := Math.identity<Float64>(4.0)\n",
		"export fun identity<T>(value: T): T do\n    return value\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the imported generic calls: %v", err)
	}
	specialized := checked["math.hex"].SpecializedFunctions
	if len(specialized) != 2 {
		t.Fatalf("math specializations = %d, want 2 (one per distinct argument set)", len(specialized))
	}
	seen := make(map[string]bool, len(specialized))
	for _, declaration := range specialized {
		if seen[declaration.Name] {
			t.Fatalf("math specializations = %#v, contain a duplicate %s", specialized, declaration.Name)
		}
		seen[declaration.Name] = true
	}
}
