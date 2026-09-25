package checker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// concreteImplementors returns the receiver type names in the parser package
// that declare a method named marker. Each concrete node kind implements its
// family marker exactly once, so the receiver set is that family's complete
// concrete inventory: a new syntax kind arrives in this set and nowhere else.
func concreteImplementors(t *testing.T, marker string) []string {
	t.Helper()
	directory := filepath.Join("..", "parser")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading %s: %v", directory, err)
	}
	found := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 || function.Name.Name != marker {
				continue
			}
			if identifier, ok := function.Recv.List[0].Type.(*ast.Ident); ok {
				found[identifier.Name] = true
			}
		}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkerFunctionFile resolves funcName to the package source file declaring
// it, so a dispatch contract follows the function through file reorganizations
// instead of pinning it to one filename.
func checkerFunctionFile(t *testing.T, funcName string) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == funcName {
				return name
			}
		}
	}
	t.Fatalf("function %s not found in package sources", funcName)
	return ""
}

// parserCasesInFunction returns every parser.<Name> type named in a case clause
// of any type switch inside funcName. Only the family the caller compares
// against matters, so nested switches contribute harmlessly: a kind cased
// anywhere in the dispatcher is handled by it.
func parserCasesInFunction(t *testing.T, funcName string) map[string]bool {
	t.Helper()
	path := checkerFunctionFile(t, funcName)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var target *ast.FuncDecl
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == funcName {
			target = function
			break
		}
	}
	if target == nil {
		t.Fatalf("%s: function %s not found", path, funcName)
	}
	found := make(map[string]bool)
	sawTypeSwitch := false
	ast.Inspect(target, func(node ast.Node) bool {
		switchStmt, ok := node.(*ast.TypeSwitchStmt)
		if !ok {
			return true
		}
		sawTypeSwitch = true
		for _, statement := range switchStmt.Body.List {
			caseClause, ok := statement.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expression := range caseClause.List {
				selector, ok := expression.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "parser" {
					found[selector.Sel.Name] = true
				}
			}
		}
		return true
	})
	if !sawTypeSwitch {
		t.Fatalf("%s: function %s has no type switch", path, funcName)
	}
	return found
}

// checkFamilyDispatch asserts that a dispatcher covers every concrete kind of
// one parser family except the documented non-application set, and that every
// parser kind the dispatcher cases belongs to the family. The exception set is
// the deliberate boundary: a kind there is handled by a sibling path, not by
// this dispatcher, so a new kind that lands there by accident still fails the
// caller's completeness half.
func checkFamilyDispatch(t *testing.T, marker string, dispatcher string, notDispatched map[string]string) {
	t.Helper()
	implementors := concreteImplementors(t, marker)
	if len(implementors) == 0 {
		t.Fatalf("found no concrete implementors of %s", marker)
	}
	cases := parserCasesInFunction(t, dispatcher)
	known := make(map[string]bool, len(implementors))
	for _, name := range implementors {
		known[name] = true
	}
	for _, name := range implementors {
		if _, excluded := notDispatched[name]; excluded {
			continue
		}
		if !cases[name] {
			t.Errorf("%s does not handle %s", dispatcher, name)
		}
	}
	for name := range cases {
		if !known[name] {
			t.Errorf("%s cases %s, which is not a %s implementation", dispatcher, name, marker)
		}
	}
	for name := range notDispatched {
		if !known[name] {
			t.Errorf("exclusion list for %s names %s, which is not a %s implementation", dispatcher, name, marker)
		}
	}
}

// Type uses are resolved by one dispatcher. Type-definition forms carry the
// same marker but are resolved by the declaration path, so they are the
// documented non-application set.
func TestTypeExpressionDispatchCoversEveryKind(t *testing.T) {
	checkFamilyDispatch(t, "typeExpressionNode", "resolveTypeUse", map[string]string{
		"AdtDefinitionExpression": "a type definition, resolved by the declaration path",
		"ObjectTypeExpression":    "a type definition, resolved by the declaration path",
	})
}

// Every non-executable top-level item is checked by the declaration pass, not
// by the entry-module executable dispatcher, so the four declaration forms are
// the documented non-application set.
func TestTopLevelItemDispatchCoversEveryExecutableKind(t *testing.T) {
	checkFamilyDispatch(t, "topLevelItemNode", "checkRootExecutable", map[string]string{
		"Declaration":         "a declaration, checked by the declaration pass",
		"FunctionDeclaration": "a declaration, checked by the declaration pass",
		"MethodDeclaration":   "a declaration, checked by the declaration pass",
		"TypeDeclaration":     "a declaration, checked by the declaration pass",
	})
}

func TestMatchPatternDispatchCoversEveryKind(t *testing.T) {
	checkFamilyDispatch(t, "matchPatternNode", "checkMatchExpression", nil)
}

func TestExternDeclarationDispatchCoversEveryKind(t *testing.T) {
	checkFamilyDispatch(t, "externDeclarationNode", "checkForeignDeclarations", nil)
}
