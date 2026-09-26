package diagnostics

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type diagnosticInventory struct {
	constructors map[ID]string
	emitted      map[ID]string
	problems     []string
}

func TestDiagnosticRegistryAndEmittersAreComplete(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate diagnostics source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	inventory, err := scanDiagnosticInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.problems) != 0 {
		t.Fatalf("diagnostic source violations:\n%s", strings.Join(inventory.problems, "\n"))
	}
	if err := validateInventory(declaredRecords, inventory); err != nil {
		t.Fatal(err)
	}
}

func scanDiagnosticInventory(root string) (diagnosticInventory, error) {
	result := diagnosticInventory{constructors: map[ID]string{}, emitted: map[ID]string{}}
	fset := token.NewFileSet()
	functions := make(map[string]*ast.FuncDecl)
	var diagnosticFiles []*ast.File
	var otherFiles []*ast.File
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, "compiler"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return result, err
	}
	for _, path := range paths {
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return result, parseErr
		}
		if filepath.Clean(filepath.Dir(path)) == filepath.Join(root, "compiler", "diagnostics") {
			diagnosticFiles = append(diagnosticFiles, file)
			for _, declaration := range file.Decls {
				if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
					functions[function.Name.Name] = function
				}
			}
		} else {
			otherFiles = append(otherFiles, file)
		}
		allowedAdapter := filepath.Clean(path) == filepath.Join(root, "compiler", "types", "diagnostic.go")
		ast.Inspect(file, func(node ast.Node) bool {
			switch current := node.(type) {
			case *ast.CompositeLit:
				if isDiagnosticLiteral(current.Type) && !allowedAdapter {
					result.problems = append(result.problems, fmt.Sprintf("%s: direct Diagnostic construction", fset.Position(current.Pos())))
				}
			case *ast.AssignStmt:
				for _, left := range current.Lhs {
					if isDiagnosticMessageField(left) && !allowedAdapter {
						result.problems = append(result.problems, fmt.Sprintf("%s: direct diagnostic message assignment", fset.Position(left.Pos())))
					}
				}
			case *ast.FuncDecl:
				if current.Type.Params == nil || filepath.Base(path) == "corelib.go" && current.Name.Name == "corelibErrorArm" || filepath.Base(path) == "strings.go" && current.Name.Name == "textErrorArm" {
					break
				}
				for _, field := range current.Type.Params.List {
					if isStringType(field.Type) {
						for _, name := range field.Names {
							if name.Name == "message" {
								result.problems = append(result.problems, fmt.Sprintf("%s: free-form message helper %s", fset.Position(name.Pos()), current.Name.Name))
							}
						}
					}
				}
			}
			return true
		})
	}

	idsByFunction := make(map[string]map[ID]bool)
	for _, file := range diagnosticFiles {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ids := make(map[ID]bool)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if name := calledName(call.Fun); name == "message" && len(call.Args) > 0 {
					if literal, ok := call.Args[0].(*ast.BasicLit); ok {
						if value, err := strconv.Unquote(literal.Value); err == nil {
							ids[ID(value)] = true
						}
					}
				}
				if name := calledName(call.Fun); name != "" {
					if _, exists := functions[name]; exists && name != function.Name.Name {
						ids[ID("@"+name)] = true
					}
				}
				return true
			})
			idsByFunction[function.Name.Name] = ids
		}
	}
	var resolve func(string, map[string]bool) map[ID]bool
	resolve = func(name string, active map[string]bool) map[ID]bool {
		found := map[ID]bool{}
		if active[name] {
			return found
		}
		active[name] = true
		for id := range idsByFunction[name] {
			if strings.HasPrefix(string(id), "@") {
				for nested := range resolve(strings.TrimPrefix(string(id), "@"), active) {
					found[nested] = true
				}
			} else {
				found[id] = true
			}
		}
		delete(active, name)
		return found
	}
	for name := range idsByFunction {
		for id := range resolve(name, map[string]bool{}) {
			result.constructors[id] = name
		}
	}
	for _, file := range otherFiles {
		aliases := make(map[string]bool)
		for _, specification := range file.Imports {
			importPath, _ := strconv.Unquote(specification.Path.Value)
			if importPath != "hexal/compiler/diagnostics" {
				continue
			}
			alias := "diagnostics"
			if specification.Name != nil {
				alias = specification.Name.Name
			}
			aliases[alias] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			alias, ok := selector.X.(*ast.Ident)
			if !ok || !aliases[alias.Name] {
				return true
			}
			for id := range resolve(selector.Sel.Name, map[string]bool{}) {
				result.emitted[id] = selector.Sel.Name
			}
			return true
		})
	}
	return result, nil
}

func validateInventory(records map[ID]record, inventory diagnosticInventory) error {
	problems := make([]string, 0)
	for id := range records {
		if _, ok := inventory.constructors[id]; !ok {
			problems = append(problems, fmt.Sprintf("active diagnostic key %q has no constructor", id))
		}
		if _, ok := inventory.emitted[id]; !ok {
			problems = append(problems, fmt.Sprintf("active diagnostic key %q has no emitter", id))
		}
	}
	for id := range inventory.constructors {
		if _, ok := records[id]; !ok {
			problems = append(problems, fmt.Sprintf("constructor key %q has no registry record", id))
		}
	}
	for id := range inventory.emitted {
		if _, ok := records[id]; !ok {
			problems = append(problems, fmt.Sprintf("emitted key %q has no registry record", id))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}

func isStringType(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "string"
}

func isDiagnosticLiteral(expression ast.Expr) bool {
	switch typ := expression.(type) {
	case *ast.Ident:
		return typ.Name == "Diagnostic"
	case *ast.SelectorExpr:
		return typ.Sel.Name == "Diagnostic"
	default:
		return false
	}
}

func isDiagnosticMessageField(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Message"
}

func calledName(expression ast.Expr) string {
	switch function := expression.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
}

func TestRegistryEmitterGuardRejectsMutations(t *testing.T) {
	base := diagnosticInventory{constructors: map[ID]string{"type.one": "One"}, emitted: map[ID]string{"type.one": "One"}}
	registered := map[ID]record{"type.one": {category: CategoryType, stage: StageChecker}}
	mutations := []struct {
		name       string
		registered map[ID]record
		inventory  diagnosticInventory
		want       string
	}{
		{name: "unknown emitted key", registered: registered, inventory: diagnosticInventory{constructors: base.constructors, emitted: map[ID]string{"type.one": "One", "type.unknown": "Unknown"}}, want: "emitted key"},
		{name: "orphan key", registered: map[ID]record{"type.one": registered["type.one"], "type.orphan": registered["type.one"]}, inventory: base, want: "active diagnostic key"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			err := validateInventory(mutation.registered, mutation.inventory)
			if err == nil || !strings.Contains(err.Error(), mutation.want) {
				t.Fatalf("validateInventory error = %v, want text containing %q", err, mutation.want)
			}
		})
	}
}

func TestDiagnosticSourceGuardRejectsUnsafeConstruction(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
		line   string
	}{
		{name: "keyless construction", source: "package sample\ntype Diagnostic struct{}\nfunc emit() { _ = Diagnostic{} }\n", want: "direct Diagnostic construction", line: ":3:"},
		{name: "message assignment", source: "package sample\ntype Diagnostic struct { Message string }\nfunc emit(d *Diagnostic) { d.Message = \"local wording\" }\n", want: "direct diagnostic message assignment", line: ":3:"},
		{name: "free-form helper", source: "package sample\nfunc errorAt(message string) {}\n", want: "free-form message helper", line: ":2:"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			issues, err := sourcePolicyIssues("compiler/sample.go", []byte(testCase.source))
			if err != nil {
				t.Fatal(err)
			}
			if len(issues) != 1 || !strings.Contains(issues[0], testCase.want) || !strings.Contains(issues[0], testCase.line) {
				t.Fatalf("source policy issues = %v, want one located issue containing %q", issues, testCase.want)
			}
		})
	}
}

func sourcePolicyIssues(filename string, source []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		return nil, err
	}
	allowedAdapter := filepath.ToSlash(filename) == "compiler/types/diagnostic.go"
	issues := make([]string, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.CompositeLit:
			if isDiagnosticLiteral(current.Type) && !allowedAdapter {
				issues = append(issues, fmt.Sprintf("%s: direct Diagnostic construction", fset.Position(current.Pos())))
			}
		case *ast.AssignStmt:
			for _, left := range current.Lhs {
				if isDiagnosticMessageField(left) && !allowedAdapter {
					issues = append(issues, fmt.Sprintf("%s: direct diagnostic message assignment", fset.Position(left.Pos())))
				}
			}
		case *ast.FuncDecl:
			if current.Type.Params == nil || filepath.Base(filename) == "corelib.go" && current.Name.Name == "corelibErrorArm" || filepath.Base(filename) == "strings.go" && current.Name.Name == "textErrorArm" {
				break
			}
			for _, field := range current.Type.Params.List {
				if isStringType(field.Type) {
					for _, name := range field.Names {
						if name.Name == "message" {
							issues = append(issues, fmt.Sprintf("%s: free-form message helper %s", fset.Position(name.Pos()), current.Name.Name))
						}
					}
				}
			}
		}
		return true
	})
	return issues, nil
}
