package main

import (
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
