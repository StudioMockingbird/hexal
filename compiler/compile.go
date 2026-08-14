package compiler

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"hexal/compiler/checker"
	"hexal/compiler/generator"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

const (
	// ExitSuccess indicates that all compiler stages completed successfully.
	ExitSuccess = 0
	// ExitFailure indicates that one or more compiler diagnostics were emitted.
	ExitFailure = 1
)

// CompilationResult contains the generated files and process-style result.
type CompilationResult struct {
	MainC string
	MainH string
	// Files is the authoritative generated-artifact map: every emitted
	// C/header file under its normalized logical key, including main.c,
	// main.h, and all modules/<canonical-path>.c/.h pairs. MainC and MainH
	// mirror Files["main.c"] and Files["main.h"]; they are never generated
	// or mutated independently.
	Files    map[string]string
	Stderr   []string
	ExitCode int
	Stats    CompilationStats
}

// CompilationStats records the work done by each compiler phase.
type CompilationStats struct {
	TokenCount       int
	SourceLines      int
	LexDuration      time.Duration
	ParseDuration    time.Duration
	CheckDuration    time.Duration
	GenerateDuration time.Duration
	PixelSubtotal    time.Duration
	TotalDuration    time.Duration
}

// Compile runs Hexal source through every stage and returns main.c, main.h,
// std.err entries, and an EXIT_SUCCESS or EXIT_FAILURE-compatible status.
//
// sources maps logical .hex filenames to complete Hexal source strings.
// entrypoint is the logical .hex filename of the selected root module and
// must name exactly one entry in sources. Every module transitively imported
// from the entrypoint is resolved against sources, so the compilation covers
// the whole reachable module graph. The compiler is exclusively an in-memory
// string transformation: it performs no filesystem reads, writes, discovery,
// or working-directory lookup.
func Compile(sources map[string]string, entrypoint string) CompilationResult {
	compileStarted := time.Now()
	stats := CompilationStats{}

	if _, ok := sources[entrypoint]; !ok {
		err := compilerTypes.NewDiagnostic(compilerTypes.ModuleError, "compile", 1, 1,
			"entrypoint "+entrypoint+" was not found in the supplied sources")
		return failureResult(err, stats, compileStarted)
	}

	started := time.Now()
	// Reachability lexes and parses every reachable module; the lex and
	// parse phases are not split out of this duration.
	order, programs, resolveErr := reachableModules(sources, entrypoint)
	stats.LexDuration = time.Since(started)
	if resolveErr != nil {
		return failureResult(resolveErr, stats, compileStarted)
	}

	// Stats sum over the reachable module set: lines via sourceLineCount,
	// tokens via a second lex pass.
	for key := range programs {
		stats.SourceLines += sourceLineCount(sources[key])
		tokens, lexErr := lexer.Lex(sources[key])
		if lexErr == nil {
			stats.TokenCount += len(tokens)
		}
	}

	started = time.Now()
	checked, checkErr := checker.CheckModules(programs, order, canonicalModuleID(entrypoint))
	stats.CheckDuration = time.Since(started)
	if checkErr != nil {
		return failureResult(checkErr, stats, compileStarted)
	}

	started = time.Now()
	files, generateErr := generator.GenerateChecked(checked, order, canonicalModuleID(entrypoint))
	stats.GenerateDuration = time.Since(started)
	if generateErr != nil {
		return failureResult(generateErr, stats, compileStarted)
	}
	finalizeStats(&stats, compileStarted)
	return CompilationResult{
		MainC:    files["main.c"],
		MainH:    files["main.h"],
		Files:    files,
		ExitCode: ExitSuccess,
		Stats:    stats,
	}
}

// canonicalModuleID strips a trailing ".hex".
func canonicalModuleID(logicalKey string) string {
	return strings.TrimSuffix(logicalKey, ".hex")
}

// resolveImportPath resolves a raw module-path literal relative to fromModule
// (a canonical id) and returns the target's canonical id. The literal keeps
// the lexer's spelling, surrounding quotes included; rules apply to the quoted
// payload's content. Rules, in order:
//   - the payload must start with "./" (exactly one) or one or more "../";
//     anything else fails with "import path <rawPath> is not relative".
//   - components: drop the "./" prefix; each "../" pops the last directory
//     component of fromModule's dir (dir = everything before the last "/");
//     popping an empty dir fails with "import resolves above the logical
//     source-map root".
//   - each remaining component must be a valid identifier, else "invalid
//     component <c> in import path <rawPath>".
//   - join remaining components with "/", then canonicalModuleID: a trailing
//     ".hex" on the path is stripped and the result is the canonical id.
//
// The caller attaches the offending Path token's line/column to the error.
func resolveImportPath(fromModule, rawPath string) (string, error) {
	path := rawPath
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		path = path[1 : len(path)-1]
	}
	if !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") {
		return "", fmt.Errorf("import path %s is not relative", rawPath)
	}
	dir := ""
	if slash := strings.LastIndex(fromModule, "/"); slash >= 0 {
		dir = fromModule[:slash]
	}
	rest := path
	for strings.HasPrefix(rest, "../") {
		if dir == "" {
			return "", fmt.Errorf("import resolves above the logical source-map root")
		}
		if slash := strings.LastIndex(dir, "/"); slash >= 0 {
			dir = dir[:slash]
		} else {
			dir = ""
		}
		rest = rest[3:]
	}
	if strings.HasPrefix(rest, "./") {
		rest = rest[2:]
	}
	// A trailing ".hex" is path spelling, not part of the canonical identity:
	// "./math.hex" and "./math" name the same module "math".
	rest = strings.TrimSuffix(rest, ".hex")
	components := strings.Split(rest, "/")
	for _, component := range components {
		// A component with its own dot (beyond a stripped ".hex") is an
		// opaque filename like "math.txt": not an identifier, but it is
		// still a legal path that simply fails lookup (RFC 0034 plan
		// Task 4 Step 5 expects "./math.txt" to be "not found").
		if strings.Contains(component, ".") {
			continue
		}
		if component == "" || !lexer.IsIdentifierStart(component[0]) {
			return "", fmt.Errorf("invalid component %s in import path %s", component, rawPath)
		}
		for i := 1; i < len(component); i++ {
			if !lexer.IsIdentifierPart(component[i]) {
				return "", fmt.Errorf("invalid component %s in import path %s", component, rawPath)
			}
		}
	}
	if dir != "" {
		rest = dir + "/" + rest
	}
	return canonicalModuleID(rest), nil
}

// reachableModules lexes and parses every module reachable from the entrypoint
// (its logical key) once, resolving imports, and returns the reachable modules'
// canonical ids in post-order (dependencies before dependents) plus the parsed
// programs keyed by logical key. A lex or parse failure in any reachable
// module aborts the scan, returning that module's merged diagnostics; every
// resolvable import error is collected and returned sorted by module
// post-order position, then line, then column.
func reachableModules(sources map[string]string, entrypoint string) ([]string, map[string]parser.Program, error) {
	state := &reachState{
		sources:  sources,
		parsed:   make(map[string]*parser.Program),
		visited:  make(map[string]bool),
		byModule: make(map[string]compilerTypes.Diagnostics),
	}
	if err := state.visit(canonicalModuleID(entrypoint)); err != nil {
		return nil, nil, err
	}
	programs := make(map[string]parser.Program, len(state.parsed))
	for key, program := range state.parsed {
		programs[key] = *program
	}
	merged := make(compilerTypes.Diagnostics, 0)
	for _, moduleID := range state.order {
		diagnostics := state.byModule[moduleID]
		sort.SliceStable(diagnostics, func(left, right int) bool {
			if diagnostics[left].Line != diagnostics[right].Line {
				return diagnostics[left].Line < diagnostics[right].Line
			}
			return diagnostics[left].Column < diagnostics[right].Column
		})
		merged = append(merged, diagnostics...)
	}
	if len(merged) > 0 {
		return state.order, programs, merged
	}
	return state.order, programs, nil
}

// reachState carries one import-resolution DFS: the parsed-program cache, the
// post-order canonical id list, the DFS stack for cycle detection, and every
// resolution diagnostic bucketed by its module.
type reachState struct {
	sources  map[string]string
	parsed   map[string]*parser.Program // logical source key -> program
	visited  map[string]bool            // canonical ids with a visit begun
	stack    []string                   // canonical ids on the current DFS path
	order    []string                   // post-order: dependencies first
	byModule map[string]compilerTypes.Diagnostics
}

// visit parses canonical and its transitive imports, then appends canonical
// to the post-order list. It returns the failing module's merged diagnostics
// when that module cannot be lexed or parsed.
func (s *reachState) visit(canonical string) error {
	if s.visited[canonical] {
		return nil
	}
	s.visited[canonical] = true
	keys := s.sourceKeyFor(canonical)
	if len(keys) == 0 {
		// Unreachable: Compile validates the entrypoint and every import
		// checks existence before recursing.
		return nil
	}
	if len(keys) > 1 {
		s.record(canonical, 1, 1,
			fmt.Sprintf("sources contain both %s and %s for module %s", keys[0], keys[1], canonical))
	}
	key := keys[0]
	tokens, lexErr := lexer.Lex(s.sources[key])
	if lexErr != nil {
		return mergeDiagnostics(lexErr, nil)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return mergeDiagnostics(nil, parseErr)
	}
	s.parsed[key] = &program

	s.stack = append(s.stack, canonical)
	imported := make(map[string]bool)
	for _, item := range program.Items {
		importDecl, ok := item.(parser.ImportDeclaration)
		if !ok {
			continue
		}
		if err := s.resolveImport(canonical, importDecl, imported); err != nil {
			return err
		}
	}
	s.stack = s.stack[:len(s.stack)-1]
	s.order = append(s.order, canonical)
	return nil
}

// resolveImport resolves one import declaration of fromModule, recording
// every resolution diagnostic it can prove and recursing into the target.
// imported holds the canonical ids fromModule has already bound in this file.
// A lex/parse failure inside the target aborts the whole scan: the target's
// imports are unknown, so the reachable set is incomplete.
func (s *reachState) resolveImport(fromModule string, importDecl parser.ImportDeclaration, imported map[string]bool) error {
	line, column := importDecl.Path.Line, importDecl.Path.Column
	rawPath := importDecl.Path.Lexeme
	target, err := resolveImportPath(fromModule, rawPath)
	if err != nil {
		s.record(fromModule, line, column, err.Error())
		return nil
	}
	if len(s.sourceKeyFor(target)) == 0 {
		s.record(fromModule, line, column, "imported module "+rawPath+" was not found")
		return nil
	}
	if imported[target] {
		s.record(fromModule, line, column, "duplicate import of canonical module "+target)
		return nil
	}
	imported[target] = true
	if at := indexIn(s.stack, target); at >= 0 {
		cycle := append(append([]string{}, s.stack[at:]...), target)
		s.record(fromModule, line, column, "import cycle: "+strings.Join(cycle, " -> "))
		return nil
	}
	return s.visit(target)
}

// sourceKeyFor returns every source key that canonicalizes to id, sorted
// ascending for a deterministic pick and error spelling.
func (s *reachState) sourceKeyFor(id string) []string {
	keys := make([]string, 0)
	for key := range s.sources {
		if canonicalModuleID(key) == id {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// record appends one resolution diagnostic to its module's bucket, with the
// position normalized to 1-based source coordinates.
func (s *reachState) record(moduleID string, line, column int, message string) {
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	s.byModule[moduleID] = append(s.byModule[moduleID], compilerTypes.Diagnostic{
		Category: compilerTypes.ModuleError,
		Stage:    "compile",
		Line:     line,
		Column:   column,
		Message:  message,
	})
}

// indexIn returns the first index of needle in haystack, or -1 when absent.
func indexIn(haystack []string, needle string) int {
	for i, candidate := range haystack {
		if candidate == needle {
			return i
		}
	}
	return -1
}

func mergeDiagnostics(errors ...error) error {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, err := range errors {
		if err == nil {
			continue
		}
		switch err := err.(type) {
		case compilerTypes.Diagnostics:
			diagnostics = append(diagnostics, err...)
		case compilerTypes.Diagnostic:
			diagnostics = append(diagnostics, err)
		default:
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.UnknownError,
				Message:  err.Error(),
			})
		}
	}
	if len(diagnostics) == 0 {
		return nil
	}
	sort.SliceStable(diagnostics, func(left, right int) bool {
		if diagnostics[left].Line != diagnostics[right].Line {
			return diagnostics[left].Line < diagnostics[right].Line
		}
		return diagnostics[left].Column < diagnostics[right].Column
	})
	return diagnostics
}

func failureResult(err error, stats CompilationStats, compileStarted time.Time) CompilationResult {
	started := time.Now()
	mainC, mainH := generator.GenerateFailure()
	stats.GenerateDuration = time.Since(started)
	finalizeStats(&stats, compileStarted)
	// Failure output is deliberate fail-closed output: only the complete
	// generated failure entrypoint files, never partial module artifacts.
	files := map[string]string{"main.c": mainC, "main.h": mainH}
	return CompilationResult{
		MainC:    mainC,
		MainH:    mainH,
		Files:    files,
		Stderr:   compilerTypes.ErrorMessages(err),
		ExitCode: ExitFailure,
		Stats:    stats,
	}
}

func finalizeStats(stats *CompilationStats, compileStarted time.Time) {
	stats.PixelSubtotal = stats.LexDuration +
		stats.ParseDuration +
		stats.CheckDuration +
		stats.GenerateDuration
	stats.TotalDuration = time.Since(compileStarted)
}

func sourceLineCount(source string) int {
	if source == "" {
		return 0
	}
	return strings.Count(source, "\n") + 1
}
