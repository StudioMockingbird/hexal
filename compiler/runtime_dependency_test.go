package compiler

import (
	"reflect"
	"testing"
)

func TestCompileReportsMimallocForDynamicAllocation(t *testing.T) {
	result := Compile(map[string]string{
		"app.hex": "let h: Heap = Heap()\n",
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
		"app.hex": "let value: Int32 = 1\n",
	}, "app.hex", Project{})
	if result.ExitCode != ExitSuccess {
		t.Fatalf("ExitCode = %d, want success: %v", result.ExitCode, result.Stderr)
	}
	if result.Dependencies == nil || len(result.Dependencies) != 0 {
		t.Fatalf("Dependencies = %v, want a non-nil empty slice", result.Dependencies)
	}
}

func TestCompileReportsLibuvForTaskProgram(t *testing.T) {
	result := Compile(map[string]string{
		"app.hex": "fun work(): Int32 do\n    return 1\nend\nfun run(): Int32 | Error do\n    let task: Task<Int32> = try spawn work()\n    return task.join()\nend\n",
	}, "app.hex", Project{})
	if result.ExitCode != ExitSuccess {
		t.Fatalf("ExitCode = %d, want success: %v", result.ExitCode, result.Stderr)
	}
	if !reflect.DeepEqual(result.Dependencies, []RuntimeDependency{RuntimeLibuv, RuntimeMimalloc}) {
		t.Fatalf("Dependencies = %v, want [%q, %q]", result.Dependencies, RuntimeLibuv, RuntimeMimalloc)
	}
}

func TestCompileFailureReturnsEmptyRuntimeDependencies(t *testing.T) {
	result := Compile(map[string]string{
		"app.hex": "let value: Int32 = \"wrong\"\n",
	}, "app.hex", Project{})
	if result.ExitCode != ExitFailure {
		t.Fatalf("ExitCode = %d, want failure", result.ExitCode)
	}
	if result.Dependencies == nil || len(result.Dependencies) != 0 {
		t.Fatalf("Dependencies = %v, want a non-nil empty slice", result.Dependencies)
	}
}
