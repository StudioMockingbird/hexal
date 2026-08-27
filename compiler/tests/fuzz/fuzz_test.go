package fuzz

import (
	"errors"
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
	"hexal/workbench/snippets"
)

// seedSources returns every individual source-file string across the whole
// snippet catalog, one seed per file rather than per snippet, so a
// multi-module snippet contributes each of its files as an independent
// single-string seed.
func seedSources(f *testing.F) []string {
	f.Helper()
	categories, err := snippets.Load()
	if err != nil {
		f.Fatalf("snippets.Load() error = %v", err)
	}
	sources := make([]string, 0)
	for _, category := range categories {
		for _, snippet := range category.Snippets {
			for _, source := range snippet.Sources {
				sources = append(sources, source)
			}
		}
	}
	return sources
}

// A small rejected corpus, added directly rather than scraped from other Go
// test literals: malformed, empty, and pathologically nested input the
// accepted snippet catalog never exercises.
var rejectedSeeds = []string{
	"",
	"fun (",
	"x: := 1",
	"module M = import \"",
	"\x00\x01\xff",
}

func addSourceSeeds(f *testing.F) {
	f.Helper()
	for _, source := range seedSources(f) {
		f.Add(source)
	}
	for _, source := range rejectedSeeds {
		f.Add(source)
	}
}

// parseDiagnostics normalizes a stage error (one Diagnostic or a Diagnostics
// slice) into a slice, so a fuzz target can check every position directly
// against the structured fields rather than round-tripping through rendered
// text a stage below compiler.Compile never produces.
func parseDiagnostics(err error) []compilerTypes.Diagnostic {
	var many compilerTypes.Diagnostics
	if errors.As(err, &many) {
		return many
	}
	var one compilerTypes.Diagnostic
	if errors.As(err, &one) {
		return []compilerTypes.Diagnostic{one}
	}
	return nil
}

// FuzzLex asserts invariants 1 and 5: Lex never panics, and for any input its
// token stream terminates with strictly-non-decreasing positions -- line
// never goes backwards, and column never goes backwards within one line.
func FuzzLex(f *testing.F) {
	addSourceSeeds(f)
	f.Fuzz(func(t *testing.T, source string) {
		tokens, err := lexer.Lex(source)
		if err != nil {
			return
		}
		lastLine, lastColumn := 0, 0
		for _, token := range tokens {
			if token.Line < lastLine || (token.Line == lastLine && token.Column < lastColumn) {
				t.Fatalf("token %+v position went backwards from %d:%d", token, lastLine, lastColumn)
			}
			lastLine, lastColumn = token.Line, token.Column
		}
	})
}

// FuzzParse asserts invariants 1 and 5: for any token stream Lex accepts,
// Parse terminates with either a program or diagnostics, never a panic
// (checked by the fuzzing framework itself) and never leaves an ill-formed
// diagnostic behind.
func FuzzParse(f *testing.F) {
	addSourceSeeds(f)
	f.Fuzz(func(t *testing.T, source string) {
		tokens, err := lexer.Lex(source)
		if err != nil {
			return
		}
		_, err = parser.Parse(tokens)
		if err == nil {
			return
		}
		for _, diagnostic := range parseDiagnostics(err) {
			if strings.TrimSpace(diagnostic.Message) == "" {
				t.Fatalf("parse diagnostic has an empty message: %+v", diagnostic)
			}
			if diagnostic.Line <= 0 || diagnostic.Column <= 0 {
				t.Fatalf("parse diagnostic has a non-positive coordinate: %+v", diagnostic)
			}
		}
	})
}

// FuzzCompile asserts all five invariants over one source string compiled as
// the sole module app.hex.
func FuzzCompile(f *testing.F) {
	addSourceSeeds(f)
	f.Fuzz(func(t *testing.T, source string) {
		sources := map[string]string{"app.hex": source}
		result := assertDeterministic(t, sources, "app.hex", compiler.Project{})
		assertFailClosed(t, result)
		assertNoUnknownError(t, result)
		assertWellFormedDiagnostics(t, result.Stderr, map[string]bool{"app.hex": true})
		if result.ExitCode == compiler.ExitSuccess {
			assertNoDispatchTripwire(t, result.Files)
		}
	})
}

// FuzzCompileMultiModule asserts all five invariants plus import-graph
// handling over three independently fuzzed sources at fixed positional keys.
// Every byte may occur in a Go fuzz string, so no delimiter or escaping
// convention could split one string into several logical keys safely; fixed
// arguments exercise missing, duplicate, cyclic, malformed, and successful
// imports without adding a serialization format.
func FuzzCompileMultiModule(f *testing.F) {
	moduleSeeds := seedSources(f)
	for index, source := range moduleSeeds {
		a := ""
		b := ""
		if index+1 < len(moduleSeeds) {
			a = moduleSeeds[index+1]
		}
		if index+2 < len(moduleSeeds) {
			b = moduleSeeds[index+2]
		}
		f.Add(source, a, b)
	}
	for _, source := range rejectedSeeds {
		f.Add(source, "", "")
	}
	f.Fuzz(func(t *testing.T, app, a, b string) {
		sources := map[string]string{"app.hex": app, "a.hex": a, "b.hex": b}
		result := assertDeterministic(t, sources, "app.hex", compiler.Project{})
		assertFailClosed(t, result)
		assertNoUnknownError(t, result)
		assertWellFormedDiagnostics(t, result.Stderr, map[string]bool{"app.hex": true, "a.hex": true, "b.hex": true})
		if result.ExitCode == compiler.ExitSuccess {
			assertNoDispatchTripwire(t, result.Files)
		}
	})
}
