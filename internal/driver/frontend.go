package driver

// RFC 0193: automatic C header binding generation. The driver asks a
// separately installed, version-qualified Clang frontend for a header's typed
// JSON AST and normalizes the supported declarations into RFC 0039 source. The
// core compiler is untouched: it consumes prepared source strings and never
// reads a header, starts a process, or parses C.
//
// Inspection is a two-command pipeline. The pinned Zig backend preprocesses a
// one-line staging translation unit under the selected target, ordered include
// roots, definitions, and effective environment, retaining line markers. The
// standalone Clang frontend then parses that preprocessed text in C23 mode
// under the same target and emits the typed JSON AST; it receives no include
// roots or definitions because preprocessing already consumed them.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hexal/compiler"
	"hexal/internal/backend"
)

// The initial inspection budget. These are conservative first-version bounds,
// not language limits; changing them requires measured importer evidence.
const (
	inspectionByteLimit = 64 << 20
	inspectionTimeout   = 30 * time.Second
	clangMinimumMajor   = 18
)

// clangFrontend is one version-qualified standalone Clang executable.
type clangFrontend struct {
	Exe     string
	Version string
	Major   int
}

var clangVersionPattern = regexp.MustCompile(`clang version (\d+)\.`)

// resolveClang locates a supported standalone Clang on PATH. It is a
// Configuration Error before staging when Clang is absent or older than 18.
func resolveClang() (clangFrontend, *BuildError) {
	exe, err := exec.LookPath("clang")
	if err != nil {
		return clangFrontend{}, configurationFailure("automatic C imports require clang 18 or newer on PATH")
	}
	command := exec.Command(exe, "--version")
	var stdout bytes.Buffer
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		return clangFrontend{}, configurationFailure("automatic C imports require clang 18 or newer on PATH")
	}
	match := clangVersionPattern.FindStringSubmatch(stdout.String())
	if match == nil {
		return clangFrontend{}, configurationFailure("automatic C imports require clang 18 or newer on PATH")
	}
	major, err := strconv.Atoi(match[1])
	if err != nil || major < clangMinimumMajor {
		return clangFrontend{}, configurationFailure("automatic C imports require clang 18 or newer on PATH")
	}
	return clangFrontend{Exe: exe, Version: strings.TrimSpace(stdout.String()), Major: major}, nil
}

// preparedBinding is one normalized binding module: its reserved logical key
// and its RFC 0039 source.
type preparedBinding struct {
	Key    string
	Header string
	System bool
	Source string
}

// inspectRequest prepares one C-header request: it preprocesses the requested
// include and normalizes the frontend's typed AST into RFC 0039 source. The
// whole inspection shares one 30-second deadline and one 64 MiB bound on each
// command's output.
func inspectRequest(selected *backend.Backend, clang clangFrontend, staging string, request compiler.CImportRequest, options headerOptions, result *BuildResult) (preparedBinding, *BuildError) {
	ctx, cancel := context.WithTimeout(context.Background(), inspectionTimeout)
	defer cancel()

	inspectDir := filepath.Join(staging, "hexalc")
	if err := os.MkdirAll(inspectDir, 0o755); err != nil {
		return preparedBinding{}, filesystemFailure(fmt.Sprintf("cannot create header inspection directory: %v", err))
	}
	// The staging translation unit's only semantic content is the requested
	// include. The quoted and system forms select the include spelling.
	include := "#include \"" + request.Header + "\"\n"
	if request.System {
		include = "#include <" + request.Header + ">\n"
	}
	stagingSource := filepath.Join(inspectDir, "inspect.c")
	if err := os.WriteFile(stagingSource, []byte(include), 0o644); err != nil {
		return preparedBinding{}, filesystemFailure(fmt.Sprintf("cannot write header inspection source: %v", err))
	}
	preprocessed := filepath.Join(inspectDir, "inspect.i")

	// Zig preprocesses with the target-relevant interface configuration. It is
	// the only stage that consumes the include roots and definitions.
	preprocessArgs := []string{"cc", "-std=c23", "-target", qualifiedTriple}
	preprocessArgs = append(preprocessArgs, options.compileOptions...)
	preprocessArgs = append(preprocessArgs, "-E", stagingSource, "-o", preprocessed)
	preprocess, failure := runBounded(selected, staging, ctx, result, StageCompile, preprocessArgs)
	if failure != nil {
		return preparedBinding{}, failure
	}
	if preprocess.ExitCode != 0 {
		return preparedBinding{}, &BuildError{Stage: StageCompile, Message: "C header preprocessing failed; a header C23 rejects needs a compatibility wrapper header", Command: lastCommand(result)}
	}

	// Clang parses the preprocessed text: no include roots or definitions, and
	// the same target and C23 mode.
	clangArgs := []string{"-target", qualifiedTriple, "-x", "c", "-std=c23", "-Xclang", "-ast-dump=json", "-fsyntax-only", preprocessed}
	ast, failure := runExternalBounded(clang.Exe, staging, selected.Environment, selected.EnvironmentOverrides, ctx, result, StageCompile, clangArgs)
	if failure != nil {
		return preparedBinding{}, failure
	}
	if ast.ExitCode != 0 {
		return preparedBinding{}, &BuildError{Stage: StageCompile, Message: "C header frontend inspection failed; a header C23 rejects needs a compatibility wrapper header", Command: lastCommand(result)}
	}
	preprocessedText, err := os.ReadFile(preprocessed)
	if err != nil {
		return preparedBinding{}, filesystemFailure(fmt.Sprintf("cannot read preprocessed header text: %v", err))
	}

	source, failure := normalizeHeader(ast.Stdout, newLineIndex(string(preprocessedText)), request, options)
	if failure != nil {
		return preparedBinding{}, failure
	}
	return preparedBinding{
		Key:    compiler.CBindingKey(qualifiedTriple, request),
		Header: request.Header,
		System: request.System,
		Source: source,
	}, nil
}

// headerOptions carries the interface configuration every interface consumer
// shares: the ordered include roots and definitions rendered as backend
// arguments.
type headerOptions struct {
	compileOptions []string
}

// freshInspectionDir creates one private directory for transient header
// inspection artifacts under the intermediate root. It is removed by the
// caller once preparation completes or fails.
func freshInspectionDir(outDir string) (string, error) {
	base := stagingPath("", outDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("cannot create intermediate directory %q: %v", base, err)
	}
	dir, err := os.MkdirTemp(base, "hexalc-*")
	if err != nil {
		return "", fmt.Errorf("cannot create header inspection directory under %q: %v", base, err)
	}
	return dir, nil
}

// lineMarker is one preprocessor line marker: at indexLine of the preprocessed
// text, the original file resumes at line.
type lineMarker struct {
	indexLine int
	file      string
	line      int
}

// lineIndex maps a preprocessed line back to its physical file and line. Clang
// elides a repeated file name from the JSON AST, so the driver recovers origin
// from the line markers the preprocessor retained.
type lineIndex struct {
	markers []lineMarker
}

// newLineIndex parses every `# <line> "<file>"` marker in the preprocessed
// text, in order.
func newLineIndex(text string) *lineIndex {
	index := &lineIndex{}
	indexLine := 0
	for _, raw := range strings.Split(text, "\n") {
		indexLine++
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			continue
		}
		line, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		file := strings.Trim(fields[2], "\"")
		index.markers = append(index.markers, lineMarker{indexLine: indexLine + 1, file: file, line: line})
	}
	return index
}

// origin resolves one preprocessed line to its physical file and line. It
// returns an empty file for a line before any marker.
func (index *lineIndex) origin(indexLine int) (string, int) {
	file := ""
	line := 0
	found := false
	for _, marker := range index.markers {
		if marker.indexLine > indexLine {
			break
		}
		file = marker.file
		line = marker.line + (indexLine - marker.indexLine)
		found = true
	}
	if !found {
		return "", 0
	}
	return file, line
}

// runBounded runs one backend command under the inspection context, bounded to
// the byte limit, and records it. It reuses the backend executable, directory,
// and environment, but adds a deadline and an output bound the ordinary
// build path does not need.
func runBounded(selected *backend.Backend, staging string, ctx context.Context, result *BuildResult, stage BuildStage, args []string) (backend.Result, *BuildError) {
	return runExternalBounded(selected.Exe, staging, selected.Environment, selected.EnvironmentOverrides, ctx, result, stage, args)
}

// runExternalBounded runs one external command with separated bounded streams,
// records it, and returns its result. Exceeding the byte bound terminates the
// command and fails the stage.
func runExternalBounded(exe, directory string, environment, overrides []string, ctx context.Context, result *BuildResult, stage BuildStage, args []string) (backend.Result, *BuildError) {
	command := exec.CommandContext(ctx, exe, args...)
	command.Dir = directory
	command.Env = environment
	stdout := newBoundedBuffer(inspectionByteLimit)
	stderr := newBoundedBuffer(inspectionByteLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	invocation := backend.Result{
		Args:   append([]string{exe}, args...),
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if ctx.Err() != nil {
		// The shared inspection deadline elapsed; partial output is discarded
		// and the exact budget diagnostic is reported, with the command
		// recorded for attribution.
		result.Commands = append(result.Commands, CommandResult{
			Stage:                stage,
			Tool:                 exe,
			Arguments:            invocation.Args,
			WorkingDirectory:     directory,
			Stdout:               invocation.Stdout,
			Stderr:               invocation.Stderr,
			ExitCode:             invocation.ExitCode,
			EnvironmentOverrides: append([]string(nil), overrides...),
		})
		return invocation, &BuildError{Stage: stage, Message: inspectionBudgetMessage, Command: lastCommand(result)}
	}
	if exit, ok := runErr.(*exec.ExitError); ok {
		invocation.ExitCode = exit.ExitCode()
	} else if runErr != nil {
		return invocation, &BuildError{Stage: stage, Message: fmt.Sprintf("cannot run %s: %v", filepath.Base(exe), runErr)}
	}
	result.Commands = append(result.Commands, CommandResult{
		Stage:                stage,
		Tool:                 exe,
		Arguments:            invocation.Args,
		WorkingDirectory:     directory,
		Stdout:               invocation.Stdout,
		Stderr:               invocation.Stderr,
		ExitCode:             invocation.ExitCode,
		EnvironmentOverrides: append([]string(nil), overrides...),
	})
	if stdout.overflow || stderr.overflow {
		return invocation, &BuildError{Stage: stage, Message: inspectionBudgetMessage, Command: lastCommand(result)}
	}
	return invocation, nil
}

// inspectionBudgetMessage is the exact first-version budget diagnostic.
const inspectionBudgetMessage = "C header inspection exceeded the initial automatic-binding budget; use a smaller wrapper header or a handwritten binding"

// lastCommand returns the most recently recorded command, which the caller
// has just appended.
func lastCommand(result *BuildResult) *CommandResult {
	if len(result.Commands) == 0 {
		return nil
	}
	return &result.Commands[len(result.Commands)-1]
}

// boundedBuffer is a bytes.Buffer that stops accepting input past limit and
// records that it overflowed, so a runaway frontend cannot exhaust memory.
type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (bounded *boundedBuffer) Write(p []byte) (int, error) {
	remaining := bounded.limit - bounded.buffer.Len()
	if remaining <= 0 {
		bounded.overflow = true
		return len(p), nil
	}
	if len(p) > remaining {
		bounded.buffer.Write(p[:remaining])
		bounded.overflow = true
		return len(p), nil
	}
	return bounded.buffer.Write(p)
}

func (bounded *boundedBuffer) String() string {
	return bounded.buffer.String()
}
