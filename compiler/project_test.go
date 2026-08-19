package compiler

import (
	"strings"
	"testing"
)

// Compile validates Project before any stage runs; these tests pin the rules
// and their messages so the configuration surface stays a compiler input like
// any other. The zero value is the contract: it must always pass.
func TestProjectZeroValueValidates(t *testing.T) {
	if err := validateProject(Project{}); err != nil {
		t.Fatalf("zero value rejected: %v", err)
	}
}

func TestProjectCommitMayEqualReserve(t *testing.T) {
	if err := validateProject(Project{TaskStackReserve: 1 << 20, TaskStackCommit: 1 << 20}); err != nil {
		t.Fatalf("commit equal to reserve rejected: %v", err)
	}
}

func TestProjectCommitExceedingReserveRejected(t *testing.T) {
	err := validateProject(Project{TaskStackReserve: 1 << 20, TaskStackCommit: 1<<20 + 4096})
	if err == nil {
		t.Fatal("commit above reserve accepted")
	}
	want := "TaskStackCommit 1052672 exceeds TaskStackReserve 1048576"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("diagnostic %q does not contain %q", err.Error(), want)
	}
}

// A defaulted commit can violate the reserve the caller did set: 8 KiB
// exceeds a 4 KiB reserve, and the caller should be told the effective
// numbers, not the zeros it supplied.
func TestProjectDefaultedCommitCanExceedSmallReserve(t *testing.T) {
	err := validateProject(Project{TaskStackReserve: 4096})
	if err == nil {
		t.Fatal("defaulted commit above a 4 KiB reserve accepted")
	}
	want := "TaskStackCommit 8192 exceeds TaskStackReserve 4096"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("diagnostic %q does not contain %q", err.Error(), want)
	}
}

func TestProjectReserveNotPageMultipleRejected(t *testing.T) {
	err := validateProject(Project{TaskStackReserve: 4097, TaskStackCommit: 4096})
	if err == nil {
		t.Fatal("reserve that is not a page multiple accepted")
	}
	want := "TaskStackReserve 4097 is not a multiple of 4096"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("diagnostic %q does not contain %q", err.Error(), want)
	}
}

func TestProjectCommitNotPageMultipleRejected(t *testing.T) {
	err := validateProject(Project{TaskStackReserve: 1 << 20, TaskStackCommit: 4097})
	if err == nil {
		t.Fatal("commit that is not a page multiple accepted")
	}
	want := "TaskStackCommit 4097 is not a multiple of 4096"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("diagnostic %q does not contain %q", err.Error(), want)
	}
}

// The rejection must fail the whole compilation through the public entry
// point, with the Configuration Error category and no artifacts.
func TestCompileRejectsInvalidProject(t *testing.T) {
	result := Compile(map[string]string{"app.hex": "value: Int32 = 1\n"}, "app.hex", Project{TaskStackReserve: 4097, TaskStackCommit: 4096})
	if result.ExitCode != ExitFailure {
		t.Fatalf("invalid project compiled successfully")
	}
	if len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "[Configuration Error] TaskStackReserve 4097 is not a multiple of 4096") {
		t.Fatalf("stderr %q lacks the Configuration Error diagnostic", result.Stderr)
	}
	if len(result.Files) != 0 {
		t.Fatalf("invalid project produced artifacts: %v", result.Files)
	}
}
