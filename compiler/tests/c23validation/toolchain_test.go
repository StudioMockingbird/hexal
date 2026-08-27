//go:build c23

package c23validation

// Toolchain discovery: each tool is a spec (override env var, PATH
// candidates, platform fallback globs), never a hardcoded path or a bare
// exec.LookPath. A tagged run resolves every required toolchain once and
// fails by name, with every discovery location tried, when one is missing.
// There is no skip and no degraded mode.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// toolchain is one resolved, version-checked compiler ready to invoke.
// Command is the full argv prefix: a plain compiler is one element, "zig cc"
// is two, so every call site appends its own flags to Command rather than
// assuming a single executable path.
type toolchain struct {
	Name          string
	Command       []string
	Version       string
	DefaultTarget string
}

// toolchainSpec is the data record for one required toolchain: how to find
// it and how to confirm it is new enough, so a machine can redirect one tool
// through its override variable without editing a test.
type toolchainSpec struct {
	name string
	// envVar names the override environment variable, e.g. HEXAL_GCC. When
	// set, its value is used directly (as a command line, so "zig cc"-style
	// multi-word overrides split on whitespace) with no further search.
	envVar string
	// pathNames are the executable names tried against PATH, in order.
	// Versioned names (clang-22) belong before the bare name so a
	// version-suffixed install is preferred to a shadowing wrapper.
	pathNames []string
	// fallbackGlobs are platform-specific absolute path patterns (may
	// contain filepath.Glob wildcards) tried after PATH, for installs that
	// exist but were never added to PATH -- the exact situation that
	// motivated this spec-driven design.
	fallbackGlobs func() []string
	// subcommand is appended to the resolved executable before any other
	// flag, e.g. "cc" for zig. Empty for a plain compiler.
	subcommand string
	// versionArgs invokes the tool to print its version banner.
	versionArgs []string
	// versionPattern extracts the leading MAJOR version number from that
	// banner.
	versionPattern *regexp.Regexp
	// minimumMajor is the lowest accepted frontend major version; an older
	// discovered tool is rejected by name and version, never silently used.
	minimumMajor  int
	defaultTarget string
}

var gccSpec = toolchainSpec{
	name:           "gcc",
	envVar:         "HEXAL_GCC",
	pathNames:      []string{"gcc-15", "gcc"},
	versionArgs:    []string{"--version"},
	versionPattern: regexp.MustCompile(`\b(\d+)\.\d+\.\d+\b`),
	minimumMajor:   15,
	defaultTarget:  "x86_64-w64-mingw32 (UCRT)",
	fallbackGlobs: func() []string {
		if runtime.GOOS != "windows" {
			return []string{"/usr/bin/gcc", "/usr/local/bin/gcc", "/opt/homebrew/bin/gcc"}
		}
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return nil
		}
		return []string{filepath.Join(local, `Microsoft\WinGet\Packages\BrechtSanders.WinLibs.*\mingw64\bin\gcc.exe`)}
	},
}

var clangSpec = toolchainSpec{
	name:           "clang",
	envVar:         "HEXAL_CLANG",
	pathNames:      []string{"clang-22", "clang-18", "clang"},
	versionArgs:    []string{"--version"},
	versionPattern: regexp.MustCompile(`clang version (\d+)\.\d+\.\d+`),
	minimumMajor:   18,
	defaultTarget:  "x86_64-pc-windows-msvc",
	fallbackGlobs: func() []string {
		if runtime.GOOS != "windows" {
			return []string{"/usr/bin/clang", "/usr/local/bin/clang", "/opt/homebrew/bin/clang"}
		}
		return []string{`C:\Program Files\LLVM\bin\clang.exe`}
	},
}

var zigSpec = toolchainSpec{
	name:           "zig",
	envVar:         "HEXAL_ZIG",
	pathNames:      []string{"zig"},
	subcommand:     "cc",
	versionArgs:    []string{"version"},
	versionPattern: regexp.MustCompile(`(\d+)\.\d+\.\d+`),
	minimumMajor:   0, // zig's own version numbering is pre-1.0; any discovered zig is accepted.
	defaultTarget:  "x86_64-windows-gnu",
	fallbackGlobs: func() []string {
		if runtime.GOOS != "windows" {
			home, _ := os.UserHomeDir()
			candidates := []string{"/usr/local/bin/zig", "/opt/homebrew/bin/zig"}
			if home != "" {
				candidates = append(candidates, filepath.Join(home, ".local/bin/zig"))
			}
			return candidates
		}
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return nil
		}
		return []string{filepath.Join(local, `Microsoft\WinGet\Packages\zig.zig_*\zig-*\zig.exe`)}
	},
}

// resolveExecutable finds spec's executable, trying the override variable,
// then PATH, then the platform fallback globs, in that order. It returns the
// resolved path and which mechanism found it, or an error listing every
// location tried so a missing tool is actionable without reading the RFC.
func resolveExecutable(spec toolchainSpec) (string, error) {
	if override := os.Getenv(spec.envVar); override != "" {
		// The override names one executable path directly; it is never
		// split on whitespace, since a Windows install path routinely
		// contains spaces (e.g. "C:\Program Files\LLVM\bin\clang.exe").
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		if resolved, err := exec.LookPath(override); err == nil {
			return resolved, nil
		}
		return "", fmt.Errorf("%s=%q does not resolve to an executable", spec.envVar, override)
	}
	tried := make([]string, 0, len(spec.pathNames)+4)
	for _, name := range spec.pathNames {
		tried = append(tried, name+" (PATH)")
		if resolved, err := exec.LookPath(name); err == nil {
			return resolved, nil
		}
	}
	if spec.fallbackGlobs != nil {
		for _, pattern := range spec.fallbackGlobs() {
			tried = append(tried, pattern)
			matches, _ := filepath.Glob(pattern)
			if len(matches) > 0 {
				return matches[0], nil
			}
		}
	}
	return "", fmt.Errorf("%s not found; tried %s, and override variable %s", spec.name, strings.Join(tried, ", "), spec.envVar)
}

// discoverToolchain resolves, invokes, and version-checks one toolchain.
// Failure is fatal by name: the tagged suite never skips a missing or
// unsupported toolchain.
func discoverToolchain(t *testing.T, spec toolchainSpec) toolchain {
	t.Helper()
	path, err := resolveExecutable(spec)
	if err != nil {
		t.Fatalf("c23 suite requires %s: %v", spec.name, err)
	}
	command := []string{path}
	if spec.subcommand != "" {
		command = append(command, spec.subcommand)
	}
	// The version banner is always queried against the bare executable
	// (e.g. "zig version", never "zig cc version"), independent of the
	// subcommand prefix later used to invoke it as a compiler.
	output, err := exec.Command(path, spec.versionArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s found at %s but failed to report its version: %v\n%s", spec.name, path, err, output)
	}
	match := spec.versionPattern.FindStringSubmatch(string(output))
	if match == nil {
		t.Fatalf("%s at %s produced an unrecognized version banner: %s", spec.name, path, output)
	}
	major, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		t.Fatalf("%s at %s produced a non-numeric version %q", spec.name, path, match[1])
	}
	if major < spec.minimumMajor {
		t.Fatalf("%s at %s is version %d, below the required minimum %d", spec.name, path, major, spec.minimumMajor)
	}
	resolved := toolchain{Name: spec.name, Command: command, Version: strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0]), DefaultTarget: spec.defaultTarget}
	t.Logf("discovered %s at %s: %s (default target %s)", spec.name, path, resolved.Version, spec.defaultTarget)
	return resolved
}

// discoverAllToolchains resolves and version-checks all three required
// toolchains for one test. Each call re-resolves rather than caching across
// tests: a cached failure would otherwise report clearly for the first
// fixture that hit it and confusingly (a zero-value toolchain) for every
// fixture after, which fails the requirement that a missing tool is
// reported by name.
func discoverAllToolchains(t *testing.T) (gcc, clang, zig toolchain) {
	t.Helper()
	return discoverToolchain(t, gccSpec), discoverToolchain(t, clangSpec), discoverToolchain(t, zigSpec)
}
