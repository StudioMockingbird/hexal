package generator

import (
	"strings"
	"testing"
)

func TestGenerateADTDefinitionAndConstruction(t *testing.T) {
	program := checkedGeneratorSource(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sw_Shape_tag", "sw_Shape_Circle", ".payload.Circle", "sw_m_r"} {
		if !strings.Contains(mainC, want) && !strings.Contains(mainH, want) {
			t.Fatalf("generated output does not contain %q: C=%q H=%q", want, mainC, mainH)
		}
	}
}

func TestGenerateADTUnitVariantsHaveNoPayload(t *testing.T) {
	program := checkedGeneratorSource(t, "type Direction = | East | West heading: Direction = Direction.East")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainH, "sw_Direction_East") || strings.Contains(mainH, "payload") {
		t.Fatalf("generated header = %q, want tag-only unit variant", mainH)
	}
	if !strings.Contains(mainC, ".tag = sw_Direction_East") {
		t.Fatalf("generated C = %q, want unit construction", mainC)
	}
}

func TestGenerateMatchTypeMode(t *testing.T) {
	program := checkedGeneratorSource(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } area: Int32 = match shape is\n| Shape.Circle then shape.r\n| Shape.Square then 0\nend")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, ".tag == sw_Shape_Circle") || !strings.Contains(mainC, ".payload.Circle.sw_m_r") {
		t.Fatalf("generated C = %q, want tag test and narrowed payload read", mainC)
	}
}

func TestGenerateMatchValueMode(t *testing.T) {
	program := checkedGeneratorSource(t, "ready: Bool = true label: Int32 = match ready\n| true then 1\n| false then 0\nend")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, "sw_v_ready") || !strings.Contains(mainC, "sw_match_result_1") {
		t.Fatalf("generated C = %q, want value-mode comparison", mainC)
	}
}

func TestGenerateMatchUnionMembers(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Float32 | Nil = nil label: Int32 = match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, ".tag ==") {
		t.Fatalf("generated C = %q, want union tag tests", mainC)
	}
}
