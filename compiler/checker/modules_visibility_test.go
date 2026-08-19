package checker

// Alias-qualified resolution of exported functions, types, and ADT variants;
// the private-name diagnostic; and the exported-interface closure check.

import (
	"testing"

	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkModules compiles app plus one dependency named math, in dependency
// order, and returns the diagnostics.
func checkModules(t *testing.T, app, math string) (map[string]Program, error) {
	t.Helper()
	appProgram := parseProgram(t, app)
	programs := map[string]parser.Program{"app.hex": appProgram, "math.hex": parseProgram(t, math)}
	// The helper's only dependency is math, so every import app writes
	// resolves there; the alias comes from the source as the resolver would
	// have recorded it.
	return CheckModules(graphOf("app", []string{"math", "app"}, programs,
		map[string][]ModuleEdge{"app": edgesToMath(appProgram)}))
}

// edgesToMath records one resolved edge per import declaration in program,
// each naming the helper's single dependency.
func edgesToMath(program parser.Program) []ModuleEdge {
	edges := make([]ModuleEdge, 0, 1)
	for _, item := range program.Items {
		if importDecl, ok := item.(parser.ImportDeclaration); ok {
			edges = append(edges, ModuleEdge{Alias: importDecl.Alias.Lexeme, Target: "math"})
		}
	}
	return edges
}

// A qualified call to an exported function resolves against the target
// module's recorded signature, and the call node carries the target module
// id for the downstream stage.
func TestQualifiedCallResolvesExportedFunction(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\nresult: Int32 := Math.add(2, 3)\n",
		"export fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the qualified call: %v", err)
	}
	declaration, ok := checked["app.hex"].Statements[0].(Declaration)
	if !ok || declaration.Name != "result" || declaration.Type != compilerTypes.Int32 {
		t.Fatalf("app statement = %#v, want result: Int32", checked["app.hex"].Statements[0])
	}
	// The checked tree mirrors a local call: the declaration's source node is
	// the CallExpression, whose callee is the FunctionReferenceExpression the
	// qualified resolution stamped with the target module id (same shape as a
	// local call, functions_test.go TestCheckedCallAndFunctionReferenceNodes).
	call := declaration.Source.Node
	if call.Kind != CallExpression || call.Operand == nil || call.Operand.Kind != FunctionReferenceExpression || call.Operand.Module != "math" {
		t.Fatalf("source node = %#v, want qualified call to module math", declaration.Source.Node)
	}
}

// A qualified call to a name the target module keeps private is the
// visibility failure at the property.
func TestQualifiedCallRejectsPrivateFunction(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\nresult: Int32 := Math.add(2, 3)\n",
		"fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n")
	requireMessage(t, err, "declaration add is private to module math")
}

// A qualified type use resolves through the alias to the target module's
// exported types; a private or unknown target name is the visibility failure.
// The exported alias is transparent, so the declaration's type is the alias
// target's identity (Int32), not the alias name.
func TestQualifiedTypeResolvesExportedAndRejectsPrivate(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\nshape: Math.Shape := 0\n",
		"export type Shape = Int32\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the qualified type: %v", err)
	}
	if declaration := checked["app.hex"].Statements[0].(Declaration); declaration.Type != compilerTypes.Int32 {
		t.Fatalf("app statement = %#v, want the transparent alias target Int32", declaration)
	}
	_, err = checkModules(t,
		"module Math = import \"./math\"\nshape: Math.Shape := 0\n",
		"type Shape = Int32\n")
	requireMessage(t, err, "declaration Shape is private to module math")
}

// An unknown leftmost name keeps the unknown-module-alias error.
func TestQualifiedTypeKeepsUnknownModuleAlias(t *testing.T) {
	requireDiagnostic(t, "shape: Nope.Shape := 0\n", "unknown module alias Nope")
}

// An exported declaration whose interface reaches a private nominal type is
// a Type Error naming the exported declaration and the private type. The
// defining module checks clean first; the closure walk runs afterward.
func TestExportedClosureRejectsPrivateType(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\n",
		"type Secret = { x: Int32 }\nexport fun f(): Secret do\n    return Secret { x = 1 }\nend\n")
	requireMessage(t, err, "exported function f exposes private type Secret")
}

// The closure walks ADT payloads, objects behind pointers, and nested
// containers; cycles through aliases terminate.
func TestExportedClosureWalksNestedAndCycles(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\n",
		"type Secret = { x: Int32 }\nexport type Node = { next: MutPtr<Node> | Nil, items: List<Secret> }\nexport fun f(): Node do\n    return Node { next = nil, items = List<Secret>.new(Heap.new()) }\nend\n")
	requireMessage(t, err, "exported function f exposes private type Secret")
}

// An exported function over only builtin and exported types closes clean.
func TestExportedClosureAcceptsExportedInterface(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\n",
		"export type Point = { x: Int32, y: Int32 }\nexport fun f(p: Ptr<Point>): Point do\n    return Point { x = 1, y = 2 }\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected a closed exported interface: %v", err)
	}
	if len(checked["math.hex"].Statements) != 1 {
		t.Fatalf("math statements = %d, want 1", len(checked["math.hex"].Statements))
	}
}

// An exported generic template closes over its interface; specialization
// requests from importers are folded in later, so this only exercises the
// template declarations themselves.
func TestExportedClosureAcceptsSpecializedGeneric(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\n",
		"export type Box<T> = { item: T }\nexport fun new_box<T>(item: T): Box<T> do\n    return Box<T> { item = item }\nend\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the generic interface: %v", err)
	}
	if len(checked["math.hex"].Statements) != 0 {
		t.Fatalf("math statements = %d, want 0 (templates emit nothing)", len(checked["math.hex"].Statements))
	}
}

// Qualified record and unit variant constructors resolve to the target
// module's exported ADT; the result carries the target's ADT identity.
func TestQualifiedVariantResolvesExportedADT(t *testing.T) {
	checked, err := checkModules(t,
		"module Math = import \"./math\"\ns: Math.Shape := Math.Circle { x = 1 }\nu: Math.Shape := Math.Square\n",
		"export type Shape = | Circle as { x: Int32 } | Square\n")
	if err != nil {
		t.Fatalf("CheckModules rejected the qualified variants: %v", err)
	}
	statements := checked["app.hex"].Statements
	if len(statements) != 2 {
		t.Fatalf("app statements = %d, want 2", len(statements))
	}
	record := statements[0].(Declaration)
	if record.Type.Name != "Shape" || record.Source.Node.Kind != AdtConstructExpression {
		t.Fatalf("record = %#v, want Shape.Circle construction", record)
	}
	unit := statements[1].(Declaration)
	if unit.Type.Name != "Shape" || unit.Source.Node.Kind != AdtConstructExpression || unit.Source.Node.VariantIndex != 1 {
		t.Fatalf("unit = %#v, want Shape.Square unit value", unit)
	}
}

// A variant the target module's exported ADTs do not carry is the
// visibility failure at the variant.
func TestQualifiedVariantRejectsUnknownExport(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\ns: Math.Shape := Math.Circle { x = 1 }\n",
		"export type Shape = | Other as { x: Int32 }\n")
	requireMessage(t, err, "declaration Circle is private to module math")
}

// Unqualified use of an exported name stays an ordinary local-name error:
// there are no wildcard imports.
func TestUnqualifiedUseOfExportedNameFails(t *testing.T) {
	_, err := checkModules(t,
		"module Math = import \"./math\"\nresult: Int32 := add(2, 3)\n",
		"export fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n")
	requireMessage(t, err, "unknown function add; functions must be declared before use")
}

// A dangling alias whose target has no source resolves nowhere: the bare
// receiver keeps failing as an unknown variable.
func TestDanglingAliasQualifiedCallKeepsUnknownVariable(t *testing.T) {
	app := parseProgram(t, "module Math = import \"./math\"\nresult: Int32 := Math.add(2, 3)\n")
	_, err := CheckModules(graphOf("app", []string{"app"}, map[string]parser.Program{"app.hex": app}, map[string][]ModuleEdge{"app": {{Alias: "Math", Target: "math"}}}))
	requireMessage(t, err, "unknown variable Math")
}
