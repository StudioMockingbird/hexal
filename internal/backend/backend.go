// Package backend drives one installed Clang executable: it reads the
// compiler's identity, invokes direct compile and link commands for the
// qualified target, and captures separated streams and the exact argument
// vector. It wraps no compiler family, plugin, or Zig distribution: this
// release qualifies exactly one backend, installed Clang, selected by explicit
// path.
//
// The backend never searches PATH, downloads during a build, or probes the
// host for target facts. Callers supply an explicit path; this package
// verifies and invokes.
package backend

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Backend is one installed Clang compiler. Exe is the resolved absolute path
// of the executable; Directory is the working directory every invocation runs
// in, empty for the caller's own. Pinning the directory is not a convenience:
// the compiler records its working directory in the debug information it
// emits, so a caller that needs reproducible debug information must pin it to
// a path derived from the build's inputs.
type Backend struct {
	Exe     string
	Version string
	Major   int
	// Directory is the working directory every invocation runs in.
	Directory string
	// Environment is the complete child environment for every build
	// invocation the driver makes: the launching process environment with the
	// explicit overrides already merged, deterministically ordered. Nil means
	// inherit the parent environment unchanged.
	Environment []string
	// EnvironmentOverrides names the explicit `-c-env` overrides in
	// occurrence order, for secret-safe command records. It never carries a
	// value.
	EnvironmentOverrides []string
}

// clangVersionPattern extracts the major version from a Clang version banner.
var clangVersionPattern = regexp.MustCompile(`clang version (\d+)\.`)

// NewBackend opens the installed Clang at exe and reads its identity from
// `<exe> --version`. Callers supply the path; nothing here searches for it.
// An executable whose banner does not name Clang fails closed: the driver
// renders the one external "not Clang 18 or newer" diagnostic from that
// failure, so the exact reason is not exposed here.
func NewBackend(exe string) (*Backend, error) {
	version, err := output(exe, "", nil, "--version")
	if err != nil {
		return nil, fmt.Errorf("cannot read compiler version: %w", err)
	}
	banner := firstNonemptyLine(version.Stdout)
	if banner == "" {
		return nil, fmt.Errorf("compiler reported no version")
	}
	match := clangVersionPattern.FindStringSubmatch(banner)
	if match == nil {
		return nil, fmt.Errorf("compiler is not Clang")
	}
	major := 0
	for _, character := range match[1] {
		major = major*10 + int(character-'0')
	}
	return &Backend{Exe: exe, Version: banner, Major: major}, nil
}

// firstNonemptyLine returns the first nonempty line of text, trimmed, or the
// empty string. The complete trimmed first nonempty version-banner line enters
// build identity; installation paths and later banner lines do not.
func firstNonemptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Identity renders the backend half of the build identity: the complete
// trimmed first version-banner line. Absolute installation paths never
// participate.
func (backend *Backend) Identity() string {
	return "clang=" + backend.Version
}

// Result is one completed child-process invocation with separated streams
// and the exact argument vector, so a failing build is reproducible from
// its record.
type Result struct {
	Args     []string
	Stdout   string
	Stderr   string
	ExitCode int
}

func output(exe, directory string, environment []string, args ...string) (Result, error) {
	command := exec.Command(exe, args...)
	command.Dir = directory
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	result := Result{
		Args:   append([]string{exe}, args...),
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if runErr != nil {
		if exit, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exit.ExitCode()
		} else {
			return result, runErr
		}
	}
	return result, nil
}

// Run invokes the backend executable with args, capturing separated streams
// and the exit status. A non-zero exit is data in the result, not a Go
// error: callers decide what a status means for their stage. Every
// invocation runs under the backend's effective environment.
func (backend *Backend) Run(args ...string) (Result, error) {
	if backend == nil || backend.Exe == "" {
		return Result{}, fmt.Errorf("no backend selected")
	}
	return output(backend.Exe, backend.Directory, backend.Environment, args...)
}
