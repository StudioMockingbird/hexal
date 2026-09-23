package checker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// parserStatementName maps a checker statement type to the type name the
// parser tree uses for the same construct. The two packages spell a call
// statement differently; every other shared form keeps its name.
var parserStatementName = map[string]string{
	"CallStatement": "CallExpression",
}

// capturesUntouched lists checker statement types captures.go intentionally
// has no case for. Break and continue bind nothing and nest no expressions;
// RootReturn is checker-synthetic and never appears in the parser tree the
// pass walks.
var capturesUntouched = map[string]string{
	"BreakStatement":      "no nested expressions or statements to analyse",
	"ContinueStatement":   "no nested expressions or statements to analyse",
	"RootReturnStatement": "synthetic; never appears in the parser tree",
}

// starvationUntouched lists checker statement types starvation.go intentionally
// has no case for. A method body joins the call graph only under free-function
// spawn and call names, never as a MethodDeclaration node; a try statement
// discards its value, so it cannot be a spawn entry, and its operand is
// scanned when the same expression is bound or called.
var starvationUntouched = map[string]string{
	"MethodDeclaration": "reaches the scanner only as a free-function spawn or call name",
	"TryStatement":      "discards its value; never a spawn entry",
}

// statementNodeTypes returns every concrete type that implements the checker's
// statementNode marker, by finding the marker method on each non-test file in
// this package.
func statementNodeTypes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name == nil || function.Name.Name != "statementNode" {
				continue
			}
			if function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver := function.Recv.List[0].Type
			if star, ok := receiver.(*ast.StarExpr); ok {
				receiver = star.X
			}
			identifier, ok := receiver.(*ast.Ident)
			if !ok {
				continue
			}
			seen[identifier.Name] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no statementNode implementations")
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// caseTypeNames returns the bare type names named in every type-switch case
// clause of one file, after stripping a package qualifier.
func caseTypeNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	found := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		switchStmt, ok := node.(*ast.TypeSwitchStmt)
		if !ok {
			return true
		}
		for _, clause := range switchStmt.Body.List {
			caseClause, ok := clause.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expression := range caseClause.List {
				found[typeName(expression)] = true
			}
		}
		return true
	})
	return found
}

// typeName renders one case-clause type expression as its bare identifier.
func typeName(expression ast.Expr) string {
	switch node := expression.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.SelectorExpr:
		return node.Sel.Name
	case *ast.StarExpr:
		return typeName(node.X)
	}
	return ""
}

// Every concrete statement form must be classified in each analysis pass:
// either the pass cases it, or an allowlist entry states why it needs no case.
// A new statement type arrives unclassified; a removed case leaves its type
// unclassified. Both fail here, naming the statement type.
func TestStatementAnalysisPassesCoverEveryStatementKind(t *testing.T) {
	kinds := statementNodeTypes(t)
	capturesCases := caseTypeNames(t, "captures.go")
	starvationCases := caseTypeNames(t, "starvation.go")

	for _, kind := range kinds {
		capturesName := kind
		if mapped, ok := parserStatementName[kind]; ok {
			capturesName = mapped
		}
		if !capturesCases[capturesName] {
			if reason, allowlisted := capturesUntouched[kind]; !allowlisted {
				t.Errorf("captures.go is missing a case for statement %s", kind)
			} else if reason == "" {
				t.Errorf("captures.go allowlist entry for %s has no reason", kind)
			}
		} else if reason, allowlisted := capturesUntouched[kind]; allowlisted {
			t.Errorf("captures.go cases %s, which its allowlist says needs no case (%s)", kind, reason)
		}

		if !starvationCases[kind] {
			if reason, allowlisted := starvationUntouched[kind]; !allowlisted {
				t.Errorf("starvation.go is missing a case for statement %s", kind)
			} else if reason == "" {
				t.Errorf("starvation.go allowlist entry for %s has no reason", kind)
			}
		} else if reason, allowlisted := starvationUntouched[kind]; allowlisted {
			t.Errorf("starvation.go cases %s, which its allowlist says needs no case (%s)", kind, reason)
		}
	}

	for kind := range capturesUntouched {
		if !containsString(kinds, kind) {
			t.Errorf("captures.go allowlist names %s, which is not a statementNode implementation", kind)
		}
	}
	for kind := range starvationUntouched {
		if !containsString(kinds, kind) {
			t.Errorf("starvation.go allowlist names %s, which is not a statementNode implementation", kind)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
