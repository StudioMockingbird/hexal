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

func TestClangMajorVersion(t *testing.T) {
	if major, ok := clangMajorVersion("clang version 23.1.1"); !ok || major != 23 {
		t.Fatalf("clang major version not extracted: %d, %v", major, ok)
	}
	if _, ok := clangMajorVersion("gcc (SUSE Linux) 16.2.0"); ok {
		t.Fatalf("non-Clang banner accepted")
	}
	if _, ok := clangMajorVersion("clang version 99999999999999999999.1"); ok {
		t.Fatalf("major version overflowing int accepted")
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
