package backend

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Backend is one verified Zig distribution: an executable plus its required
// lib/ tree. It carries no PATH search, no download, and no host probing
// beyond asking the executable for its own identity.
type Backend struct {
	Exe           string
	Version       string
	LibDir        string
	ClangVersion  string
	LinkerDefault string
}

var libDirPattern = regexp.MustCompile(`\.lib_dir\s*=\s*"((?:[^"\\]|\\.)*)"`)
var clangVersionPattern = regexp.MustCompile(`(?m)^(?:zig\s*:\s*)?clang version (\S+)`)

// NewBackend opens the Zig distribution rooted at exe and reads its
// identity: exact version, lib_dir from `zig env`, and the bundled Clang
// version from `zig cc -v`. Callers supply the path; nothing here searches
// for it.
func NewBackend(exe string) (*Backend, error) {
	version, err := output(exe, "version")
	if err != nil {
		return nil, fmt.Errorf("zig version failed for %q: %w", exe, err)
	}
	backend := &Backend{Exe: exe, Version: strings.TrimSpace(version.Stdout)}
	env, err := output(exe, "env")
	if err == nil {
		if match := libDirPattern.FindStringSubmatch(env.Stdout); match != nil {
			backend.LibDir = strings.ReplaceAll(match[1], `\\`, `\`)
		}
	}
	ccVersion, err := output(exe, "cc", "-v")
	if err == nil {
		combined := ccVersion.Stdout + ccVersion.Stderr
		if match := clangVersionPattern.FindStringSubmatch(combined); match != nil {
			backend.ClangVersion = match[1]
		}
	}
	return backend, nil
}

// Validate checks the structural integrity the driver needs before any
// build: a reported version, a readable lib_dir inside the distribution. It
// does not enforce the release pin; the driver owns that policy.
func (backend *Backend) Validate() error {
	if backend == nil {
		return fmt.Errorf("no backend selected")
	}
	if backend.Version == "" {
		return fmt.Errorf("backend %q reported no version", backend.Exe)
	}
	if backend.LibDir == "" {
		return fmt.Errorf("backend %q reported no lib_dir; the distribution may be incomplete", backend.Exe)
	}
	info, err := os.Stat(backend.LibDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("backend lib_dir %q is not a readable directory; the distribution is incomplete and cannot be relocated away from its lib/ tree", backend.LibDir)
	}
	return nil
}

// CheckPinned enforces the release pin: only the exact pinned version
// satisfies it. A newer version does not; bumping is a deliberate
// re-qualification recorded in the lock record, never an incidental upgrade.
func (backend *Backend) CheckPinned(pinned string) error {
	if err := backend.Validate(); err != nil {
		return err
	}
	if backend.Version != pinned {
		return fmt.Errorf("backend is Zig %s, but this release pins %s", backend.Version, pinned)
	}
	return nil
}

// Identity renders the backend half of the build identity: pinned version,
// archive filename and digest, and the bundled Clang version. Absolute
// installation paths never participate.
func (backend *Backend) Identity(record LockRecord) string {
	return strings.Join([]string{
		"zig=" + backend.Version,
		"archive=" + record.Filename,
		"digest=" + record.SHA256,
		"clang=" + backend.ClangVersion,
	}, "\n")
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

func output(exe string, args ...string) (Result, error) {
	command := exec.Command(exe, args...)
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
// error: callers decide what a status means for their stage.
func (backend *Backend) Run(args ...string) (Result, error) {
	if backend == nil || backend.Exe == "" {
		return Result{}, fmt.Errorf("no backend selected")
	}
	return output(backend.Exe, args...)
}
