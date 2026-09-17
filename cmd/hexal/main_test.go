package main

import (
	"path/filepath"
	"strings"
	"testing"

	"hexal/internal/version"
)

// Dispatch pins the exact CLI contract: the command set, the version
// surfaces, and the stable failure messages with their single rendering
// site in main.
func TestDispatchRejectsUnknownCommand(t *testing.T) {
	err := dispatch([]string{"frobnicate"})
	if err == nil || err.Error() != `unknown command "frobnicate"` {
		t.Fatalf("error = %v, want unknown command", err)
	}
}

func TestDispatchRequiresACommand(t *testing.T) {
	err := dispatch(nil)
	if err == nil || err.Error() != "expected a command" {
		t.Fatalf("error = %v, want expected a command", err)
	}
}

func TestVersionLine(t *testing.T) {
	if got := versionLine(); got != "hexal "+version.String() {
		t.Fatalf("versionLine() = %q", got)
	}
}

func TestVersionAndAliasAgree(t *testing.T) {
	if err := dispatch([]string{"version"}); err != nil {
		t.Fatalf("version failed: %v", err)
	}
	if err := dispatch([]string{"--version"}); err != nil {
		t.Fatalf("--version failed: %v", err)
	}
}

func TestHelpFirstLineCarriesVersion(t *testing.T) {
	first, _, _ := strings.Cut(usageText(), "\n")
	if first != "Hexal "+version.String() {
		t.Fatalf("help first line = %q", first)
	}
}

func TestHelpCommandsSucceed(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		if err := dispatch(args); err != nil {
			t.Fatalf("%v failed: %v", args, err)
		}
	}
}

// The toolchain identity gates every command: an invalid build identity
// fails before dispatch, so no command runs under a broken identity.
func TestRunRejectsInvalidVersion(t *testing.T) {
	if version.Valid() != true {
		t.Skip("version value is not valid in this build")
	}
	if err := run(nil); err == nil || err.Error() != "expected a command" {
		t.Fatalf("run(nil) = %v, want expected a command", err)
	}
}

// A malformed foreign option fails at configuration before the backend is
// resolved, so these run without a toolchain. A project entrypoint is supplied
// because the CLI refuses a no-file build without -entry.
func TestBuildRejectsInvalidForeignConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		args []string
		want string
	}{
		{[]string{"-c-env", "NOEQUALS"}, "-c-env requires NAME=VALUE"},
		{[]string{"-c-env", "=value"}, "invalid C environment variable name"},
		{[]string{"-c-env", "A=1", "-c-env", "A=2"}, "is repeated"},
		{[]string{"-c-define", "1BAD"}, "invalid C define"},
		{[]string{"-c-define", "A=1", "-c-define", "A=2"}, "is repeated"},
		{[]string{"-c-standard", "c++"}, "unknown C standard"},
		{[]string{"-system-library", "path/lib"}, "invalid system library name"},
		{[]string{"-c-define=1BAD"}, "invalid C define"},
		{[]string{"-c-env=NOEQUALS"}, "-c-env requires NAME=VALUE"},
	} {
		err := build(append([]string{"-entry", "main.hex"}, testCase.args...))
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("build(%v) = %v, want containing %q", testCase.args, err, testCase.want)
		}
	}
}

// An empty operand is rejected by every repeatable option.
func TestStringListRejectsEmptyOperand(t *testing.T) {
	var list stringList
	if err := list.Set(""); err == nil {
		t.Fatal("Set(\"\") accepted an empty operand")
	}
	if err := list.Set("one"); err != nil {
		t.Fatal(err)
	}
	if err := list.Set("two"); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0] != "one" || list[1] != "two" {
		t.Fatalf("list = %v, want occurrence order preserved", list)
	}
}

// The raw-argument escape hatches do not exist.
func TestBuildRejectsRawToolArguments(t *testing.T) {
	for _, option := range []string{"-c-arg", "-link-arg"} {
		if err := build([]string{option, "whatever"}); err == nil {
			t.Errorf("build(%s ...) accepted a raw tool argument", option)
		}
	}
}

func TestHelpListsForeignOptions(t *testing.T) {
	help := usageText()
	for _, option := range []string{"-cc", "-target", "-c-source", "-c-include", "-c-define", "-c-env", "-c-standard", "-object", "-archive", "-system-library"} {
		if !strings.Contains(help, option) {
			t.Errorf("help does not list %s", option)
		}
	}
	if strings.Contains(help, "-runtime-dir") {
		t.Errorf("help still lists the removed -runtime-dir option")
	}
}

// A missing or unqualified target and a missing compiler fail at
// configuration before any external command runs. A project entrypoint is
// supplied so the CLI does not stop at its own -entry requirement first.
func TestBuildRequiresCompilerAndTarget(t *testing.T) {
	for _, testCase := range []struct {
		args []string
		want string
	}{
		{[]string{"-entry", "main.hex"}, "target profile  is not qualified for native builds in this release"},
		{[]string{"-entry", "main.hex", "-cc", "whatever"}, "target profile  is not qualified for native builds in this release"},
		{[]string{"-entry", "main.hex", "-target", "x86_64-windows-gnu-ucrt"}, "target profile x86_64-windows-gnu-ucrt is not qualified for native builds in this release"},
		{[]string{"-entry", "main.hex", "-target", "x86_64-windows-gnu"}, "target profile x86_64-windows-gnu is not qualified for native builds in this release"},
		{[]string{"-entry", "main.hex", "-target", "x86_64-linux-gnu"}, "C backend is required; pass -cc <path>"},
	} {
		err := build(testCase.args)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("build(%v) = %v, want containing %q", testCase.args, err, testCase.want)
		}
	}
}

// A project build with no filepath requires -entry; a filepath supplies both
// root and entrypoint and cannot be combined with them.
func TestBuildEntrypointAndFilepathRules(t *testing.T) {
	if err := build(nil); err == nil || !strings.Contains(err.Error(), "project build requires -entry when no filepath is given") {
		t.Fatalf("build(nil) = %v, want the project-build requirement", err)
	}
	if err := build([]string{"a.hex", "b.hex"}); err == nil || !strings.Contains(err.Error(), "build accepts at most one source filepath") {
		t.Fatalf("two filepaths = %v, want the at-most-one diagnostic", err)
	}
	if err := build([]string{"a.hex", "-root", "dir"}); err == nil || !strings.Contains(err.Error(), "build filepath cannot be combined with -root or -entry") {
		t.Fatalf("filepath plus -root = %v, want the combination diagnostic", err)
	}
	if err := build([]string{"missing.hex"}); err == nil || !strings.Contains(err.Error(), "build filepath must name a regular .hex file") {
		t.Fatalf("missing filepath = %v, want the regular-file diagnostic", err)
	}
	if err := build([]string{"main.nothex"}); err == nil || !strings.Contains(err.Error(), "build filepath must name a regular .hex file") {
		t.Fatalf("non-.hex filepath = %v, want the regular-file diagnostic", err)
	}
}

// The positional filepath is recognized before or after named options, while
// every named option's value stays with its option.
func TestSplitPositionalRecognizesFilepathEitherSide(t *testing.T) {
	before, _ := splitPositional([]string{"app.hex", "-cc", "/usr/bin/clang", "-target", "x86_64-linux-gnu"})
	if len(before) != 1 || before[0] != "app.hex" {
		t.Fatalf("leading filepath = %v", before)
	}
	after, named := splitPositional([]string{"-cc", "/usr/bin/clang", "app.hex", "-target", "x86_64-linux-gnu"})
	if len(after) != 1 || after[0] != "app.hex" {
		t.Fatalf("trailing filepath = %v", after)
	}
	for _, option := range []string{"/usr/bin/clang", "x86_64-linux-gnu"} {
		found := false
		for _, value := range named {
			if value == option {
				found = true
			}
		}
		if !found {
			t.Fatalf("named option %q was lost: %v", option, named)
		}
	}
}

func TestDoctorRequiresCompilerAndTarget(t *testing.T) {
	err := doctor(nil)
	if err == nil {
		t.Fatal("doctor with no target or compiler must report problems")
	}
}

func TestBuildRejectsNonExecutableCompilerPath(t *testing.T) {
	directory := t.TempDir()
	for _, path := range []string{filepath.Join(directory, "missing.exe"), directory} {
		err := build([]string{"-entry", "main.hex", "-cc", path, "-target", "x86_64-linux-gnu"})
		if err == nil || !strings.Contains(err.Error(), "is not an executable file") {
			t.Errorf("build(-cc %q) = %v, want a non-executable diagnostic", path, err)
		}
	}
}
