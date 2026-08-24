package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"testing"
)

// expressionKindConstants returns every ExpressionKind constant name declared
// in operands.go's const block, in source order. Only the first ValueSpec in
// an iota block states the type explicitly; every later bare spec in the same
// GenDecl inherits it, so once a GenDecl is confirmed to declare
// ExpressionKind, every name in every spec of that GenDecl belongs to it.
func expressionKindConstants(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "checker", "operands.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var names []string
	for _, declaration := range file.Decls {
		genDecl, ok := declaration.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST || len(genDecl.Specs) == 0 {
			continue
		}
		first, ok := genDecl.Specs[0].(*ast.ValueSpec)
		if !ok {
			continue
		}
		typeIdent, ok := first.Type.(*ast.Ident)
		if !ok || typeIdent.Name != "ExpressionKind" {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range valueSpec.Names {
				names = append(names, name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s: found no ExpressionKind constants", path)
	}
	return names
}

// dispatchedExpressionKinds finds funcName's own top-level switch on
// node.Kind and returns the set of checker.<Kind> names named in its case
// clauses. Grouped case clauses (case checker.A, checker.B:) contribute both
// names. The default clause contributes nothing.
func dispatchedExpressionKinds(t *testing.T, path string, funcName string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var target *ast.FuncDecl
	for _, declaration := range file.Decls {
		funcDecl, ok := declaration.(*ast.FuncDecl)
		if ok && funcDecl.Name.Name == funcName {
			target = funcDecl
			break
		}
	}
	if target == nil {
		t.Fatalf("%s: function %s not found", path, funcName)
	}
	var found []*ast.SwitchStmt
	ast.Inspect(target, func(node ast.Node) bool {
		switchStmt, ok := node.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		selector, ok := switchStmt.Tag.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if ok && ident.Name == "node" && selector.Sel.Name == "Kind" {
			found = append(found, switchStmt)
		}
		return true
	})
	if len(found) != 1 {
		t.Fatalf("%s: %s has %d switches on node.Kind, want exactly 1", path, funcName, len(found))
	}
	kinds := make(map[string]bool)
	for _, clause := range found[0].Body.List {
		caseClause, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range caseClause.List {
			selector, ok := expression.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			ident, ok := selector.X.(*ast.Ident)
			if ok && ident.Name == "checker" {
				kinds[selector.Sel.Name] = true
			}
		}
	}
	return kinds
}

// The generator's two primary expression dispatchers must each explicitly
// own every concrete ExpressionKind. InvalidExpression is the sole
// intentional exception: it has no case in either switch and falls through
// to each switch's fail-closed default, since a valid checked program never
// produces it.
func TestExpressionDispatchersCoverEveryConcreteKind(t *testing.T) {
	kinds := expressionKindConstants(t)
	renderKinds := dispatchedExpressionKinds(t, "render.go", "renderExpressionUncheckedWithState")
	validateKinds := dispatchedExpressionKinds(t, "validation.go", "validateExpressionNode")

	var missingRender, missingValidate []string
	for _, kind := range kinds {
		if kind == "InvalidExpression" {
			if renderKinds[kind] {
				t.Errorf("renderExpressionUncheckedWithState explicitly cases InvalidExpression; it must fall through to the default instead")
			}
			if validateKinds[kind] {
				t.Errorf("validateExpressionNode explicitly cases InvalidExpression; it must fall through to the default instead")
			}
			continue
		}
		if !renderKinds[kind] {
			missingRender = append(missingRender, kind)
		}
		if !validateKinds[kind] {
			missingValidate = append(missingValidate, kind)
		}
	}
	sort.Strings(missingRender)
	sort.Strings(missingValidate)
	if len(missingRender) > 0 {
		t.Errorf("renderExpressionUncheckedWithState is missing cases for: %v", missingRender)
	}
	if len(missingValidate) > 0 {
		t.Errorf("validateExpressionNode is missing cases for: %v", missingValidate)
	}
}
