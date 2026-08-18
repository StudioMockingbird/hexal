package generator

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"strings"
	"testing"

	"hexal/compiler/checker"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func TestGenerateInt32Declaration(t *testing.T) {
	program := checker.Program{
		Statements: []checker.Statement{checker.Declaration{
			Name:   "x",
			Type:   compilerTypes.Int32,
			Source: intSource(compilerTypes.Int32, 13, "13"),
		}},
	}

	wantRoot := "#include \"modules/app.h\"\n\nint main(void) {\n    const int32_t hex_v_x = 13;\n    return 0;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatal(err)
	}
	if files["modules/app.c"] != wantRoot {
		t.Fatalf("modules/app.c = %q, want %q", files["modules/app.c"], wantRoot)
	}
	if files["hexal.h"] == "" {
		t.Fatalf("hexal.h is missing from the generated artifacts: %v", files)
	}
	for key := range files {
		if key != "hexal.h" && key != "modules/app.c" && key != "modules/app.h" {
			t.Fatalf("unexpected generated artifact %q", key)
		}
	}
}

func TestGenerateTaggedUnionDeclaration(t *testing.T) {
	tokens, err := lexer.Lex("value: Int32 | Float64 = 1")
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
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootH, "hex_union_7_int32_t6_double") || !strings.Contains(rootC, ".tag") {
		t.Fatalf("generated union output = C:%q H:%q, want tagged representation", rootC, rootH)
	}
}

func TestDiscoverGeneratedUnionHelpers(t *testing.T) {
	tokens, err := lexer.Lex("value: Int32 | Float64 = 1")
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
	state := discoverGeneratedUnions(program)
	if len(state.order) != 1 || state.order[0].CName != "hex_union_7_int32_t6_double" {
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
	forged.CName = "hex_union_forged"
	if supportedGeneratedType(forged) {
		t.Fatal("forged tagged union metadata was accepted")
	}
}

func TestGenerateUnionOperations(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Float64 = 1 active: Bool = value is Int32 maybe: Int32 | Float64 | Nil = nil present: Bool = maybe != nil left: Int32 | Bool = true right: Bool | Int32 = false same: Bool = left == right small: Int32 | Bool = true wide: Int32 | Bool | Nil = small")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"typedef enum hex_union_7_int32_t6_double_tag",
		"typedef struct hex_union_7_int32_t6_double",
		".tag == hex_union_7_int32_t6_double_tag_member_0",
		"hex_union_4_bool7_int32_t_equal",
		"hex_internal_widen_hex_union_4_bool7_int32_t_to_hex_union_4_bool7_int32_t9_nullptr_t",
	} {
		if !strings.Contains(rootC, want) && !strings.Contains(rootH, want) {
			t.Fatalf("generated output does not contain %q: C=%q H=%q", want, rootC, rootH)
		}
	}
}

func TestGenerateUnionTruthiness(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Bool | Nil = true if value then noop: Int32 = 0 end")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_union_4_bool7_int32_t9_nullptr_t_truthy") || !strings.Contains(rootH, "static bool hex_union_4_bool7_int32_t9_nullptr_t_truthy") {
		t.Fatalf("truthiness output = C:%q H:%q, want tagged truthiness helper", rootC, rootH)
	}
}

func TestGenerateNarrowedUnionPayloadRead(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Float64 = 1 if value is Int32 then result: Int32 = value end")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_v_value.payload.member_0") {
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
	})
	if err == nil || !strings.Contains(err.Error(), "Unknown Error") {
		t.Fatalf("render error = %v, want fail-closed Unknown Error", err)
	}
}

func checkedGeneratorSource(t *testing.T, source string) checker.Program {
	t.Helper()
	tokens, err := lexer.Lex(source)
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
	return program
}

func TestGenerateBoolDeclaration(t *testing.T) {
	program := checker.Program{
		Statements: []checker.Statement{checker.Declaration{
			Name:   "enabled",
			Type:   compilerTypes.Bool,
			Source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Bool, Constant: constant.MakeBool(true), Literal: "true"},
		}},
	}

	want := "#include \"modules/app.h\"\n\nint main(void) {\n    const bool hex_v_enabled = true;\n    return 0;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if rootC != want {
		t.Fatalf("modules/app.c = %q, want %q", rootC, want)
	}
}

func TestGenerateHexadecimalInt32Declaration(t *testing.T) {
	program := checker.Program{
		Statements: []checker.Statement{checker.Declaration{
			Name:   "mask",
			Type:   compilerTypes.Int32,
			Source: intSource(compilerTypes.Int32, 255, "0xFF"),
		}},
	}

	want := "#include \"modules/app.h\"\n\nint main(void) {\n    const int32_t hex_v_mask = 0xFF;\n    return 0;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if rootC != want {
		t.Fatalf("modules/app.c = %q, want %q", rootC, want)
	}
}

func TestGenerateStatementsInOrder(t *testing.T) {
	program := checker.Program{
		Statements: []checker.Statement{
			checker.Declaration{Name: "x", Type: compilerTypes.Int32, Mutable: true, Source: intSource(compilerTypes.Int32, 13, "13")},
			checker.Assignment{Name: "x", Type: compilerTypes.Int32, Target: checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: variableNode("x")}, Source: intSource(compilerTypes.Int32, 14, "14")},
		},
	}

	want := "#include \"modules/app.h\"\n\nint main(void) {\n    int32_t hex_v_x = 13;\n    hex_v_x = 14;\n    return 0;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if rootC != want {
		t.Fatalf("modules/app.c = %q, want %q", rootC, want)
	}
}

func TestGeneratePointerDeclarationAndAssignments(t *testing.T) {
	mutPtrInt32 := compilerTypes.MutPtrType(compilerTypes.Int32)
	program := checker.Program{
		Statements: []checker.Statement{
			checker.Declaration{Name: "x", Type: compilerTypes.Int32, Mutable: true, Source: intSource(compilerTypes.Int32, 13, "13")},
			checker.Declaration{
				Name:    "p",
				Type:    mutPtrInt32,
				Mutable: true,
				Source:  checker.Operand{Kind: checker.VariableOperand, Type: mutPtrInt32, Node: addressNode("x")},
			},
			checker.Assignment{
				Name:   "p",
				Type:   compilerTypes.Int32,
				Target: checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: dereferenceNode("p")},
				Source: intSource(compilerTypes.Int32, 14, "14"),
			},
			checker.Assignment{Name: "p", Type: mutPtrInt32, Target: checker.Operand{Kind: checker.VariableOperand, Type: mutPtrInt32, Node: variableNode("p")}, Source: checker.Operand{Kind: checker.VariableOperand, Type: mutPtrInt32, Node: addressNode("x")}},
		},
	}

	wantC := "#include \"modules/app.h\"\n\n" +
		"int main(void) {\n" +
		"    int32_t hex_v_x = 13;\n" +
		"    int32_t *hex_v_p = &hex_v_x;\n" +
		"    *hex_v_p = 14;\n" +
		"    hex_v_p = &hex_v_x;\n" +
		"    return 0;\n" +
		"}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if rootC != wantC {
		t.Fatalf("modules/app.c = %q, want %q", rootC, wantC)
	}
}

func TestGenerateNestedPointerExpressions(t *testing.T) {
	mutPtrInt32 := compilerTypes.MutPtrType(compilerTypes.Int32)
	ptrMutPtrInt32 := compilerTypes.PtrType(mutPtrInt32)
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "x", Type: compilerTypes.Int32, Mutable: true, Source: intSource(compilerTypes.Int32, 1, "1")},
		checker.Declaration{Name: "p", Type: mutPtrInt32, Source: checker.Operand{Kind: checker.VariableOperand, Type: mutPtrInt32, Node: addressNode("x")}},
		checker.Declaration{Name: "pp", Type: ptrMutPtrInt32, Source: checker.Operand{Kind: checker.VariableOperand, Type: ptrMutPtrInt32, Node: addressNode("p")}},
		checker.Declaration{Name: "y", Type: compilerTypes.Int32, Source: checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: nestedDereferenceNode("pp")}},
	}}
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"int32_t hex_v_x = 1;", "int32_t *const hex_v_p = &hex_v_x;", "int32_t *const *const hex_v_pp = &hex_v_p;", "const int32_t hex_v_y = *(*hex_v_pp);"} {
		if !strings.Contains(rootC, want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC, want)
		}
	}
	if strings.Contains(rootC, "hexal_alloc") || strings.Contains(rootC, "free(") || strings.Contains(rootC, "Hexal_Ref") {
		t.Fatalf("modules/app.c contains removed ownership machinery: %q", rootC)
	}
}

func TestGenerateCheckedRejectsForgedAssignmentTargetType(t *testing.T) {
	mutPtrInt32 := compilerTypes.MutPtrType(compilerTypes.Int32)
	ptrInt32 := compilerTypes.PtrType(compilerTypes.Int32)
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "x", Type: compilerTypes.Int32, Mutable: true, Source: intSource(compilerTypes.Int32, 1, "1")},
		checker.Declaration{Name: "p", Type: mutPtrInt32, Mutable: true, Source: checker.Operand{
			Kind: checker.VariableOperand,
			Type: mutPtrInt32,
			Node: addressNode("x"),
		}},
		checker.Assignment{
			Name: "p",
			Type: ptrInt32,
			Target: checker.Operand{
				Kind: checker.VariableOperand,
				Type: mutPtrInt32,
				Node: variableNode("p"),
			},
			Source: checker.Operand{Kind: checker.VariableOperand, Type: mutPtrInt32, Node: variableNode("p")},
		},
	}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	assertGeneratorUnknownError(t, err)
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output for forged assignment target: rootC=%q rootH=%q", rootC, rootH)
	}
}

func TestGenerateCheckedRejectsDuplicateDeclarationNames(t *testing.T) {
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "value", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
		checker.Declaration{Name: "value", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 2, "2")},
	}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	assertGeneratorUnknownError(t, err)
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output for duplicate declaration: rootC=%q rootH=%q", rootC, rootH)
	}
}

func TestGenerateCheckedRejectsDuplicateGeneratedObjectCNames(t *testing.T) {
	firstEnvironment := compilerTypes.NewEnvironment()
	first := firstEnvironment.BeginObject("Point", 1, 1)
	first = firstEnvironment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	secondEnvironment := compilerTypes.NewEnvironment()
	second := secondEnvironment.BeginObject("Point", 2, 1)
	second = secondEnvironment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	program := checker.Program{TypeDeclarations: []checker.TypeDeclaration{
		{Name: "Point", Type: first},
		{Name: "Point", Type: second},
	}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	assertGeneratorUnknownError(t, err)
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output for duplicate object C name: rootC=%q rootH=%q", rootC, rootH)
	}
}

func TestGenerateCheckedRejectsForgedDeclarationNames(t *testing.T) {
	for _, name := range []string{"value-name", "1value", "café"} {
		t.Run(name, func(t *testing.T) {
			program := checker.Program{Statements: []checker.Statement{checker.Declaration{
				Name:   name,
				Type:   compilerTypes.Int32,
				Source: intSource(compilerTypes.Int32, 1, "1"),
			}}}
			_, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestGenerateCheckedRejectsForgedTypeAndMemberNames(t *testing.T) {
	for _, name := range []string{"Type-name", "1Type", "café"} {
		t.Run("type "+name, func(t *testing.T) {
			_, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: name, Type: compilerTypes.Int32}}}}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
		})

		t.Run("member "+name, func(t *testing.T) {
			environment := compilerTypes.NewEnvironment()
			point := environment.BeginObject("Point", 1, 1)
			point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: name, Type: compilerTypes.Int32}})
			_, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}}}}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderRejectsForgedValueNames(t *testing.T) {
	for _, name := range []string{"value-name", "1value", "café"} {
		t.Run(name, func(t *testing.T) {
			_, err := renderExpression(variableNode(name))
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestGeneratePointerDeclaratorCombinations(t *testing.T) {
	testCases := []struct {
		typ     compilerTypes.Type
		mutable bool
		want    string
	}{
		{compilerTypes.PtrType(compilerTypes.Int32), false, "const int32_t *const hex_v_a;"},
		{compilerTypes.MutPtrType(compilerTypes.Int32), false, "int32_t *const hex_v_b;"},
		{compilerTypes.PtrType(compilerTypes.PtrType(compilerTypes.Int32)), false, "const int32_t *const *const hex_v_c;"},
		{compilerTypes.MutPtrType(compilerTypes.PtrType(compilerTypes.Int32)), false, "const int32_t **const hex_v_d;"},
		{compilerTypes.PtrType(compilerTypes.MutPtrType(compilerTypes.Int32)), false, "int32_t *const *const hex_v_e;"},
		{compilerTypes.MutPtrType(compilerTypes.MutPtrType(compilerTypes.Int32)), false, "int32_t **const hex_v_f;"},
		{compilerTypes.PtrType(compilerTypes.Int32), true, "const int32_t *hex_v_g;"},
		{compilerTypes.MutPtrType(compilerTypes.Int32), true, "int32_t *hex_v_h;"},
	}
	for index, testCase := range testCases {
		name := []string{"a", "b", "c", "d", "e", "f", "g", "h"}[index]
		if got := declaration(testCase.typ, "hex_v_"+name, testCase.mutable) + ";"; got != testCase.want {
			t.Fatalf("declarator for %q (mutable=%v) = %q, want %q", testCase.typ.Name, testCase.mutable, got, testCase.want)
		}
	}
}

func TestGenerateLineDirectives(t *testing.T) {
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "x", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 13, "13"), SourceLine: 4, SourceColumn: 1},
	}}
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "#line 4 \"app.hex\"\n    const int32_t hex_v_x = 13;") {
		t.Fatalf("modules/app.c = %q, want a line directive before the declaration", rootC)
	}
}

func TestPrivateCNameUsesOneUnconditionalPrefix(t *testing.T) {
	testCases := []struct {
		kind   NameKind
		source string
		owner  string
		want   string
	}{
		{ValueName, "main", "", "hex_v_main"},
		{ValueName, "int", "3_app", "hex_v_int"},
		{ValueName, "INT32_MAX", "3_app", "hex_v_INT32_MAX"},
		{ValueName, "hex_v_score", "3_app", "hex_v_hex_v_score"},
		{TypeName, "Point", "3_app", "hex_t_3_app_Point"},
		{TypeName, "Point", "", "hex_t_Point"},
		{MemberName, "x", "3_app", "hex_m_x"},
		{FunctionName, "add", "3_app", "hex_f_3_app_add"},
		{FunctionName, "add", "", "hex_f_add"},
	}
	for _, testCase := range testCases {
		if got := PrivateCName(testCase.kind, testCase.source, testCase.owner); got != testCase.want {
			t.Errorf("PrivateCName(%v, %q) = %q, want %q", testCase.kind, testCase.source, got, testCase.want)
		}
	}
}

func TestGenerateCheckedFailsClosedForUnknownExpression(t *testing.T) {
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{
			Name: "x",
			Type: compilerTypes.Int32,
			Source: checker.Operand{
				Kind: checker.VariableOperand,
				Type: compilerTypes.Int32,
				Node: checker.Expression{Kind: checker.InvalidExpression},
			},
		},
	}}
	_, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err == nil || !strings.Contains(err.Error(), "[Unknown Error]") {
		t.Fatalf("GenerateChecked error = %v, want structured Unknown Error", err)
	}
}

func TestGenerateCheckedRejectsLoopControlOutsideGeneratedLoop(t *testing.T) {
	condition := checker.Operand{
		Kind:     checker.ConstantOperand,
		Type:     compilerTypes.Bool,
		Constant: constant.MakeBool(true),
		Literal:  "true",
	}
	for _, testCase := range []struct {
		name      string
		statement checker.Statement
	}{
		{name: "break", statement: checker.BreakStatement{}},
		{name: "continue", statement: checker.ContinueStatement{}},
		{name: "break under if", statement: checker.IfStatement{Condition: condition, Then: []checker.Statement{checker.BreakStatement{}}}},
		{name: "continue under if", statement: checker.IfStatement{Condition: condition, Then: []checker.Statement{checker.ContinueStatement{}}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			files, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{Statements: []checker.Statement{testCase.statement}}}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if rootC != "" || rootH != "" {
				t.Fatalf("generated output for loop control outside a loop: rootC=%q rootH=%q", rootC, rootH)
			}
		})
	}
}

func TestWriteStatementsRejectsLoopControlOutsideGeneratedLoop(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		statement checker.Statement
	}{
		{name: "break", statement: checker.BreakStatement{SourceLine: 1}},
		{name: "continue", statement: checker.ContinueStatement{SourceLine: 1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var body strings.Builder
			err := writeStatementsAt(&body, []checker.Statement{testCase.statement}, &expressionValidation{}, nil, false, "    ", nil)
			assertGeneratorUnknownError(t, err)
			if body.Len() != 0 {
				t.Fatalf("rendered loop control outside a loop: %q", body.String())
			}
		})
	}
}

func TestGenerateCheckedPreservesNestedLoopContext(t *testing.T) {
	condition := checker.Operand{
		Kind:     checker.ConstantOperand,
		Type:     compilerTypes.Bool,
		Constant: constant.MakeBool(true),
		Literal:  "true",
	}
	program := checker.Program{Statements: []checker.Statement{
		checker.WhileStatement{
			Condition: condition,
			Body: []checker.Statement{
				checker.IfStatement{Condition: condition, Then: []checker.Statement{checker.ContinueStatement{}}},
				checker.WhileStatement{
					Condition: condition,
					Body:      []checker.Statement{checker.IfStatement{Condition: condition, Then: []checker.Statement{checker.BreakStatement{}}}},
				},
				checker.BreakStatement{},
			},
		},
	}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if rootH == "" || strings.Count(rootC, "while (true) {") != 2 || strings.Count(rootC, "break;") != 2 || strings.Count(rootC, "continue;") != 1 {
		t.Fatalf("nested loop output = %q, want two loops, two breaks, and one continue", rootC)
	}
}

func TestGenerateCheckedRestoresLoopContextAfterLoop(t *testing.T) {
	condition := checker.Operand{
		Kind:     checker.ConstantOperand,
		Type:     compilerTypes.Bool,
		Constant: constant.MakeBool(true),
		Literal:  "true",
	}
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{Statements: []checker.Statement{
		checker.WhileStatement{Condition: condition, Body: []checker.Statement{checker.ContinueStatement{}}},
		checker.BreakStatement{},
	}}}, []string{"app"}, "app")
	assertGeneratorUnknownError(t, err)
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output after loop-context leak: rootC=%q rootH=%q", rootC, rootH)
	}
}

func TestGenerateCheckedRejectsForgedReturningDeclarationWithoutReturn(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	resultType := compilerTypes.Int32
	function := checker.FunctionDeclaration{
		Name:   "missing_return",
		Result: &resultType,
		Type:   environment.FunType(nil, &resultType),
	}

	object := environment.BeginObject("Point", 1, 1)
	object = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	method := checker.MethodDeclaration{
		Name:        "missing_return",
		Object:      object.Object,
		SelfType:    object,
		SelfBinding: 1,
		Result:      &resultType,
	}

	for _, testCase := range []struct {
		name      string
		statement checker.Statement
		program   checker.Program
	}{
		{name: "function", statement: function},
		{name: "method", statement: method, program: checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: object}}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.program.Statements = []checker.Statement{testCase.statement}
			files, err := GenerateChecked(map[string]checker.Program{"app.hex": testCase.program}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if rootC != "" || rootH != "" {
				t.Fatalf("generated output for forged missing return: rootC=%q rootH=%q", rootC, rootH)
			}
		})
	}
}

// Function and method declarations remain module-level only.
func TestGenerateCheckedRejectsNestedDeclarationsInModuleBlocks(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	function := checker.FunctionDeclaration{Name: "nested", Type: environment.FunType(nil, nil)}
	method := checker.MethodDeclaration{Name: "nested", Object: point.Object, SelfType: point}
	condition := checker.Operand{
		Kind:     checker.ConstantOperand,
		Type:     compilerTypes.Bool,
		Constant: constant.MakeBool(true),
		Literal:  "true",
	}

	for _, declaration := range []struct {
		name      string
		statement checker.Statement
	}{
		{name: "function", statement: function},
		{name: "method", statement: method},
	} {
		for _, block := range []struct {
			name      string
			statement checker.Statement
		}{
			{name: "if", statement: checker.IfStatement{Condition: condition, Then: []checker.Statement{declaration.statement}}},
			{name: "while", statement: checker.WhileStatement{Condition: condition, Body: []checker.Statement{declaration.statement}}},
		} {
			t.Run(declaration.name+" in "+block.name, func(t *testing.T) {
				files, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{
					TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}},
					Statements:       []checker.Statement{block.statement},
				}}, []string{"app"}, "app")
				assertGeneratorUnknownError(t, err)
				rootC, rootH := files["modules/app.c"], files["modules/app.h"]
				if rootC != "" || rootH != "" {
					t.Fatalf("generated output for nested declaration: rootC=%q rootH=%q", rootC, rootH)
				}
			})
		}
	}
}

func TestWriteStatementsRejectsNestedDeclarationsInModuleBlocks(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	function := checker.FunctionDeclaration{Name: "nested", Type: environment.FunType(nil, nil)}
	condition := checker.Operand{
		Kind:     checker.ConstantOperand,
		Type:     compilerTypes.Bool,
		Constant: constant.MakeBool(true),
		Literal:  "true",
	}
	var body strings.Builder
	err := writeStatementsAt(&body, []checker.Statement{
		checker.IfStatement{Condition: condition, Then: []checker.Statement{function}},
	}, &expressionValidation{}, nil, false, "    ", nil)
	assertGeneratorUnknownError(t, err)
}

func TestGenerateCheckedFailsClosedForUnknownTypeDeclaration(t *testing.T) {
	program := checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Alias"}}}
	_, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err == nil || !strings.Contains(err.Error(), "[Unknown Error]") {
		t.Fatalf("GenerateChecked error = %v, want structured Unknown Error", err)
	}
}

func TestGenerateCheckedRejectsForgedTopLevelScalarMetadata(t *testing.T) {
	forgedCName := compilerTypes.Int32
	forgedCName.CName = "forged_int_t"
	forgedScalarKind := compilerTypes.Int32
	forgedScalarKind.ScalarKind = compilerTypes.ScalarUnsignedInteger
	testCases := []checker.Program{
		{TypeDeclarations: []checker.TypeDeclaration{{Name: "Alias", Type: forgedCName}}},
		{Statements: []checker.Statement{checker.Declaration{
			Name:   "value",
			Type:   forgedCName,
			Source: intSource(compilerTypes.Int32, 13, "13"),
		}}},
		{Statements: []checker.Statement{checker.Declaration{
			Name:   "value",
			Type:   compilerTypes.Int32,
			Source: intSource(forgedCName, 13, "13"),
		}}},
		{Statements: []checker.Statement{checker.Declaration{
			Name:   "value",
			Type:   forgedScalarKind,
			Source: intSource(forgedScalarKind, 13, "13"),
		}}},
	}
	for index, program := range testCases {
		files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
		rootC, rootH := files["modules/app.c"], files["modules/app.h"]
		diagnostic, ok := err.(compilerTypes.Diagnostic)
		if !ok {
			t.Errorf("case %d error = %T %v, want compilerTypes.Diagnostic", index, err, err)
			continue
		}
		if diagnostic.Category != compilerTypes.UnknownError || diagnostic.Stage != "generator" {
			t.Errorf("case %d diagnostic = %#v, want generator Unknown Error", index, diagnostic)
		}
		if rootC != "" || rootH != "" {
			t.Errorf("case %d returned generated C for forged metadata: rootC=%q rootH=%q", index, rootC, rootH)
		}
	}
}

func TestGenerateCheckedUsesCanonicalTypes(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	node := environment.BeginObject("Node", 1, 1)
	nodePointer := environment.PtrType(node)
	node = environment.CompleteObject("Node", []compilerTypes.ObjectMember{{Name: "next", Type: nodePointer}})
	pointer := environment.PtrType(compilerTypes.Int32)
	nestedPointer := environment.PtrType(environment.MutPtrType(pointer))

	forgedObject := point
	forgedObjectData := *point.Object
	forgedObjectData.Members = append([]compilerTypes.ObjectMember(nil), point.Object.Members...)
	forgedObjectData.Members[0].Type.CName = "forged_int_t"
	forgedObject.Object = &forgedObjectData
	forgedPointer := pointer
	forgedElement := *pointer.Element
	forgedElement.CName = "forged_int_t"
	forgedPointer.Element = &forgedElement

	for _, testCase := range []struct {
		name     string
		typ      compilerTypes.Type
		valid    bool
		declName string
	}{
		{name: "scalar", typ: compilerTypes.Int32, valid: true, declName: "Scalar"},
		{name: "object", typ: point, valid: true, declName: "Point"},
		{name: "recursive object", typ: node, valid: true, declName: "Node"},
		{name: "pointer", typ: pointer, valid: true, declName: "IntPointer"},
		{name: "nested pointer", typ: nestedPointer, valid: true, declName: "NestedPointer"},
		{name: "forged object", typ: forgedObject, valid: false, declName: "Point"},
		{name: "forged pointer", typ: forgedPointer, valid: false, declName: "ForgedPointer"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: testCase.declName, Type: testCase.typ}}}}, []string{"app"}, "app")
			if testCase.valid {
				if err != nil {
					t.Fatalf("GenerateChecked() error = %v", err)
				}
				return
			}
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestGenerateCheckedRejectsForgedPointerElementMetadata(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		pointeeWritable bool
	}{
		{name: "Ptr", pointeeWritable: false},
		{name: "MutPtr", pointeeWritable: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			forged := compilerTypes.PtrType(compilerTypes.Int32)
			forgedElement := compilerTypes.UInt8
			forged.Element = &forgedElement
			forged.PointeeWritable = testCase.pointeeWritable
			forged.Name = testCase.name + "<UInt8>"
			forged.CName = compilerTypes.UInt8.CName + "*"

			files, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Forged", Type: forged}}}}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if rootC != "" || rootH != "" {
				t.Fatalf("generated output for forged pointer metadata: rootC=%q rootH=%q", rootC, rootH)
			}
		})
	}
}

func TestGenerateCheckedRejectsMalformedGeneratedTypes(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	pointer := environment.PtrType(point)
	invalidScalar := compilerTypes.Int32
	invalidScalar.CName = "forged_int_t"
	missingElement := pointer
	missingElement.Element = nil
	invalidElement := pointer
	invalidElementCopy := invalidScalar
	invalidElement.Element = &invalidElementCopy
	foreignEnvironment := compilerTypes.NewEnvironment()
	foreignPoint := foreignEnvironment.BeginObject("Point", 1, 1)
	foreignPoint = foreignEnvironment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	foreignIdentity := pointer
	foreignElement := foreignPoint
	foreignIdentity.Element = &foreignElement
	emptyObject := *point.Object
	emptyObject.Members = nil
	malformedObject := point
	malformedObject.Object = &emptyObject
	forgedObjectData := *point.Object
	forgedObjectData.CName = "forged_Point"
	forgedObject := point
	forgedObject.Object = &forgedObjectData
	invalidMemberObject := *point.Object
	invalidMemberObject.Members = append([]compilerTypes.ObjectMember(nil), point.Object.Members...)
	invalidMemberObject.Members[0].Type = invalidScalar
	malformedMemberObject := point
	malformedMemberObject.Object = &invalidMemberObject

	for _, testCase := range []struct {
		name string
		typ  compilerTypes.Type
	}{
		{name: "missing pointer element", typ: missingElement},
		{name: "invalid pointer element", typ: invalidElement},
		{name: "foreign pointer identity", typ: foreignIdentity},
		{name: "empty object", typ: malformedObject},
		{name: "forged object C name", typ: forgedObject},
		{name: "invalid object member type", typ: malformedMemberObject},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Type: testCase.typ}}}}, []string{"app"}, "app")
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderRejectsForgedMemberType(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	forgedMember := point.Object.Members[0]
	forgedMember.Type = compilerTypes.Int32
	forgedMember.Type.CName = "forged_int_t"
	node := checker.Expression{
		Kind:    checker.MemberExpression,
		Operand: expressionPointer(variableNode("point")),
		Member:  &forgedMember,
	}
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: node})
	assertGeneratorUnknownError(t, err)
}

func TestRenderExpressionOperand(t *testing.T) {
	source := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: variableNode("value"),
	}
	got, err := renderOperand(source)
	if err != nil {
		t.Fatalf("renderOperand() error = %v", err)
	}
	if got != "hex_v_value" {
		t.Fatalf("rendered expression operand = %q, want %q", got, "hex_v_value")
	}
}

func TestRenderConstantExpression(t *testing.T) {
	source := intSource(compilerTypes.Int32, 13, "13")
	node := checker.Expression{
		Kind:       checker.ConstantExpression,
		Constant:   &source,
		ResultType: compilerTypes.Int32,
	}
	got, err := renderExpression(node)
	if err != nil {
		t.Fatalf("renderExpression() error = %v", err)
	}
	if got != "13" {
		t.Fatalf("rendered constant expression = %q, want %q", got, "13")
	}
}

func TestRenderSignedInt8Addition(t *testing.T) {
	node := binaryExpression(checker.AddOperator, compilerTypes.Int8, compilerTypes.Int8, variableNode("left"), variableNode("right"))
	got, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int8, Node: node})
	if err != nil {
		t.Fatalf("renderOperand() error = %v", err)
	}
	want := "hex_wrap_add_int8_t(hex_v_left, hex_v_right)"
	if got != want {
		t.Fatalf("signed Int8 addition = %q, want %q", got, want)
	}
}

// Signed wrapping +, -, *, and unary - lower through ckd_* helpers; no
// unsigned intermediate or reconstruction ternary remains.
func TestRenderSignedArithmeticUsesWrapHelpers(t *testing.T) {
	testCases := []struct {
		typ      compilerTypes.Type
		helper   string
		operator checker.Operator
	}{
		{compilerTypes.Int8, "hex_wrap_add_int8_t", checker.AddOperator},
		{compilerTypes.Int16, "hex_wrap_sub_int16_t", checker.SubtractOperator},
		{compilerTypes.Int32, "hex_wrap_mul_int32_t", checker.MultiplyOperator},
		{compilerTypes.Int64, "hex_wrap_add_int64_t", checker.AddOperator},
	}
	for _, testCase := range testCases {
		node := binaryExpression(testCase.operator, testCase.typ, testCase.typ, variableNode("left"), variableNode("right"))
		got, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression(%s, %s) error = %v", testCase.typ.Name, testCase.operator, err)
		}
		want := testCase.helper + "(hex_v_left, hex_v_right)"
		if got != want {
			t.Errorf("signed %s = %q, want %q", testCase.typ.Name, got, want)
		}
	}

	for _, typ := range []compilerTypes.Type{compilerTypes.Int8, compilerTypes.Int16, compilerTypes.Int32, compilerTypes.Int64} {
		node := unaryExpression(checker.NegateOperator, typ, typ, variableNode("value"))
		got, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression(%s unary -) error = %v", typ.Name, err)
		}
		want := "hex_wrap_neg_" + typ.CName + "(hex_v_value)"
		if got != want {
			t.Errorf("signed %s negation = %q, want %q", typ.Name, got, want)
		}
	}
}

// Every covered unsigned type lowers one binary operation to one uintmax_t
// seed on the left operand and one narrowing cast at the result boundary
// (RFC 0072) — no per-node widening/narrowing pair, and no width-picked
// intermediate.
func TestRenderUnsignedRingOperationSeedsOnceAndNarrowsOnce(t *testing.T) {
	for _, typ := range unsignedRingTypes() {
		for operator, text := range map[checker.Operator]string{
			checker.AddOperator: "+", checker.SubtractOperator: "-", checker.MultiplyOperator: "*",
		} {
			node := binaryExpression(operator, typ, typ, variableNode("left"), variableNode("right"))
			got, err := renderExpression(node)
			if err != nil {
				t.Fatalf("renderExpression(%s %s) error = %v", typ.Name, text, err)
			}
			want := fmt.Sprintf("(%s)((uintmax_t)hex_v_left %s hex_v_right)", unsignedRingCName(typ), text)
			if got != want {
				t.Errorf("unsigned %s %s = %q, want %q", typ.Name, text, got, want)
			}
		}
	}
}

// A left-associated chain is one maximal ring tree: one seed at its leftmost
// operand, one narrowing at its root, and nothing in between.
func TestRenderUnsignedRingChainSeedsOnlyItsLeftmostOperand(t *testing.T) {
	for _, typ := range unsignedRingTypes() {
		inner := binaryExpression(checker.AddOperator, typ, typ, variableNode("a"), variableNode("b"))
		outer := binaryExpression(checker.AddOperator, typ, typ, inner, variableNode("c"))
		node := binaryExpression(checker.AddOperator, typ, typ, outer, variableNode("d"))
		got, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression(%s chain) error = %v", typ.Name, err)
		}
		want := fmt.Sprintf("(%s)((uintmax_t)hex_v_a + hex_v_b + hex_v_c + hex_v_d)", unsignedRingCName(typ))
		if got != want {
			t.Errorf("unsigned %s chain = %q, want %q", typ.Name, got, want)
		}
		if strings.Count(got, "uintmax_t") != 1 {
			t.Errorf("unsigned %s chain = %q, want exactly one uintmax_t seed", typ.Name, got)
		}
	}
}

// A right-nested ring subtree evaluates before its parent converts anything,
// so it carries its own seed.
func TestRenderUnsignedRingRightSubtreeCarriesItsOwnSeed(t *testing.T) {
	typ := compilerTypes.UInt32
	inner := binaryExpression(checker.MultiplyOperator, typ, typ, variableNode("b"), variableNode("c"))
	node := binaryExpression(checker.AddOperator, typ, typ, variableNode("a"), inner)
	got, err := renderExpression(node)
	if err != nil {
		t.Fatalf("renderExpression(right subtree) error = %v", err)
	}
	want := "(uint32_t)((uintmax_t)hex_v_a + (uintmax_t)hex_v_b * hex_v_c)"
	if got != want {
		t.Errorf("right-nested ring subtree = %q, want %q", got, want)
	}
}

// Mixed +, -, and * trees keep their AST grouping. Each case states the C
// text that must parse the same way the checked tree is shaped; the
// regrouping the construction must not permit is named beside it.
func TestRenderUnsignedRingTreesPreserveGrouping(t *testing.T) {
	typ := compilerTypes.UInt32
	ring := func(operator checker.Operator, left, right checker.Expression) checker.Expression {
		return binaryExpression(operator, typ, typ, left, right)
	}
	a, b, c := variableNode("a"), variableNode("b"), variableNode("c")
	for _, testCase := range []struct {
		name     string
		node     checker.Expression
		want     string
		regroups string
	}{
		{
			name: "ring subtree on the right",
			node: ring(checker.MultiplyOperator, a, ring(checker.SubtractOperator, b, c)),
			want: "(uint32_t)((uintmax_t)hex_v_a * ((uintmax_t)hex_v_b - hex_v_c))", regroups: "(a*b)-c",
		},
		{
			name: "ring subtree on the left",
			node: ring(checker.MultiplyOperator, ring(checker.AddOperator, a, b), c),
			want: "(uint32_t)(((uintmax_t)hex_v_a + hex_v_b) * hex_v_c)", regroups: "a+(b*c)",
		},
		{
			name: "higher-precedence ring subtree on the right needs no grouping",
			node: ring(checker.AddOperator, a, ring(checker.MultiplyOperator, b, c)),
			want: "(uint32_t)((uintmax_t)hex_v_a + (uintmax_t)hex_v_b * hex_v_c)", regroups: "",
		},
		{
			name: "equal-precedence ring subtree on the right keeps grouping",
			node: ring(checker.SubtractOperator, a, ring(checker.SubtractOperator, b, c)),
			want: "(uint32_t)((uintmax_t)hex_v_a - ((uintmax_t)hex_v_b - hex_v_c))", regroups: "(a-b)-c",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderExpression(testCase.node)
			if err != nil {
				t.Fatalf("renderExpression error = %v", err)
			}
			if got != testCase.want {
				t.Errorf("got %q, want %q (regrouping to avoid: %s)", got, testCase.want, testCase.regroups)
			}
		})
	}
}

// unsignedRingTypes lists every type RFC 0072 covers. Byte is UInt8's alias
// and Size is the only one whose C name is not exact-width.
func unsignedRingTypes() []compilerTypes.Type {
	return []compilerTypes.Type{
		compilerTypes.UInt8, compilerTypes.UInt16, compilerTypes.UInt32,
		compilerTypes.UInt64, compilerTypes.SizeType,
	}
}

func unsignedRingCName(typ compilerTypes.Type) string {
	name, ok := unsignedCName(typ)
	if !ok {
		return typ.CName
	}
	return name
}

// Generated C contains no generic target-profile probes. Toolchain
// qualification (8-bit bytes, exact-width integers, IEC float representations)
// is a supported-toolchain contract owned outside generated source.
func TestNoTargetProfileProbesEmitted(t *testing.T) {
	header, err := hexalHeader(hexalHeaderInput{})
	if err != nil {
		t.Fatalf("hexalHeader() error = %v", err)
	}
	for _, forbidden := range []string{
		"CHAR_BIT",
		"static_assert(sizeof(uint8_t)",
		"static_assert(sizeof(int32_t)",
		"FLT_RADIX",
		"FLT_MANT_DIG",
		"DBL_MANT_DIG",
		"FLT_IS_IEC_60559",
		"DBL_IS_IEC_60559",
		"#include <stdbool.h>",
		"#include <limits.h>",
		"#include <float.h>",
		"hex_eos",
	} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal.h = %q, target profile probe %q must not be emitted", header, forbidden)
		}
	}
	if strings.Contains(header, "static_assert") {
		t.Fatalf("hexal.h = %q, no source-dependent Size assertion is present", header)
	}
}

func TestRenderEveryOperationOperator(t *testing.T) {
	left := variableNode("left")
	right := variableNode("right")
	testCases := []struct {
		name string
		node checker.Expression
		want string
	}{
		{"negate", unaryExpression(checker.NegateOperator, compilerTypes.Int32, compilerTypes.Int32, left), "hex_wrap_neg_int32_t(hex_v_left)"},
		{"logical not", unaryExpression(checker.LogicalNotOperator, compilerTypes.Bool, compilerTypes.Bool, left), "(!hex_v_left)"},
		{"add", binaryExpression(checker.AddOperator, compilerTypes.Float64, compilerTypes.Float64, left, right), "(hex_v_left + hex_v_right)"},
		{"subtract", binaryExpression(checker.SubtractOperator, compilerTypes.Float64, compilerTypes.Float64, left, right), "(hex_v_left - hex_v_right)"},
		{"multiply", binaryExpression(checker.MultiplyOperator, compilerTypes.Float64, compilerTypes.Float64, left, right), "(hex_v_left * hex_v_right)"},
		{"divide", binaryExpression(checker.DivideOperator, compilerTypes.Int32, compilerTypes.Int32, left, right), "hex_div_int32_t(hex_v_left, hex_v_right)"},
		{"remainder", binaryExpression(checker.RemainderOperator, compilerTypes.Int32, compilerTypes.Int32, left, right), "hex_rem_int32_t(hex_v_left, hex_v_right)"},
		{"equal", binaryExpression(checker.EqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left == hex_v_right)"},
		{"not equal", binaryExpression(checker.NotEqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left != hex_v_right)"},
		{"less", binaryExpression(checker.LessOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left < hex_v_right)"},
		{"less equal", binaryExpression(checker.LessEqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left <= hex_v_right)"},
		{"greater", binaryExpression(checker.GreaterOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left > hex_v_right)"},
		{"greater equal", binaryExpression(checker.GreaterEqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left >= hex_v_right)"},
		{"logical and", binaryExpression(checker.LogicalAndOperator, compilerTypes.Bool, compilerTypes.Bool, left, right), "(hex_v_left && hex_v_right)"},
		{"logical or", binaryExpression(checker.LogicalOrOperator, compilerTypes.Bool, compilerTypes.Bool, left, right), "(hex_v_left || hex_v_right)"},
	}
	for _, testCase := range testCases {
		got, err := renderExpression(testCase.node)
		if err != nil {
			t.Errorf("%s error = %v", testCase.name, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("%s = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

func TestRenderOperationsAlwaysParenthesizeNestedExpressions(t *testing.T) {
	inner := binaryExpression(checker.AddOperator, compilerTypes.Float64, compilerTypes.Float64, variableNode("left"), variableNode("right"))
	outer := binaryExpression(checker.MultiplyOperator, compilerTypes.Float64, compilerTypes.Float64, inner, variableNode("scale"))
	got, err := renderExpression(outer)
	if err != nil {
		t.Fatalf("renderExpression() error = %v", err)
	}
	want := "((hex_v_left + hex_v_right) * hex_v_scale)"
	if got != want {
		t.Fatalf("nested operation = %q, want %q", got, want)
	}
}

func TestRenderOperationsRejectMismatchedNestedChildTypes(t *testing.T) {
	inner := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right"))
	testCases := []checker.Expression{
		unaryExpression(checker.NegateOperator, compilerTypes.Int64, compilerTypes.Int64, inner),
		binaryExpression(checker.AddOperator, compilerTypes.Int64, compilerTypes.Int64, inner, variableNode("right")),
		binaryExpression(checker.EqualOperator, compilerTypes.Int64, compilerTypes.Bool, inner, variableNode("right")),
	}
	for index, node := range testCases {
		_, err := renderExpression(node)
		diagnostic, ok := err.(compilerTypes.Diagnostic)
		if !ok {
			t.Errorf("case %d error = %T %v, want compilerTypes.Diagnostic", index, err, err)
			continue
		}
		if diagnostic.Category != compilerTypes.UnknownError || diagnostic.Stage != "generator" {
			t.Errorf("case %d diagnostic = %#v, want generator Unknown Error", index, diagnostic)
		}
	}
}

func TestRenderOperandRejectsMismatchedRootExpressionType(t *testing.T) {
	node := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right"))
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int64, Node: node})
	diagnostic, ok := err.(compilerTypes.Diagnostic)
	if !ok {
		t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
	}
	if diagnostic.Category != compilerTypes.UnknownError || diagnostic.Stage != "generator" {
		t.Fatalf("diagnostic = %#v, want generator Unknown Error", diagnostic)
	}
}

func TestRenderMalformedIntegerConstantsFailsClosed(t *testing.T) {
	tooLarge := constant.MakeFromLiteral("18446744073709551616", gotoken.INT, 0)
	testCases := []struct {
		name   string
		source checker.Operand
	}{
		{name: "missing", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32}},
		{name: "wrong constant kind", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32, Constant: constant.MakeString("not an integer")}},
		{name: "outside target range", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int8, Constant: constant.MakeInt64(128)}},
		{name: "outside uint64", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.UInt64, Constant: tooLarge}},
		{name: "negative unsigned", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.UInt8, Constant: constant.MakeInt64(-1)}},
		{name: "invalid radix", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32, Constant: constant.MakeInt64(1), Radix: checker.LiteralRadix(255)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source)
			diagnostic, ok := err.(compilerTypes.Diagnostic)
			if !ok {
				t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
			}
			if diagnostic.Category != compilerTypes.UnknownError || diagnostic.Stage != "generator" {
				t.Fatalf("diagnostic = %#v, want generator Unknown Error", diagnostic)
			}
		})
	}
}

func TestRenderRejectsInconsistentIntegerMetadata(t *testing.T) {
	positiveWithNegativeFlag := intSource(compilerTypes.Int32, 1, "1")
	positiveWithNegativeFlag.Negative = true
	negativeWithoutNegativeFlag := intSource(compilerTypes.Int32, -1, "1")
	negativeLiteralMismatch := intSource(compilerTypes.Int32, -2, "1")
	negativeLiteralMismatch.Negative = true
	positiveLiteralMismatch := intSource(compilerTypes.Int32, 2, "1")
	testCases := []struct {
		name   string
		source checker.Operand
	}{
		{name: "positive value marked negative", source: positiveWithNegativeFlag},
		{name: "negative value missing negative flag", source: negativeWithoutNegativeFlag},
		{name: "negative literal mismatch", source: negativeLiteralMismatch},
		{name: "positive literal mismatch", source: positiveLiteralMismatch},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source)
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderRejectsMalformedFloatConstants(t *testing.T) {
	testCases := []struct {
		name   string
		source checker.Operand
	}{
		{
			name: "float32 bits exceed width",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float32,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.5",
				FloatBits: uint64(math.Float32bits(1.5)) | uint64(1)<<32,
			},
		},
		{
			name: "float64 bits disagree with constant",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.5",
				FloatBits: math.Float64bits(2.5),
			},
		},
		{
			name: "float sign metadata disagrees with bits",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.5",
				Negative:  true,
				FloatBits: math.Float64bits(1.5),
			},
		},
		{
			name: "float literal disagrees with constant",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.25",
				FloatBits: math.Float64bits(1.5),
			},
		},
		{
			name: "non numeric constant",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeString("1.5"),
				Literal:   "1.5",
				FloatBits: math.Float64bits(1.5),
			},
		},
		{
			name: "NaN literal metadata",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeUnknown(),
				Literal:   "NaN",
				FloatBits: math.Float64bits(math.NaN()),
			},
		},
		{
			name: "infinity literal metadata",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeUnknown(),
				Literal:   "Inf",
				FloatBits: math.Float64bits(math.Inf(1)),
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source)
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderSupportsValidFoldedFloatSpecialValues(t *testing.T) {
	for _, testCase := range []struct {
		name string
		typ  compilerTypes.Type
		bits uint64
		want string
	}{
		{name: "float32 infinity", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(float32(math.Inf(1)))), want: "INFINITY"},
		{name: "float64 negative infinity", typ: compilerTypes.Float64, bits: math.Float64bits(math.Inf(-1)), want: "-INFINITY"},
		{name: "float64 NaN", typ: compilerTypes.Float64, bits: math.Float64bits(math.NaN()), want: "NAN"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderOperand(checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      testCase.typ,
				Constant:  constant.MakeUnknown(),
				FloatBits: testCase.bits,
				Negative:  testCase.want == "-INFINITY",
			})
			if err != nil {
				t.Fatalf("renderOperand() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("rendered special value = %q, want %q", got, testCase.want)
			}
		})
	}
}

// Every finite float literal renders as the shortest readable decimal C
// literal that reparses to the exact checked IEEE bits, with the f suffix
// for Float32, a fractional point on integral mantissas, and the retained
// negative-zero sign.
func TestRenderFiniteFloatLiteralsRoundTripDecimal(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		typ      compilerTypes.Type
		bits     uint64
		negative bool
		value    constant.Value
		want     string
	}{
		{name: "float32 three quarters", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(0.75)), value: constant.MakeFloat64(0.75), want: "0.75f"},
		{name: "float64 three quarters", typ: compilerTypes.Float64, bits: math.Float64bits(0.75), value: constant.MakeFloat64(0.75), want: "0.75"},
		{name: "float32 whole", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(3)), value: constant.MakeFloat64(3), want: "3.0f"},
		{name: "float64 whole", typ: compilerTypes.Float64, bits: math.Float64bits(3), value: constant.MakeFloat64(3), want: "3.0"},
		{name: "float64 negative zero", typ: compilerTypes.Float64, bits: math.Float64bits(math.Copysign(0, -1)), negative: true, value: constant.MakeFloat64(0), want: "-0.0"},
		{name: "float32 positive zero", typ: compilerTypes.Float32, bits: 0, value: constant.MakeFloat64(0), want: "0.0f"},
		{name: "float64 min subnormal", typ: compilerTypes.Float64, bits: 1, value: constant.MakeFloat64(math.Float64frombits(1)), want: "5e-324"},
		{name: "float64 max finite", typ: compilerTypes.Float64, bits: math.Float64bits(math.MaxFloat64), value: constant.MakeFloat64(math.MaxFloat64), want: "1.7976931348623157e+308"},
		{name: "float32 max finite", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(math.MaxFloat32)), value: constant.MakeFloat64(float64(math.MaxFloat32)), want: "3.4028235e+38f"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderOperand(checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      testCase.typ,
				Constant:  testCase.value,
				FloatBits: testCase.bits,
				Negative:  testCase.negative,
			})
			if err != nil {
				t.Fatalf("renderOperand() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("rendered finite value = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRenderRejectsInvalidAddressDereferenceChildren(t *testing.T) {
	constantSource := intSource(compilerTypes.Int32, 1, "1")
	constantChild := checker.Expression{Kind: checker.ConstantExpression, Constant: &constantSource, ResultType: compilerTypes.Int32}
	invalidAddress := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: addressNode("value"),
	}
	invalidAddressChild := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.MutPtrType(compilerTypes.Int32),
		Node: checker.Expression{Kind: checker.AddressOfExpression, Operand: &constantChild},
	}
	invalidDereference := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: checker.Expression{Kind: checker.DereferenceExpression, Operand: &constantChild},
	}
	invalidDereferenceMetadata := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: checker.Expression{Kind: checker.DereferenceExpression, Operand: expressionPointer(variableNode("value")), OperandType: compilerTypes.Int32},
	}
	for _, testCase := range []struct {
		name   string
		source checker.Operand
	}{
		{name: "address-of scalar result", source: invalidAddress},
		{name: "address-of non-place child", source: invalidAddressChild},
		{name: "dereference scalar child", source: invalidDereference},
		{name: "dereference scalar metadata", source: invalidDereferenceMetadata},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source)
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestGenerateCheckedValidatesPlaceMetadata(t *testing.T) {
	mutPtrInt32 := compilerTypes.MutPtrType(compilerTypes.Int32)
	ptrInt32 := compilerTypes.PtrType(compilerTypes.Int32)
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32, Mutable: true}})
	pointValue := &checker.ObjectValue{
		Type: point,
		Initializers: []checker.ObjectMemberValue{{
			Member: &point.Object.Members[0],
			Source: intSource(compilerTypes.Int32, 1, "1"),
		}},
	}
	receiver := variableNode("point")
	member := checker.Expression{
		Kind:    checker.MemberExpression,
		Operand: &receiver,
		Member:  &point.Object.Members[0],
	}

	testCases := []struct {
		name      string
		program   checker.Program
		wantError bool
		wantC     string
	}{
		{
			name: "fixed variable cannot produce MutPtr",
			program: checker.Program{Statements: []checker.Statement{
				checker.Declaration{Name: "x", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
				checker.Declaration{Name: "p", Type: mutPtrInt32, Source: checker.Operand{
					Kind: checker.VariableOperand,
					Type: mutPtrInt32,
					Node: addressNode("x"),
				}},
			}},
			wantError: true,
		},
		{
			name: "read-only variable cannot be assigned",
			program: checker.Program{Statements: []checker.Statement{
				checker.Declaration{Name: "x", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
				checker.Assignment{
					Name:   "x",
					Type:   compilerTypes.Int32,
					Target: checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: variableNode("x")},
					Source: intSource(compilerTypes.Int32, 2, "2"),
				},
			}},
			wantError: true,
		},
		{
			name: "fixed member cannot produce MutPtr",
			program: checker.Program{
				TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}},
				Statements: []checker.Statement{
					checker.Declaration{Name: "point", Type: point, Source: checker.Operand{Kind: checker.ObjectOperand, Type: point, Object: pointValue}},
					checker.Declaration{Name: "p", Type: mutPtrInt32, Source: checker.Operand{
						Kind: checker.VariableOperand,
						Type: mutPtrInt32,
						Node: checker.Expression{Kind: checker.AddressOfExpression, Operand: &member},
					}},
				},
			},
			wantError: true,
		},
		{
			name: "fixed variable produces Ptr",
			program: checker.Program{Statements: []checker.Statement{
				checker.Declaration{Name: "x", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
				checker.Declaration{Name: "p", Type: ptrInt32, Source: checker.Operand{
					Kind: checker.VariableOperand,
					Type: ptrInt32,
					Node: addressNode("x"),
				}},
			}},
			wantC: "const int32_t *const hex_v_p = &hex_v_x;",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files, err := GenerateChecked(map[string]checker.Program{"app.hex": testCase.program}, []string{"app"}, "app")
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if testCase.wantError {
				assertGeneratorUnknownError(t, err)
				if rootC != "" || rootH != "" {
					t.Fatalf("generated output for forged place metadata: rootC=%q rootH=%q", rootC, rootH)
				}
				return
			}
			if err != nil {
				t.Fatalf("GenerateChecked() error = %v", err)
			}
			if !strings.Contains(rootC, testCase.wantC) {
				t.Fatalf("modules/app.c = %q, want fragment %q", rootC, testCase.wantC)
			}
		})
	}
}

func TestGenerateCheckedValidatesNestedPlaceMetadataAndIgnoresOperandFlags(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32, Mutable: true}})
	mutPtrPoint := environment.MutPtrType(point)
	mutPtrMutPtrPoint := environment.MutPtrType(mutPtrPoint)
	pointValue := &checker.ObjectValue{
		Type: point,
		Initializers: []checker.ObjectMemberValue{{
			Member: &point.Object.Members[0],
			Source: intSource(compilerTypes.Int32, 1, "1"),
		}},
	}

	pp := variableNode("pp")
	dereferencedPP := checker.Expression{Kind: checker.DereferenceExpression, Operand: &pp}
	dereferencedP := checker.Expression{Kind: checker.DereferenceExpression, Operand: &dereferencedPP}
	target := checker.Expression{Kind: checker.MemberExpression, Operand: &dereferencedP, Member: &point.Object.Members[0]}
	program := checker.Program{
		TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}},
		Statements: []checker.Statement{
			checker.Declaration{Name: "point", Type: point, Mutable: true, Source: checker.Operand{Kind: checker.ObjectOperand, Type: point, Object: pointValue}},
			checker.Declaration{Name: "p", Type: mutPtrPoint, Mutable: true, Source: checker.Operand{Kind: checker.VariableOperand, Type: mutPtrPoint, Node: addressNode("point")}},
			checker.Declaration{Name: "pp", Type: mutPtrMutPtrPoint, Mutable: true, Source: checker.Operand{Kind: checker.VariableOperand, Type: mutPtrMutPtrPoint, Node: addressNode("p")}},
			checker.Assignment{
				Name: "x",
				Type: compilerTypes.Int32,
				Target: checker.Operand{
					Kind:        checker.VariableOperand,
					Type:        compilerTypes.Int32,
					Node:        target,
					Addressable: false,
					Writable:    false,
				},
				Source: intSource(compilerTypes.Int32, 2, "2"),
			},
		},
	}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(rootC, "(*(*hex_v_pp)).hex_m_x = 2;") {
		t.Fatalf("modules/app.c = %q, want nested dereference/member assignment", rootC)
	}
}

func TestRenderRejectsScalarDereferenceReceiver(t *testing.T) {
	node := checker.Expression{
		Kind:       checker.DereferenceExpression,
		Operand:    expressionPointer(variableNode("value")),
		ResultType: compilerTypes.Int32,
	}
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: node})
	assertGeneratorUnknownError(t, err)
}

func TestRenderRejectsForeignMemberReceiver(t *testing.T) {
	foreignEnvironment := compilerTypes.NewEnvironment()
	foreignPoint := foreignEnvironment.BeginObject("Point", 1, 1)
	foreignPoint = foreignEnvironment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	node := checker.Expression{
		Kind:    checker.MemberExpression,
		Operand: expressionPointer(variableNode("point")),
		Member:  &foreignPoint.Object.Members[0],
	}
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: node})
	assertGeneratorUnknownError(t, err)
}

func TestGenerateCheckedRejectsMismatchedObjectIdentity(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	foreignEnvironment := compilerTypes.NewEnvironment()
	foreignPoint := foreignEnvironment.BeginObject("Point", 1, 1)
	foreignPoint = foreignEnvironment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	forged := point
	forged.Object = foreignPoint.Object
	_, err := GenerateChecked(map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: forged}}}}, []string{"app"}, "app")
	assertGeneratorUnknownError(t, err)
}

// An object reachable from the module's statements is part of the module's
// generated definitions even without a type declaration (the validator
// admits reachable object types).
func TestGenerateCheckedAcceptsReachableObjectReferenceWithoutTypeDeclaration(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	value := &checker.ObjectValue{
		Type: point,
		Initializers: []checker.ObjectMemberValue{{
			Member: &point.Object.Members[0],
			Source: intSource(compilerTypes.Int32, 1, "1"),
		}},
	}
	program := checker.Program{Statements: []checker.Statement{checker.Declaration{
		Name:   "point",
		Type:   point,
		Source: checker.Operand{Kind: checker.ObjectOperand, Type: point, Object: value},
	}}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(files["modules/app.h"], "typedef struct hex_t_Point hex_t_Point;") {
		t.Fatalf("modules/app.h = %q, want the reachable object definition", files["modules/app.h"])
	}
}

func TestRenderRejectsNestedMalformedOperation(t *testing.T) {
	inner := unaryExpression(checker.NegateOperator, compilerTypes.Int32, compilerTypes.Int32, addressNode("value"))
	outer := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, inner, variableNode("other"))
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: outer})
	assertGeneratorUnknownError(t, err)
}

func TestTypeRequirementsTraverseOperationNodes(t *testing.T) {
	float32Comparison := binaryExpression(checker.EqualOperator, compilerTypes.Float32, compilerTypes.Bool, variableNode("a"), variableNode("b"))
	float64Comparison := binaryExpression(checker.EqualOperator, compilerTypes.Float64, compilerTypes.Bool, variableNode("c"), variableNode("d"))
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "one", Type: compilerTypes.Bool, Source: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Bool, Node: float32Comparison}},
		checker.Declaration{Name: "two", Type: compilerTypes.Bool, Source: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Bool, Node: float64Comparison}},
	}}
	requirements := &cHeaderRequirements{}
	collectTypeRequirements(program, requirements)
	// Float comparisons emit no headers: the representation facts are a
	// toolchain contract, not program facts.
	if len(requirements.headers) != 0 {
		t.Fatalf("collectTypeRequirements() = %v, want no headers for float-only operations", requirements.headers)
	}
}

func TestRenderMalformedOperationFailsClosed(t *testing.T) {
	testCases := []checker.Expression{
		{Kind: checker.UnaryOperationExpression, Operator: checker.NegateOperator, OperandType: compilerTypes.Int32, ResultType: compilerTypes.Int32},
		{Kind: checker.BinaryOperationExpression, Operator: checker.AddOperator, OperandType: compilerTypes.Int32, ResultType: compilerTypes.Int32},
		binaryExpression(checker.InvalidOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right")),
	}
	for index, node := range testCases {
		_, err := renderExpression(node)
		if err == nil || !strings.Contains(err.Error(), "[Unknown Error]") {
			t.Errorf("malformed operation %d error = %v, want structured Unknown Error", index, err)
		}
	}
}

func TestRenderBinaryOperationRejectsInvalidMetadata(t *testing.T) {
	invalidScalar := compilerTypes.Type{Name: "Invalid", CName: "invalid_t", ScalarKind: compilerTypes.ScalarUnsignedInteger, Bits: 7}
	testCases := []struct {
		name string
		node checker.Expression
	}{
		{
			name: "arithmetic result type",
			node: binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.UInt32, variableNode("left"), variableNode("right")),
		},
		{
			name: "unsupported operand type",
			node: binaryExpression(checker.EqualOperator, invalidScalar, compilerTypes.Bool, variableNode("left"), variableNode("right")),
		},
		{
			name: "unsupported operation",
			node: binaryExpression(checker.Operator(255), compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right")),
		},
	}
	for _, testCase := range testCases {
		_, err := renderExpression(testCase.node)
		if err == nil || !strings.Contains(err.Error(), "[Unknown Error]") {
			t.Errorf("%s error = %v, want structured Unknown Error", testCase.name, err)
		}
	}
}

func TestRenderOperationsRejectMalformedScalarMetadata(t *testing.T) {
	fakeFloat := compilerTypes.Float64
	fakeFloat.Name = "FakeFloat64"
	fakeFloat.CName = "fake_float_t"
	testCases := []struct {
		name string
		node checker.Expression
	}{
		{
			name: "comparison result must be Bool",
			node: binaryExpression(checker.EqualOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right")),
		},
		// A non-Bool logical operand is valid (its truthiness is rendered),
		// so only a non-Bool result stays malformed.
		{
			name: "logical result must be Bool",
			node: binaryExpression(checker.LogicalAndOperator, compilerTypes.Bool, compilerTypes.Int32, variableNode("left"), variableNode("right")),
		},
		{
			name: "remainder rejects Float64",
			node: binaryExpression(checker.RemainderOperator, compilerTypes.Float64, compilerTypes.Float64, variableNode("left"), variableNode("right")),
		},
		{
			name: "ordering rejects Bool",
			node: binaryExpression(checker.LessOperator, compilerTypes.Bool, compilerTypes.Bool, variableNode("left"), variableNode("right")),
		},
		{
			name: "unary fake scalar",
			node: unaryExpression(checker.NegateOperator, fakeFloat, compilerTypes.Float64, variableNode("value")),
		},
		{
			name: "binary fake scalar",
			node: binaryExpression(checker.AddOperator, compilerTypes.Float64, fakeFloat, variableNode("left"), variableNode("right")),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderExpression(testCase.node)
			diagnostic, ok := err.(compilerTypes.Diagnostic)
			if !ok {
				t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
			}
			if diagnostic.Category != compilerTypes.UnknownError {
				t.Errorf("diagnostic category = %q, want %q", diagnostic.Category, compilerTypes.UnknownError)
			}
			if diagnostic.Stage != "generator" {
				t.Errorf("diagnostic stage = %q, want generator", diagnostic.Stage)
			}
		})
	}
}

// Conditions render through truthiness: nil as false, a nullable as a null
// test, and an always-truthy value as a comma evaluation.
func TestRenderTruthinessConditions(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	nullable := environment.NullableType(environment.PtrType(compilerTypes.Int32))

	for _, testCase := range []struct {
		name      string
		condition checker.Operand
		want      string
	}{
		{
			name:      "nil",
			condition: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Nil, Node: checker.Expression{Kind: checker.NilExpression, ResultType: compilerTypes.Nil}},
			want:      "if (false) {",
		},
		{
			name: "always truthy",
			condition: checker.Operand{
				Kind:     checker.ConstantOperand,
				Type:     compilerTypes.Int32,
				Constant: constant.MakeInt64(0),
				Literal:  "0",
				Node:     checker.Expression{Kind: checker.ConstantExpression, ResultType: compilerTypes.Int32},
			},
			want: "if (((void)(0), true)) {",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var body strings.Builder
			err := writeStatementsAt(&body, []checker.Statement{checker.IfStatement{SourceLine: 1, ConditionLine: 1, Condition: testCase.condition, Then: []checker.Statement{}}}, &expressionValidation{}, nil, false, "", nil)
			if err != nil {
				t.Fatalf("writeStatementsAt() error = %v", err)
			}
			if !strings.Contains(body.String(), testCase.want) {
				t.Fatalf("body = %q, want %q", body.String(), testCase.want)
			}
		})
	}

	// A nullable binding renders as a null test; the binding must be
	// registered so the variable's type and name resolve.
	state := &expressionValidation{}
	if _, err := state.allocateBinding(1, "maybe", nullable, true); err != nil {
		t.Fatalf("allocateBinding() error = %v", err)
	}
	condition := checker.Operand{
		Kind: checker.VariableOperand,
		Type: nullable,
		Node: checker.Expression{Kind: checker.VariableExpression, Name: "maybe", Binding: 1, ResultType: nullable},
	}
	var body strings.Builder
	err := writeStatementsAt(&body, []checker.Statement{checker.IfStatement{SourceLine: 1, ConditionLine: 1, Condition: condition, Then: []checker.Statement{}}}, state, nil, false, "", nil)
	if err != nil {
		t.Fatalf("writeStatementsAt() error = %v", err)
	}
	if !strings.Contains(body.String(), "if ((hex_v_maybe != nullptr)) {") {
		t.Fatalf("body = %q, want a nullable null-test condition", body.String())
	}
}

// Logical operations render mixed and non-Bool operands through their
// truthiness; constant operands need no bindings.
func TestRenderLogicalOperationWithMixedOperands(t *testing.T) {
	boolConstant := func(value bool) checker.Expression {
		literal := "false"
		if value {
			literal = "true"
		}
		return constantExpression(checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Bool, Constant: constant.MakeBool(value), Literal: literal})
	}
	for _, testCase := range []struct {
		name string
		node checker.Expression
		want string
	}{
		{
			name: "int and bool",
			node: binaryExpression(checker.LogicalAndOperator, compilerTypes.Int32, compilerTypes.Bool,
				constantExpression(intSource(compilerTypes.Int32, 1, "1")), boolConstant(true)),
			want: "(((void)(1), true) && true)",
		},
		{
			name: "nil or bool",
			node: binaryExpression(checker.LogicalOrOperator, compilerTypes.Nil, compilerTypes.Bool,
				checker.Expression{Kind: checker.NilExpression, ResultType: compilerTypes.Nil}, boolConstant(false)),
			want: "(false || false)",
		},
		{
			name: "not int",
			node: unaryExpression(checker.LogicalNotOperator, compilerTypes.Int32, compilerTypes.Bool,
				constantExpression(intSource(compilerTypes.Int32, 0, "0"))),
			want: "(!((void)(0), true))",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderExpression(testCase.node)
			if err != nil {
				t.Fatalf("renderExpression() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("rendered = %q, want %q", got, testCase.want)
			}
		})
	}
}

func intSource(typ compilerTypes.Type, value int64, literal string) checker.Operand {
	radix := checker.DecimalRadix
	if strings.HasPrefix(literal, "0x") {
		radix = checker.HexadecimalRadix
	} else if strings.HasPrefix(literal, "0b") {
		radix = checker.BinaryRadix
	}
	return checker.Operand{Kind: checker.ConstantOperand, Type: typ, Constant: constant.MakeInt64(value), Literal: literal, Radix: radix}
}

func variableNode(name string) checker.Expression {
	return checker.Expression{Kind: checker.VariableExpression, Name: name}
}

func constantExpression(source checker.Operand) checker.Expression {
	return checker.Expression{Kind: checker.ConstantExpression, Constant: &source, ResultType: source.Type}
}

func expressionPointer(expression checker.Expression) *checker.Expression {
	return &expression
}

func assertGeneratorUnknownError(t *testing.T, err error) {
	t.Helper()
	diagnostic, ok := err.(compilerTypes.Diagnostic)
	if !ok {
		t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
	}
	if diagnostic.Category != compilerTypes.UnknownError || diagnostic.Stage != "generator" {
		t.Fatalf("diagnostic = %#v, want generator Unknown Error", diagnostic)
	}
}

func unaryExpression(operator checker.Operator, operandType, resultType compilerTypes.Type, operand checker.Expression) checker.Expression {
	return checker.Expression{
		Kind:        checker.UnaryOperationExpression,
		Operand:     &operand,
		Operator:    operator,
		OperandType: operandType,
		ResultType:  resultType,
	}
}

func binaryExpression(operator checker.Operator, operandType, resultType compilerTypes.Type, left, right checker.Expression) checker.Expression {
	return checker.Expression{
		Kind:        checker.BinaryOperationExpression,
		Left:        &left,
		Right:       &right,
		Operator:    operator,
		OperandType: operandType,
		ResultType:  resultType,
	}
}

func addressNode(name string) checker.Expression {
	operand := variableNode(name)
	return checker.Expression{Kind: checker.AddressOfExpression, Operand: &operand}
}

func dereferenceNode(name string) checker.Expression {
	operand := variableNode(name)
	return checker.Expression{Kind: checker.DereferenceExpression, Operand: &operand}
}

func nestedDereferenceNode(name string) checker.Expression {
	operand := dereferenceNode(name)
	return checker.Expression{Kind: checker.DereferenceExpression, Operand: &operand}
}

// Function lowering: definitions at file scope, function-pointer
// declarators, calls, and returns.

func funReferenceNode(name string, typ compilerTypes.Type) checker.Expression {
	return checker.Expression{Kind: checker.FunctionReferenceExpression, Name: name, ResultType: typ}
}

func callNode(callee checker.Expression, calleeType, resultType compilerTypes.Type, arguments ...checker.Operand) checker.Expression {
	return checker.Expression{
		Kind:        checker.CallExpression,
		Operand:     &callee,
		Arguments:   arguments,
		OperandType: calleeType,
		ResultType:  resultType,
	}
}

func variableOperand(name string, typ compilerTypes.Type) checker.Operand {
	return checker.Operand{Kind: checker.VariableOperand, Type: typ, Node: variableNode(name)}
}

// identityDeclaration is the checked form of a function returning its only
// parameter unchanged.
func identityDeclaration(fun compilerTypes.Type, result *compilerTypes.Type) checker.FunctionDeclaration {
	return checker.FunctionDeclaration{
		Name:       "identity",
		Parameters: []checker.FunctionParameter{{Name: "x", Type: compilerTypes.Int32}},
		Result:     result,
		Type:       fun,
		Body: []checker.Statement{checker.ReturnStatement{
			Value: &checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: variableNode("x")},
		}},
	}
}

func TestGenerateFunctionDefinition(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	program := checker.Program{Statements: []checker.Statement{identityDeclaration(fun, &result)}}

	want := "#include \"modules/app.h\"\n\n" +
		"static int32_t hex_f_m3_app_identity(const int32_t hex_v_x) {\n" +
		"    return hex_v_x;\n" +
		"}\n\n" +
		"int main(void) {\n    return 0;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if gotC != want {
		t.Fatalf("modules/app.c = %q, want %q", gotC, want)
	}
}

func TestGenerateNoReturnFunctionLowersToVoid(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, nil)
	program := checker.Program{Statements: []checker.Statement{checker.FunctionDeclaration{
		Name:       "reset",
		Parameters: []checker.FunctionParameter{{Name: "x", Type: compilerTypes.Int32}},
		Type:       fun,
		Body:       []checker.Statement{checker.ReturnStatement{}},
	}}}

	want := "static void hex_f_m3_app_reset(const int32_t hex_v_x) {\n    return;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
	}
}

func TestGenerateZeroParameterFunction(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType(nil, &result)
	program := checker.Program{Statements: []checker.Statement{checker.FunctionDeclaration{
		Name:   "zero",
		Result: &result,
		Type:   fun,
		Body: []checker.Statement{checker.ReturnStatement{Value: &checker.Operand{
			Kind: checker.ExpressionOperand,
			Type: compilerTypes.Int32,
			Node: constantExpression(intSource(compilerTypes.Int32, 0, "0")),
		}}},
	}}}

	want := "static int32_t hex_f_m3_app_zero(void) {\n    return 0;\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
	}
}

// The stored pointer type carries unqualified parameters even though the
// definition binds const int32_t; the type keeps the unqualified spelling.
func TestGenerateFunctionPointerObjects(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	reference := checker.Operand{Kind: checker.ExpressionOperand, Type: fun, Node: funReferenceNode("identity", fun)}
	program := checker.Program{Statements: []checker.Statement{
		identityDeclaration(fun, &result),
		checker.Declaration{Name: "callback", Type: fun, Source: reference},
		checker.Declaration{Name: "selected", Type: fun, Mutable: true, Source: reference},
	}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	for _, want := range []string{
		"    int32_t (*const hex_v_callback)(int32_t) = hex_f_m3_app_identity;\n",
		"    int32_t (*hex_v_selected)(int32_t) = hex_f_m3_app_identity;\n",
	} {
		if !strings.Contains(gotC, want) {
			t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
		}
	}
	if strings.Contains(gotC, ")(const int32_t)") {
		t.Fatalf("modules/app.c = %q, function-pointer parameters must stay unqualified", gotC)
	}
}

func TestGenerateFunctionPointerParameter(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	call := callNode(variableNode("callback"), fun, compilerTypes.Int32, variableOperand("value", compilerTypes.Int32))
	program := checker.Program{Statements: []checker.Statement{checker.FunctionDeclaration{
		Name: "apply",
		Parameters: []checker.FunctionParameter{
			{Name: "callback", Type: fun},
			{Name: "value", Type: compilerTypes.Int32},
		},
		Result: &result,
		Type:   environment.FunType([]compilerTypes.Type{fun, compilerTypes.Int32}, &result),
		Body: []checker.Statement{checker.ReturnStatement{
			Value: &checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: call},
		}},
	}}}

	want := "static int32_t hex_f_m3_app_apply(int32_t (*const hex_v_callback)(int32_t), const int32_t hex_v_value) {\n" +
		"    return hex_v_callback(hex_v_value);\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
	}
}

func TestGenerateCallExpression(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	call := callNode(funReferenceNode("identity", fun), fun, compilerTypes.Int32, intSource(compilerTypes.Int32, 13, "13"))
	program := checker.Program{Statements: []checker.Statement{
		identityDeclaration(fun, &result),
		checker.Declaration{Name: "total", Type: compilerTypes.Int32, Source: checker.Operand{
			Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: call,
		}},
	}}

	want := "    const int32_t hex_v_total = hex_f_m3_app_identity(13);\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
	}
}

func TestGenerateCallStatement(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, nil)
	call := callNode(funReferenceNode("reset", fun), fun, compilerTypes.Type{}, intSource(compilerTypes.Int32, 13, "13"))
	program := checker.Program{Statements: []checker.Statement{
		checker.FunctionDeclaration{
			Name:       "reset",
			Parameters: []checker.FunctionParameter{{Name: "x", Type: compilerTypes.Int32}},
			Type:       fun,
		},
		checker.CallStatement{Call: checker.Operand{Kind: checker.ExpressionOperand, Node: call}},
	}}

	want := "    hex_f_m3_app_reset(13);\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
	}
}

func TestGenerateSelfRecursiveFunction(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	call := callNode(funReferenceNode("loop", fun), fun, compilerTypes.Int32, variableOperand("n", compilerTypes.Int32))
	program := checker.Program{Statements: []checker.Statement{checker.FunctionDeclaration{
		Name:       "loop",
		Parameters: []checker.FunctionParameter{{Name: "n", Type: compilerTypes.Int32}},
		Result:     &result,
		Type:       fun,
		Body: []checker.Statement{checker.ReturnStatement{
			Value: &checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: call},
		}},
	}}}

	want := "static int32_t hex_f_m3_app_loop(const int32_t hex_v_n) {\n    return hex_f_m3_app_loop(hex_v_n);\n}\n"
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
	}
}

// Definitions follow the object typedefs, which live in the module header,
// and precede main, in source order.
func TestGenerateFunctionDefinitionsPrecedeMainInSourceOrder(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	point := environment.BeginObject("Point", 1, 1)
	point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: "x", Type: compilerTypes.Int32}})
	second := identityDeclaration(fun, &result)
	second.Name = "second"
	program := checker.Program{
		TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}},
		Statements: []checker.Statement{
			identityDeclaration(fun, &result),
			second,
			checker.Declaration{Name: "x", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
		},
	}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	gotC, gotH := files["modules/app.c"], files["modules/app.h"]
	first := strings.Index(gotC, "hex_f_m3_app_identity")
	next := strings.Index(gotC, "hex_f_m3_app_second")
	run := strings.Index(gotC, "int main(void)")
	if first < 0 || next < first || run < next {
		t.Fatalf("modules/app.c = %q, want hex_f_m3_app_identity then hex_f_m3_app_second then main", gotC)
	}
	if !strings.Contains(gotH, "struct hex_t_Point {") {
		t.Fatalf("modules/app.h = %q, want the object definition region", gotH)
	}
}

func TestGenerateFunctionBodyLineDirectives(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)
	declaration := identityDeclaration(fun, &result)
	declaration.SourceLine = 3
	declaration.Body = []checker.Statement{checker.ReturnStatement{
		Value:      &checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: variableNode("x")},
		SourceLine: 4,
	}}
	program := checker.Program{Statements: []checker.Statement{declaration}}

	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	gotC := files["modules/app.c"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	for _, want := range []string{
		"#line 3 \"app.hex\"\nstatic int32_t hex_f_m3_app_identity(",
		"#line 4 \"app.hex\"\n    return hex_v_x;",
	} {
		if !strings.Contains(gotC, want) {
			t.Fatalf("modules/app.c = %q, want it to contain %q", gotC, want)
		}
	}
}
