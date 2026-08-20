package hexal_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// commentCitation matches any spec number or spec-directory pointer that a
// comment must not carry: RFC or ADR followed by digits, or a docs/specs/
// path. docs/status.md and docs/reference.md are living documents and are not
// matched.
var commentCitation = regexp.MustCompile(`RFC\s*\d+|ADR\s*\d+|docs/specs/`)

// commentViolation is one offending comment, reported with its file, line, and
// full text so the fix is immediate.
type commentViolation struct {
	file string
	line int
	text string
}

// scanCommentPolicy parses one Go file and returns every comment that violates
// the policy: one that cites a spec number or docs/specs/ path, or one that
// contains a rune above ASCII. Comments are read from file.Comments, never
// from the syntax tree: unattached comments live only in File.Comments and
// would silently escape an ast.Inspect walk.
func scanCommentPolicy(fset *token.FileSet, path string, file *ast.File) []commentViolation {
	var violations []commentViolation
	for _, group := range file.Comments {
		for _, comment := range group.List {
			position := fset.Position(comment.Pos())
			if commentCitation.MatchString(comment.Text) {
				violations = append(violations, commentViolation{path, position.Line, comment.Text})
			}
			for _, r := range comment.Text {
				if r > unicode.MaxASCII {
					violations = append(violations, commentViolation{path, position.Line, comment.Text})
					break
				}
			}
		}
	}
	return violations
}

// scanCommentPolicyWalk scans every .go file under roots and returns the
// concatenated violations. Only comment text is inspected: non-ASCII inside a
// string literal or other token is legitimate source data and never reaches
// this scanner.
func scanCommentPolicyWalk(t *testing.T, roots ...string) []commentViolation {
	t.Helper()
	var violations []commentViolation
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			violations = append(violations, scanCommentPolicy(fset, relative, file)...)
			return nil
		})
	}
	return violations
}

// The guard's job: every comment in the compiler and the workbench complies.
// Any newly added violation fails here with its file and line.
func TestCommentPolicyAppliesToCompilerAndWorkbench(t *testing.T) {
	violations := scanCommentPolicyWalk(t, "compiler", "workbench")
	if len(violations) == 0 {
		return
	}
	var builder strings.Builder
	for _, violation := range violations {
		fmt.Fprintf(&builder, "\n%s:%d: %s", violation.file, violation.line, violation.text)
	}
	t.Fatalf("comment policy violations:%s", builder.String())
}

// Synthetic sources drive the scanner directly. Each violation must be
// reported with the synthetic filename and line.
func TestCommentPolicyRejectsSpecCitations(t *testing.T) {
	for _, testCase := range []struct {
		comment string
		want    string
	}{
		{"// RFC 0074 defines this.", "RFC 0074"},
		{"// The table lives in ADR 0003.", "ADR 0003"},
		{"// See docs/specs/0099 for the plan.", "docs/specs/"},
	} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "synthetic.go", "package p\n"+testCase.comment+"\n", parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing synthetic source: %v", err)
		}
		violations := scanCommentPolicy(fset, "synthetic.go", file)
		if len(violations) != 1 {
			t.Fatalf("%q produced %d violations, want 1", testCase.comment, len(violations))
		}
		violation := violations[0]
		if violation.file != "synthetic.go" || violation.line != 2 || !strings.Contains(violation.text, testCase.want) {
			t.Fatalf("%q -> %+v, want file synthetic.go line 2 containing %q", testCase.comment, violation, testCase.want)
		}
	}
}

func TestCommentPolicyRejectsNonASCIIComments(t *testing.T) {
	fset := token.NewFileSet()
	source := "package p\n// An em dash \u2014 is not allowed here.\n"
	file, err := parser.ParseFile(fset, "synthetic.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing synthetic source: %v", err)
	}
	violations := scanCommentPolicy(fset, "synthetic.go", file)
	if len(violations) != 1 {
		t.Fatalf("produced %d violations, want 1", len(violations))
	}
	if violations[0].file != "synthetic.go" || violations[0].line != 2 {
		t.Fatalf("got %+v, want synthetic.go line 2", violations[0])
	}
}

// Non-ASCII inside a string literal is legitimate test data and must not
// trigger the guard, which inspects comments only.
func TestCommentPolicyIgnoresNonASCIIInStringLiterals(t *testing.T) {
	fset := token.NewFileSet()
	source := "package p\nvar text = \"caf\u00e9 \U0001F980\"\n"
	file, err := parser.ParseFile(fset, "synthetic.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing synthetic source: %v", err)
	}
	if violations := scanCommentPolicy(fset, "synthetic.go", file); len(violations) != 0 {
		t.Fatalf("string literal produced %d violations, want 0: %+v", len(violations), violations)
	}
}
