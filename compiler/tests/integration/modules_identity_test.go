package integration

import (
	"testing"
)

func TestSameNamedTypesInDifferentModulesAreDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex":    "module Math = import \"./math\"\nmodule Shapes = import \"./shapes\"\nm: Math.Point = 0\ns: Shapes.Point = m\n",
		"math.hex":   "export type Point = Int32\n",
		"shapes.hex": "export type Point = Int32\n",
	}
	// Both aliases resolve to transparent Int32 aliases, so the assignment
	// is legal; the point is that neither resolver leaks the other module.
	result := compileMulti(sources, "app.hex")
	wantMultiSuccess(t, result, "app", "math", "shapes")
}

func TestNominalTypesAcrossModulesStayDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex":    "module Math = import \"./math\"\nmodule Shapes = import \"./shapes\"\nm: Math.Point = Math.make()\ns: Shapes.Point = m\n",
		"math.hex":   "export type Point = { x: Int32, y: Int32 }\nexport fun make(): Point do\n    return Point { x = 1, y = 2 }\nend\n",
		"shapes.hex": "export type Point = { x: Int32, y: Int32 }\n",
	}
	// math.Point and shapes.Point are distinct nominal identities despite
	// identical structure and name: the cross-module assignment fails even
	// though both sides render as "Point".
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "expected Point initializer, got Point")
}

func TestCannotDeclareMethodsForImportedType(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\nimpl Geometry.Point.rotate(): Geometry.Point do\n    return self\nend\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "cannot declare methods for imported type Geometry.Point")
}

func TestCannotDeclareMethodsThroughAliasOfImportedType(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\ntype LocalPoint = Geometry.Point\nimpl LocalPoint.rotate(): LocalPoint do\n    return self\nend\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "cannot declare methods for imported type LocalPoint")
}

func TestMethodCallsOnImportedTypesWork(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\np: Geometry.Point = Geometry.make()\nlength: Int32 = p.length_squared()\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\nexport fun make(): Point do\n    return Point { x = 3, y = 4 }\nend\nexport impl Point.length_squared(): Int32 do\n    return self.x * self.x + self.y * self.y\nend\n",
	}
	wantMultiSuccess(t, compileMulti(sources, "app.hex"), "app", "geometry")
}

func TestPrivateMethodOnExportedTypeRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\np: Geometry.Point = Geometry.make()\nlength: Int32 = p.length_squared()\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\nexport fun make(): Point do\n    return Point { x = 3, y = 4 }\nend\nimpl Point.length_squared(): Int32 do\n    return self.x * self.x + self.y * self.y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "declaration length_squared is private to module geometry")
}

func TestGenericSpecializationsOwnedByDefiningModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\na: Int32 = Math.identity<Int32>(1)\nb: Float64 = Math.identity<Float64>(2.0)\nc: Int32 = Math.identity<Int32>(3)\n",
		"math.hex": "export fun identity<T>(value: T): T do\n    return value\nend\n",
	}
	wantMultiSuccess(t, compileMulti(sources, "app.hex"), "app", "math")
}
