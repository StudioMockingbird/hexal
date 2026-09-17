package backend

import "testing"

func TestFirstNonemptyLine(t *testing.T) {
	got := firstNonemptyLine("\n   \n  clang version 23.1.1 (something)\nsecond\n")
	if got != "clang version 23.1.1 (something)" {
		t.Fatalf("firstNonemptyLine = %q", got)
	}
	if firstNonemptyLine("\n \n") != "" {
		t.Fatalf("blank banner produced a line")
	}
}

func TestClangVersionPattern(t *testing.T) {
	match := clangVersionPattern.FindStringSubmatch("clang version 23.1.1")
	if match == nil || match[1] != "23" {
		t.Fatalf("clang version not extracted: %q", match)
	}
	if clangVersionPattern.FindStringSubmatch("gcc (SUSE Linux) 16.2.0") != nil {
		t.Fatalf("non-Clang banner matched")
	}
}

func TestIdentityUsesTheBannerLineOnly(t *testing.T) {
	selected := &Backend{Exe: "/usr/bin/clang", Version: "clang version 23.1.1", Major: 23}
	if got := selected.Identity(); got != "clang=clang version 23.1.1" {
		t.Fatalf("Identity = %q", got)
	}
}

func TestRunWithoutBackendFailsClosed(t *testing.T) {
	var selected *Backend
	if _, err := selected.Run("--version"); err == nil {
		t.Fatal("nil backend ran")
	}
}
