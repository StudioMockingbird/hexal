package generator

import (
	"go/constant"
	"strings"
	"testing"

	"hexal/compiler/checker"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
	"hexal/stdlib"
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
	files := generateOne(t, program)
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

// An induced checked-tree inconsistency surfaces as a fail-closed
// [Unknown Error] return from GenerateChecked, never as a panic and never as
// a user-facing category.
func TestGenerateCheckedReportsInvariantBreakAsUnknownError(t *testing.T) {
	program := checkedGeneratorSource(t, "fun answer(value: Int32): Int32 do\n    return value * 3\nend\nlet started: Int32 = answer(6)\n")
	tampered := false
	for index, statement := range program.Statements {
		function, ok := statement.(checker.FunctionDeclaration)
		if !ok || function.Name != "answer" {
			continue
		}
		returnStatement, ok := function.Body[0].(checker.ReturnStatement)
		if !ok || returnStatement.Value == nil {
			continue
		}
		returnStatement.Value.Node.Kind = checker.ExpressionKind(250)
		function.Body[0] = returnStatement
		program.Statements[index] = function
		tampered = true
	}
	if !tampered {
		t.Fatal("no function found to tamper")
	}
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	if err == nil {
		t.Fatalf("tampered program generated %d artifacts; want an invariant-break error", len(files))
	}
	if files != nil {
		t.Fatalf("files = %v, want nil on failure", files)
	}
	messages := compilerTypes.ErrorMessages(err)
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "[Unknown Error]") {
		t.Fatalf("error messages = %q, want an [Unknown Error] entry", joined)
	}
}

// testSpanTable is the shared source table for programs the generator tests
// check from real source. checkedGeneratorSource and the multi-module helpers
// register each logical source here, so a carried span resolves to the same
// line and column a compilation derives; a hand-built program with no spans
// keeps the zero position and the no-location rendering. The #line tests build
// their own table with controlled text.
var testSpanTable = span.NewTable()

// The embedded source stdlib is reachable from a checked test program (a
// std.io or std.fs call specializes a stdlib function), so its spans name
// stdlib logical keys too. Register them once; a per-test source overwrites
// only its own key.
func init() {
	for key, text := range stdlib.Sources() {
		testSpanTable.Add(key, text)
	}
}

func checkedGeneratorSource(t *testing.T, source string) checker.Program {
	t.Helper()
	testSpanTable.Add("test.hex", source)
	tokens, err := lexer.Lex("test.hex", source)
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	assertGeneratorUnknownError(t, err)
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output for forged assignment target: rootC := %q rootH=%q", rootC, rootH)
	}
}

func TestGenerateCheckedRejectsDuplicateDeclarationNames(t *testing.T) {
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "value", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 1, "1")},
		checker.Declaration{Name: "value", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 2, "2")},
	}}

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	assertGeneratorUnknownError(t, err)
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output for duplicate declaration: rootC := %q rootH=%q", rootC, rootH)
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

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	assertGeneratorUnknownError(t, err)
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output for duplicate object C name: rootC := %q rootH=%q", rootC, rootH)
	}
}

// Forged-name rejection is table-driven over (name kind, spelling) so every
// kind exercises every invalid shape; value spellings are lowercase-first and
// type spellings uppercase-first because the kinds carry distinct rules.
var invalidIdentifierCases = []struct {
	kind      string
	spellings []string
}{
	{"declaration", []string{"value-name", "1value", "café"}},
	{"type", []string{"Type-name", "1Type", "café"}},
	{"member", []string{"Type-name", "1Type", "café"}},
	{"value", []string{"value-name", "1value", "café"}},
}

func TestGenerateCheckedRejectsForgedDeclarationNames(t *testing.T) {
	for _, name := range invalidIdentifierCases[0].spellings {
		t.Run(name, func(t *testing.T) {
			program := checker.Program{Statements: []checker.Statement{checker.Declaration{
				Name:   name,
				Type:   compilerTypes.Int32,
				Source: intSource(compilerTypes.Int32, 1, "1"),
			}}}
			_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestGenerateCheckedRejectsForgedTypeAndMemberNames(t *testing.T) {
	for _, name := range invalidIdentifierCases[1].spellings {
		t.Run("type "+name, func(t *testing.T) {
			_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: name, Type: compilerTypes.Int32}}}}, Config{SourceTable: testSpanTable})
			assertGeneratorUnknownError(t, err)
		})

		t.Run("member "+name, func(t *testing.T) {
			environment := compilerTypes.NewEnvironment()
			point := environment.BeginObject("Point", 1, 1)
			point = environment.CompleteObject("Point", []compilerTypes.ObjectMember{{Name: name, Type: compilerTypes.Int32}})
			_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}}}}, Config{SourceTable: testSpanTable})
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderRejectsForgedValueNames(t *testing.T) {
	for _, name := range invalidIdentifierCases[3].spellings {
		t.Run(name, func(t *testing.T) {
			_, err := renderExpression(variableNode(name), newLiteralRegistry())
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
		"static int32_t hex_f_m3_app_identity(int32_t);\n\n" +
		"static int32_t hex_f_m3_app_identity(const int32_t hex_v_x) {\n" +
		"    return hex_v_x;\n" +
		"}\n\n" +
		"int main(void) {\n    return 0;\n}\n"
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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

	files := generateOne(t, program)
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
	table, functionSpan := appSourceAtLine(3)
	_, returnSpan := appSourceAtLine(4)
	declaration := identityDeclaration(fun, &result)
	declaration.Span = functionSpan
	declaration.Body = []checker.Statement{checker.ReturnStatement{
		Value: &checker.Operand{Kind: checker.VariableOperand, Type: compilerTypes.Int32, Node: variableNode("x")},
		Span:  returnSpan,
	}}
	program := checker.Program{Statements: []checker.Statement{declaration}}

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: table})
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

// appModuleGraph is the single-module graph these tests generate against:
// one node, canonical "app", read from source key "app.hex". The generator
// reads only Order, Root, and each node's LogicalKey, so the parsed program
// stays empty.
func appModuleGraph() *checker.ModuleGraph {
	return checker.SingleModuleGraph(parser.Program{})
}

// moduleGraphOf builds the graph a multi-module test would otherwise receive
// from reachability: dependency-first ids, each node's exact source key, and
// the resolved import edges stated explicitly rather than re-derived.
func moduleGraphOf(root string, order []string, parsed map[string]parser.Program, edges map[string][]checker.ModuleEdge) *checker.ModuleGraph {
	graph := &checker.ModuleGraph{Order: order, Modules: make(map[string]checker.ModuleNode, len(order)), Root: root}
	for _, canonical := range order {
		key := canonical + ".hex"
		graph.Modules[canonical] = checker.ModuleNode{
			Canonical: canonical, LogicalKey: key, Program: parsed[key], Imports: edges[canonical],
		}
	}
	return graph
}

// appSourceAtLine builds the one-file source table a #line test needs to
// resolve a span, and returns the zero-width span at the start of the given
// 1-based line. The file has one single-letter line per entry, so line n begins
// at byte 2*(n-1); a test reads a directive from the table exactly as a
// compilation does.
func appSourceAtLine(line int) (*span.Table, span.Span) {
	table := span.NewTable()
	table.Add("app.hex", "a\nb\nc\nd\ne")
	return table, span.Span{File: "app.hex", Start: 2 * (line - 1)}
}

// generateOne generates the single-module program these unit tests are built
// from and fails the test on any error. It replaces the call-plus-three-line
// error check that appeared verbatim at over a hundred sites.
func generateOne(t *testing.T, program checker.Program) map[string]string {
	t.Helper()
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	return files
}
