package compiler

import (
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
	MainC    string
	MainH    string
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
// must name exactly one entry in sources. The compiler is exclusively an
// in-memory string transformation: it performs no filesystem reads, writes,
// discovery, or working-directory lookup.
func Compile(sources map[string]string, entrypoint string) CompilationResult {
	compileStarted := time.Now()
	stats := CompilationStats{}

	source, ok := sources[entrypoint]
	if !ok {
		err := compilerTypes.NewDiagnostic(compilerTypes.ModuleError, "compile", 1, 1,
			"entrypoint "+entrypoint+" was not found in the supplied sources")
		return failureResult(err, stats, compileStarted)
	}

	stats.SourceLines = sourceLineCount(source)

	started := time.Now()
	tokens, err := lexer.Lex(source)
	stats.LexDuration = time.Since(started)
	stats.TokenCount = len(tokens)
	if err != nil {
		return failureResult(err, stats, compileStarted)
	}

	started = time.Now()
	syntax, err := parser.Parse(tokens)
	stats.ParseDuration = time.Since(started)

	started = time.Now()
	checked, checkErr := checker.Check(syntax)
	stats.CheckDuration = time.Since(started)
	if diagnostics := mergeDiagnostics(err, checkErr); diagnostics != nil {
		return failureResult(diagnostics, stats, compileStarted)
	}

	started = time.Now()
	mainC, mainH, generateErr := generator.GenerateChecked(checked)
	stats.GenerateDuration = time.Since(started)
	if generateErr != nil {
		return failureResult(generateErr, stats, compileStarted)
	}
	finalizeStats(&stats, compileStarted)
	files := map[string]string{"main.c": mainC, "main.h": mainH}
	return CompilationResult{
		MainC:    mainC,
		MainH:    mainH,
		Files:    files,
		ExitCode: ExitSuccess,
		Stats:    stats,
	}
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
