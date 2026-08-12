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

	wantC := "#include \"main.h\"\n\nint main(void) {\n    const int32_t hex_v_x = 13;\n    return EXIT_SUCCESS;\n}\n"
	gotC, _ := Generate(program)
	if gotC != wantC {
		t.Fatalf("main.c = %q, want %q", gotC, wantC)
	}
}

func TestGenerateFailure(t *testing.T) {
	gotC, _ := GenerateFailure()
	wantC := "#include \"main.h\"\n\nint main(void) {\n    return EXIT_FAILURE;\n}\n"
	if gotC != wantC {
		t.Fatalf("failure output = %q, want %q", gotC, wantC)
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
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainH, "hex_internal_union_1") || !strings.Contains(mainC, ".tag") {
		t.Fatalf("generated union output = C:%q H:%q, want tagged representation", mainC, mainH)
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
	state, err := discoverGeneratedUnions(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.order) != 1 || state.order[0].CName != "hex_internal_union_1" {
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
	forged.CName = "hex_internal_union_forged"
	if supportedGeneratedType(forged) {
		t.Fatal("forged tagged union metadata was accepted")
	}
}

func TestGenerateUnionOperations(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Float64 = 1 active: Bool = value is Int32 maybe: Int32 | Float64 | Nil = nil present: Bool = maybe != nil left: Int32 | Bool = true right: Bool | Int32 = false same: Bool = left == right small: Int32 | Bool = true wide: Int32 | Bool | Nil = small")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"typedef enum hex_internal_union_1_tag",
		"typedef struct hex_internal_union_1",
		".tag == hex_internal_union_1_tag_member_0",
		"hex_internal_union_3_equal",
		"hex_internal_widen_hex_internal_union_3_to_hex_internal_union_4",
	} {
		if !strings.Contains(mainC, want) && !strings.Contains(mainH, want) {
			t.Fatalf("generated output does not contain %q: C=%q H=%q", want, mainC, mainH)
		}
	}
}

func TestGenerateUnionTruthiness(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Bool | Nil = true if value noop: Int32 = 0 end")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, "hex_internal_union_1_truthy") || !strings.Contains(mainH, "static bool hex_internal_union_1_truthy") {
		t.Fatalf("truthiness output = C:%q H:%q, want tagged truthiness helper", mainC, mainH)
	}
}

func TestGenerateNarrowedUnionPayloadRead(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 | Float64 = 1 if value is Int32 result: Int32 = value end")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, "hex_v_value.payload.member_0") {
		t.Fatalf("generated C = %q, want narrowed payload read", mainC)
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

	want := "#include \"main.h\"\n\nint main(void) {\n    const bool hex_v_enabled = true;\n    return EXIT_SUCCESS;\n}\n"
	gotC, _ := Generate(program)
	if gotC != want {
		t.Fatalf("main.c = %q, want %q", gotC, want)
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

	want := "#include \"main.h\"\n\nint main(void) {\n    const int32_t hex_v_mask = 0xFF;\n    return EXIT_SUCCESS;\n}\n"
	gotC, _ := Generate(program)
	if gotC != want {
		t.Fatalf("main.c = %q, want %q", gotC, want)
	}
}

func TestGenerateStatementsInOrder(t *testing.T) {
	program := checker.Program{
		Statements: []checker.Statement{
			checker.Declaration{Name: "x", Type: compilerTypes.Int32, Mutable: true, Source: intSource(compilerTypes.Int32, 13, "13")},
			checker.Assignment{Name: "x", Type: compilerTypes.Int32, Target: checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: variableNode("x")}, Source: intSource(compilerTypes.Int32, 14, "14")},
		},
	}

	want := "#include \"main.h\"\n\nint main(void) {\n    int32_t hex_v_x = 13;\n    hex_v_x = 14;\n    return EXIT_SUCCESS;\n}\n"
	gotC, _ := Generate(program)
	if gotC != want {
		t.Fatalf("main.c = %q, want %q", gotC, want)
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

	wantC := "#include \"main.h\"\n\n" +
		"int main(void) {\n" +
		"    int32_t hex_v_x = 13;\n" +
		"    int32_t *hex_v_p = &hex_v_x;\n" +
		"    *hex_v_p = 14;\n" +
		"    hex_v_p = &hex_v_x;\n" +
		"    return EXIT_SUCCESS;\n" +
		"}\n"
	gotC, _ := Generate(program)
	if gotC != wantC {
		t.Fatalf("main.c = %q, want %q", gotC, wantC)
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
	gotC, _ := Generate(program)
	for _, want := range []string{"int32_t hex_v_x = 1;", "int32_t *const hex_v_p = &hex_v_x;", "int32_t *const *const hex_v_pp = &hex_v_p;", "const int32_t hex_v_y = *(*hex_v_pp);"} {
		if !strings.Contains(gotC, want) {
			t.Fatalf("main.c = %q, want %q", gotC, want)
		}
	}
	if strings.Contains(gotC, "hexal_alloc") || strings.Contains(gotC, "free(") || strings.Contains(gotC, "Hexal_Ref") {
		t.Fatalf("main.c contains removed ownership machinery: %q", gotC)
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

	mainC, mainH, err := GenerateChecked(program)
	assertGeneratorUnknownError(t, err)
	if mainC != "" || mainH != "" {
		t.Fatalf("generated output for forged assignment target: mainC=%q mainH=%q", mainC, mainH)
	}
}

func TestGenerateCheckedRejectsDuplicateDeclarationNames(t *testing.T) {
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "value", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
		checker.Declaration{Name: "value", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 2, "2")},
	}}

	mainC, mainH, err := GenerateChecked(program)
	assertGeneratorUnknownError(t, err)
	if mainC != "" || mainH != "" {
		t.Fatalf("generated output for duplicate declaration: mainC=%q mainH=%q", mainC, mainH)
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

	mainC, mainH, err := GenerateChecked(program)
	assertGeneratorUnknownError(t, err)
	if mainC != "" || mainH != "" {
		t.Fatalf("generated output for duplicate object C name: mainC=%q mainH=%q", mainC, mainH)
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
			_, _, err := GenerateChecked(program)
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestGenerateCheckedRejectsForgedTypeAndMemberNames(t *testing.T) {
	for _, name := range []string{"Type-name", "1Type", "café"} {
		t.Run("type "+name, func(t *testing.T) {
			_, _, err := GenerateChecked(checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: name, Type: compilerTypes.Int32}}})
			assertGeneratorUnknownError(t, err)
		})

		t.Run("member "+name, func(t *testing.T) {
			environment := compilerTypes.NewEnvironment()
			point := environment.BeginObject("Point", 1, 1)
			point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: name, Type: compilerTypes.Int32}})
			_, _, err := GenerateChecked(checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}}})
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
	gotC, _ := Generate(program)
	if !strings.Contains(gotC, "#line 4 \"main.hexal\"\n    const int32_t hex_v_x = 13;") {
		t.Fatalf("main.c = %q, want a line directive before the declaration", gotC)
	}
}

func TestPrivateCNameUsesOneUnconditionalPrefix(t *testing.T) {
	testCases := []struct {
		kind   NameKind
		source string
		want   string
	}{
		{ValueName, "main", "hex_v_main"},
		{ValueName, "int", "hex_v_int"},
		{ValueName, "INT32_MAX", "hex_v_INT32_MAX"},
		{ValueName, "hex_v_score", "hex_v_hex_v_score"},
		{TypeName, "Point", "hex_t_Point"},
		{MemberName, "x", "hex_m_x"},
	}
	for _, testCase := range testCases {
		if got := PrivateCName(testCase.kind, testCase.source); got != testCase.want {
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
	_, _, err := GenerateChecked(program)
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
			mainC, mainH, err := GenerateChecked(checker.Program{Statements: []checker.Statement{testCase.statement}})
			assertGeneratorUnknownError(t, err)
			if mainC != "" || mainH != "" {
				t.Fatalf("generated output for loop control outside a loop: mainC=%q mainH=%q", mainC, mainH)
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

	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if mainH == "" || strings.Count(mainC, "while (true) {") != 2 || strings.Count(mainC, "break;") != 2 || strings.Count(mainC, "continue;") != 1 {
		t.Fatalf("nested loop output = %q, want two loops, two breaks, and one continue", mainC)
	}
}

func TestGenerateCheckedRestoresLoopContextAfterLoop(t *testing.T) {
	condition := checker.Operand{
		Kind:     checker.ConstantOperand,
		Type:     compilerTypes.Bool,
		Constant: constant.MakeBool(true),
		Literal:  "true",
	}
	mainC, mainH, err := GenerateChecked(checker.Program{Statements: []checker.Statement{
		checker.WhileStatement{Condition: condition, Body: []checker.Statement{checker.ContinueStatement{}}},
		checker.BreakStatement{},
	}})
	assertGeneratorUnknownError(t, err)
	if mainC != "" || mainH != "" {
		t.Fatalf("generated output after loop-context leak: mainC=%q mainH=%q", mainC, mainH)
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
			mainC, mainH, err := GenerateChecked(testCase.program)
			assertGeneratorUnknownError(t, err)
			if mainC != "" || mainH != "" {
				t.Fatalf("generated output for forged missing return: mainC=%q mainH=%q", mainC, mainH)
			}
		})
	}
}

// Spec 0015: function and method declarations remain module-level only.
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
				mainC, mainH, err := GenerateChecked(checker.Program{
					TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}},
					Statements:       []checker.Statement{block.statement},
				})
				assertGeneratorUnknownError(t, err)
				if mainC != "" || mainH != "" {
					t.Fatalf("generated output for nested declaration: mainC=%q mainH=%q", mainC, mainH)
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
	_, _, err := GenerateChecked(program)
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
		mainC, mainH, err := GenerateChecked(program)
		diagnostic, ok := err.(compilerTypes.Diagnostic)
		if !ok {
			t.Errorf("case %d error = %T %v, want compilerTypes.Diagnostic", index, err, err)
			continue
		}
		if diagnostic.Category != compilerTypes.UnknownError || diagnostic.Stage != "generator" {
			t.Errorf("case %d diagnostic = %#v, want generator Unknown Error", index, diagnostic)
		}
		if mainC != "" || mainH != "" {
			t.Errorf("case %d returned generated C for forged metadata: mainC=%q mainH=%q", index, mainC, mainH)
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
			_, _, err := GenerateChecked(checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: testCase.declName, Type: testCase.typ}}})
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

			mainC, mainH, err := GenerateChecked(checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Forged", Type: forged}}})
			assertGeneratorUnknownError(t, err)
			if mainC != "" || mainH != "" {
				t.Fatalf("generated output for forged pointer metadata: mainC=%q mainH=%q", mainC, mainH)
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
			_, _, err := GenerateChecked(checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Type: testCase.typ}}})
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
	want := "((uint64_t)(uint8_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) <= (uint64_t)INT8_MAX ? (int8_t)(uint8_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) : INT8_MIN + (int8_t)((uint64_t)(uint8_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) - (uint64_t)INT8_MAX - (uint64_t)1))"
	if got != want {
		t.Fatalf("signed Int8 addition = %q, want %q", got, want)
	}
}

func TestRenderSignedArithmeticUsesPromotionSafeUnsignedIntermediate(t *testing.T) {
	testCases := []struct {
		typ      compilerTypes.Type
		unsigned string
		minimum  string
		maximum  string
	}{
		{compilerTypes.Int8, "uint8_t", "INT8_MIN", "INT8_MAX"},
		{compilerTypes.Int16, "uint16_t", "INT16_MIN", "INT16_MAX"},
		{compilerTypes.Int32, "uint32_t", "INT32_MIN", "INT32_MAX"},
		{compilerTypes.Int64, "uint64_t", "INT64_MIN", "INT64_MAX"},
	}
	for _, testCase := range testCases {
		for _, operator := range []checker.Operator{checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator} {
			node := binaryExpression(operator, testCase.typ, testCase.typ, variableNode("left"), variableNode("right"))
			got, err := renderExpression(node)
			if err != nil {
				t.Fatalf("renderExpression(%s, %s) error = %v", testCase.typ.Name, operator, err)
			}
			operatorText, _ := binaryCOperator(operator)
			unsignedResult := fmt.Sprintf("(%s)((uint64_t)hex_v_left %s (uint64_t)hex_v_right)", testCase.unsigned, operatorText)
			want := fmt.Sprintf("((uint64_t)%s <= (uint64_t)%s ? (%s)%s : %s + (%s)((uint64_t)%s - (uint64_t)%s - (uint64_t)1))", unsignedResult, testCase.maximum, testCase.typ.CName, unsignedResult, testCase.minimum, testCase.typ.CName, unsignedResult, testCase.maximum)
			if got != want {
				t.Errorf("signed %s %s = %q, want %q", testCase.typ.Name, operator, got, want)
			}
		}

		node := unaryExpression(checker.NegateOperator, testCase.typ, testCase.typ, variableNode("value"))
		got, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression(%s unary -) error = %v", testCase.typ.Name, err)
		}
		unsignedResult := fmt.Sprintf("(%s)((uint64_t)0 - (uint64_t)hex_v_value)", testCase.unsigned)
		want := fmt.Sprintf("((uint64_t)%s <= (uint64_t)%s ? (%s)%s : %s + (%s)((uint64_t)%s - (uint64_t)%s - (uint64_t)1))", unsignedResult, testCase.maximum, testCase.typ.CName, unsignedResult, testCase.minimum, testCase.typ.CName, unsignedResult, testCase.maximum)
		if got != want {
			t.Errorf("signed %s negation = %q, want %q", testCase.typ.Name, got, want)
		}
	}
}

func TestRenderUnsignedArithmeticCastsOperandsAndResult(t *testing.T) {
	for _, typ := range []compilerTypes.Type{compilerTypes.UInt8, compilerTypes.UInt16, compilerTypes.UInt32, compilerTypes.UInt64} {
		node := binaryExpression(checker.AddOperator, typ, typ, variableNode("left"), variableNode("right"))
		got, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression(%s) error = %v", typ.Name, err)
		}
		intermediate := typ.CName
		if compilerTypes.Equal(typ, compilerTypes.UInt8) || compilerTypes.Equal(typ, compilerTypes.UInt16) {
			intermediate = compilerTypes.UInt32.CName
		} else {
			intermediate = compilerTypes.UInt64.CName
		}
		want := fmt.Sprintf("(%s)((%s)hex_v_left + (%s)hex_v_right)", typ.CName, intermediate, intermediate)
		if got != want {
			t.Errorf("unsigned %s addition = %q, want %q", typ.Name, got, want)
		}
	}
}

func TestRenderUnsignedNarrowMultiplicationUsesUInt32Intermediate(t *testing.T) {
	for _, typ := range []compilerTypes.Type{compilerTypes.UInt8, compilerTypes.UInt16} {
		node := binaryExpression(checker.MultiplyOperator, typ, typ, variableNode("left"), variableNode("right"))
		got, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression(%s) error = %v", typ.Name, err)
		}
		want := fmt.Sprintf("(%s)((uint32_t)hex_v_left * (uint32_t)hex_v_right)", typ.CName)
		if got != want {
			t.Errorf("unsigned %s multiplication = %q, want %q", typ.Name, got, want)
		}
	}
}

// The emitted #error guards are asserted textually. Proving the preprocessor
// actually rejects a target with the macros undefined needs a C toolchain; see
// spec 0013 for the deferred c23 build-tag suite.
func TestFloatTargetAssertionsFailClosed(t *testing.T) {
	mainH := header(true, true, false, nil)
	for _, want := range []string{
		"static_assert(sizeof(float) == 4 && FLT_MANT_DIG == 24 && FLT_MAX_EXP == 128, \"Hexal Float32 requires the binary32 value set\");",
		"#if !defined(FLT_IS_IEC_60559) || FLT_IS_IEC_60559 != 1\n#error \"Hexal Float32 requires IEC 60559\"\n#endif",
		"static_assert(sizeof(double) == 8 && DBL_MANT_DIG == 53 && DBL_MAX_EXP == 1024, \"Hexal Float64 requires the binary64 value set\");",
		"#if !defined(DBL_IS_IEC_60559) || DBL_IS_IEC_60559 != 1\n#error \"Hexal Float64 requires IEC 60559\"\n#endif",
	} {
		if !strings.Contains(mainH, want) {
			t.Fatalf("main.h = %q, want %q", mainH, want)
		}
	}

	// A header emits only the guards for the float kinds the program uses.
	for _, testCase := range []struct {
		name    string
		mainH   string
		want    string
		notWant string
	}{
		{name: "Float32", mainH: header(true, false, false, nil), want: "FLT_IS_IEC_60559", notWant: "DBL_IS_IEC_60559"},
		{name: "Float64", mainH: header(false, true, false, nil), want: "DBL_IS_IEC_60559", notWant: "FLT_IS_IEC_60559"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if !strings.Contains(testCase.mainH, testCase.want) {
				t.Fatalf("main.h = %q, want %s guard", testCase.mainH, testCase.want)
			}
			if strings.Contains(testCase.mainH, testCase.notWant) {
				t.Fatalf("main.h = %q, want no %s guard", testCase.mainH, testCase.notWant)
			}
		})
	}
}

func TestGenerateSignedWrappingBoundaries(t *testing.T) {
	minimum := intSource(compilerTypes.Int8, -128, "128")
	minimum.Negative = true
	typ := compilerTypes.Int8
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "value", Type: typ, Mutable: true, Source: intSource(typ, 127, "127")},
		checker.Declaration{
			Name: "wrapped",
			Type: typ,
			Source: checker.Operand{
				Kind: checker.ExpressionOperand,
				Type: typ,
				Node: binaryExpression(checker.AddOperator, typ, typ, variableNode("value"), constantExpression(intSource(typ, 1, "1"))),
			},
		},
		checker.Declaration{Name: "minimum", Type: typ, Mutable: true, Source: minimum},
		checker.Declaration{
			Name: "underflow",
			Type: typ,
			Source: checker.Operand{
				Kind: checker.ExpressionOperand,
				Type: typ,
				Node: binaryExpression(checker.SubtractOperator, typ, typ, variableNode("minimum"), constantExpression(intSource(typ, 1, "1"))),
			},
		},
		checker.Declaration{
			Name: "negated",
			Type: typ,
			Source: checker.Operand{
				Kind: checker.ExpressionOperand,
				Type: typ,
				Node: unaryExpression(checker.NegateOperator, typ, typ, variableNode("minimum")),
			},
		},
	}}
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	wantWrapped := "((uint64_t)(uint8_t)((uint64_t)hex_v_value + (uint64_t)1) <= (uint64_t)INT8_MAX ? (int8_t)(uint8_t)((uint64_t)hex_v_value + (uint64_t)1) : INT8_MIN + (int8_t)((uint64_t)(uint8_t)((uint64_t)hex_v_value + (uint64_t)1) - (uint64_t)INT8_MAX - (uint64_t)1))"
	if !strings.Contains(mainC, wantWrapped) {
		t.Fatalf("main.c = %q, want conditional signed wrap for Int8 127 + 1", mainC)
	}
	// The wrap operands must never reach C as a plain narrowing conversion of a
	// signed value, which is implementation-defined before C23.
	for _, forbidden := range []string{
		"const int8_t hex_v_wrapped = (int8_t)(uint8_t)((uint64_t)hex_v_value + (uint64_t)1);",
		"const int8_t hex_v_negated = (int8_t)(uint64_t)((uint64_t)0 - (uint64_t)hex_v_minimum);",
	} {
		if strings.Contains(mainC, forbidden) {
			t.Fatalf("main.c contains implementation-defined signed conversion %q", forbidden)
		}
	}
	if mainH == "" {
		t.Fatal("GenerateChecked() returned an empty header")
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
		{"negate", unaryExpression(checker.NegateOperator, compilerTypes.Int32, compilerTypes.Int32, left), "((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_left) <= (uint64_t)INT32_MAX ? (int32_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_left) : INT32_MIN + (int32_t)((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_left) - (uint64_t)INT32_MAX - (uint64_t)1))"},
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
			mainC, mainH, err := GenerateChecked(testCase.program)
			if testCase.wantError {
				assertGeneratorUnknownError(t, err)
				if mainC != "" || mainH != "" {
					t.Fatalf("generated output for forged place metadata: mainC=%q mainH=%q", mainC, mainH)
				}
				return
			}
			if err != nil {
				t.Fatalf("GenerateChecked() error = %v", err)
			}
			if !strings.Contains(mainC, testCase.wantC) {
				t.Fatalf("main.c = %q, want fragment %q", mainC, testCase.wantC)
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

	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(mainC, "(*(*hex_v_pp)).hex_m_x = 2;") {
		t.Fatalf("main.c = %q, want nested dereference/member assignment", mainC)
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
	_, _, err := GenerateChecked(checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: forged}}})
	assertGeneratorUnknownError(t, err)
}

func TestGenerateCheckedRejectsObjectReferenceWithoutTypeDeclaration(t *testing.T) {
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

	_, _, err := GenerateChecked(program)
	assertGeneratorUnknownError(t, err)
}

func TestRenderRejectsNestedMalformedOperation(t *testing.T) {
	inner := unaryExpression(checker.NegateOperator, compilerTypes.Int32, compilerTypes.Int32, addressNode("value"))
	outer := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, inner, variableNode("other"))
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: outer})
	assertGeneratorUnknownError(t, err)
}

func TestUsedFloatTypesTraversesOperationNodes(t *testing.T) {
	float32Comparison := binaryExpression(checker.EqualOperator, compilerTypes.Float32, compilerTypes.Bool, variableNode("a"), variableNode("b"))
	float64Comparison := binaryExpression(checker.EqualOperator, compilerTypes.Float64, compilerTypes.Bool, variableNode("c"), variableNode("d"))
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "one", Type: compilerTypes.Bool, Source: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Bool, Node: float32Comparison}},
		checker.Declaration{Name: "two", Type: compilerTypes.Bool, Source: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Bool, Node: float64Comparison}},
	}}
	float32Used, float64Used, nilUsed := usedFloatTypes(program)
	if !float32Used || !float64Used || nilUsed {
		t.Fatalf("usedFloatTypes() = (%v, %v, %v), want (true, true, false)", float32Used, float64Used, nilUsed)
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
		// RFC 0023: a non-Bool logical operand is valid (its truthiness is
		// rendered), so only a non-Bool result stays malformed.
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

// RFC 0023: conditions render through truthiness — nil as false, a nullable
// as a null test, and an always-truthy value as a comma evaluation.
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
			want: "if ((0, true)) {",
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
	if !strings.Contains(body.String(), "if ((hex_v_maybe != NULL)) {") {
		t.Fatalf("body = %q, want a nullable null-test condition", body.String())
	}
}

// RFC 0023: logical operations render mixed and non-Bool operands through
// their truthiness; constant operands need no bindings.
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
			want: "((1, true) && true)",
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
			want: "(!(0, true))",
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

// Function lowering (spec 0008 "C23 lowering"): definitions at file scope,
// function-pointer declarators, calls, and returns.

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

	want := "#include \"main.h\"\n\n" +
		"static int32_t hex_f_identity(const int32_t hex_v_x) {\n" +
		"    return hex_v_x;\n" +
		"}\n\n" +
		"int main(void) {\n    return EXIT_SUCCESS;\n}\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if gotC != want {
		t.Fatalf("main.c = %q, want %q", gotC, want)
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

	want := "static void hex_f_reset(const int32_t hex_v_x) {\n    return;\n}\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
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

	want := "static int32_t hex_f_zero(void) {\n    return 0;\n}\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
	}
}

// The stored pointer type carries unqualified parameters even though the
// definition binds const int32_t. Spec 0008 forbids "correcting" that.
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

	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	for _, want := range []string{
		"    int32_t (*const hex_v_callback)(int32_t) = hex_f_identity;\n",
		"    int32_t (*hex_v_selected)(int32_t) = hex_f_identity;\n",
	} {
		if !strings.Contains(gotC, want) {
			t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
		}
	}
	if strings.Contains(gotC, ")(const int32_t)") {
		t.Fatalf("main.c = %q, function-pointer parameters must stay unqualified", gotC)
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

	want := "static int32_t hex_f_apply(int32_t (*const hex_v_callback)(int32_t), const int32_t hex_v_value) {\n" +
		"    return hex_v_callback(hex_v_value);\n}\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
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

	want := "    const int32_t hex_v_total = hex_f_identity(13);\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
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

	want := "    hex_f_reset(13);\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
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

	want := "static int32_t hex_f_loop(const int32_t hex_v_n) {\n    return hex_f_loop(hex_v_n);\n}\n"
	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if !strings.Contains(gotC, want) {
		t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
	}
}

// Definitions follow the object typedefs, which live in main.h, and precede
// main, in source order.
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

	gotC, gotH, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	first := strings.Index(gotC, "hex_f_identity")
	next := strings.Index(gotC, "hex_f_second")
	main := strings.Index(gotC, "int main(void)")
	if first < 0 || next < first || main < next {
		t.Fatalf("main.c = %q, want hex_f_identity then hex_f_second then main", gotC)
	}
	if !strings.Contains(gotH, "struct hex_t_Point {") {
		t.Fatalf("main.h = %q, want the object definition region", gotH)
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

	gotC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	for _, want := range []string{
		"#line 3 \"main.hexal\"\nstatic int32_t hex_f_identity(",
		"#line 4 \"main.hexal\"\n    return hex_v_x;",
	} {
		if !strings.Contains(gotC, want) {
			t.Fatalf("main.c = %q, want it to contain %q", gotC, want)
		}
	}
}
