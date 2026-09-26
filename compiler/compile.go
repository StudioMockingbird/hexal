// Package compiler is the Hexal compiler entry point: it lexes, parses,
// resolves the module graph, checks, and generates C from an in-memory source
// map. It performs no filesystem access: Compile takes logical source keys and
// returns generated artifacts as strings.
package compiler

import (
	"errors"
	"slices"
	"strings"
	"time"

	"hexal/compiler/checker"
	"hexal/compiler/corelib"
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/generator"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/graph"
	"hexal/stdlib"
)

const (
	// ExitSuccess indicates that all compiler stages completed successfully.
	ExitSuccess = 0
	// ExitFailure indicates that one or more compiler diagnostics were emitted.
	ExitFailure = 1
)

// CompilationResult contains the generated files and process-style result.
type CompilationResult struct {
	// Files is the authoritative generated-artifact map: every emitted
	// C/header file under its normalized logical key. Success returns
	// "hexal.h", one modules/<canonical-path>.c/.h pair per reachable
	// module, and the demand-driven component artifacts under hexal/
	// (hexal/runtime.c when a selected path can trap, and each selected
	// component pair); failure returns a non-nil empty map. A build driver
	// must compile every ".c" entry, not only those under modules/.
	Files map[string]string
	// Dependencies is the sorted, duplicate-free, path-free set of native
	// runtime identities required by Files. Failure returns an empty slice.
	Dependencies []RuntimeDependency
	// Stderr is every diagnostic of the failing stage, already rendered and
	// ordered; it is empty on success. A compilation reports the first stage
	// that failed, not every stage that would have.
	Stderr []string
	// ExitCode is ExitSuccess or ExitFailure, suitable for a process status.
	ExitCode int
	// HasCompilerDefect reports whether Stderr carries an Unknown Error
	// diagnostic: a failure of the compiler itself rather than a rejection of
	// the program. It is derived from the structured diagnostics before they
	// are rendered, so a caller can attribute a bug without parsing message
	// text. It is false on success and for every ordinary rejection.
	HasCompilerDefect bool
	// Stats records the work each compiler phase performed.
	Stats CompilationStats
}

// CompilationStats records the work done by each compiler phase. Durations are
// wall-clock and measured per stage; they are diagnostics for a human reading a
// build, never an input to compilation.
type CompilationStats struct {
	// TokenCount and SourceLines sum over the reachable module set only:
	// a source in the map that no import reaches contributes nothing.
	TokenCount  int
	SourceLines int
	// LexDuration covers reachability as a whole: lexing, parsing, and import
	// resolution fold into one pass and one duration.
	LexDuration      time.Duration
	CheckDuration    time.Duration
	GenerateDuration time.Duration
	// PhaseSubtotal is the sum of the three stage durations; TotalDuration
	// additionally covers everything outside them, so the difference is the
	// entry point's own overhead.
	PhaseSubtotal time.Duration
	TotalDuration time.Duration
}

// panicSeam runs once per compilePipeline invocation and does nothing in
// production. A test in this package may replace it to inject a panic and
// prove Compile's recovery wrapper contains it; no exported API reaches it.
var panicSeam = func() {}

// Compile runs Hexal source through every stage and returns the generated
// artifact map, std.err entries, and an EXIT_SUCCESS or EXIT_FAILURE-compatible
// status.
//
// sources maps logical .hex filenames to complete Hexal source strings.
// entrypoint is the logical .hex filename of the selected root module and
// must name exactly one entry in sources. Every module transitively imported
// from the entrypoint is resolved against sources, so the compilation covers
// the whole reachable module graph. project carries build-time settings
// that are not part of the language; its zero value selects every default.
// The compiler is exclusively an in-memory string transformation: it performs
// no filesystem reads, writes, discovery, or working-directory lookup.
//
// A panic in any stage is a compiler defect. Compile recovers it, discards
// any partial stage output, and returns a failed result carrying one fixed
// Unknown Error diagnostic that names no panic value, Go stack, or host path;
// an ordinary rejection is a Diagnostic returned by a stage, never a panic,
// and passes through unrecovered and unchanged.
func Compile(sources map[string]string, entrypoint string, project Project) (result CompilationResult) {
	compileStarted := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			stats := CompilationStats{}
			finalizeStats(&stats, compileStarted)
			diagnostic := compilerTypes.Locationless(diag.UnknownCompiler())
			result = CompilationResult{
				Files:             map[string]string{},
				Dependencies:      []RuntimeDependency{},
				Stderr:            compilerTypes.ErrorMessages(diagnostic),
				ExitCode:          ExitFailure,
				HasCompilerDefect: true,
				Stats:             stats,
			}
		}
	}()
	return compilePipeline(sources, entrypoint, project, compileStarted)
}

// compilePipeline is Compile's unexported body, run under Compile's recovery
// deferral. It never returns partial output on its own: every early exit goes
// through failureResult.
func compilePipeline(sources map[string]string, entrypoint string, project Project, compileStarted time.Time) CompilationResult {
	panicSeam()
	stats := CompilationStats{}

	// Configuration is validated before any stage runs and fails closed: an
	// invalid Project is a compiler input like any other.
	if err := validateProject(project); err != nil {
		return failureResult(err, stats, compileStarted)
	}

	if _, ok := sources[entrypoint]; !ok {
		err := compilerTypes.At(diag.MissingEntrypoint(entrypoint), span.Span{}, span.Position{Line: 1, Column: 1})
		return failureResult(err, stats, compileStarted)
	}
	if message := validateLogicalKey(entrypoint); !message.IsZero() {
		diagnostic := compilerTypes.At(message, span.Span{}, span.Position{Line: 1, Column: 1})
		return failureResult(diagnostic, stats, compileStarted)
	}

	started := time.Now()
	// Reachability lexes and parses every reachable module and returns the
	// token counts it observed; the lex, parse, and resolution phases fold
	// into one duration and one lex pass.
	graph, resolveErr := reachableModulesTarget(sources, entrypoint, string(project.Target))
	stats.LexDuration = time.Since(started)
	if resolveErr != nil {
		return failureResult(resolveErr, stats, compileStarted)
	}

	// Stats sum over the reachable module set: lines and tokens from the
	// parse pass result, with no second lex.
	for _, moduleID := range graph.Order {
		node := graph.Modules[moduleID]
		stats.SourceLines += node.SourceLines
		stats.TokenCount += node.TokenCount
	}

	// One table owns every logical source this compilation can render a
	// location for. The checker resolves a diagnostic span and the generator
	// resolves a #line or runtime failure span through it, so a location is
	// always derived from the same text the lexer scanned.
	table := sourceTable(sources)

	started = time.Now()
	checked, checkErr := checker.CheckModulesForTarget(graph, project.Target, table)
	stats.CheckDuration = time.Since(started)
	if checkErr != nil {
		return failureResult(checkErr, stats, compileStarted)
	}

	started = time.Now()
	generated, generateErr := generator.GenerateCheckedWithMetadata(graph, checked, generator.Config{
		TaskStackReserve: project.TaskStackReserve,
		TaskStackCommit:  project.TaskStackCommit,
		Target:           project.Target,
		SourceTable:      table,
	})
	stats.GenerateDuration = time.Since(started)
	if generateErr != nil {
		return failureResult(generateErr, stats, compileStarted)
	}
	finalizeStats(&stats, compileStarted)
	return CompilationResult{
		Files:        generated.Files,
		Dependencies: runtimeDependencies(generated.Dependencies),
		ExitCode:     ExitSuccess,
		Stats:        stats,
	}
}

// canonicalFromLogicalKey strips a trailing ".hex" from a logical source key,
// the one place an id is derived from a key. Every other consumer reads a
// node's LogicalKey instead of rebuilding it.
func canonicalFromLogicalKey(logicalKey string) string {
	return strings.TrimSuffix(logicalKey, ".hex")
}

// validateLogicalKey enforces the module-path allowlist on a source-map key:
// relative, "/" as its only separator, exactly one trailing ".hex", and every
// path component a Hexal identifier (an ASCII letter, then ASCII letters,
// digits, or "_"). Logical keys are compiler identities, not host filesystem
// paths, so this is deliberately narrower than what a filesystem would
// accept: it closes preprocessor injection and path traversal through a
// crafted key by construction rather than by blocking specific characters.
func validateLogicalKey(key string) diag.Message {
	const suffix = ".hex"
	if !strings.HasSuffix(key, suffix) {
		return diag.InvalidLogicalKey(key, diag.LogicalKeyInvalid)
	}
	stem := key[:len(key)-len(suffix)]
	if first, _, _ := strings.Cut(stem, "/"); first == "std" {
		// The std collection is compiler-owned; reserving the logical-key
		// prefix keeps a user module from claiming a stdlib canonical
		// identity (a core library's std/<path> or a source module's).
		return diag.InvalidLogicalKey(key, diag.LogicalKeyReservedStd)
	} else if first == "hexalc" {
		// hexalc holds only compiler-derived prepared C bindings, which the
		// resolver marks before visiting; a user key there would let project
		// source impersonate a prepared binding.
		return diag.InvalidLogicalKey(key, diag.LogicalKeyReservedCBindings)
	}
	for _, component := range strings.Split(stem, "/") {
		if component == "" || !lexer.IsIdentifierStart(component[0]) {
			return diag.InvalidLogicalKey(key, diag.LogicalKeyInvalid)
		}
		for i := 1; i < len(component); i++ {
			if !lexer.IsIdentifierPart(component[i]) {
				return diag.InvalidLogicalKey(key, diag.LogicalKeyInvalid)
			}
		}
	}
	return diag.Message{}
}

// resolveImportPath resolves a relative quoted module-path literal relative to
// fromModule (a canonical id) and returns the target's canonical id. The
// literal keeps the lexer's spelling, surrounding quotes included; rules apply
// to the quoted payload's content. Rules, in order:
//   - the payload must start with "./" (exactly one) or one or more "../";
//     anything else fails closed, because the parser already rejects a quoted
//     non-relative payload with its own migration diagnostic.
//   - components: drop the "./" prefix; each "../" pops the last directory
//     component of fromModule's dir (dir = everything before the last "/");
//     popping an empty dir fails with "import resolves above the logical
//     source-map root".
//   - each remaining component must be a valid identifier, else "invalid
//     component <c> in import path <rawPath>".
//   - join remaining components with "/", then canonicalFromLogicalKey: a trailing
//     ".hex" on the path is stripped and the result is the canonical id.
//
// The caller attaches the offending reference token's line/column to the error.
func resolveImportPath(fromModule, rawPath string) (string, diag.Message) {
	path := rawPath
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		path = path[1 : len(path)-1]
	}
	if !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") {
		return "", diag.ImportPathError(diag.ImportPathNotRelative, rawPath, "")
	}
	dir := ""
	if slash := strings.LastIndex(fromModule, "/"); slash >= 0 {
		dir = fromModule[:slash]
	}
	rest := path
	for strings.HasPrefix(rest, "../") {
		if dir == "" {
			return "", diag.ImportPathError(diag.ImportPathAboveRoot, rawPath, "")
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
		// opaque filename like "math.txt": not an identifier, but still a
		// legal path that simply fails lookup as "not found".
		if strings.Contains(component, ".") {
			continue
		}
		if component == "" || !lexer.IsIdentifierStart(component[0]) {
			return "", diag.ImportPathError(diag.ImportPathInvalidComponent, rawPath, component)
		}
		for i := 1; i < len(component); i++ {
			if !lexer.IsIdentifierPart(component[i]) {
				return "", diag.ImportPathError(diag.ImportPathInvalidComponent, rawPath, component)
			}
		}
	}
	if dir != "" {
		rest = dir + "/" + rest
	}
	return canonicalFromLogicalKey(rest), diag.Message{}
}

// resolveStdlibPath joins one dotted standard-library reference's components
// into the slash-separated canonical identity the module graph, generated
// names, and artifact keys already use. `std.program` -> `std/program`,
// `std.crypto.hash` -> `std/crypto/hash`.
func resolveStdlibPath(components []lexer.Token) string {
	paths := make([]string, 0, len(components)+1)
	paths = append(paths, "std")
	for _, component := range components {
		paths = append(paths, component.Lexeme)
	}
	return strings.Join(paths, "/")
}

// reachableModules lexes and parses every module reachable from the entrypoint
// (its logical key) once, resolving imports, and returns the authoritative
// module graph: canonical ids in post-order (dependencies before dependents),
// each with the exact source key it was read from, its parsed program, and its
// resolved imports. A lex or parse failure in any reachable module aborts the
// scan, returning that module's merged diagnostics; every resolvable import
// error is collected and returned sorted by module post-order position, then
// line, then column.
func reachableModules(sources map[string]string, entrypoint string) (*checker.ModuleGraph, error) {
	return reachableModulesTarget(sources, entrypoint, "")
}

// reachableModulesTarget is reachableModules with the selected qualified
// target profile. A reachable C import requires it and resolves to the
// prepared binding module the driver inserted under its deterministic
// reserved key.
func reachableModulesTarget(sources map[string]string, entrypoint, target string) (*checker.ModuleGraph, error) {
	root := canonicalFromLogicalKey(entrypoint)
	state := &reachState{
		sources:              sources,
		keysByCanonical:      indexSourceKeys(sources),
		stdlibSources:        stdlibSourcesByCanonical(),
		nodes:                make(map[string]*checker.ModuleNode),
		walk:                 graph.NewWalker[string](),
		byModule:             make(map[string]compilerTypes.Diagnostics),
		target:               target,
		prepared:             make(map[string]bool),
		preparedExpectations: make(map[string]preparedExpectation),
	}
	if err := state.visit(root); err != nil {
		return nil, err
	}
	state.validatePreparedBindings()
	order := state.walk.Order()
	moduleGraph := &checker.ModuleGraph{
		Order:   order,
		Modules: make(map[string]checker.ModuleNode, len(order)),
		Root:    root,
	}
	// Order is the membership authority: a node is published only for a
	// module that completed its visit, so the two can never disagree.
	for _, moduleID := range order {
		moduleGraph.Modules[moduleID] = *state.nodes[moduleID]
	}
	merged := make(compilerTypes.Diagnostics, 0)
	for _, moduleID := range order {
		diagnostics := state.byModule[moduleID]
		slices.SortStableFunc(diagnostics, compilerTypes.CompareDiagnostic)
		merged = append(merged, diagnostics...)
	}
	if len(merged) > 0 {
		return moduleGraph, merged
	}
	return moduleGraph, nil
}

// DiscoverCImports returns every reachable C header request in deterministic
// module order, using the ordinary parser and module traversal. It performs no
// filesystem or process operation and never validates a prepared binding: the
// driver prepares the requests it returns and compiles the augmented map.
func DiscoverCImports(sources map[string]string, entrypoint string) ([]CImportRequest, error) {
	state := &reachState{
		sources:         sources,
		keysByCanonical: indexSourceKeys(sources),
		stdlibSources:   stdlibSourcesByCanonical(),
		nodes:           make(map[string]*checker.ModuleNode),
		walk:            graph.NewWalker[string](),
		byModule:        make(map[string]compilerTypes.Diagnostics),
		discover:        true,
		prepared:        make(map[string]bool),
	}
	if err := state.visit(canonicalFromLogicalKey(entrypoint)); err != nil {
		return nil, err
	}
	return state.requests, nil
}

// reachState carries one import-resolution DFS: the node under construction
// per canonical id, the graph walker that owns the visited set, active path,
// cycle detection, and post-order, and every resolution diagnostic bucketed by
// its module. Each node records the token count observed while lexing it, so
// the caller never re-lexes for stats.
type reachState struct {
	sources map[string]string
	// keysByCanonical groups every supplied source key by
	// canonicalFromLogicalKey, each bucket sorted once at construction, so
	// resolution reads the index instead of rescanning sources. It is derived
	// data only: sourceTable and import authority keep the original logical
	// keys, and the index never validates or rewrites them early.
	keysByCanonical map[string][]string
	// stdlibSources holds the embedded source stdlib modules, keyed by
	// canonical id; it is read-only for the whole walk.
	stdlibSources map[string]string
	nodes         map[string]*checker.ModuleNode // canonical id -> node under construction
	// walk owns the graph mechanics: which canonical ids a visit has begun,
	// the active DFS path cycle detection consults, and the dependency-first
	// post-order the caller reads once resolution finishes.
	walk     *graph.Walker[string]
	byModule map[string]compilerTypes.Diagnostics
	// target is the selected qualified profile; a reachable C import requires
	// it because `long`, plain `char`, layout, and calling ABI are
	// target-dependent.
	target string
	// discover collects C header requests without validating prepared
	// bindings; the driver calls it before preparing them.
	discover bool
	// prepared marks the canonical ids the compiler derived for prepared C
	// bindings, which may use the otherwise-reserved hexalc prefix.
	prepared map[string]bool
	// requests collects reachable C header requests in visit order.
	requests []CImportRequest
	// preparedExpectations records the header each prepared binding key must
	// declare, keyed by the binding's canonical id. It is validated after the
	// whole walk, once every prepared module has been parsed.
	preparedExpectations map[string]preparedExpectation
}

// preparedExpectation is one C import's demand on a prepared binding: the
// header it requested and the import site that must be blamed when the
// prepared module does not declare that header.
type preparedExpectation struct {
	request    CImportRequest
	fromModule string
	line       int
	column     int
	display    string
}

// validatePreparedBindings verifies that every prepared binding declares the
// header its key was derived from. A binding module that names a different
// header is a mismatch, reported with the same Configuration Error as an absent
// binding.
func (s *reachState) validatePreparedBindings() {
	for canonical, expectation := range s.preparedExpectations {
		node := s.nodes[canonical]
		if node == nil {
			// The prepared module failed to lex or parse; that failure already
			// aborts the scan.
			continue
		}
		if preparedModuleNamesHeader(node.Program, expectation.request) {
			continue
		}
		s.record(expectation.fromModule, expectation.line, expectation.column, diag.PreparedBindingMissing(expectation.display))
	}
}

// preparedModuleNamesHeader reports whether a prepared binding module declares
// an `extern c from` block for the requested header identity. The system and
// quoted forms are distinct identities and never coalesce here.
func preparedModuleNamesHeader(program parser.Program, request CImportRequest) bool {
	for _, block := range program.Externs {
		reference := block.Header
		if reference.Kind != parser.CHeaderImportReference {
			continue
		}
		if reference.CHeader == request.Header && reference.System == request.System {
			return true
		}
	}
	return false
}

// visit parses canonical and its transitive imports, then appends canonical
// to the post-order list. It returns the failing module's merged diagnostics
// when that module cannot be lexed or parsed.
func (s *reachState) visit(canonical string) error {
	if !s.walk.Enter(canonical) {
		return nil
	}
	key, text, ok := s.sourceFor(canonical)
	if !ok {
		// Unreachable: Compile validates the entrypoint and every import
		// checks existence before recursing. Leave anyway so Enter and Leave
		// stay balanced and the active path never keeps a phantom node.
		s.walk.Leave()
		return nil
	}
	if !s.prepared[canonical] {
		// A prepared C binding legitimately uses the reserved hexalc
		// namespace; every other module keeps the ordinary key rules.
		if message := validateLogicalKey(key); !message.IsZero() {
			diagnostic := compilerTypes.At(message, span.Span{}, span.Position{Line: 1, Column: 1}).InModule(key)
			return diagnostic
		}
	}
	tokens, lexErr := lexer.Lex(key, text)
	if lexErr != nil {
		return stampModule(mergeDiagnostics(lexErr, nil), key)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return stampModule(mergeDiagnostics(nil, parseErr), key)
	}
	s.nodes[canonical] = &checker.ModuleNode{
		Canonical:   canonical,
		LogicalKey:  key,
		Program:     program,
		TokenCount:  len(tokens),
		SourceLines: sourceLineCount(text),
	}

	imported := make(map[string]bool)
	if program.Import != nil {
		for _, entry := range program.Import.Entries {
			if err := s.resolveImport(canonical, entry, imported); err != nil {
				return err
			}
		}
	}
	s.walk.Leave()
	return nil
}

// resolveImport resolves one import entry of fromModule, recording every
// resolution diagnostic it can prove and recursing into the target. imported
// holds the canonical ids fromModule has already bound in this file. A
// lex/parse failure inside the target aborts the whole scan: the target's
// imports are unknown, so the reachable set is incomplete.
//
// The reference's kind, never a string prefix, decides which tables the
// resolver consults: a dotted `std.<component>` reference names the
// standard-library identity `std/<component>...`, while a relative quoted path
// keeps its lexical source-map arithmetic even when that identity begins with
// the reserved `std` prefix.
func (s *reachState) resolveImport(fromModule string, importDecl parser.ImportEntry, imported map[string]bool) error {
	line, column := importDecl.Reference.Token.Line, importDecl.Reference.Token.Column
	var target string
	switch importDecl.Reference.Kind {
	case parser.RelativeImportReference:
		rawPath := importDecl.Reference.RelativePath.Lexeme
		resolved, message := resolveImportPath(fromModule, rawPath)
		if !message.IsZero() {
			s.record(fromModule, line, column, message)
			return nil
		}
		target = resolved
	case parser.StandardLibraryImportReference:
		target = resolveStdlibPath(importDecl.Reference.Components)
	case parser.CHeaderImportReference:
		request := CImportRequest{Header: importDecl.Reference.CHeader, System: importDecl.Reference.System}
		if s.discover {
			s.requests = append(s.requests, request)
			return nil
		}
		if s.target == "" {
			s.record(fromModule, line, column, diag.CInteropNeedsTarget())
			return nil
		}
		key := CBindingKey(s.target, request)
		if _, present := s.sources[key]; !present {
			s.record(fromModule, line, column, diag.PreparedBindingMissing(importDecl.Reference.DisplaySpelling))
			return nil
		}
		target = strings.TrimSuffix(key, ".hex")
		s.prepared[target] = true
		s.preparedExpectations[target] = preparedExpectation{
			request:    request,
			fromModule: fromModule,
			line:       line,
			column:     column,
			display:    importDecl.Reference.DisplaySpelling,
		}
	default:
		// The parser is the only reference producer; an unknown kind is an
		// internal contract break, never a silently resolved import.
		s.record(fromModule, line, column, diag.UnknownImportReference())
		return nil
	}
	// The edge is the resolution result, recorded in source order for every
	// import whose path resolves. A path that does not resolve has no target
	// to name, and its diagnostic already fails the compilation.
	node := s.nodes[fromModule]
	edge := checker.ModuleEdge{Alias: importDecl.Alias.Lexeme, Target: target}
	if importDecl.Reference.Kind == parser.CHeaderImportReference {
		edge.CImport = true
		edge.CDisplay = importDecl.Reference.DisplaySpelling
	}
	node.Imports = append(node.Imports, edge)
	switch {
	case importDecl.Reference.Kind == parser.StandardLibraryImportReference:
		if corelib.IsModule(target) {
			// A core library has no Hexal source and never joins the module
			// graph: it publishes no ModuleNode, is never lexed or parsed,
			// and is resolved directly by the checker against the corelib
			// table. It still shares the one canonical import identity, so a
			// second alias naming it is the ordinary duplicate diagnostic.
			if imported[target] {
				s.record(fromModule, line, column, diag.DuplicateImport(target))
				return nil
			}
			imported[target] = true
			return nil
		}
		if stdlib.IsSourceModule(target) {
			if imported[target] {
				s.record(fromModule, line, column, diag.DuplicateImport(target))
				return nil
			}
			imported[target] = true
			if cycle, ok := s.walk.Cycle(target); ok {
				s.record(fromModule, line, column, diag.ImportCycle(strings.Join(cycle, " -> ")))
				return nil
			}
			return s.visit(target)
		}
		s.record(fromModule, line, column, diag.UnknownStdlibModule(importDecl.Reference.DisplaySpelling))
		return nil
	default:
	}
	if len(s.sourceKeyFor(target)) == 0 {
		s.record(fromModule, line, column, diag.ImportedModuleNotFound(importDecl.Reference.DisplaySpelling))
		return nil
	}
	if imported[target] {
		s.record(fromModule, line, column, diag.DuplicateImport(target))
		return nil
	}
	imported[target] = true
	if cycle, ok := s.walk.Cycle(target); ok {
		s.record(fromModule, line, column, diag.ImportCycle(strings.Join(cycle, " -> ")))
		return nil
	}
	return s.visit(target)
}

// indexSourceKeys groups every supplied source key by
// canonicalFromLogicalKey with each bucket sorted ascending: the one place
// the canonical-identity scan runs, so callers see a deterministic pick and
// error spelling without rescanning the source map.
func indexSourceKeys(sources map[string]string) map[string][]string {
	index := make(map[string][]string, len(sources))
	for key := range sources {
		canonical := canonicalFromLogicalKey(key)
		index[canonical] = append(index[canonical], key)
	}
	for _, keys := range index {
		slices.Sort(keys)
	}
	return index
}

// sourceKeyFor returns every source key that canonicalizes to id, sorted
// ascending for a deterministic pick and error spelling.
func (s *reachState) sourceKeyFor(id string) []string {
	return s.keysByCanonical[id]
}

// stdlibSourcesByCanonical rekeys the embedded source stdlib modules by
// canonical id ("std/ascii") for graph resolution. Each compilation gets a
// fresh copy, so no walk can mutate a later compilation's stdlib.
func stdlibSourcesByCanonical() map[string]string {
	sources := stdlib.Sources()
	byCanonical := make(map[string]string, len(sources))
	for key, text := range sources {
		byCanonical[strings.TrimSuffix(strings.TrimPrefix(key, "stdlib/"), ".hex")] = text
	}
	return byCanonical
}

// sourceTable builds the compilation's one source table from every logical
// source it can render a location for: each supplied source key, which already
// includes the prepared C bindings the driver inserted under their reserved
// keys, and every embedded source stdlib module under the same logical key the
// resolver lexes it from. A span's File is one of these keys, never a host
// path, so the table resolves a carried span without filesystem access.
func sourceTable(sources map[string]string) *span.Table {
	table := span.NewTable()
	for key, text := range sources {
		table.Add(key, text)
	}
	for canonical, text := range stdlibSourcesByCanonical() {
		table.Add(stdlib.SourceKey(canonical), text)
	}
	return table
}

// sourceFor returns the source key and text of one canonical module: an
// embedded source stdlib module when canonical reserves the std collection,
// otherwise the unique user source key that canonicalizes to it.
func (s *reachState) sourceFor(canonical string) (string, string, bool) {
	if text, ok := s.stdlibSources[canonical]; ok {
		if userKeys := s.sourceKeyFor(canonical); len(userKeys) > 0 {
			// A user key cannot shadow a stdlib canonical identity even
			// though the std module wins resolution.
			s.record(canonical, 1, 1, diag.InvalidLogicalKey(userKeys[0], diag.LogicalKeyReservedStd))
		}
		return stdlib.SourceKey(canonical), text, true
	}
	keys := s.sourceKeyFor(canonical)
	if len(keys) == 0 {
		return "", "", false
	}
	if len(keys) > 1 {
		s.record(canonical, 1, 1, diag.DuplicateSourceKeys(keys[0], keys[1], canonical))
	}
	return keys[0], s.sources[keys[0]], true
}

// record appends one resolution diagnostic to its module's bucket, with the
// position normalized to 1-based source coordinates. It is recordCategory
// with the ModuleError category every ordinary resolution failure carries.
func (s *reachState) record(moduleID string, line, column int, message diag.Message) {
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	logicalKey := ""
	if node, ok := s.nodes[moduleID]; ok {
		logicalKey = node.LogicalKey
	} else if keys := s.sourceKeyFor(moduleID); len(keys) > 0 {
		logicalKey = keys[0]
	}
	s.byModule[moduleID] = append(s.byModule[moduleID], compilerTypes.At(
		message, span.Span{}, span.Position{Line: line, Column: column}).InModule(logicalKey))
}

// mergeDiagnostics folds every stage error into one sorted diagnostic set. It
// traverses wrappers with errors.As rather than asserting concrete types, so a
// wrapped diagnostic sorts by its own position instead of degrading to an
// Unknown Error at 0:0.
func mergeDiagnostics(stageErrors ...error) error {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, err := range stageErrors {
		if err == nil {
			continue
		}
		var many compilerTypes.Diagnostics
		if errors.As(err, &many) {
			diagnostics = append(diagnostics, many...)
			continue
		}
		var one compilerTypes.Diagnostic
		if errors.As(err, &one) {
			diagnostics = append(diagnostics, one)
			continue
		}
		diagnostics = append(diagnostics, compilerTypes.Locationless(diag.UnknownCompiler()))
	}
	if len(diagnostics) == 0 {
		return nil
	}
	// Position only: every caller passes one module's diagnostics, and each
	// stage already emits modules in dependency order. Sorting on the module
	// here would reorder them alphabetically for no gain.
	slices.SortStableFunc(diagnostics, compilerTypes.CompareDiagnostic)
	return diagnostics
}

func failureResult(err error, stats CompilationStats, compileStarted time.Time) CompilationResult {
	finalizeStats(&stats, compileStarted)
	// A failed source program has no valid generated project: the result
	// carries the failure status itself, so no failure C program or partial
	// module artifact is emitted and Files stays non-nil and empty.
	return CompilationResult{
		Files:             map[string]string{},
		Dependencies:      []RuntimeDependency{},
		Stderr:            compilerTypes.ErrorMessages(err),
		ExitCode:          ExitFailure,
		HasCompilerDefect: compilerTypes.HasUnknownError(err),
		Stats:             stats,
	}
}

func finalizeStats(stats *CompilationStats, compileStarted time.Time) {
	stats.PhaseSubtotal = stats.LexDuration +
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

// stampModule attributes a stage error to one module's logical source key.
// Lexing and parsing report positions without knowing which module they were
// handed; the reachability walk is the only pass that knows both.
func stampModule(err error, logicalKey string) error {
	return compilerTypes.StampModule(err, logicalKey)
}
