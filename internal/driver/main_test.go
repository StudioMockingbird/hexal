package driver

// A helper-process stand-in for the Zig compiler. Ordinary tests must not
// invoke a real compiler, but the driver's version and target checks still
// need an executable that answers `version`, `env`, and `cc`. Re-invoking this
// test binary with a fake version answers those probes; the environment
// variable is absent in every ordinary run, so the tests proceed normally.

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if version := os.Getenv("HEXAL_FAKE_ZIG_VERSION"); version != "" {
		os.Exit(fakeZig(version, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeZig emulates just enough of the Zig command surface for the driver's
// selection checks.
func fakeZig(version string, args []string) int {
	if len(args) == 0 {
		return 1
	}
	switch args[0] {
	case "version":
		fmt.Println(version)
		return 0
	case "env":
		fmt.Printf(".lib_dir = %q\n", os.TempDir())
		return 0
	case "cc":
		// Every cc invocation succeeds; the fake only exercises identity and
		// version checking.
		return 0
	default:
		return 1
	}
}
