package compiler

import (
	"reflect"
	"testing"
)

func TestCompileReportsMimallocForDynamicAllocation(t *testing.T) {
	result := Compile(map[string]string{
		"app.hex": "h: Heap := Heap()\n",
	}, "app.hex", Project{})
	if result.ExitCode != ExitSuccess {
		t.Fatalf("ExitCode = %d, want success: %v", result.ExitCode, result.Stderr)
	}
	if !reflect.DeepEqual(result.Dependencies, []RuntimeDependency{RuntimeMimalloc}) {
		t.Fatalf("Dependencies = %v, want [%q]", result.Dependencies, RuntimeMimalloc)
	}
}

func TestCompileOmitsMimallocForScalarProgram(t *testing.T) {
	result := Compile(map[string]string{
		"app.hex": "value: Int32 := 1\n",
	}, "app.hex", Project{})
	if result.ExitCode != ExitSuccess {
		t.Fatalf("ExitCode = %d, want success: %v", result.ExitCode, result.Stderr)
	}
	if result.Dependencies == nil || len(result.Dependencies) != 0 {
		t.Fatalf("Dependencies = %v, want a non-nil empty slice", result.Dependencies)
	}
}

func TestCompileFailureReturnsEmptyRuntimeDependencies(t *testing.T) {
	result := Compile(map[string]string{
		"app.hex": "value: Int32 := \"wrong\"\n",
	}, "app.hex", Project{})
	if result.ExitCode != ExitFailure {
		t.Fatalf("ExitCode = %d, want failure", result.ExitCode)
	}
	if result.Dependencies == nil || len(result.Dependencies) != 0 {
		t.Fatalf("Dependencies = %v, want a non-nil empty slice", result.Dependencies)
	}
}
