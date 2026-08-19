package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

func TestGenerateADTDefinitionAndConstruction(t *testing.T) {
	program := checkedGeneratorSource(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hex_Shape_tag", "hex_Shape_Circle", ".payload.Circle", "hex_m_r"} {
		if !strings.Contains(rootC, want) && !strings.Contains(rootH, want) {
			t.Fatalf("generated output does not contain %q: C=%q H=%q", want, rootC, rootH)
		}
	}
}

func TestGenerateADTUnitVariantsHaveNoPayload(t *testing.T) {
	program := checkedGeneratorSource(t, "type Direction = | East | West heading: Direction = Direction.East")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootH, "hex_Direction_East") || strings.Contains(rootH, "payload") {
		t.Fatalf("generated header = %q, want tag-only unit variant", rootH)
	}
	if !strings.Contains(rootC, ".tag = hex_Direction_East") {
		t.Fatalf("generated C = %q, want unit construction", rootC)
	}
}

func TestGenerateMatchTypeMode(t *testing.T) {
	program := checkedGeneratorSource(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } area: Int32 = match shape is\n| Shape.Circle then shape.r\n| Shape.Square then 0\nend")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, ".tag == hex_Shape_Circle") || !strings.Contains(rootC, ".payload.Circle.hex_m_r") {
		t.Fatalf("generated C = %q, want tag test and narrowed payload read", rootC)
	}
}

func TestGenerateMatchValueMode(t *testing.T) {
	program := checkedGeneratorSource(t, "ready: Bool = true label: Int32 = match ready\n| true then 1\n| false then 0\nend")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_v_ready") || !strings.Contains(rootC, "hex_match_result_1") {
		t.Fatalf("generated C = %q, want value-mode comparison", rootC)
	}
}

func TestGenerateMatchUnionMembers(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Float32 | Nil = nil label: Int32 = match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, ".tag ==") {
		t.Fatalf("generated C = %q, want union tag tests", rootC)
	}
}
