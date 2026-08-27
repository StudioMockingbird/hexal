package compiler

import (
	"strings"
	"testing"
)

// The module-path allowlist accepts a logical key only when it is relative,
// uses "/" as its only separator, ends in exactly one ".hex" extension, and
// spells every path component as a Hexal identifier.
func TestValidateLogicalKeyAccepts(t *testing.T) {
	for _, key := range []string{
		"app.hex",
		"graphics/shapes_2.hex",
		"Graphics/Shapes2.hex",
		"a/b/c.hex",
		"private_module.hex",
	} {
		if err := validateLogicalKey(key); err != nil {
			t.Errorf("validateLogicalKey(%q) = %v, want accepted", key, err)
		}
	}
}

func TestValidateLogicalKeyRejects(t *testing.T) {
	for _, key := range []string{
		"app",                     // no ".hex" extension
		"app.hex.hex",             // more than one terminal ".hex"
		"/app.hex",                // absolute/leading separator
		"a//b.hex",                // empty component
		"a/1b.hex",                // component starts with a digit
		"my-module.hex",           // punctuation in a component
		"foo.bar/baz.hex",         // a component containing its own dot
		"a/../b.hex",              // traversal component
		"../../../etc/passwd.hex", // traversal above the root
		".hex",                    // no component before the extension
	} {
		err := validateLogicalKey(key)
		if err == nil {
			t.Errorf("validateLogicalKey(%q) accepted, want rejected", key)
			continue
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("validateLogicalKey(%q) error = %q, want it to name the offending key", key, err.Error())
		}
	}
}

// The entrypoint key is validated before its source is lexed: a malformed
// entrypoint is rejected as a Module Error naming the key, with no artifacts
// and no partial statistics.
func TestCompileRejectsInvalidEntrypointKey(t *testing.T) {
	result := Compile(map[string]string{"../escape.hex": "x: Int32 := 1\n"}, "../escape.hex", Project{})
	if result.ExitCode != ExitFailure {
		t.Fatalf("ExitCode = %d, want ExitFailure", result.ExitCode)
	}
	if len(result.Files) != 0 {
		t.Fatalf("Files = %v, want empty", result.Files)
	}
	if len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "../escape.hex") {
		t.Fatalf("Stderr = %v, want a diagnostic naming the entrypoint key", result.Stderr)
	}
}

// An invalid key that no import ever reaches is ignored entirely, exactly
// like any other unreachable source-map entry: it produces no diagnostic,
// artifact, or statistic, and does not stop the reachable set from compiling.
func TestCompileIgnoresUnreachableInvalidKey(t *testing.T) {
	sources := map[string]string{
		"app.hex":            "x: Int32 := 1\n",
		"../unreachable.hex": "y: Int32 := 1\n",
	}
	result := Compile(sources, "app.hex", Project{})
	if result.ExitCode != ExitSuccess {
		t.Fatalf("ExitCode = %d, want ExitSuccess: %v", result.ExitCode, result.Stderr)
	}
	for key := range result.Files {
		if strings.Contains(key, "unreachable") {
			t.Fatalf("Files = %v, must not name the unreachable invalid key", result.Files)
		}
	}
}

// The recovery wrapper is reachable only through the unexported panicSeam,
// which no exported API can trigger. A recovered panic returns exactly one
// fixed Unknown Error, a non-nil empty Files map, and finalized statistics --
// never the panic value, a Go stack, or a host path.
func TestCompileRecoversFromInjectedPanic(t *testing.T) {
	original := panicSeam
	panicSeam = func() { panic("injected panic value: /secret/host/path") }
	defer func() { panicSeam = original }()

	result := Compile(map[string]string{"app.hex": "x: Int32 := 1\n"}, "app.hex", Project{})

	if result.ExitCode != ExitFailure {
		t.Fatalf("ExitCode = %d, want ExitFailure", result.ExitCode)
	}
	if result.Files == nil || len(result.Files) != 0 {
		t.Fatalf("Files = %v, want a non-nil empty map", result.Files)
	}
	if len(result.Stderr) != 1 {
		t.Fatalf("Stderr = %v, want exactly one diagnostic", result.Stderr)
	}
	if strings.Contains(result.Stderr[0], "injected panic value") || strings.Contains(result.Stderr[0], "/secret/host/path") {
		t.Fatalf("Stderr = %v, must not expose the panic value or a host path", result.Stderr)
	}
	if result.Stats.TotalDuration < 0 {
		t.Fatalf("Stats.TotalDuration = %v, want finalized (non-negative) statistics", result.Stats.TotalDuration)
	}
}

// Panic recovery must not swallow ordinary diagnostics: ordinary source
// rejections still report their own diagnostics unchanged.
func TestCompileOrdinaryDiagnosticsUnaffectedByRecovery(t *testing.T) {
	result := Compile(map[string]string{"app.hex": "x: Int32 := \"not an int\"\n"}, "app.hex", Project{})
	if result.ExitCode != ExitFailure {
		t.Fatalf("ExitCode = %d, want ExitFailure", result.ExitCode)
	}
	if len(result.Stderr) == 0 {
		t.Fatal("Stderr is empty, want the ordinary type-mismatch diagnostic")
	}
}
