package driver

// Command-line foreign C build inputs. The driver accepts explicitly
// supplied C sources, include directories, preprocessor definitions, an
// environment override map, a foreign dialect, precompiled objects, static
// archives, and named system libraries. It normalizes and validates them, then
// compiles and links them beside the generated Hexal artifacts. The core
// compiler is untouched: none of these values enters compiler.Project or
// compiler.Compile.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"hexal/internal/backend"
)

// defaultForeignDialect is the dialect for every foreign C source when
// -c-standard is absent. Modern enough for ordinary libraries without
// claiming that foreign source is C23.
const defaultForeignDialect = "c17"

// foreignDialects is the accepted -c-standard set. The driver passes the
// spelling through to the selected Clang and fails if Clang rejects it.
//
// The set is driver-owned build policy, not a compiler fact, so it is not a
// registry record: the registry holds compiler-owned language and runtime
// facts, and the arc keeps toolchain qualification in this package for the same
// reason. The migration inventory names "foreign dialect and required-header
// policy tables" as one target; the required-header half is the component
// registry's RequiredCHeaders, and the dialect half has its single owner here.
var foreignDialects = map[string]bool{
	"c89": true, "c99": true, "c11": true, "c17": true, "c23": true,
	"gnu89": true, "gnu99": true, "gnu11": true, "gnu17": true, "gnu23": true,
}

// environmentOverride is one parsed -c-env NAME=VALUE entry.
type environmentOverride struct {
	Name  string
	Value string
}

// foreignConfig is the validated, rooted foreign build configuration. It is
// driver state only; it is never copied into compiler.Project.
type foreignConfig struct {
	Sources         []string
	IncludeDirs     []string
	Defines         []string
	Standard        string
	Objects         []string
	Archives        []string
	SystemLibraries []string
	// environment is the complete normalized child environment, deterministically
	// ordered, that every generated-C, foreign-C, and link invocation runs under.
	environment []string
	// overrideNames are the explicit override names in occurrence order, for
	// secret-safe command records.
	overrideNames []string
	// overrides are the parsed overrides, kept for secret-safe identity
	// hashes. Their plaintext never enters a record.
	overrides []environmentOverride
	// active reports whether any effectful foreign option was supplied.
	active bool
}

// configureForeign validates and normalizes every foreign option before any
// external command runs. Configuration failures are configuration-stage;
// missing or wrong-kind paths are filesystem-stage.
func configureForeign(options BuildOptions) (foreignConfig, *BuildError) {
	var config foreignConfig

	overrides, failure := parseEnvironmentOverrides(options.CEnvironment)
	if failure != nil {
		return config, failure
	}
	config.environment = effectiveEnvironment(os.Environ(), overrides)
	config.overrideNames = overrideNames(overrides)
	config.overrides = overrides

	standard := options.CStandard
	if standard == "" {
		standard = defaultForeignDialect
	}
	if !foreignDialects[standard] {
		return config, configurationFailure("unknown C standard " + standard)
	}
	config.Standard = standard

	if failure := validateDefines(options.CDefines); failure != nil {
		return config, failure
	}
	config.Defines = append([]string(nil), options.CDefines...)

	if failure := validateSystemLibraries(options.SystemLibraries); failure != nil {
		return config, failure
	}
	config.SystemLibraries = append([]string(nil), options.SystemLibraries...)

	return config, nil
}

// resolveForeignRootedPaths resolves and validates every path input against
// the resolved source root and enforces the duplicate rules. It runs after
// configureForeign and after the root is known.
func (config *foreignConfig) resolveForeignRootedPaths(root string, options BuildOptions) *BuildError {
	sources, failure := resolveFiles(root, options.CSources, "-c-source")
	if failure != nil {
		return failure
	}
	if failure := rejectDuplicatePaths(sources, "C source"); failure != nil {
		return failure
	}
	config.Sources = sources

	includeDirs, failure := resolveDirectories(root, options.CIncludeDirs, "-c-include")
	if failure != nil {
		return failure
	}
	if failure := rejectDuplicatePaths(includeDirs, "C include directory"); failure != nil {
		return failure
	}
	config.IncludeDirs = includeDirs

	objects, failure := resolveFiles(root, options.Objects, "-object")
	if failure != nil {
		return failure
	}
	if failure := rejectDuplicatePaths(objects, "object"); failure != nil {
		return failure
	}
	config.Objects = objects

	archives, failure := resolveFiles(root, options.Archives, "-archive")
	if failure != nil {
		return failure
	}
	config.Archives = archives

	config.active = len(config.Sources) > 0 || len(config.IncludeDirs) > 0 ||
		len(config.Defines) > 0 || len(config.overrideNames) > 0 ||
		len(config.Objects) > 0 || len(config.Archives) > 0 || len(config.SystemLibraries) > 0
	return nil
}

// moduleCompileOptions renders the user include and define arguments that
// reach generated module translation units and foreign C compilations. Order
// is preserved: include directories then definitions, each in occurrence
// order.
func (config foreignConfig) moduleCompileOptions() []string {
	options := make([]string, 0, len(config.IncludeDirs)+len(config.Defines))
	for _, directory := range config.IncludeDirs {
		options = append(options, "-I"+directory)
	}
	for _, define := range config.Defines {
		options = append(options, "-D"+define)
	}
	return options
}

// systemLibraryOptions renders one linker argument per named system library in
// occurrence order, through the backend's own translation.
func (config foreignConfig) systemLibraryOptions() []string {
	options := make([]string, 0, len(config.SystemLibraries))
	for _, library := range config.SystemLibraries {
		options = append(options, backend.SystemLibraryArgument(library))
	}
	return options
}

// environmentOverrideHashes renders one secret-safe SHA-256 per explicit
// override, covering its name and value. The plaintext never leaves this
// function.
func environmentOverrideHashes(config foreignConfig) []string {
	hashes := make([]string, 0, len(config.overrides))
	for _, override := range config.overrides {
		sum := sha256.Sum256([]byte(override.Name + "=" + override.Value))
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	return hashes
}

// parseEnvironmentOverrides parses `NAME=VALUE` entries. The split occurs at
// the first `=`; the value may be empty or contain further `=`. Names are
// deduplicated under host equality.
func parseEnvironmentOverrides(entries []string) ([]environmentOverride, *BuildError) {
	seen := make(map[string]bool, len(entries))
	overrides := make([]environmentOverride, 0, len(entries))
	for _, entry := range entries {
		index := strings.IndexByte(entry, '=')
		if index < 0 {
			return nil, configurationFailure("-c-env requires NAME=VALUE")
		}
		name, value := entry[:index], entry[index+1:]
		if name == "" || strings.ContainsRune(name, 0) || strings.ContainsRune(name, '=') {
			return nil, configurationFailure("invalid C environment variable name " + name)
		}
		if strings.ContainsRune(value, 0) {
			return nil, configurationFailure("invalid C environment value for " + name)
		}
		key := hostEnvironmentKey(name)
		if seen[key] {
			return nil, configurationFailure("C environment variable " + name + " is repeated")
		}
		seen[key] = true
		overrides = append(overrides, environmentOverride{Name: name, Value: value})
	}
	return overrides, nil
}

// overrideNames lists override names in occurrence order.
func overrideNames(overrides []environmentOverride) []string {
	names := make([]string, 0, len(overrides))
	for _, override := range overrides {
		names = append(names, override.Name)
	}
	return names
}

// hostEnvironmentKey folds one environment-variable name under the execution
// host's equality rule: ASCII case-insensitive on Windows, byte-sensitive on
// POSIX.
func hostEnvironmentKey(name string) string {
	if os.PathSeparator == '\\' {
		return asciiUpper(name)
	}
	return name
}

// asciiUpper folds ASCII letters only, matching the host rule exactly without
// locale or Unicode participation.
func asciiUpper(value string) string {
	buffer := []byte(value)
	for index, character := range buffer {
		if character >= 'a' && character <= 'z' {
			buffer[index] = character - ('a' - 'A')
		}
	}
	return string(buffer)
}

// effectiveEnvironment merges explicit overrides over the inherited
// environment and returns one deterministically ordered child environment with
// no duplicate keys. The override's own name spelling is retained.
func effectiveEnvironment(inherited []string, overrides []environmentOverride) []string {
	type entry struct{ name, value string }
	byKey := make(map[string]entry, len(inherited)+len(overrides))
	for _, raw := range inherited {
		index := strings.IndexByte(raw, '=')
		if index < 0 {
			continue
		}
		byKey[hostEnvironmentKey(raw[:index])] = entry{name: raw[:index], value: raw[index+1:]}
	}
	for _, override := range overrides {
		byKey[hostEnvironmentKey(override.Name)] = entry{name: override.Name, value: override.Value}
	}
	keys := slices.Sorted(maps.Keys(byKey))
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		current := byKey[key]
		environment = append(environment, current.name+"="+current.value)
	}
	return environment
}

// validateDefines enforces `name` or `name=value` shape, one definition per
// name. A value may be empty: the C compiler defines it as an empty macro.
func validateDefines(defines []string) *BuildError {
	seen := make(map[string]bool, len(defines))
	for _, define := range defines {
		name, value := define, ""
		if index := strings.IndexByte(define, '='); index >= 0 {
			name, value = define[:index], define[index+1:]
		}
		if !validMacroName(name) || strings.ContainsRune(value, 0) {
			return configurationFailure("invalid C define " + define)
		}
		if seen[name] {
			return configurationFailure("C define " + name + " is repeated")
		}
		seen[name] = true
	}
	return nil
}

// validMacroName reports whether name matches [A-Za-z_][A-Za-z0-9_]*.
func validMacroName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		letter := character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		if index == 0 {
			if !letter {
				return false
			}
			continue
		}
		if !letter && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

// validateSystemLibraries enforces a nonempty logical name over the allowed
// character set. A path is never accepted.
func validateSystemLibraries(libraries []string) *BuildError {
	for _, library := range libraries {
		if library == "" {
			return configurationFailure("system library name must not be empty")
		}
		for index := 0; index < len(library); index++ {
			character := library[index]
			allowed := character == '_' || character == '-' || character == '.' || character == '+' ||
				character >= '0' && character <= '9' ||
				character >= 'a' && character <= 'z' ||
				character >= 'A' && character <= 'Z'
			if !allowed {
				return configurationFailure("invalid system library name " + library)
			}
		}
	}
	return nil
}

// resolveFiles resolves option paths against root, requires each to exist as a
// regular file, and preserves occurrence order.
func resolveFiles(root string, paths []string, option string) ([]string, *BuildError) {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		absolute, err := resolveForeignInput(root, path)
		if err != nil {
			return nil, filesystemFailure(fmt.Sprintf("%s %q: %v", option, path, err))
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, filesystemFailure(fmt.Sprintf("%s %q: %v", option, absolute, err))
		}
		if !info.Mode().IsRegular() {
			return nil, filesystemFailure(fmt.Sprintf("%s %q is not a regular file", option, absolute))
		}
		resolved = append(resolved, absolute)
	}
	return resolved, nil
}

// resolveDirectories resolves option paths against root and requires each to
// exist as a directory, preserving occurrence order.
func resolveDirectories(root string, paths []string, option string) ([]string, *BuildError) {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		absolute, err := resolveForeignInput(root, path)
		if err != nil {
			return nil, filesystemFailure(fmt.Sprintf("%s %q: %v", option, path, err))
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, filesystemFailure(fmt.Sprintf("%s %q: %v", option, absolute, err))
		}
		if !info.IsDir() {
			return nil, filesystemFailure(fmt.Sprintf("%s %q is not a directory", option, absolute))
		}
		resolved = append(resolved, absolute)
	}
	return resolved, nil
}

// resolveForeignInput resolves one relative foreign path against the resolved
// source root, or keeps an absolute path. Absolute paths are accepted because
// installed SDKs and prebuilt libraries commonly live outside the project.
func resolveForeignInput(root, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

// rejectDuplicatePaths rejects a repeated normalized path. Duplicate include
// roots, sources, and objects are all not useful: only archives and system
// libraries may repeat because static link resolution can make repetition
// meaningful.
func rejectDuplicatePaths(paths []string, kind string) *BuildError {
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		key := path
		if os.PathSeparator == '\\' {
			key = asciiUpper(path)
		}
		if seen[key] {
			return configurationFailure("duplicate " + kind + " " + path)
		}
		seen[key] = true
	}
	return nil
}

// foreignObjectName derives one deterministic, collision-free object name from
// a normalized source path and its zero-based occurrence ordinal. Equal
// basenames in different directories never collide.
func foreignObjectName(staging, source string, ordinal int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", ordinal, source)))
	return filepath.Join(staging, "foreign", "foreign-"+hex.EncodeToString(digest[:8])+".o")
}

// compileForeignSources compiles every supplied C source separately, in
// occurrence order, using the selected dialect, mode, and Clang triple. The
// first failure records its exact command and stops the build.
func compileForeignSources(selected *backend.Backend, staging string, config foreignConfig, mode BuildMode, extraOptions []string, triple string, result *BuildResult) ([]string, *BuildError) {
	objects := make([]string, 0, len(config.Sources))
	if len(config.Sources) == 0 {
		return objects, nil
	}
	if err := os.MkdirAll(filepath.Join(staging, "foreign"), 0o755); err != nil {
		return nil, filesystemFailure(fmt.Sprintf("cannot create foreign object directory: %v", err))
	}
	options := append(append([]string(nil), extraOptions...), ForeignCompileOptions(mode)...)
	options = append(options, config.moduleCompileOptions()...)
	for ordinal, source := range config.Sources {
		object := foreignObjectName(staging, source, ordinal)
		invocation, err := selected.CompileOneDialect(triple, config.Standard, options, source, object)
		if err != nil {
			return nil, &BuildError{Stage: StageCompile, Message: fmt.Sprintf("cannot run backend: %v", err)}
		}
		record := CommandResult{
			Stage:                StageCompile,
			Tool:                 selected.Exe,
			Arguments:            invocation.Args,
			WorkingDirectory:     staging,
			Stdout:               invocation.Stdout,
			Stderr:               invocation.Stderr,
			ExitCode:             invocation.ExitCode,
			EnvironmentOverrides: append([]string(nil), config.overrideNames...),
		}
		result.Commands = append(result.Commands, record)
		if invocation.ExitCode != 0 {
			return nil, &BuildError{
				Stage:   StageCompile,
				Message: fmt.Sprintf("C compilation of %s failed", filepath.Base(source)),
				Command: &result.Commands[len(result.Commands)-1],
			}
		}
		objects = append(objects, object)
	}
	return objects, nil
}
