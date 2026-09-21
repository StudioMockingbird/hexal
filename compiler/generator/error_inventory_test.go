package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Every message a component reports as an Error is authored text copied into
// the inline 256-byte message, and every ErrorKind.Other header the runtime
// produces is copied into the inline 128-byte header, so none may exceed its
// bound: an authored message that trapped on construction would turn a
// recoverable failure into a crash.
const (
	errorMessageBound = 256
	errorHeaderBound  = 128
)

// The message constants are string constants named ...Message... or ...Failed,
// declared by the generator files that own each component.
func TestBuiltInErrorMessagesFitTheInlineMessage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for index, identifier := range value.Names {
					if !isErrorMessageName(identifier.Name) || index >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					text, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					checked++
					if len(text) > errorMessageBound {
						t.Errorf("%s.%s is %d bytes, above the %d-byte Error message", name, identifier.Name, len(text), errorMessageBound)
					}
				}
			}
		}
	}
	if checked < 40 {
		t.Fatalf("only %d message constants were inventoried; the naming rule no longer finds the component messages", checked)
	}
}

func isErrorMessageName(name string) bool {
	return strings.Contains(name, "Message") || strings.HasSuffix(name, "Failed")
}

// The component sources build their fallback ErrorKind.Other headers from a
// fixed local text; each must fit the inline header.
func TestRuntimeErrorHeadersFitTheInlineHeader(t *testing.T) {
	pattern := regexp.MustCompile(`static const char text\[\] = "([^"]*)";`)
	sources, err := filepath.Glob(filepath.Join("packages", "*.c"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range sources {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(contents), -1) {
			checked++
			if len(match[1]) > errorHeaderBound {
				t.Errorf("%s header %q is %d bytes, above the %d-byte header", name, match[1], len(match[1]), errorHeaderBound)
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d runtime headers were inventoried", checked)
	}
}
