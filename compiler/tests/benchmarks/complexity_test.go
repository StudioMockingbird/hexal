package benchmarks

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fzipp/gocyclo"
	"github.com/uudashr/gocognit"
)

// Complexity of the compiler's own Go source. Two metrics:
// cyclomatic is the actual branching a test would have to cover, cognitive is
// the perceived difficulty of reading it. Neither substitutes for the other:
// a flat switch returning a string per case scores high on the first and 1 on
// the second, and is genuinely trivial.
//
// Halstead measures and the Maintainability Index were prototyped and dropped:
// volume correlated with line count at r = 0.957, and MI is a formula over
// volume, cyclomatic, and lines, so it carried nothing of its own.
//
// The report asserts nothing about the numbers. The benchmarking policy
// governs: no threshold, no gate; a measurement is compared deliberately by a
// human.
//
//	go test -run TestComplexityReport -v ./compiler/tests/benchmarks

// functionComplexity is one function's measurements.
type functionComplexity struct {
	pkg        string
	name       string
	file       string
	cyclomatic int
	cognitive  int
	lines      int
}

// measureComplexity walks every non-test .go file under root and measures each
// function with a body. Declarations without one (external linkage) have no
// complexity to report.
func measureComplexity(t *testing.T, root string) []functionComplexity {
	t.Helper()
	measured := make([]functionComplexity, 0, 1024)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			measured = append(measured, functionComplexity{
				pkg:        filepath.ToSlash(filepath.Dir(path)),
				name:       function.Name.Name,
				file:       filepath.ToSlash(path),
				cyclomatic: gocyclo.Complexity(function),
				cognitive:  gocognit.Complexity(function),
				lines:      fset.Position(function.End()).Line - fset.Position(function.Pos()).Line + 1,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return measured
}

// TestComplexityReport prints the report. It fails only if the walk found no
// functions, which would mean a wrong path or a parse failure reading as "no
// complexity found" rather than as an error.
func TestComplexityReport(t *testing.T) {
	measured := measureComplexity(t, filepath.Join("..", ".."))
	if len(measured) == 0 {
		t.Fatal("measured no functions; the walk root or the parse is wrong")
	}

	slices.SortFunc(measured, func(left, right functionComplexity) int {
		if left.cyclomatic != right.cyclomatic {
			return right.cyclomatic - left.cyclomatic
		}
		return strings.Compare(left.name, right.name)
	})

	t.Logf("measured %d functions with a body under compiler/", len(measured))

	t.Log("worst 15 by cyclomatic complexity:")
	for _, entry := range measured[:min(15, len(measured))] {
		t.Logf("  %-46s cyc=%-4d cog=%-4d lines=%-4d %s",
			entry.name, entry.cyclomatic, entry.cognitive, entry.lines, entry.file)
	}

	byCognitive := slices.Clone(measured)
	slices.SortFunc(byCognitive, func(left, right functionComplexity) int {
		if left.cognitive != right.cognitive {
			return right.cognitive - left.cognitive
		}
		return strings.Compare(left.name, right.name)
	})
	t.Log("worst 15 by cognitive complexity:")
	for _, entry := range byCognitive[:min(15, len(byCognitive))] {
		t.Logf("  %-46s cog=%-4d cyc=%-4d lines=%-4d %s",
			entry.name, entry.cognitive, entry.cyclomatic, entry.lines, entry.file)
	}

	// Distribution over the cyclomatic bands golangci-lint's defaults imply,
	// so the shape of the tail is visible and not just its worst member.
	bands := map[string]int{}
	for _, entry := range measured {
		switch {
		case entry.cyclomatic > 50:
			bands["  >50 "]++
		case entry.cyclomatic > 20:
			bands[" 21-50"]++
		case entry.cyclomatic > 10:
			bands[" 11-20"]++
		default:
			bands["  <=10"]++
		}
	}
	t.Log("cyclomatic distribution:")
	for _, band := range []string{"  >50 ", " 21-50", " 11-20", "  <=10"} {
		t.Logf("  %s %d", band, bands[band])
	}

	worstByPackage := map[string]functionComplexity{}
	for _, entry := range measured {
		if current, seen := worstByPackage[entry.pkg]; !seen || entry.cyclomatic > current.cyclomatic {
			worstByPackage[entry.pkg] = entry
		}
	}
	t.Log("worst function per package:")
	for _, pkg := range slices.Sorted(maps.Keys(worstByPackage)) {
		entry := worstByPackage[pkg]
		t.Logf("  %-34s cyc=%-4d cog=%-4d %s", pkg, entry.cyclomatic, entry.cognitive, entry.name)
	}
}

// TestNoThirdPartyImportsOutsideBenchmarks is a layering invariant: the
// complexity libraries are confined to this package's test files, so nothing
// the compiler or the workbench builds or links changes. Phrased on non-test
// files because this package lives under compiler/: `go build ./compiler/...`
// matches it, and only test files keep it empty to the build.
func TestNoThirdPartyImportsOutsideBenchmarks(t *testing.T) {
	fset := token.NewFileSet()
	for _, root := range []string{filepath.Join("..", "..", ".."), filepath.Join("..", "..", "..", "workbench")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				// bin/ holds built binaries; .git holds no Go source.
				if name := entry.Name(); name == ".git" || name == "bin" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			for _, imported := range file.Imports {
				route := strings.Trim(imported.Path.Value, `"`)
				if strings.HasPrefix(route, "hexal/") || !strings.Contains(strings.Split(route, "/")[0], ".") {
					continue
				}
				t.Errorf("%s imports the third-party %q; non-test code must stay dependency-free", path, route)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}
