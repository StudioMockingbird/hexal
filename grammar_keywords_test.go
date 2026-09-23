package hexal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/exp/ebnf"
)

// contextualKeywords are grammar word terminals that are deliberately not
// lexer keywords. Each is an ordinary identifier everywhere except one shown
// position: extern, from, c, opaque, constant, and global appear only in
// C-interop constructs, and std only as the standard-library import prefix.
// Written here they become a checked fact rather than prose.
var contextualKeywords = []string{"c", "constant", "extern", "from", "global", "opaque", "std"}

// wordTerminalPattern keeps only lowercase word-shaped terminals; punctuation
// and operator terminals are not keywords and are not this guard's subject.
var wordTerminalPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// lexerKeywords reads the keyword map's literal keys from the lexer source
// through go/ast. A regex over the map body matches nothing useful; the AST
// yields the exact set the lexer reserves.
func lexerKeywords(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("compiler", "lexer", "lexer.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	keywords := make(map[string]bool)
	for _, declaration := range file.Decls {
		value, ok := declaration.(*ast.GenDecl)
		if !ok || value.Tok != token.VAR {
			continue
		}
		for _, spec := range value.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != "keywords" {
				continue
			}
			if len(valueSpec.Values) != 1 {
				t.Fatalf("%s: keywords has %d values, want exactly 1", path, len(valueSpec.Values))
			}
			elements, ok := valueSpec.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s: keywords value is %T, want a composite literal", path, valueSpec.Values[0])
			}
			for _, element := range elements.Elts {
				keyValue, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				literal, ok := keyValue.Key.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				key, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				keywords[key] = true
			}
		}
	}
	if len(keywords) == 0 {
		t.Fatalf("%s: found no keyword map entries", path)
	}
	return keywords
}

// grammarWordTerminals collects word-shaped terminals from uppercase-named
// productions only: this grammar names syntactic productions CamelCase and
// lexical ones snake_case, and keywords appear only in the former. The meta
// and same-line sentinels are notation, not terminals.
func grammarWordTerminals(t *testing.T) map[string]bool {
	t.Helper()
	grammar, err := ebnf.Parse("GRAMMAR.ebnf", strings.NewReader(grammarSource))
	if err != nil {
		t.Fatalf("grammar parse failed: %v", err)
	}
	terminals := make(map[string]bool)
	for name, production := range grammar {
		if production == nil || production.Name == nil || !isUpper(name) {
			continue
		}
		walkExpression(production.Expr, func(token string) {
			if token == "meta" || token == "same-line" {
				return
			}
			if wordTerminalPattern.MatchString(token) {
				terminals[token] = true
			}
		})
	}
	return terminals
}

// walkExpression visits every Token string nested in one EBNF expression.
func walkExpression(expression ebnf.Expression, visit func(string)) {
	switch node := expression.(type) {
	case ebnf.Alternative:
		for _, child := range node {
			walkExpression(child, visit)
		}
	case ebnf.Sequence:
		for _, child := range node {
			walkExpression(child, visit)
		}
	case *ebnf.Token:
		visit(node.String)
	case *ebnf.Group:
		walkExpression(node.Body, visit)
	case *ebnf.Option:
		walkExpression(node.Body, visit)
	case *ebnf.Repetition:
		walkExpression(node.Body, visit)
	case *ebnf.Name:
	case *ebnf.Range:
	case *ebnf.Bad:
	default:
		panic("unexpected EBNF expression type")
	}
}

func isUpper(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// The normative grammar and the lexer must reserve the same word set, modulo
// the declared contextual keywords. A drift in either direction is a language
// change nothing else would catch: the grammar can be self-consistent and
// still describe a different language than the one that ships.
func TestGrammarWordTerminalsMatchLexerKeywords(t *testing.T) {
	keywords := lexerKeywords(t)
	terminals := grammarWordTerminals(t)
	allowed := make(map[string]bool, len(contextualKeywords))
	for _, word := range contextualKeywords {
		allowed[word] = true
	}

	for _, escape := range []string{"b", "e", "n", "r", "t", "x"} {
		if terminals[escape] {
			t.Errorf("grammar terminal %q is an escape-sequence character; uppercase-production collection is broken", escape)
		}
	}
	if terminals["meta"] {
		t.Error("grammar terminal \"meta\" is a sentinel, not a terminal")
	}
	if terminals["same-line"] {
		t.Error("grammar terminal \"same-line\" is a sentinel, not a terminal")
	}
	if allowed["unsafe"] {
		t.Error("unsafe is a lexer keyword, not a contextual keyword; it must not sit on the allowlist")
	}

	var missingFromGrammar []string
	for word := range keywords {
		if !terminals[word] {
			missingFromGrammar = append(missingFromGrammar, word)
		}
	}
	sort.Strings(missingFromGrammar)
	if len(missingFromGrammar) > 0 {
		t.Errorf("lexer keywords missing from GRAMMAR.ebnf: %v", missingFromGrammar)
	}

	var undeclared []string
	for word := range terminals {
		if !keywords[word] && !allowed[word] {
			undeclared = append(undeclared, word)
		}
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("grammar word terminals that are neither lexer keywords nor declared contextual keywords: %v", undeclared)
	}

	for _, word := range contextualKeywords {
		if !terminals[word] {
			t.Errorf("declared contextual keyword %q is missing from GRAMMAR.ebnf", word)
		}
		if keywords[word] {
			t.Errorf("contextual keyword %q is also a lexer keyword; it must be one or the other", word)
		}
	}

	want := len(keywords) + len(contextualKeywords)
	if len(terminals) != want {
		t.Errorf("grammar has %d word terminals, want %d (lexer keywords %d + contextual %d)",
			len(terminals), want, len(keywords), len(contextualKeywords))
	}
	t.Logf("grammar word terminals: %d = %d lexer keywords + %d contextual", len(terminals), len(keywords), len(contextualKeywords))
}
