package checker

import (
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

func TestCheckTransparentAliasesAndPointerInterning(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Coordinate = Int32 type A = Ptr<Coordinate> type B = Ptr<Int32>"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	if len(checked.Statements) != 0 || len(checked.TypeDeclarations) != 3 {
		t.Fatalf("checked type declarations/statements = %d/%d, want 3/0", len(checked.TypeDeclarations), len(checked.Statements))
	}
	if checked.TypeDeclarations[0].Type.Name != "Int32" {
		t.Fatalf("Coordinate type = %#v, want canonical Int32", checked.TypeDeclarations[0].Type)
	}
	if !compilerTypes.Equal(checked.TypeDeclarations[1].Type, checked.TypeDeclarations[2].Type) {
		t.Fatalf("pointer aliases did not share canonical type: %#v != %#v", checked.TypeDeclarations[1].Type, checked.TypeDeclarations[2].Type)
	}
}

func TestCheckAliasSelfReferencePrecedesLookup(t *testing.T) {
	for _, source := range []string{
		"type Coordinate = Coordinate",
		"type CoordinatePtr = Ptr<CoordinatePtr>",
	} {
		_, err := Check(parseProgram(t, source))
		if err == nil || !strings.Contains(err.Error(), "type alias ") || !strings.Contains(err.Error(), "cannot reference itself") {
			t.Fatalf("Check(%q) error = %v, want focused self-reference diagnostic", source, err)
		}
		if strings.Contains(err.Error(), "unknown type") {
			t.Fatalf("Check(%q) reported ordinary lookup instead of self-reference: %v", source, err)
		}
	}
}
