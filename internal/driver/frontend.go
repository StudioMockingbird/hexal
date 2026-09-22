package driver

// Automatic C header binding generation. The driver asks the selected,
// version-qualified Clang for a header's typed JSON AST and normalizes the
// supported declarations into Hexal source. The core compiler is untouched: it
// consumes prepared source strings and never reads a header, starts a process,
// or parses C.
//
// Inspection is a two-command pipeline over the one selected Clang executable.
// Clang preprocesses a one-line staging translation unit under the selected
// target, ordered include roots, definitions, and effective environment,
// retaining line markers. The same executable then parses that preprocessed
// text in C23 mode under the same target and emits the typed JSON AST; it
// receives no include roots or definitions because preprocessing already
// consumed them.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"hexal/compiler"
	compilerConfig "hexal/compiler/config"
	"hexal/internal/backend"
)

// clangMinimumMajor is the oldest Clang major the importer accepts. It is a
// toolchain floor, not a language limit: changing it requires measured
// importer evidence.
const clangMinimumMajor = 18

// preparedBinding is one normalized binding module: its reserved logical key
// and its normalized Hexal source.
type preparedBinding struct {
	Key    string
	Header string
	System bool
	Source string
}

// inspectRequest prepares one C-header request: it preprocesses the requested
// include and normalizes the selected Clang's typed AST into Hexal source. The
// whole inspection shares one 30-second deadline and one 64 MiB bound on each
// command's output.
func inspectRequest(selected *backend.Backend, staging string, request compiler.CImportRequest, options headerOptions, result *BuildResult) (preparedBinding, *BuildError) {
	ctx, cancel := context.WithTimeout(context.Background(), compilerConfig.ForeignInspectionTimeout)
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

	// Clang preprocesses with the target-relevant interface configuration. It
	// is the only stage that consumes the include roots and definitions.
	preprocessArgs := []string{"--target=" + qualifiedTriple, "-std=c23"}
	preprocessArgs = append(preprocessArgs, linuxFeatureDefines...)
	preprocessArgs = append(preprocessArgs, options.compileOptions...)
	preprocessArgs = append(preprocessArgs, "-E", stagingSource, "-o", preprocessed)
	preprocess, failure := runBounded(selected, staging, ctx, result, StageCompile, preprocessArgs)
	if failure != nil {
		return preparedBinding{}, failure
	}
	if preprocess.ExitCode != 0 {
		return preparedBinding{}, &BuildError{Stage: StageCompile, Message: fmt.Sprintf("C header %s is not accepted as C23; import a compatibility wrapper header", request.Header), Command: lastCommand(result)}
	}

	// The same Clang parses the preprocessed text: no include roots or
	// definitions, and the same target and C23 mode.
	clangArgs := []string{"--target=" + qualifiedTriple, "-x", "c", "-std=c23", "-Xclang", "-ast-dump=json", "-fsyntax-only", preprocessed}
	ast, failure := runExternalBounded(selected.Exe, staging, selected.Environment, selected.EnvironmentOverrides, ctx, result, StageCompile, clangArgs)
	if failure != nil {
		return preparedBinding{}, failure
	}
	if ast.ExitCode != 0 {
		return preparedBinding{}, &BuildError{Stage: StageCompile, Message: fmt.Sprintf("C header %s is not accepted as C23; import a compatibility wrapper header", request.Header), Command: lastCommand(result)}
	}
	preprocessedText, err := os.ReadFile(preprocessed)
	if err != nil {
		return preparedBinding{}, filesystemFailure(fmt.Sprintf("cannot read preprocessed header text: %v", err))
	}

	// Clang's own preprocessor output supplies the object-like macro
	// inventory. Candidates are then type-checked under the same header,
	// target, includes, definitions, and environment; only those Clang proves
	// are value expressions are exposed, and the generated C still names the
	// macro, so Clang performs the real expansion and constant evaluation.
	macroTypes, macroFailure := objectMacroTypes(selected, staging, stagingSource, request, options, ctx, result)
	if macroFailure != nil {
		return preparedBinding{}, macroFailure
	}
	options.macroTypes = macroTypes

	source, failure := normalizeHeader(ast.Stdout, newLineIndex(string(preprocessedText)), request, options)
	if failure != nil {
		return preparedBinding{}, failure
	}
	return preparedBinding{
		Key:    compiler.CBindingKey(options.target, request),
		Header: request.Header,
		System: request.System,
		Source: source,
	}, nil
}

// objectMacroTypes returns the type Clang proved for every object-like macro
// defined in the requested header. It runs Clang's preprocessor with -dD to
// recover the macro definitions, filters to object-like macros whose
// definition originates in the requested header, then asks Clang to type a
// probe expression for each candidate. Macros that are not value expressions
// are simply absent from the result and are omitted by the normalizer.
func objectMacroTypes(selected *backend.Backend, staging, stagingSource string, request compiler.CImportRequest, options headerOptions, ctx context.Context, result *BuildResult) (map[string]string, *BuildError) {
	inventoryArgs := []string{"--target=" + qualifiedTriple, "-std=c23"}
	inventoryArgs = append(inventoryArgs, linuxFeatureDefines...)
	inventoryArgs = append(inventoryArgs, options.compileOptions...)
	inventoryArgs = append(inventoryArgs, "-E", "-dD", stagingSource)
	inventory, failure := runBounded(selected, staging, ctx, result, StageCompile, inventoryArgs)
	if failure != nil {
		return nil, failure
	}
	if inventory.ExitCode != 0 {
		// The preprocessing pass already validated the header; a failure here
		// is not attributable, so no macro is imported and no later stage runs.
		return nil, nil
	}
	macros := objectMacroInventory(inventory.Stdout, request.Header)
	if len(macros) == 0 {
		return nil, nil
	}

	// One batch probe is the common case. If any candidate is not an
	// expression the batch fails to compile, so each candidate is probed
	// alone; Clang's verdict, not a heuristic, decides.
	types, batchFailure := probeMacroBatch(selected, staging, request, options, macros, ctx, result)
	if batchFailure != nil {
		return nil, batchFailure
	}
	if types != nil {
		return types, nil
	}
	isolated := make(map[string]string)
	for _, name := range macros {
		single, singleFailure := probeMacroBatch(selected, staging, request, options, []string{name}, ctx, result)
		if singleFailure != nil {
			return nil, singleFailure
		}
		if qualType, ok := single[name]; ok {
			isolated[name] = qualType
		}
	}
	return isolated, nil
}

// objectMacroInventory lists the object-like macro names defined in the
// requested header, in deterministic order. Function-like macros (a '('
// immediately after the name), empty-bodied macros, builtin origins, and
// underscore-prefixed names are excluded.
func objectMacroInventory(text, header string) []string {
	base := macroHeaderBase(header)
	current := ""
	seen := make(map[string]bool)
	names := make([]string, 0)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "# ") {
			if file := macroMarkerFile(line); file != "" {
				current = file
			}
			continue
		}
		if !strings.HasPrefix(line, "#define") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#define")))
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if strings.Contains(name, "(") || strings.HasPrefix(name, "_") {
			continue
		}
		if base != "" && macroHeaderBase(current) != base {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// macroMarkerFile extracts the file named by one `# <line> "<file>"` marker.
func macroMarkerFile(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	return strings.Trim(fields[2], "\"")
}

// macroHeaderBase returns the final path component of a header spelling.
func macroHeaderBase(path string) string {
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		return path[index+1:]
	}
	return path
}

// probeMacroBatch type-checks one probe declaration per macro in a single
// Clang invocation and returns the proved qualType for each. A batch that
// fails to compile returns (nil, nil): that is Clang saying at least one
// candidate is not a value expression, not a harness error. A nil map and a
// non-nil error is a harness failure.
func probeMacroBatch(selected *backend.Backend, staging string, request compiler.CImportRequest, options headerOptions, macros []string, ctx context.Context, result *BuildResult) (map[string]string, *BuildError) {
	include := "#include \"" + request.Header + "\"\n"
	if request.System {
		include = "#include <" + request.Header + ">\n"
	}
	var source strings.Builder
	source.WriteString(include)
	for index, name := range macros {
		fmt.Fprintf(&source, "__typeof__(%s) hex_macro_probe_%d;\n", name, index)
	}
	path := filepath.Join(staging, "hexalc", "macros.c")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, filesystemFailure(fmt.Sprintf("cannot create macro probe directory: %v", err))
	}
	if err := os.WriteFile(path, []byte(source.String()), 0o644); err != nil {
		return nil, filesystemFailure(fmt.Sprintf("cannot write macro probe source: %v", err))
	}
	args := []string{"--target=" + qualifiedTriple, "-std=c23"}
	args = append(args, linuxFeatureDefines...)
	args = append(args, options.compileOptions...)
	args = append(args, "-Xclang", "-ast-dump=json", "-fsyntax-only", path)
	ast, failure := runExternalBounded(selected.Exe, staging, selected.Environment, selected.EnvironmentOverrides, ctx, result, StageCompile, args)
	if failure != nil {
		return nil, failure
	}
	if ast.ExitCode != 0 {
		return nil, nil
	}
	var document struct {
		Inner []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
			Type struct {
				QualType          string `json:"qualType"`
				DesugaredQualType string `json:"desugaredQualType"`
			} `json:"type"`
		} `json:"inner"`
	}
	if err := json.Unmarshal([]byte(ast.Stdout), &document); err != nil {
		return nil, nil
	}
	types := make(map[string]string, len(macros))
	for _, node := range document.Inner {
		if node.Kind != "VarDecl" {
			continue
		}
		// A __typeof__ declaration prints the typeof sugar in qualType; the
		// desugared spelling is the type the macro expression actually has.
		qualType := node.Type.DesugaredQualType
		if qualType == "" {
			qualType = node.Type.QualType
		}
		if qualType == "" {
			continue
		}
		for index, name := range macros {
			if node.Name == fmt.Sprintf("hex_macro_probe_%d", index) {
				types[name] = qualType
			}
		}
	}
	return types, nil
}

// headerOptions carries the interface configuration every interface consumer
// shares: the ordered include roots and definitions rendered as backend
// arguments, the Hexal target profile identity the prepared binding key is
// derived from, and the object-like macro types Clang proved (name to C
// qualType) for the requested header.
type headerOptions struct {
	compileOptions []string
	target         string
	macroTypes     map[string]string
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
	stdout := newBoundedBuffer(compilerConfig.ForeignInspectionByteLimit)
	stderr := newBoundedBuffer(compilerConfig.ForeignInspectionByteLimit)
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
