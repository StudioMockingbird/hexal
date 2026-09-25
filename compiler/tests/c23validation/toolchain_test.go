//go:build c23

package c23validation

// Toolchain discovery: the tagged suite needs exactly one external compiler,
// an installed Clang 18 or newer. Resolution is data-driven (override env
// var, versioned PATH names, bounded fallback locations), never a hardcoded
// path or a bare exec.LookPath. A tagged run resolves the compiler once per
// test binary and fails with one actionable message, naming every location
// tried, when it is missing or too old. There is no skip and no degraded
// mode.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// toolchain is one resolved, version-checked compiler ready to invoke.
// Command is the argv prefix and always holds exactly one executable path:
// this suite has one compiler, so no call site strips a subcommand prefix.
type toolchain struct {
	Name          string
	Command       []string
	Version       string
	DefaultTarget string
}

// clangOverrideEnv names the environment variable that redirects discovery
// to one exact executable path; it is the only override the suite honors.
const clangOverrideEnv = "HEXAL_CLANG"

// minimumClangMajor is the oldest accepted Clang frontend. It matches the
// production driver's own floor so a tagged run qualifies the same compiler
// the driver would accept.
const minimumClangMajor = 18

// newestSearchedClangMajor bounds the versioned PATH search. It is above
// every released Clang, so a newer versioned install beats an unversioned
// name a distribution happens to point at an older compiler.
const newestSearchedClangMajor = 40

// clangVersionPattern extracts the major version from the banner. It matches
// only a product name ending in "clang" with a major and at least one minor
// component, never an unrelated tool that merely prints a version.
var clangVersionPattern = regexp.MustCompile(`clang version (\d+)\.\d+`)

// clangPathNames lists the executable names tried against PATH, newest
// versioned name first and the bare name last.
func clangPathNames() []string {
	names := make([]string, 0, newestSearchedClangMajor-minimumClangMajor+2)
	for major := newestSearchedClangMajor; major >= minimumClangMajor; major-- {
		names = append(names, fmt.Sprintf("clang-%d", major))
	}
	return append(names, "clang")
}

// clangFallbackGlobs are the bounded absolute locations tried after PATH, for
// an install that exists but was never added to PATH.
func clangFallbackGlobs() []string {
	if runtime.GOOS != "windows" {
		return []string{"/usr/bin/clang", "/usr/local/bin/clang", "/opt/homebrew/bin/clang"}
	}
	return []string{`C:\Program Files\LLVM\bin\clang.exe`}
}

// resolveClangExecutable finds the Clang executable, trying the HEXAL_CLANG
// override, then the versioned PATH names, then the bounded fallback
// locations, in that order. It returns the resolved path or an error listing
// every location tried so a missing compiler is actionable.
func resolveClangExecutable() (string, error) {
	if override := os.Getenv(clangOverrideEnv); override != "" {
		// The override names one executable path directly; it is never
		// split on whitespace, since an install path may contain spaces.
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		if resolved, err := exec.LookPath(override); err == nil {
			return resolved, nil
		}
		return "", fmt.Errorf("%s=%q does not resolve to an executable", clangOverrideEnv, override)
	}
	tried := make([]string, 0, len(clangPathNames())+len(clangFallbackGlobs()))
	for _, name := range clangPathNames() {
		tried = append(tried, name+" (PATH)")
		if resolved, err := exec.LookPath(name); err == nil {
			return resolved, nil
		}
	}
	for _, pattern := range clangFallbackGlobs() {
		tried = append(tried, pattern)
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("clang not found; tried %s, and override variable %s", strings.Join(tried, ", "), clangOverrideEnv)
}

// resolveClang resolves, invokes, and version-checks the one required
// compiler. It returns an error rather than calling t.Fatalf so it is safe to
// run inside sync.Once.
func resolveClang() (toolchain, error) {
	path, err := resolveClangExecutable()
	if err != nil {
		return toolchain{}, fmt.Errorf("c23 suite requires clang: %w", err)
	}
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return toolchain{}, fmt.Errorf("clang found at %s but failed to report its version: %v\n%s", path, err, output)
	}
	match := clangVersionPattern.FindStringSubmatch(string(output))
	if match == nil {
		return toolchain{}, fmt.Errorf("clang at %s produced an unrecognized version banner: %s", path, output)
	}
	major, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return toolchain{}, fmt.Errorf("clang at %s produced a non-numeric version %q", path, match[1])
	}
	if major < minimumClangMajor {
		return toolchain{}, fmt.Errorf("clang at %s is version %d, below the required minimum %d", path, major, minimumClangMajor)
	}
	return toolchain{
		Name:          "clang",
		Command:       []string{path},
		Version:       strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0]),
		DefaultTarget: hostTriple(),
	}, nil
}

// hostTriple is the Clang toolchain triple for this host's qualified target.
func hostTriple() string {
	if runtime.GOOS == "windows" {
		return "x86_64-w64-windows-gnu"
	}
	return "x86_64-linux-gnu"
}

var (
	clangOnce      sync.Once
	cachedClang    toolchain
	cachedClangErr error
)

// clangToolchain resolves and version-checks the one required compiler
// exactly once per test binary run, however many subtests call it.
// resolveClang takes no *testing.T so it is safe to run inside sync.Once;
// every caller here -- the one that ran Once.Do and every one that didn't --
// reports the same cached error through its own t. A naive sync.Once around a
// t.Fatalf-calling function would not work: t's Fatalf unwinds the calling
// goroutine via runtime.Goexit rather than a normal return, but sync.Once
// still marks itself done regardless (its internal doSlow sets the done flag
// via a defer), so every later caller would silently receive a zero-value
// toolchain with no error at all, failing the requirement that a missing
// compiler is reported by name.
func clangToolchain(t *testing.T) toolchain {
	t.Helper()
	clangOnce.Do(func() {
		cachedClang, cachedClangErr = resolveClang()
	})
	if cachedClangErr != nil {
		t.Fatal(cachedClangErr)
	}
	return cachedClang
}
