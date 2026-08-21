package generator

import (
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

// A program with no program-owned equality types emits no hexal/equality.h.
func TestEqualityComponentAbsentWithoutProgramOwnedTypes(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 := 1")
	files := generateOne(t, program)
	if _, exists := files["hexal/equality.h"]; exists {
		t.Fatalf("scalar-only program emitted hexal/equality.h: %v", files)
	}
}

// A program with only module-owned equality types emits no hexal/equality.h.
func TestEqualityComponentAbsentWithModuleOwnedOnly(t *testing.T) {
	program := checkedGeneratorSource(t, "type Point = { x: Int32, y: Int32 }\nfun demo(): Int32 do\n    left: Point := Point { x = 1, y = 2 }\n    right: Point := Point { x = 1, y = 2 }\n    same: Bool := left == right\n    if same then return 1 else return 0 end\nend")
	files := generateOne(t, program)
	if _, exists := files["hexal/equality.h"]; exists {
		t.Fatalf("module-owned-only equality program emitted hexal/equality.h: %v", files)
	}
}

// A program using print with module-owned types emits no hexal/equality.h.
func TestEqualityComponentNotTriggeredByPrint(t *testing.T) {
	program := checkedGeneratorSource(t, "type Point = { x: Int32, y: Int32 }\nfun demo(): Int32 do\n    p: Point := Point { x = 1, y = 2 }\n    print(p)\n    return 0\nend")
	files := generateOne(t, program)
	if _, exists := files["hexal/equality.h"]; exists {
		t.Fatalf("print-only program emitted hexal/equality.h: %v", files)
	}
}

// isProgramOwnedEqualityType correctly classifies types.
func TestIsProgramOwnedEqualityType(t *testing.T) {
	tests := []struct {
		name string
		typ  compilerTypes.Type
		want bool
	}{
		{"module object", compilerTypes.Type{Object: &compilerTypes.ObjectType{ModuleID: "m1"}}, false},
		{"program object", compilerTypes.Type{Object: &compilerTypes.ObjectType{ModuleID: ""}}, true},
		{"string", compilerTypes.StringType, false},
		{"scalar", compilerTypes.Int32, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isProgramOwnedEqualityType(tt.typ); got != tt.want {
				t.Fatalf("isProgramOwnedEqualityType(%v) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

// Equality component is deterministic.
func TestEqualityComponentDeterministic(t *testing.T) {
	merged := &programEmission{
		equalityTypes: []compilerTypes.Type{
			{Object: &compilerTypes.ObjectType{ModuleID: ""}},
		},
	}
	first, err := renderComponentArtifacts(merged, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	second, err := renderComponentArtifacts(merged, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	if first["hexal/equality.h"] != second["hexal/equality.h"] {
		t.Fatalf("equivalent programs rendered hexal/equality.h differently")
	}
}

// The equality.h artifact has the correct guard.
func TestEqualityComponentGuard(t *testing.T) {
	merged := &programEmission{
		equalityTypes: []compilerTypes.Type{
			{Object: &compilerTypes.ObjectType{ModuleID: ""}},
		},
	}
	files, err := renderComponentArtifacts(merged, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	equalityH, exists := files["hexal/equality.h"]
	if !exists {
		t.Fatalf("expected hexal/equality.h")
	}
	if !strings.HasPrefix(equalityH, "#ifndef HEXAL_EQUALITY_H\n#define HEXAL_EQUALITY_H\n") {
		t.Fatalf("hexal/equality.h lost its guard: %q", equalityH)
	}
	if !strings.HasSuffix(equalityH, "\n#endif\n") {
		t.Fatalf("hexal/equality.h must end with #endif: %q", equalityH)
	}
}

// Program-owned collection helpers carry the component headers that define
// their parameter and member types.
func TestEqualityComponentIncludesCollectionDependencies(t *testing.T) {
	program := checkedGeneratorSource(t, "a: Array<Int32, 2> := [1, 2]\nb: Array<Int32, 2> := [1, 2]\nsame: Bool := a == b\n")
	files := generateOne(t, program)
	header := files["hexal/equality.h"]
	if !strings.Contains(header, "#include \"hexal/array.h\"") {
		t.Fatalf("hexal/equality.h = %q, want the Array component include", header)
	}
	if !strings.Contains(header, "hex_equal_hex_array_Int32_2") {
		t.Fatalf("hexal/equality.h = %q, want the Array equality helper", header)
	}
	for _, standardHeader := range []string{"stddef.h", "string.h", "stdlib.h"} {
		if strings.Contains(header, "#include <"+standardHeader+">") {
			t.Fatalf("Array<Int32, 2>-only equality.h includes unused <%s>:\n%s", standardHeader, header)
		}
	}
}

func TestModuleEqualityWriterSkipsProgramOwnedHelpers(t *testing.T) {
	typ := compilerTypes.Type{
		Name:         "Array<Int32, 2>",
		CName:        "hex_array_Int32_2",
		CanonicalKey: "array:Int32:2",
		Array:        &compilerTypes.ArrayInfo{Element: compilerTypes.Int32, Length: 2},
	}
	var output strings.Builder
	writeEqualityDefinitions(&output, &generatedEqualityState{order: []compilerTypes.Type{typ}}, nil)
	if output.Len() != 0 {
		t.Fatalf("module equality output = %q, program-owned helper must be component-owned", output.String())
	}
}
