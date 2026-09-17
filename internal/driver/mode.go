package driver

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/backend"
	"hexal/internal/version"
)

// BuildMode names one backend option set. A mode never changes language
// semantics: the compiler emits byte-identical generated C in every mode, and
// every program's stdout, stderr, exit status, and runtime trap messages are
// identical across modes. Only optimization, debug information, backstop
// instrumentation, and executable size differ. The one observable difference
// is resource exhaustion that depends on code generation, most importantly
// Task stack depth, because stack frame sizes differ between an unoptimized
// and an optimized build. That is a resource limit, not semantics.
type BuildMode string

// The two modes. There is deliberately no size-oriented or check-removing
// variant: bounds, overflow, division, conversion, freed-state, and handle
// checks are language semantics, not debug assertions, so no mode may drop
// them.
const (
	ModeDebug   BuildMode = "debug"
	ModeRelease BuildMode = "release"
)

// unknownModeMessage is the single rendering of the invalid-mode diagnostic,
// shared by flag parsing and by the driver's own guard so an embedding caller
// that skips the flag surface gets the same text.
func unknownModeMessage(value string) string {
	return fmt.Sprintf("unknown build mode %s; expected debug or release", value)
}

// ParseMode resolves a mode flag value. An empty value selects the default;
// any other unrecognized value fails before compilation.
func ParseMode(value string) (BuildMode, error) {
	switch BuildMode(value) {
	case "":
		return ModeDebug, nil
	case ModeDebug, ModeRelease:
		return BuildMode(value), nil
	}
	return "", fmt.Errorf("%s", unknownModeMessage(value))
}

// ModeOptions is one mode's exact backend option set: the options every
// generated translation unit is compiled with, and the options the executable
// link receives. Vendored native dependencies are compiled identically in
// every mode, so they appear in neither list.
type ModeOptions struct {
	Compile []string
	Link    []string
}

// modeOptionTable is the one place a mode selects backend options. The
// backend package receives whatever list this produces and stays unaware that
// modes exist at all.
//
// debug keeps the program unoptimized and fully described: no optimization,
// target-native debug information, and the backend's own default
// undefined-behavior sanitizer set made non-recoverable. That sanitizer set
// is a backstop against compiler defects, never a language check: generated C
// is required to be free of undefined behavior, so a report is always a
// generator defect and must terminate the program rather than let it
// continue.
//
// release optimizes, emits no debug information, drops the sanitizer backstop
// the contract says can never fire, and places each function and datum in its
// own section so the link can collect what the program never references.
// Neither -fwrapv nor -fno-strict-aliasing is added: they would hide exactly
// the generator defects the release conformance run exists to catch.
//
// Both modes disable floating-point contraction. Contraction into a fused
// multiply-add changes floating results, and float output must never depend
// on the mode or on a future baseline CPU choice.
var modeOptionTable = map[BuildMode]ModeOptions{
	ModeDebug: {
		Compile: []string{"-O0", "-g", "-ffp-contract=off", "-fsanitize=undefined", "-fno-sanitize-recover=all"},
		Link:    []string{"-fsanitize=undefined", "-fno-sanitize-recover=all"},
	},
	ModeRelease: {
		Compile: []string{"-O2", "-g0", "-ffp-contract=off", "-fno-sanitize=undefined", "-ffunction-sections", "-fdata-sections"},
		Link:    []string{"-s", "-Wl,--gc-sections"},
	},
}

// Options returns mode's backend option set. The returned slices are fresh
// copies: callers append their own include and dependency options to them.
func Options(mode BuildMode) ModeOptions {
	table := modeOptionTable[mode]
	return ModeOptions{
		Compile: append([]string(nil), table.Compile...),
		Link:    append([]string(nil), table.Link...),
	}
}

// ForeignCompileOptions returns the mode's optimization and debug-information
// choices for a foreign C translation unit. It deliberately drops the mode's
// undefined-behavior backstop flags: that instrumentation is a check on
// Hexal-generated C and is never imposed on unmodified third-party source.
func ForeignCompileOptions(mode BuildMode) []string {
	options := make([]string, 0, len(modeOptionTable[mode].Compile))
	for _, option := range modeOptionTable[mode].Compile {
		if strings.Contains(option, "sanitize") {
			continue
		}
		options = append(options, option)
	}
	return options
}

// resolveMode applies the default and rejects an unrecognized value before
// any compilation runs.
func resolveMode(mode BuildMode) (BuildMode, error) {
	if mode == "" {
		return ModeDebug, nil
	}
	if _, known := modeOptionTable[mode]; !known {
		return "", fmt.Errorf("%s", unknownModeMessage(string(mode)))
	}
	return mode, nil
}

// backendIdentity renders the backend half of a build identity from the
// selected installed Clang's banner line, so an identity names the exact
// compiler that produced it.
func backendIdentity(selected *backend.Backend) string {
	return selected.Identity()
}

// buildIdentity is the lowercase full SHA-256 of a length-delimited stream
// naming everything one build's output depends on: the Hexal version, the
// backend identity, the target profile and runtime ABI, the mode, every
// generated artifact name and its bytes in sorted order, the runtime
// dependencies, the exact mode compile and link options, the runtime pack's
// manifest digest and selected payload hashes, and secret-safe environment
// override hashes. Each field is preceded by its byte length and each
// variable-length group by its element count, so no two distinct field
// sequences can encode to the same stream.
//
// No staging or host-absolute path participates. The mode options are the only
// options that vary independently of the fields above; dependency and pack
// include options are a pure function of the dependency list and manifest
// digest already hashed here, so hashing them would add host paths without
// adding information.
func buildIdentity(mode BuildMode, backendIdentity string, files map[string]string, dependencies []compiler.RuntimeDependency, compileOptions, linkOptions []string, target compilerTypes.TargetProfileID, pack packInputs, environmentHashes []string) string {
	hasher := sha256.New()
	var length [8]byte
	write := func(field string) {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		hasher.Write(length[:])
		hasher.Write([]byte(field))
	}
	count := func(n int) { write(strconv.Itoa(n)) }

	write(version.String())
	write(backendIdentity)
	write(qualifiedTriple)
	write(string(target))
	write(strconv.FormatUint(uint64(compiler.RuntimeABIVersion), 10))
	write(string(mode))

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	count(len(names))
	for _, name := range names {
		write(name)
		write(files[name])
	}

	count(len(dependencies))
	for _, dependency := range dependencies {
		write(string(dependency))
	}

	count(len(compileOptions))
	for _, option := range compileOptions {
		write(option)
	}
	count(len(linkOptions))
	for _, option := range linkOptions {
		write(option)
	}

	write(pack.ManifestDigest)
	count(len(pack.PayloadHashes))
	for _, digest := range pack.PayloadHashes {
		write(digest)
	}
	count(len(environmentHashes))
	for _, digest := range environmentHashes {
		write(digest)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// identityStagingDir is the build's intermediate tree, named after the build
// identity rather than freshly randomized.
//
// The name is load-bearing, not cosmetic. The backend records the absolute
// path of every object it compiles and of the executable it links inside the
// debug information it emits, so a randomly named tree would make the debug
// information of an unchanged program differ on every build and the
// byte-equality rule below could never hold. Deriving the name from the
// identity makes every path in the build a function of the build's inputs.
//
// Any tree left by an earlier run of the same identity is removed first, so
// every path underneath is written by this build and a half-finished earlier
// tree cannot be mistaken for input.
func identityStagingDir(outDir, identity string) (string, error) {
	base := stagingPath("", outDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("cannot create intermediate directory %q: %v", base, err)
	}
	staging := filepath.Join(base, "build-"+identity)
	if err := os.RemoveAll(staging); err != nil {
		return "", fmt.Errorf("cannot clear staging directory %q: %v", staging, err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", fmt.Errorf("cannot create staging directory %q: %v", staging, err)
	}
	return staging, nil
}

// versionedBasename is the identity-carrying stem a debug build links under,
// so the backend names its debug information after it and a repeated identical
// build records the same internal path.
func versionedBasename(output, identity string) string {
	stem := strings.TrimSuffix(filepath.Base(output), exeSuffix())
	return stem + "." + identity
}
