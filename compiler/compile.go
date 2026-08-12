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
func Compile(source string) CompilationResult {
	compileStarted := time.Now()
	stats := CompilationStats{SourceLines: sourceLineCount(source)}

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
	return CompilationResult{
		MainC:    mainC,
		MainH:    mainH,
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
	return CompilationResult{
		MainC:    mainC,
		MainH:    mainH,
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
