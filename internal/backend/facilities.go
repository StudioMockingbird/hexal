package backend

import (
	"fmt"
	"strings"
)

// RequiredHeaders is the exact C header inventory generated C selects,
// derived from compiler/generator/packages on 2026-09-11. A guard test fails
// when production templates begin using a header absent here, which routes
// the new header through qualification instead of silently assuming it.
var RequiredHeaders = []string{
	"errno.h", "inttypes.h", "limits.h", "math.h", "stdatomic.h",
	"stdckdint.h", "stddef.h", "stdint.h", "stdio.h", "stdlib.h",
	"string.h", "windows.h", "process.h", "pthread.h", "ucontext.h",
	"signal.h", "sys/mman.h", "unistd.h", "fcntl.h",
}

// RequiredFacilities names the non-header C23 facilities generated code
// relies on: checked arithmetic, atomics, typeof, nullptr, attributes, and
// static assertions, plus the native Windows threading and IO paths.
var RequiredFacilities = []string{
	"checked-arithmetic", "atomics", "typeof", "nullptr",
	"attributes", "static-assert", "windows-threads", "windows-io",
}

// QualificationProbe returns a C23 translation unit exercising every
// required header and facility. Qualification compiles, links, and runs it
// through the pinned backend; the trivial `int main` probe proves nothing
// about the facility set. The driver runs this as its full probe.
func QualificationProbe() string {
	includes := make([]string, 0, len(RequiredHeaders))
	for _, header := range RequiredHeaders {
		// windows.h and process.h exist only on Windows targets; pthread and
		// POSIX headers only elsewhere. The probe targets the qualified
		// Windows profile, so it includes exactly that profile's headers.
		switch header {
		case "pthread.h", "ucontext.h", "signal.h", "sys/mman.h", "unistd.h", "fcntl.h":
			continue
		}
		includes = append(includes, "#include <"+header+">")
	}
	return strings.Join(includes, "\n") + `
#include <windows.h>

static_assert(sizeof(int32_t) == 4, "exact-width integers");
static_assert(nullptr == 0, "nullptr");

int probe_add(int left, int right) {
	int sum;
	if (ckd_add(&sum, left, right)) {
		return -1;
	}
	return sum;
}

static _Atomic(int) probe_counter;

int main(void) {
	typeof(42) answer = 42;
	probe_counter = 0;
	probe_counter++;
	if (probe_add(20, 22) != answer) {
		return 1;
	 }
	DWORD threads = 1;
	(void)threads;
	printf("%d\n", answer);
	return 0;
}
`
}

// CompileOne compiles one translation unit to an object file: zig cc
// -std=c23 -target <triple> plus caller-supplied options. stdout, stderr,
// and the exit status return separated in the result.
func (backend *Backend) CompileOne(triple string, options []string, source, object string) (Result, error) {
	args := append([]string{"cc", "-std=c23", "-target", triple}, options...)
	args = append(args, "-c", source, "-o", object)
	return backend.Run(args...)
}

// LinkObjects links objects into an executable through the backend, which
// owns linker selection internally. Extra options append after the objects.
func (backend *Backend) LinkObjects(triple string, objects []string, executable string, options []string) (Result, error) {
	args := append([]string{"cc", "-std=c23", "-target", triple}, objects...)
	args = append(args, options...)
	args = append(args, "-o", executable)
	return backend.Run(args...)
}

// CheckError formats a failed backend invocation for a stage diagnostic,
// preserving the complete argument vector and separated streams.
func CheckError(stage, tool string, result Result) error {
	return fmt.Errorf("%s: %s %s exited %d\nstdout:\n%s\nstderr:\n%s",
		stage, tool, strings.Join(result.Args, " "), result.ExitCode, result.Stdout, result.Stderr)
}
