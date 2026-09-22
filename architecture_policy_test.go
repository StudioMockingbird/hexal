package hexal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two packages the refactoring arc creates, at the exact paths decided
// for them. Neither may sit under an internal/ directory: the driver reads
// both, and Go's internal rule would place compiler/internal/config out of
// its reach.
const (
	configPackagePath   = "compiler/config"
	specdataPackagePath = "compiler/specdata"
)

// arcPackageDir reports the directory of one arc package and whether it
// exists yet. These guards are written before the packages are, so an absent
// package is a pending invariant rather than a failure: the guard starts
// enforcing the moment the directory appears.
func arcPackageDir(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return path, true
}

// parseArcPackage parses every non-test .go file of one arc package.
func parseArcPackage(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files = append(files, file)
	}
	return fset, files
}

// stringLiterals returns every string literal in one file, with its position.
func stringLiterals(fset *token.FileSet, file *ast.File) map[string]token.Position {
	found := make(map[string]token.Position)
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconvUnquote(literal.Value)
		if err != nil {
			return true
		}
		if _, seen := found[value]; !seen {
			found[value] = fset.Position(literal.Pos())
		}
		return true
	})
	return found
}

// strconvUnquote trims a Go string literal's quotes without importing strconv
// for one call; a raw literal keeps its content verbatim.
func strconvUnquote(literal string) (string, error) {
	if len(literal) < 2 {
		return "", os.ErrInvalid
	}
	return literal[1 : len(literal)-1], nil
}

// The registry sits below compiler/types in the import graph. compiler/corelib
// already imports compiler/types and stores live Type values, so a specdata
// that imported compiler/types would close a cycle. Identifiers cross this
// boundary; types do not.
func TestSpecdataImportsNoCompilerPackage(t *testing.T) {
	dir, exists := arcPackageDir(specdataPackagePath)
	if !exists {
		t.Skip("compiler/specdata does not exist yet; this guard enforces its import boundary once it does")
	}
	fset, files := parseArcPackage(t, dir)
	for _, file := range files {
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if strings.HasPrefix(path, "hexal/compiler/") {
				position := fset.Position(spec.Pos())
				t.Errorf("%s imports %s; specdata must import no compiler package, or the types <- specdata <- types cycle closes", position, path)
			}
		}
	}
}

// Target identity, target semantic facts, and target qualification are three
// different things. Qualification depends on the installed backend, pack
// availability, and host state, none of which the compiler may observe, so it
// stays in internal/driver and never reaches the registry.
func TestSpecdataDeclaresNoDriverFact(t *testing.T) {
	dir, exists := arcPackageDir(specdataPackagePath)
	if !exists {
		t.Skip("compiler/specdata does not exist yet; this guard keeps driver facts out of it once it does")
	}
	driverFields := []string{"toolchaintriple", "triple", "archive", "packpath", "qualified", "sdk", "sysroot"}
	fset, files := parseArcPackage(t, dir)
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok {
				return true
			}
			for _, name := range field.Names {
				lowered := strings.ToLower(name.Name)
				for _, banned := range driverFields {
					if lowered == banned {
						t.Errorf("%s: field %s is a driver fact; qualification and toolchain state stay in internal/driver", fset.Position(name.Pos()), name.Name)
					}
				}
			}
			return true
		})
	}
}

// Records describe type constructors, never specializations. String<N> makes
// a per-specialization registry impossible because N is unbounded, so a
// record naming a concrete argument list is the model being abandoned.
func TestSpecdataRecordsNameNoSpecialization(t *testing.T) {
	dir, exists := arcPackageDir(specdataPackagePath)
	if !exists {
		t.Skip("compiler/specdata does not exist yet; this guard enforces constructor records once it does")
	}
	fset, files := parseArcPackage(t, dir)
	for _, file := range files {
		for value, position := range stringLiterals(fset, file) {
			if strings.Contains(value, "<") && strings.Contains(value, ">") {
				t.Errorf("%s: %q names a concrete specialization; records describe constructors, and C names are derived from arguments", position, value)
			}
		}
	}
}

// Registry validity is a property of the source tree, so it is checked where
// source-tree properties are checked. An init() panic would crash every
// consumer of the compiler over a maintainer's mistake, and Go's init has no
// error path to do anything gentler.
func TestSpecdataValidatesFromTestNotInit(t *testing.T) {
	dir, exists := arcPackageDir(specdataPackagePath)
	if !exists {
		t.Skip("compiler/specdata does not exist yet; this guard enforces its validation contract once it does")
	}
	fset, files := parseArcPackage(t, dir)
	declaresValidate := false
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			switch function.Name.Name {
			case "init":
				t.Errorf("%s: specdata declares init(); registry validation belongs in Validate() called from a test, not in package initialization", fset.Position(function.Pos()))
			case "Validate":
				declaresValidate = true
			}
		}
	}
	if !declaresValidate {
		t.Error("compiler/specdata declares no Validate(); the registry must be checkable without being consumed")
	}
	if !packageTestCalls(t, dir, "Validate(") {
		t.Error("no test in compiler/specdata calls Validate(); an unvalidated registry is an unchecked one")
	}
}

// packageTestCalls reports whether any _test.go file in dir contains needle.
func packageTestCalls(t *testing.T, dir, needle string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		if strings.Contains(string(body), needle) {
			return true
		}
	}
	return false
}

// The registry holds facts, not behavior. Component demand stays explicit Go
// functions in the generator, so a func-typed record field would be the
// predicate language this arc declined to invent.
func TestSpecdataDeclaresNoBehaviorField(t *testing.T) {
	dir, exists := arcPackageDir(specdataPackagePath)
	if !exists {
		t.Skip("compiler/specdata does not exist yet; this guard keeps behavior out of records once it does")
	}
	fset, files := parseArcPackage(t, dir)
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			structure, ok := node.(*ast.StructType)
			if !ok || structure.Fields == nil {
				return true
			}
			for _, field := range structure.Fields.List {
				if _, isFunc := field.Type.(*ast.FuncType); isFunc {
					t.Errorf("%s: a record field is func-typed; demand and behavior stay explicit Go functions, not data", fset.Position(field.Pos()))
				}
			}
			return true
		})
	}
}

// Diagnostic wording stays with the phase that emits it, because earliest
// diagnostic ownership ties a message to the phase that can prove it. The
// proxy for "wording" is a sentence-shaped literal: long, spaced, and not a
// path, identifier, or C fragment.
func TestConfigAndSpecdataHoldNoDiagnosticText(t *testing.T) {
	for _, path := range []string{configPackagePath, specdataPackagePath} {
		dir, exists := arcPackageDir(path)
		if !exists {
			continue
		}
		fset, files := parseArcPackage(t, dir)
		for _, file := range files {
			for value, position := range stringLiterals(fset, file) {
				if looksLikeDiagnostic(value) {
					t.Errorf("%s: %q reads as diagnostic wording; messages stay in the phase that emits them", position, value)
				}
			}
		}
	}
}

// looksLikeDiagnostic is a deliberate proxy, not a parser: at least four
// words and twenty characters, with no path separator or template action.
func looksLikeDiagnostic(value string) bool {
	if len(value) < 20 || strings.ContainsAny(value, "/{}") {
		return false
	}
	return len(strings.Fields(value)) >= 4
}

// Both packages sit at ordinary paths. A package named config or specdata
// anywhere else, and especially under an internal/ tree, is the decision
// being reversed by accident.
func TestArcPackagesAreAtDecidedPaths(t *testing.T) {
	decided := map[string]string{
		"config":   configPackagePath,
		"specdata": specdataPackagePath,
	}
	filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "lib", ".git", ".tmp", "modules", "packages":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if parseErr != nil {
			return nil
		}
		want, tracked := decided[file.Name.Name]
		if !tracked {
			return nil
		}
		got := filepath.ToSlash(filepath.Dir(path))
		if got != want {
			t.Errorf("package %s is at %s; it belongs at %s, and never under an internal/ tree the driver cannot import", file.Name.Name, got, want)
		}
		return nil
	})
}

// One package, files split by domain. Cross-domain reference checking is the
// registry's purpose and wants one Validate(); a subpackage would need a
// coordinator importing all of them for the same coupling with more parts.
func TestSpecdataIsOnePackage(t *testing.T) {
	dir, exists := arcPackageDir(specdataPackagePath)
	if !exists {
		t.Skip("compiler/specdata does not exist yet; this guard keeps it one package once it does")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		nested := filepath.Join(dir, entry.Name())
		children, err := os.ReadDir(nested)
		if err != nil {
			continue
		}
		for _, child := range children {
			if strings.HasSuffix(child.Name(), ".go") {
				t.Errorf("%s declares a nested package; specdata is one package whose files are split by domain", nested)
				break
			}
		}
	}
}
