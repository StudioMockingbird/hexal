package generator

// Generation invariants: line directives, private C naming, loop and
// return structure, declaration and metadata validation, and place and
// object-identity checking.

import (
	"go/constant"
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func TestGenerateLineDirectives(t *testing.T) {
	table, declarationSpan := appSourceAtLine(4)
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "x", Type: compilerTypes.Int32, Source: intSource(compilerTypes.Int32, 13, "13"), Span: declarationSpan},
	}}
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: table})
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
		kind   nameKind
		source string
		owner  string
		want   string
	}{
		{valueName, "main", "", "hex_v_main"},
		{valueName, "int", "3_app", "hex_v_int"},
		{valueName, "INT32_MAX", "3_app", "hex_v_INT32_MAX"},
		{valueName, "hex_v_score", "3_app", "hex_v_hex_v_score"},
		{typeName, "Point", "3_app", "hex_t_3_app_Point"},
		{typeName, "Point", "", "hex_t_Point"},
		{memberName, "x", "3_app", "hex_m_x"},
		{functionName, "add", "3_app", "hex_f_3_app_add"},
		{functionName, "add", "", "hex_f_add"},
	}
	for _, testCase := range testCases {
		if got := privateCName(testCase.kind, testCase.source, testCase.owner); got != testCase.want {
			t.Errorf("privateCName(%v, %q) = %q, want %q", testCase.kind, testCase.source, got, testCase.want)
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
	_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	if err == nil || !strings.Contains(err.Error(), "[Unknown Error ") {
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
			files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{Statements: []checker.Statement{testCase.statement}}}, Config{SourceTable: testSpanTable})
			assertGeneratorUnknownError(t, err)
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if rootC != "" || rootH != "" {
				t.Fatalf("generated output for loop control outside a loop: rootC := %q rootH=%q", rootC, rootH)
			}
		})
	}
}

func TestWriteStatementsRejectsLoopControlOutsideGeneratedLoop(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		statement checker.Statement
	}{
		{name: "break", statement: checker.BreakStatement{}},
		{name: "continue", statement: checker.ContinueStatement{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var body strings.Builder
			err := writeStatementsAt(&body, []checker.Statement{testCase.statement}, &expressionValidation{}, statementFrame{}, "    ")
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

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{Statements: []checker.Statement{
		checker.WhileStatement{Condition: condition, Body: []checker.Statement{checker.ContinueStatement{}}},
		checker.BreakStatement{},
	}}}, Config{SourceTable: testSpanTable})
	assertGeneratorUnknownError(t, err)
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if rootC != "" || rootH != "" {
		t.Fatalf("generated output after loop-context leak: rootC := %q rootH=%q", rootC, rootH)
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
			files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": testCase.program}, Config{SourceTable: testSpanTable})
			assertGeneratorUnknownError(t, err)
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if rootC != "" || rootH != "" {
				t.Fatalf("generated output for forged missing return: rootC := %q rootH=%q", rootC, rootH)
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
				files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{
					TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: point}},
					Statements:       []checker.Statement{block.statement},
				}}, Config{SourceTable: testSpanTable})
				assertGeneratorUnknownError(t, err)
				rootC, rootH := files["modules/app.c"], files["modules/app.h"]
				if rootC != "" || rootH != "" {
					t.Fatalf("generated output for nested declaration: rootC := %q rootH=%q", rootC, rootH)
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
	}, &expressionValidation{}, statementFrame{}, "    ")
	assertGeneratorUnknownError(t, err)
}

func TestGenerateCheckedFailsClosedForUnknownTypeDeclaration(t *testing.T) {
	program := checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Alias"}}}
	_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
	if err == nil || !strings.Contains(err.Error(), "[Unknown Error ") {
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
		files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
		rootC, rootH := files["modules/app.c"], files["modules/app.h"]
		diagnostic, ok := err.(compilerTypes.Diagnostic)
		if !ok {
			t.Errorf("case %d error = %T %v, want compilerTypes.Diagnostic", index, err, err)
			continue
		}
		if diagnostic.Message.Category() != compilerTypes.UnknownError || diagnostic.Message.Stage() != "generator" {
			t.Errorf("case %d diagnostic = %#v, want generator Unknown Error", index, diagnostic)
		}
		if rootC != "" || rootH != "" {
			t.Errorf("case %d returned generated C for forged metadata: rootC := %q rootH=%q", index, rootC, rootH)
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
			_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: testCase.declName, Type: testCase.typ}}}}, Config{SourceTable: testSpanTable})
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

			files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Forged", Type: forged}}}}, Config{SourceTable: testSpanTable})
			assertGeneratorUnknownError(t, err)
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if rootC != "" || rootH != "" {
				t.Fatalf("generated output for forged pointer metadata: rootC := %q rootH=%q", rootC, rootH)
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
			_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Type: testCase.typ}}}}, Config{SourceTable: testSpanTable})
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
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: node}, newLiteralRegistry())
	assertGeneratorUnknownError(t, err)
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
			files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": testCase.program}, Config{SourceTable: testSpanTable})
			rootC, rootH := files["modules/app.c"], files["modules/app.h"]
			if testCase.wantError {
				assertGeneratorUnknownError(t, err)
				if rootC != "" || rootH != "" {
					t.Fatalf("generated output for forged place metadata: rootC := %q rootH=%q", rootC, rootH)
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

	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{SourceTable: testSpanTable})
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
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: node}, newLiteralRegistry())
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
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: node}, newLiteralRegistry())
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
	_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": checker.Program{TypeDeclarations: []checker.TypeDeclaration{{Name: "Point", Type: forged}}}}, Config{SourceTable: testSpanTable})
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

	files := generateOne(t, program)
	if !strings.Contains(files["modules/app.h"], "typedef struct hex_t_Point hex_t_Point;") {
		t.Fatalf("modules/app.h = %q, want the reachable object definition", files["modules/app.h"])
	}
}
