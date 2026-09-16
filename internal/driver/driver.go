// Package driver connects the in-memory compiler to the filesystem and to a C
// toolchain. ADR 0055 owns its contract: the compiler stays string-in/
// string-out and process-free, and everything that touches a path or spawns a
// process lives here. The package is internal: it is the CLI's implementation,
// not a supported embedding API.
package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/backend"
	"hexal/internal/version"
)

// BuildStage distinguishes the pipeline stages a failure is attributed to.
// Configuration, filesystem, Hexal-compilation, C-compilation, and linking
// stay separate; compiler diagnostics pass through unchanged.
type BuildStage string

// The concrete build stages in pipeline order.
const (
	StageConfiguration BuildStage = "configuration"
	StageFilesystem    BuildStage = "filesystem"
	StageHexal         BuildStage = "hexal-compilation"
	StageCompile       BuildStage = "c-compilation"
	StageLink          BuildStage = "link"
)

// CommandResult records one completed external invocation with separated
// streams and the exact argument vector, so a failing build is reproducible
// from its record. The complete process environment is deliberately not
// captured: EnvironmentOverrides names the explicit `-c-env` overrides the
// invocation ran under, never their values.
type CommandResult struct {
	Stage            BuildStage
	Tool             string
	Arguments        []string
	WorkingDirectory string
	Stdout           string
	Stderr           string
	ExitCode         int
	// EnvironmentOverrides lists the names of the explicit environment
	// overrides applied to this invocation, in command-line occurrence order.
	// Values are never recorded.
	EnvironmentOverrides []string
}

// BuildError is one failed build: the earliest failing stage, a message,
// and the external command record when an invoked command failed. A Hexal
// compilation failure carries no command; the compiler's rendered
// diagnostics are printed verbatim by the build itself.
type BuildError struct {
	Stage   BuildStage
	Message string
	Command *CommandResult
}

func (err *BuildError) Error() string {
	if err == nil {
		return "unknown build failure"
	}
	if err.Command != nil {
		return fmt.Sprintf("%s: %s (exit %d)", err.Stage, err.Message, err.Command.ExitCode)
	}
	return fmt.Sprintf("%s: %s", err.Stage, err.Message)
}

// configurationFailure reports an invalid, duplicate, or unsafe command-line
// or environment configuration.
func configurationFailure(message string) *BuildError {
	return &BuildError{Stage: StageConfiguration, Message: message}
}

// filesystemFailure reports a missing, unreadable, or wrong-kind input path.
func filesystemFailure(message string) *BuildError {
	return &BuildError{Stage: StageFilesystem, Message: message}
}

// BuildResult is one completed build: the published executable, the Hexal
// version recorded once at project level, and the command record for every
// external invocation. A failed build returns its populated result with
// every command completed before failure. Command records never repeat the
// version.
type BuildResult struct {
	Executable   string
	HexalVersion string
	Commands     []CommandResult
}

// BuildOptions carries what ADR 0055's Configuration section resolves from
// flags and conventions. There is no project manifest in v1. The `C*`,
// `Objects`, and `SystemLibraries` fields are RFC 0192's explicit foreign
// build inputs; they are driver configuration and never enter
// compiler.Project or compiler.Compile.
type BuildOptions struct {
	Root       string    // source root; defaults to the working directory
	Entrypoint string    // logical key; defaults to main.hex
	OutDir     string    // intermediate root; defaults to <root>/build
	Output     string    // executable path; defaults to <outdir>/<entrypoint>.exe
	Mode       BuildMode // backend option set; defaults to debug
	// CSources are foreign C translation units compiled separately and linked.
	CSources []string
	// CIncludeDirs are header search directories added to generated module and
	// foreign C compilations.
	CIncludeDirs []string
	// CDefines are `name[=value]` preprocessor definitions applied to
	// generated module and foreign C compilations.
	CDefines []string
	// CEnvironment are `NAME=VALUE` environment overrides applied to every
	// external frontend, compiler, and linker invocation.
	CEnvironment []string
	// CStandard selects the dialect for every foreign C source; empty selects
	// c17.
	CStandard string
	// Objects are precompiled object files to link in occurrence order.
	Objects []string
	// Archives are static archives to link in occurrence order.
	Archives []string
	// SystemLibraries are target system libraries linked by logical name in
	// occurrence order.
	SystemLibraries []string
}

// hexalFailureMessage renders the Hexal-stage failure. Ordinary diagnostics
// stay stable and concise; only a compiler-defect Unknown Error carries the
// toolchain version, so a bug report is attributable without touching every
// user-facing message.
func hexalFailureMessage(diagnostics []string) string {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic, "[Unknown Error]") {
			return fmt.Sprintf("compilation failed (Hexal %s)", version.String())
		}
	}
	return "compilation failed"
}

// qualifiedTriple is the one target RFC 0052 qualifies, named explicitly
// rather than relying on the backend default.
const qualifiedTriple = string(compilerTypes.TargetX86_64WindowsGNU)

// stagingBaseName is the intermediate directory under the output root. Only
// this exact directory is skipped during source discovery; a different
// source directory named build remains valid.
const stagingBaseName = ".hexal"

// Build runs the whole pipeline: configuration, discovery, Hexal
// compilation, per-translation-unit C compilation, linking, and atomic
// publication. It returns the populated result with a *BuildError naming
// the earliest failing stage.
func Build(options BuildOptions) (BuildResult, error) {
	var result BuildResult
	result.HexalVersion = version.String()
	root := options.Root
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return result, &BuildError{Stage: StageConfiguration, Message: fmt.Sprintf("cannot resolve source root: %v", err)}
	}
	entrypoint := options.Entrypoint
	if entrypoint == "" {
		entrypoint = "main.hex"
	}
	mode, err := resolveMode(options.Mode)
	if err != nil {
		return result, configurationFailure(err.Error())
	}

	// Foreign build configuration is validated before any external command:
	// its environment, dialect, definitions, and library names first, then its
	// rooted paths.
	foreign, foreignFailure := configureForeign(options)
	if foreignFailure != nil {
		return result, foreignFailure
	}
	if foreignFailure := foreign.resolveForeignRootedPaths(root, options); foreignFailure != nil {
		return result, foreignFailure
	}

	if err := checkHost(); err != nil {
		return result, configurationFailure(err.Error())
	}
	backend, err := resolveBackend()
	if err != nil {
		return result, configurationFailure(err.Error())
	}
	// Every generated-C, foreign-C, and link invocation runs under the one
	// normalized effective environment; identity discovery above already ran
	// under the inherited environment.
	backend.Environment = foreign.environment

	sources, err := discover(root, stagingPath(root, options.OutDir))
	if err != nil {
		return result, &BuildError{Stage: StageFilesystem, Message: err.Error()}
	}
	if _, ok := sources[entrypoint]; !ok {
		return result, &BuildError{Stage: StageConfiguration, Message: fmt.Sprintf("entrypoint %q not found under %s", entrypoint, root)}
	}

	outDir := options.OutDir
	if outDir == "" {
		outDir = filepath.Join(root, "build")
	}

	// RFC 0193: prepare one binding source for every reachable C-header
	// import, then compile the copied source map. The original map is never
	// mutated and no binding file is written.
	compiledSources := sources
	requests, discoverErr := compiler.DiscoverCImports(sources, entrypoint)
	if discoverErr != nil {
		return result, &BuildError{Stage: StageHexal, Message: "C import discovery failed"}
	}
	if len(requests) > 0 {
		clang, clangFailure := resolveClang()
		if clangFailure != nil {
			return result, clangFailure
		}
		inspection, inspectionErr := freshInspectionDir(outDir)
		if inspectionErr != nil {
			return result, filesystemFailure(inspectionErr.Error())
		}
		defer os.RemoveAll(inspection)
		compiledSources = make(map[string]string, len(sources)+len(requests))
		for key, content := range sources {
			compiledSources[key] = content
		}
		options := headerOptions{compileOptions: foreign.moduleCompileOptions()}
		prepared := make(map[string]bool, len(requests))
		for _, request := range requests {
			key := compiler.CBindingKey(qualifiedTriple, request)
			if prepared[key] {
				// Equal target/header-form/header-payload requests prepare one
				// binding.
				continue
			}
			binding, bindingFailure := inspectRequest(backend, clang, inspection, request, options, &result)
			if bindingFailure != nil {
				return result, bindingFailure
			}
			prepared[binding.Key] = true
			compiledSources[binding.Key] = binding.Source
		}
	}

	compileResult := compiler.Compile(compiledSources, entrypoint, compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if len(compileResult.Stderr) > 0 {
		// Diagnostics are the compiler's output, not the driver's. Print them
		// verbatim and fail without adding a wrapping line.
		for _, diagnostic := range compileResult.Stderr {
			fmt.Fprintln(os.Stderr, diagnostic)
		}
		return result, &BuildError{Stage: StageHexal, Message: hexalFailureMessage(compileResult.Stderr)}
	}

	// The mode selects backend options here and nowhere else; the dependency
	// options added further below are mode-independent by design.
	selected := Options(mode)

	// The identity is derived before anything touches the filesystem, because
	// it names the staging tree: every object path and the linked executable
	// path end up inside the debug information, so they must be functions of
	// the build's inputs rather than of a random directory name. A build with
	// foreign inputs is the exception: the content-derived identity omits
	// foreign file bytes, options, headers, and the effective environment, so
	// it must never be reused as a cache key. Such a build receives a fresh
	// private staging tree whose random token also names its published debug
	// information, so two foreign builds can never claim one identity.
	var identity string
	var staging string
	if foreign.active {
		staging, err = freshStagingDir(outDir)
		if err != nil {
			return result, filesystemFailure(err.Error())
		}
		identity = strings.TrimPrefix(filepath.Base(staging), "build-")
	} else {
		identity = buildIdentity(mode, backendIdentity(backend), compileResult.Files, compileResult.Dependencies, selected.Compile, selected.Link)
		staging, err = identityStagingDir(outDir, identity)
		if err != nil {
			return result, filesystemFailure(err.Error())
		}
	}
	// The staging tree is disposable: every build removes its own tree once
	// publication completes or fails, so repeated builds cannot accumulate
	// stale trees beside the executable.
	defer os.RemoveAll(staging)
	// The backend records its own working directory in the debug information
	// it emits, so it is pinned to the identity-named tree for the same reason
	// that tree is named after the identity at all.
	backend.Directory = staging

	cFiles, err := materialize(staging, compileResult.Files)
	if err != nil {
		return result, &BuildError{Stage: StageFilesystem, Message: err.Error()}
	}
	native, err := materializeDependencies(staging, compileResult.Dependencies)
	if err != nil {
		return result, &BuildError{Stage: StageFilesystem, Message: err.Error()}
	}

	output := options.Output
	if output == "" {
		name := strings.TrimSuffix(filepath.Base(entrypoint), ".hex")
		output = filepath.Join(outDir, name+exeSuffix())
	}
	// Resolved once here: the debug information published beside the
	// executable is named from this directory, so a relative -out must not
	// reach that decision unresolved.
	output, err = filepath.Abs(output)
	if err != nil {
		return result, &BuildError{Stage: StageFilesystem, Message: fmt.Sprintf("cannot resolve output path %q: %v", options.Output, err)}
	}
	if err := validateOutputDestination(output); err != nil {
		return result, &BuildError{Stage: StageFilesystem, Message: err.Error()}
	}

	backend.EnvironmentOverrides = foreign.overrideNames

	compileOptions := append(selected.Compile, native.compileOptions...)
	linkOptions := append(selected.Link, native.linkOptions...)

	if err := compileNativeDependencies(backend, staging, native, &result); err != nil {
		return result, err
	}
	if err := compileTranslationUnitsWithOptions(backend, staging, cFiles, compileOptions, foreign.moduleCompileOptions(), &result); err != nil {
		return result, err
	}
	foreignObjects, foreignFailure := compileForeignSources(backend, staging, foreign, mode, &result)
	if foreignFailure != nil {
		return result, foreignFailure
	}
	// The five link groups in order: generated Hexal objects with their
	// runtime dependencies in their existing deterministic order, compiled
	// foreign-source objects, precompiled objects, static archives, then named
	// system libraries as linker options.
	objects := append(cFilesToObjects(staging, cFiles), native.linkObjects...)
	objects = append(objects, foreignObjects...)
	objects = append(objects, foreign.Objects...)
	objects = append(objects, foreign.Archives...)
	linkOptions = append(linkOptions, foreign.systemLibraryOptions()...)

	// The executable is linked in staging under a basename carrying the build
	// identity, so the backend names its debug information after that exact
	// identity and the published executable records that name internally. A
	// failed link or a failed publication leaves the previously published
	// executable and every published debug file untouched; the staging tree,
	// including this link, is removed either way.
	stagedExe := filepath.Join(staging, versionedBasename(output, identity)+exeSuffix())
	if err := linkObjectsWithOptions(backend, staging, objects, linkOptions, stagedExe, &result); err != nil {
		return result, err
	}
	if mode == ModeDebug {
		stagedPDB := strings.TrimSuffix(stagedExe, exeSuffix()) + ".pdb"
		if _, statErr := os.Stat(stagedPDB); statErr == nil {
			publishedPDB := filepath.Join(filepath.Dir(output), filepath.Base(stagedPDB))
			if err := publishVersionedPDB(stagedPDB, publishedPDB); err != nil {
				return result, &BuildError{Stage: StageFilesystem, Message: err.Error()}
			}
		}
	}
	if err := publishExecutable(stagedExe, output); err != nil {
		return result, &BuildError{Stage: StageFilesystem, Message: err.Error()}
	}
	result.Executable = output
	return result, nil
}

// checkHost rejects every host outside the qualified v1 scope before any
// tool runs. Cross-compilation is out of v1, so the host running the build
// is the only host a build may target.
func checkHost() error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("host %s/%s is not qualified; v1 builds x86-64 Windows", runtime.GOOS, runtime.GOARCH)
	}
	return nil
}

// resolveBackend locates the installed backend through PATH: the first
// zig on the search path. It never downloads during a build. The pin is
// enforced by exact version match; a newer version does not satisfy it.
func resolveBackend() (*backend.Backend, error) {
	exe, err := exec.LookPath("zig")
	if err != nil {
		return nil, fmt.Errorf("zig not found on PATH; install Zig %s (see the backend lock record) and retry", backend.PinnedZigWindows().Version)
	}
	selected, err := backend.NewBackend(exe)
	if err != nil {
		return nil, fmt.Errorf("backend %q is unusable: %v", exe, err)
	}
	if err := selected.Validate(); err != nil {
		return nil, fmt.Errorf("backend invalid: %v", err)
	}
	pinned := backend.PinnedZigWindows().Version
	if err := selected.CheckPinned(pinned); err != nil {
		return nil, err
	}
	return selected, nil
}

// stagingPath resolves the intermediate directory discovery skips: exactly
// this directory, not every directory that happens to share a name.
func stagingPath(root, outDir string) string {
	if outDir == "" {
		outDir = filepath.Join(root, "build")
	}
	absolute, err := filepath.Abs(filepath.Join(outDir, stagingBaseName))
	if err != nil {
		return filepath.Join(outDir, stagingBaseName)
	}
	return absolute
}

// freshStagingDir creates one fresh per-build staging directory under the
// intermediate root.
func freshStagingDir(outDir string) (string, error) {
	base := stagingPath("", outDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("cannot create intermediate directory %q: %v", base, err)
	}
	staging, err := os.MkdirTemp(base, "build-*")
	if err != nil {
		return "", fmt.Errorf("cannot create staging directory under %q: %v", base, err)
	}
	return staging, nil
}

// discover walks the source root and maps each .hex file to a normalized
// logical key: relative and "/"-separated. Keys are the one contract the
// driver owes the compiler; the compiler parses imports, resolves them, and
// determines reachability, so discovery never implements a second import
// resolver. A source entry that is a symlink or a junction is rejected:
// following one would compile code from outside the rooted tree. Directory
// symlinks are not descended into; the exact staging directory is skipped.
func discover(root, staging string) (map[string]string, error) {
	sources := map[string]string{}
	folded := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("source %q is a symlink; links are not followed", path)
			}
			// Every entry is probed, not just directories: the walker
			// reports a junction as a non-directory without the symlink
			// bit, so gating on IsDir would let one through silently.
			if isJunction(path) {
				return fmt.Errorf("source %q is a junction; junctions are not followed", path)
			}
		}
		if info.IsDir() {
			if path != root && (path == staging || strings.HasPrefix(info.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".hex" {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		key := filepath.ToSlash(relative)
		if first, ok := folded[strings.ToLower(key)]; ok && first != key {
			return fmt.Errorf("logical keys %q and %q collide under Windows case folding", first, key)
		}
		folded[strings.ToLower(key)] = key
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sources[key] = string(content)
		return nil
	})
	return sources, err
}

// materialize writes every generated artifact under the staging root and
// returns the .c files in deterministic logical-key order. Artifact keys
// arrive already validated by the compiler (RFC 0126); the containment,
// symlink, and case-collision checks below are the backstop this component
// keeps because it is the one that touches a filesystem. Content is written
// byte-for-byte: line endings and #line mappings are never altered.
func materialize(staging string, files map[string]string) ([]string, error) {
	absoluteStaging, err := filepath.Abs(staging)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := map[string]string{}
	var cFiles []string
	for _, key := range keys {
		target := filepath.Join(absoluteStaging, filepath.FromSlash(key))
		if !strings.HasPrefix(target, absoluteStaging+string(os.PathSeparator)) {
			return nil, fmt.Errorf("artifact %q escapes the output root", key)
		}
		if lowered, ok := seen[strings.ToLower(key)]; ok && lowered != key {
			return nil, fmt.Errorf("artifacts %q and %q collide under Windows case folding", lowered, key)
		}
		seen[strings.ToLower(key)] = key
		if info, statErr := os.Lstat(target); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("artifact %q resolves through a symlink", key)
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(files[key]), 0o644); err != nil {
			return nil, err
		}
		if strings.HasSuffix(key, ".c") {
			cFiles = append(cFiles, target)
		}
	}
	if len(cFiles) == 0 {
		return nil, fmt.Errorf("no C artifacts were generated")
	}
	return cFiles, nil
}

// cFilesToObjects maps staged sources to their sibling objects, preserving
// the deterministic order so link order never depends on Go map iteration.
func cFilesToObjects(staging string, cFiles []string) []string {
	objects := make([]string, 0, len(cFiles))
	for _, source := range cFiles {
		objects = append(objects, strings.TrimSuffix(source, ".c")+".o")
	}
	return objects
}

// compileTranslationUnitsWithOptions compiles every generated .c in
// deterministic logical-key order: one backend invocation per translation
// unit, each its own C-compilation stage record with separated streams. The
// caller supplies the complete option list, mode options included. moduleOptions
// carries the RFC 0192 user include and define arguments and reaches only
// generated module translation units (`modules/*.c`): compiler-owned runtime
// components and bundled dependencies never see user options, and the
// compiler-owned staging include root precedes them so user input cannot
// shadow hexal.h or a bundled component header.
func compileTranslationUnitsWithOptions(backend *backend.Backend, staging string, cFiles, options, moduleOptions []string, result *BuildResult) error {
	for _, source := range cFiles {
		object := strings.TrimSuffix(source, ".c") + ".o"
		compileOptions := append([]string{"-I", staging}, options...)
		if isModuleTranslationUnit(staging, source) {
			compileOptions = append(compileOptions, moduleOptions...)
		}
		invocation, err := backend.CompileOne(qualifiedTriple, compileOptions, source, object)
		if err != nil {
			return &BuildError{Stage: StageCompile, Message: fmt.Sprintf("cannot run backend: %v", err)}
		}
		result.Commands = append(result.Commands, CommandResult{
			Stage:                StageCompile,
			Tool:                 backend.Exe,
			Arguments:            invocation.Args,
			WorkingDirectory:     staging,
			Stdout:               invocation.Stdout,
			Stderr:               invocation.Stderr,
			ExitCode:             invocation.ExitCode,
			EnvironmentOverrides: append([]string(nil), backend.EnvironmentOverrides...),
		})
		if invocation.ExitCode != 0 {
			return &BuildError{
				Stage:   StageCompile,
				Message: fmt.Sprintf("C compilation of %s failed", filepath.Base(source)),
				Command: &result.Commands[len(result.Commands)-1],
			}
		}
	}
	return nil
}

// isModuleTranslationUnit reports whether source is one of a module's own
// generated translation units. Component and dependency C are compiler-owned;
// only module C carries user code that may include a foreign header.
func isModuleTranslationUnit(staging, source string) bool {
	relative, err := filepath.Rel(staging, source)
	if err != nil {
		return false
	}
	return strings.HasPrefix(filepath.ToSlash(relative), "modules/")
}

// linkObjectsWithOptions links every object through the backend in
// deterministic order into executable, which the caller places in staging so
// a failed link can never disturb a published executable. The backend owns
// linker selection; the driver records the exact command.
func linkObjectsWithOptions(backend *backend.Backend, staging string, objects, options []string, executable string, result *BuildResult) error {
	invocation, err := backend.LinkObjects(qualifiedTriple, objects, executable, options)
	if err != nil {
		return &BuildError{Stage: StageLink, Message: fmt.Sprintf("cannot run backend: %v", err)}
	}
	result.Commands = append(result.Commands, CommandResult{
		Stage:                StageLink,
		Tool:                 backend.Exe,
		Arguments:            invocation.Args,
		WorkingDirectory:     staging,
		Stdout:               invocation.Stdout,
		Stderr:               invocation.Stderr,
		ExitCode:             invocation.ExitCode,
		EnvironmentOverrides: append([]string(nil), backend.EnvironmentOverrides...),
	})
	if invocation.ExitCode != 0 {
		return &BuildError{
			Stage:   StageLink,
			Message: "linking failed",
			Command: &result.Commands[len(result.Commands)-1],
		}
	}
	return nil
}

// validateOutputDestination resolves the final executable's parent and
// rejects non-regular destinations. An explicit -out authorizes only the
// final executable outside the intermediate directory.
func validateOutputDestination(output string) error {
	absolute, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("cannot resolve output path %q: %v", output, err)
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("cannot create output directory %q: %v", parent, err)
	}
	if info, err := os.Lstat(absolute); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output destination %q exists and is not a regular file", absolute)
		}
	}
	return nil
}

// publishExecutable atomically replaces any existing regular executable
// with the linked temporary sibling. A previously published executable is
// never touched before this point, so failure preserves it.
func publishExecutable(tempExe, output string) error {
	absolute, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("cannot resolve output path %q: %v", output, err)
	}
	if err := replaceFile(tempExe, absolute); err != nil {
		return fmt.Errorf("cannot publish executable %q: %v", absolute, err)
	}
	return nil
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}
