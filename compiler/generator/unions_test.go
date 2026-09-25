package generator

// Tagged unions: declaration and helper discovery, union operations and
// truthiness, narrowed payload reads, and forged-member rejection.

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func TestGenerateTaggedUnionDeclaration(t *testing.T) {
	testSpanTable.Add("test.hex", "let value: Int32 | Float64 = 1")
	tokens, err := lexer.Lex("test.hex", "let value: Int32 | Float64 = 1")
	if err != nil {
		t.Fatal(err)
	}
	syntax, err := parser.Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	program, err := checker.Check(syntax)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootH, "hex_t_Int32_Float64") || !strings.Contains(rootC, ".tag") {
		t.Fatalf("generated union output = C:%q H:%q, want tagged representation", rootC, rootH)
	}
}

func TestDiscoverGeneratedUnionHelpers(t *testing.T) {
	testSpanTable.Add("test.hex", "let value: Int32 | Float64 = 1")
	tokens, err := lexer.Lex("test.hex", "let value: Int32 | Float64 = 1")
	if err != nil {
		t.Fatal(err)
	}
	syntax, err := parser.Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	program, err := checker.Check(syntax)
	if err != nil {
		t.Fatal(err)
	}
	state, err := discoverGeneratedUnions(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.order) != 1 || state.order[0].CName != "hex_t_Int32_Float64" {
		t.Fatalf("union state = %#v, want one deterministic helper", state)
	}
}

func TestSupportedGeneratedUnionTypeRejectsForgedMetadata(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	union := environment.UnionType([]compilerTypes.Type{compilerTypes.Int32, compilerTypes.Float64})
	if !supportedGeneratedType(union) {
		t.Fatal("canonical tagged union was rejected")
	}
	forged := union
	forged.CName = "hex_t_forged"
	if supportedGeneratedType(forged) {
		t.Fatal("forged tagged union metadata was accepted")
	}
}

func TestGenerateUnionOperations(t *testing.T) {
	program := checkedGeneratorSource(t, "let value: Int32 | Float64 = 1 let active: Bool = value is Int32 let maybe: Int32 | Float64 | Nil = nil let present: Bool = maybe != nil let left: Int32 | Bool = true let right: Bool | Int32 = false let same: Bool = left == right let small: Int32 | Bool = true let wide: Int32 | Bool | Nil = small")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"typedef struct hex_t_Int32_Float64",
		".tag == hex_tag_Int32",
		"hex_t_Bool_Int32_equal",
		"hex_internal_widen_hex_t_Bool_Int32_to_hex_t_Bool_Int32_Nil",
	} {
		if !strings.Contains(rootC, want) && !strings.Contains(rootH, want) {
			t.Fatalf("generated output does not contain %q: C := %q H=%q", want, rootC, rootH)
		}
	}
}

func TestGenerateUnionTruthiness(t *testing.T) {
	program := checkedGeneratorSource(t, "let value: Int32 | Bool | Nil = true if value then let noop: Int32 = 0 end")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_t_Bool_Int32_Nil_truthy") || !strings.Contains(rootH, "static bool hex_t_Bool_Int32_Nil_truthy") {
		t.Fatalf("truthiness output = C:%q H:%q, want tagged truthiness helper", rootC, rootH)
	}
}

func TestGenerateNarrowedUnionPayloadRead(t *testing.T) {
	program := checkedGeneratorSource(t, "let value: Int32 | Float64 = 1 if value is Int32 then let result: Int32 = value end")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_v_value.payload.hex_m_Int32") {
		t.Fatalf("generated C = %q, want narrowed payload read", rootC)
	}
}

func TestGenerateRejectsForgedUnionMemberIndex(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	union := environment.UnionType([]compilerTypes.Type{compilerTypes.Int32, compilerTypes.Float64})
	_, err := renderExpression(checker.Expression{
		Kind:        checker.UnionInjectionExpression,
		OperandType: compilerTypes.Int32,
		ResultType:  union,
		MemberIndex: -1,
	}, newLiteralRegistry())
	if err == nil || !strings.Contains(err.Error(), "Unknown Error") {
		t.Fatalf("render error = %v, want fail-closed Unknown Error", err)
	}
}
