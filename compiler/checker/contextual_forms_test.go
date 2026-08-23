package checker

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"hexal/compiler/parser"
)

// The inference predicate must stay in step with every form that threads the
// expected type inward, and the failure mode is not symmetric: a form the
// predicate misses is *accepted* by `:=` and silently defaulted, which lets
// `match` take Int32 from nowhere. A form it wrongly
// includes only over-rejects, which is loud.
//
// Nothing enforces that mechanically, so this does. It enumerates the
// functions that read context.expected and compares them against the reviewed
// set below. A new reader fails this test rather than slipping through.
//
// **When this test fails, do not just add the name.** Decide first whether the
// new reader forwards the expected type to a sub-expression. If it does, the
// form belongs in contextualExpression's forInference cases; if it merely
// consumes the expected type for itself, it does not; see the accepted
// qualified-variant and function-reference cases in the integration tests.
// Then record it here with which of the two it is.
func TestEveryReaderOfTheExpectedTypeIsClassified(t *testing.T) {
	reviewed := []string{
		// The expression switch. It both forwards (literals, string, nil,
		// array, match) and consumes for itself (qualified variant, unit
		// variant, generic function reference); the split lives in
		// contextualExpression, not here.
		"checkExpression",
		// Forwards to every arm, which is why a match of bare literals is
		// contextual and one with typed arms is not.
		"checkMatchArm",
		// Forward to their operands; contextualExpression recurses to match.
		"checkUnaryExpression",
		"checkBinaryExpression",
		// Consumes the expected type for itself, exactly like the accepted
		// generic function reference case: a generic literal specializes
		// directly against an exact expected Fun<...> type rather than
		// forwarding it to a checked sub-expression.
		"checkGenericAnonymousFunctionLiteral",
	}
	slices.Sort(reviewed)

	found := readersOfExpectedType(t)
	if !slices.Equal(found, reviewed) {
		t.Fatalf("the set of functions reading context.expected changed.\n  found:    %v\n  reviewed: %v\nRead this test's comment before editing it.", found, reviewed)
	}
}

// readersOfExpectedType returns the sorted, distinct names of the functions in
// this package whose bodies mention context.expected.
func readersOfExpectedType(t *testing.T) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	packages, err := goparser.ParseDir(fileSet, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the checker package: %v", err)
	}
	names := []string{}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				function, ok := node.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					return true
				}
				ast.Inspect(function.Body, func(inner ast.Node) bool {
					selector, ok := inner.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "expected" {
						return true
					}
					if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "context" {
						if !slices.Contains(names, function.Name.Name) {
							names = append(names, function.Name.Name)
						}
					}
					return true
				})
				return true
			})
		}
	}
	slices.Sort(names)
	return names
}

// Extending contextualExpression must not change which contextual unions are
// accepted today. The forInference flag is what guarantees that, and this is
// the assertion for it: every form `:=` adds must stay non-contextual on the
// union path.
func TestUnionInjectionPredicateIsUnchangedByTheInferenceCases(t *testing.T) {
	for _, source := range []string{
		`"hexal"`,
		`nil`,
		`[1, 2, 3]`,
		`match ready | true then 1 | false then 0 end`,
	} {
		expression := parseOneExpression(t, source)
		if isContextualExpression(expression) {
			t.Fatalf("%s became contextual on the union-injection path; only := may treat it so", source)
		}
		if !isContextualForInference(expression) {
			t.Fatalf("%s is not contextual for inference, so := would accept it and default its type", source)
		}
	}
	// The forms the union path already treats as contextual are unchanged
	// under both questions.
	for _, source := range []string{`0`, `1.5`, `-1`, `1 + 2`} {
		expression := parseOneExpression(t, source)
		if !isContextualExpression(expression) || !isContextualForInference(expression) {
			t.Fatalf("%s must stay contextual on both paths", source)
		}
	}
}

// parseOneExpression parses one Hexal expression by wrapping it in the
// smallest declaration that accepts it, then returns the initializer.
func parseOneExpression(t *testing.T, source string) parser.Expression {
	t.Helper()
	program := parseProgram(t, "fun probe(ready: Bool) do\n    value: Int32 := "+source+"\nend")
	for _, item := range program.Items {
		function, ok := item.(parser.FunctionDeclaration)
		if !ok {
			continue
		}
		for _, statement := range function.Body {
			if declaration, ok := statement.(parser.Declaration); ok {
				return declaration.Initializer
			}
		}
	}
	t.Fatalf("no declaration parsed from %q", source)
	return nil
}
