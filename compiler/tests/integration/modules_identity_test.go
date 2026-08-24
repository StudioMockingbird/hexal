package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestSameNamedTypesInDifferentModulesAreDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex":    "module Math = import \"./math\"\nmodule Shapes = import \"./shapes\"\nm: Math.Point := 0\ns: Shapes.Point := m\n",
		"math.hex":   "export type Point = Int32\n",
		"shapes.hex": "export type Point = Int32\n",
	}
	// Both aliases resolve to transparent Int32 aliases, so the assignment
	// is legal; the point is that neither resolver leaks the other module.
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "math", "shapes")
}

func TestNominalTypesAcrossModulesStayDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex":    "module Math = import \"./math\"\nmodule Shapes = import \"./shapes\"\nm: Math.Point := Math.make()\ns: Shapes.Point := m\n",
		"math.hex":   "export type Point = { x: Int32, y: Int32 }\nexport fun make(): Point do\n    return Point { x = 1, y = 2 }\nend\n",
		"shapes.hex": "export type Point = { x: Int32, y: Int32 }\n",
	}
	// math.Point and shapes.Point are distinct nominal identities despite
	// identical structure and name: the cross-module assignment fails even
	// though both sides render as "Point".
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "expected Point initializer; got Point")
}

func TestCannotDeclareMethodsForImportedType(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\nimpl Geometry.Point.rotate(): Geometry.Point do\n    return self\nend\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "cannot declare methods for imported type Geometry.Point")
}

func TestCannotDeclareMethodsThroughAliasOfImportedType(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\ntype LocalPoint = Geometry.Point\nimpl LocalPoint.rotate(): LocalPoint do\n    return self\nend\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "cannot declare methods for imported type LocalPoint")
}

func TestMethodCallsOnImportedTypesWork(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\np: Geometry.Point := Geometry.make()\nlength: Int32 := p.length_squared()\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\nexport fun make(): Point do\n    return Point { x = 3, y = 4 }\nend\nexport impl Point.length_squared(): Int32 do\n    return (self.x * self.x) + (self.y * self.y)\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "geometry")
}

func TestPrivateMethodOnExportedTypeRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":      "module Geometry = import \"./geometry\"\np: Geometry.Point := Geometry.make()\nlength: Int32 := p.length_squared()\n",
		"geometry.hex": "export type Point = { x: Int32, y: Int32 }\nexport fun make(): Point do\n    return Point { x = 3, y = 4 }\nend\nimpl Point.length_squared(): Int32 do\n    return (self.x * self.x) + (self.y * self.y)\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "declaration length_squared is private to module geometry")
}

func TestGenericSpecializationsOwnedByDefiningModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\na: Int32 := Math.identity<Int32>(1)\nb: Float64 := Math.identity<Float64>(2.0)\nc: Int32 := Math.identity<Int32>(3)\n",
		"math.hex": "export fun identity<T>(value: T): T do\n    return value\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

// Two modules exporting same-named types with different layouts still keep
// distinct canonical identities inside the builtin generics: every container
// family must emit two specializations, each sized from its own element, and
// never collapse onto one C type.
func TestSameNamedTypesProduceDistinctContainerSpecializations(t *testing.T) {
	sources := map[string]string{
		"m.hex": "export type Point = { x: Int32 }\nexport fun point(): Point do\n    return Point { x = 1 }\nend\n",
		"s.hex": "export type Point = { y: Int64, z: Int64 }\nexport fun point(): Point do\n    return Point { y = 1, z = 2 }\nend\n",
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nfun demo(h: Heap) do\n" +
			"    lm: List<M.Point> := List<M.Point>.new(h)\n" +
			"    ls: List<S.Point> := List<S.Point>.new(h)\n" +
			"    dm: Dict<Int32, M.Point> := Dict<Int32, M.Point>.new(h)\n" +
			"    ds: Dict<Int32, S.Point> := Dict<Int32, S.Point>.new(h)\n" +
			"    pm: M.Point := M.point()\n" +
			"    ps: S.Point := S.point()\n" +
			"    am: Array<M.Point, 2> := [pm, pm]\n" +
			"    arr_s: Array<S.Point, 2> := [ps, ps]\n" +
			"    vm: View<M.Point> := am.slice(0, 1)\n" +
			"    vs: View<S.Point> := arr_s.slice(0, 1)\n" +
			"end\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "m", "s")
	header := result.Files["modules/app.h"]
	if strings.Count(header, "typedef struct hex_list_Point") != 2 {
		t.Fatalf("modules/app.h has %d List<Point> typedefs, want 2", strings.Count(header, "typedef struct hex_list_Point"))
	}
	if !strings.Contains(header, "hex_list_Point_m1_") {
		t.Fatalf("modules/app.h %v, want a module-qualified List typedef alongside the base name", header)
	}
	if !strings.Contains(header, "sizeof(hex_t_m1_m_Point)") || !strings.Contains(header, "sizeof(hex_t_m1_s_Point)") {
		t.Fatalf("modules/app.h sizes %v, want one sizeof per element layout", header)
	}
	if strings.Count(header, "typedef struct hex_dict_Int32_Point") != 2 {
		t.Fatalf("modules/app.h has %d Dict<Int32, Point> typedefs, want 2", strings.Count(header, "typedef struct hex_dict_Int32_Point"))
	}
	if !strings.Contains(header, "hex_dict_Int32_Point_m1_") {
		t.Fatalf("modules/app.h %v, want a module-qualified Dict typedef alongside the base name", header)
	}
	if strings.Count(header, "typedef struct hex_array_Point_2") != 2 {
		t.Fatalf("modules/app.h has %d Array<Point, 2> typedefs, want 2", strings.Count(header, "typedef struct hex_array_Point_2"))
	}
	if !strings.Contains(header, "hex_array_Point_2_m1_") {
		t.Fatalf("modules/app.h %v, want a module-qualified Array typedef alongside the base name", header)
	}
	if strings.Count(header, "typedef struct hex_view_Point") != 2 {
		t.Fatalf("modules/app.h has %d View<Point> typedefs, want 2", strings.Count(header, "typedef struct hex_view_Point"))
	}
	if !strings.Contains(header, "hex_view_Point_m1_") {
		t.Fatalf("modules/app.h %v, want a module-qualified View typedef alongside the base name", header)
	}
}

// Identity is nominal, not structural: same-named types with identical
// layouts are still two distinct element types, so their containers stay two
// specializations.
func TestIdenticalLayoutStillNominalDistinctAcrossModules(t *testing.T) {
	sources := map[string]string{
		"m.hex":   "export type Point = { x: Int32, y: Int32 }\n",
		"s.hex":   "export type Point = { x: Int32, y: Int32 }\n",
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nfun demo(h: Heap) do\n    a: List<M.Point> := List<M.Point>.new(h)\n    b: List<S.Point> := List<S.Point>.new(h)\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "m", "s")
	list := result.Files["modules/app.h"]
	if strings.Count(list, "typedef struct hex_list_Point") != 2 {
		t.Fatalf("modules/app.h has %d List<Point> typedefs, want 2 (identical layouts still nominal)", strings.Count(list, "typedef struct hex_list_Point"))
	}
}

// Same-named types as union members keep both payload spellings in the one
// discriminated union, so each value carries its own module's layout.
func TestSameNamedTypeUnionMembersStayDistinct(t *testing.T) {
	sources := map[string]string{
		"m.hex": "export type Point = { x: Int32 }\nexport fun point(): Point do\n    return Point { x = 1 }\nend\n",
		"s.hex": "export type Point = { y: Int64, z: Int64 }\nexport fun point(): Point do\n    return Point { y = 1, z = 2 }\nend\n",
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nfun demo() do\n" +
			"    pm: M.Point := M.point()\n" +
			"    u: (M.Point | S.Point) := pm\n" +
			"end\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "m", "s")
	app := result.Files["modules/app.h"]
	if !strings.Contains(app, "hex_t_m1_m_Point hex_m_m1_m_Point;") || !strings.Contains(app, "hex_t_m1_s_Point hex_m_m1_s_Point;") {
		t.Fatalf("modules/app.h %v, want both same-named Point payload spellings in the union", app)
	}
}

// Control group: one module with one same-named type yields exactly one
// specialization and no module-qualified suffix.
func TestSingleModuleProducesSingleSpecialization(t *testing.T) {
	sources := map[string]string{
		"app.hex": "type Point = { x: Int32, y: Int32 }\nfun demo(h: Heap) do\n    a: List<Point> := List<Point>.new(h)\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app")
	list := result.Files["modules/app.h"]
	if strings.Count(list, "typedef struct hex_list_Point") != 1 {
		t.Fatalf("modules/app.h has %d List<Point> typedefs, want exactly 1", strings.Count(list, "typedef struct hex_list_Point"))
	}
	if strings.Contains(list, "_m1_") {
		t.Fatalf("modules/app.h %v, single-module control must carry no qualifier", list)
	}
}

// The same builtin generic identity is shared across module boundaries: a
// container built in one module type-checks in another as a parameter, a
// return, an object member, and a generic argument. Same-named collision
// cases run in the same compilation.
func TestBuiltinGenericIdentitySharedAcrossModules(t *testing.T) {
	sources := map[string]string{
		"m.hex": "export type Point = { x: Int32 }\n",
		"s.hex": "export type Point = { y: Int64, z: Int64 }\n",
		"lib.hex": "export fun take_list(v: List<Int32>): Nil | Error do\n    return nil\nend\n" +
			"export fun take_dict(v: Dict<Int32, Int32>): Nil | Error do\n    return nil\nend\n" +
			"export fun take_array(v: Array<Int32, 2>): Int32 do\n    return v[0]\nend\n" +
			"export fun take_view(v: View<Int32>): Int32 do\n    return v[0]\nend\n" +
			"export type Holder = { values: List<Int32> }\n" +
			"export fun make_holder(values: List<Int32>): Holder do\n    return Holder { values = values }\nend\n" +
			"export fun take_holder(h: Holder): Nil | Error do\n    return nil\nend\n" +
			"export fun make_list(h: Heap): List<Int32> do\n    return List<Int32>.new(h)\nend\n" +
			"export fun identity<T>(value: T): T do\n    return value\nend\n",
		"app.hex": "module Lib = import \"./lib\"\nmodule M = import \"./m\"\nmodule S = import \"./s\"\n" +
			"fun demo(h: Heap): Nil | Error do\n" +
			"    l: List<Int32> := Lib.make_list(h)\n" +
			"    Lib.take_list(l)\n" +
			"    d: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n" +
			"    Lib.take_dict(d)\n" +
			"    a: Array<Int32, 2> := [1, 2]\n" +
			"    Lib.take_array(a)\n" +
			"    v: View<Int32> := a.slice(0, 1)\n" +
			"    Lib.take_view(v)\n" +
			"    holder: Lib.Holder := Lib.make_holder(l)\n" +
			"    Lib.take_holder(holder)\n" +
			"    same: List<Int32> := Lib.identity<List<Int32>>(l)\n" +
			"    lm: List<M.Point> := List<M.Point>.new(h)\n" +
			"    ls: List<S.Point> := List<S.Point>.new(h)\n" +
			"    return nil\n" +
			"end\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "lib", "m", "s")
	// A builtin element has no owning module, so its specialization stays in
	// the program-wide component and is shared by every module that uses it.
	component := result.Files["hexal/list.h"]
	if strings.Count(component, "typedef struct hex_list_Int32") != 1 {
		t.Fatalf("hexal/list.h %v, want exactly one List<Int32> typedef shared across modules", component)
	}
	if strings.Contains(component, "typedef struct hex_list_Point") {
		t.Fatalf("hexal/list.h %v, want no module-typed specialization: the component cannot declare a per-module type", component)
	}
	// Module-owned elements move to the consuming module's header, where
	// their element typedefs are available.
	header := result.Files["modules/app.h"]
	if strings.Count(header, "typedef struct hex_list_Point") != 2 {
		t.Fatalf("modules/app.h %v, want two List<Point> typedefs in the same run", header)
	}
}

// A specialization over a module-owned element is emitted into the consuming
// module's header, after the element's own typedef. Module headers never
// include one another, so a program-wide component cannot carry a per-module
// type; placing the specialization beside its element is what keeps every
// spelled type declared before use.
//
// No test compiles generated C, so ordering is asserted on the text.
func TestModuleOwnedCollectionElementsDeclareBeforeUse(t *testing.T) {
	point := "export type Point = { x: Int32, y: Int32 }\n"
	color := "export type Color = { r: Int32 }\n"
	prelude := "module M = import \"./m\"\nmodule S = import \"./s\"\nh: Heap := Heap.new()\n"

	cases := []struct {
		name           string
		body           string
		specialization string
		element        string
	}{
		{"List", "l: List<M.Point> := List<M.Point>.new(h)\n",
			"typedef struct hex_list_Point", "struct hex_t_m1_m_Point {"},
		{"Dict value", "d: Dict<Int32, M.Point> := Dict<Int32, M.Point>.new(h)\n",
			"typedef struct hex_dict_Int32_Point", "struct hex_t_m1_m_Point {"},
		// Two owning modules contributing elements to one header is the shape
		// that would break placement if re-emission order did not follow
		// dependency order.
		{"two owning modules",
			"d: Dict<Int32, M.Point> := Dict<Int32, M.Point>.new(h)\ne: Dict<Int32, S.Color> := Dict<Int32, S.Color>.new(h)\n",
			"typedef struct hex_dict_Int32_Color", "struct hex_t_m1_s_Color {"},
		{"nested List", "l: List<List<M.Point>> := List<List<M.Point>>.new(h)\n",
			"typedef struct hex_list_List_Point_", "struct hex_t_m1_m_Point {"},
		{"Array", "fun f(p: M.Point) do\n    a: Array<M.Point, 2> := [p, p]\n    q: M.Point := a[0]\nend\n",
			"typedef struct hex_array_Point_2", "struct hex_t_m1_m_Point {"},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			result := compiler.Compile(map[string]string{
				"m.hex": point, "s.hex": color, "app.hex": prelude + item.body,
			}, "app.hex", compiler.Project{})
			assertMultiModuleSuccess(t, result, "app", "m", "s")
			header := result.Files["modules/app.h"]
			specialization := strings.Index(header, item.specialization)
			if specialization < 0 {
				t.Fatalf("modules/app.h %v, want the %s specialization", header, item.name)
			}
			element := strings.Index(header, item.element)
			if element < 0 {
				t.Fatalf("modules/app.h spells %s without declaring %s", item.specialization, item.element)
			}
			if element > specialization {
				t.Fatalf("modules/app.h declares %s at %d before its element at %d; the element must precede it",
					item.specialization, specialization, element)
			}
		})
	}
}
