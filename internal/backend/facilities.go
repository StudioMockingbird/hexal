package backend

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// requiredHeaders is the exact C header inventory generated C selects for the
// Linux target, derived from compiler/generator/packages. A guard test fails
// when production templates begin using a header absent here, which routes the
// new header through qualification instead of silently assuming it.
var requiredHeaders = []string{
	"errno.h", "inttypes.h", "limits.h", "math.h", "stdatomic.h",
	"stdckdint.h", "stddef.h", "stdint.h", "stdio.h", "stdlib.h",
	"string.h", "pthread.h", "ucontext.h", "signal.h", "sys/mman.h",
	"unistd.h", "fcntl.h",
}

// requiredWindowsHeaders is the exact C header inventory generated C selects
// for the Windows target. It keeps the common C23 and libc surface and
// swaps the POSIX-only headers for windows.h; the Windows pack is what
// supplies libuv, mimalloc, and utf8proc under that inventory.
var requiredWindowsHeaders = []string{
	"errno.h", "inttypes.h", "limits.h", "math.h", "stdatomic.h",
	"stdckdint.h", "stddef.h", "stdint.h", "stdio.h", "stdlib.h",
	"strings.h", "signal.h", "windows.h",
}

// QualificationProbe returns a C23 translation unit exercising every required
// header and facility for the selected target profile. Qualification compiles,
// links, and runs it through the selected backend; the trivial `int main`
// probe proves nothing about the facility set. The driver runs this as its
// full probe.
func QualificationProbe(target compilerTypes.TargetProfileID) string {
	headers := requiredHeaders
	thread := "pthread_t thread = pthread_self();"
	if !isLinuxTarget(target) {
		headers = requiredWindowsHeaders
		thread = "DWORD thread = GetCurrentThreadId();"
	}
	includes := make([]string, 0, len(headers))
	for _, header := range headers {
		includes = append(includes, "#include <"+header+">")
	}
	return strings.Join(includes, "\n") + `
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
	` + thread + `
	(void)thread;
	printf("%d\n", answer);
	return 0;
}
`
}

// isLinuxTarget reports whether the profile is a POSIX/Linux target. The
// facility probe's header set and thread primitive branch on this the same
// way generated code does.
func isLinuxTarget(target compilerTypes.TargetProfileID) bool {
	return strings.Contains(string(target), "linux")
}

// CompileOne compiles one generated translation unit to an object file:
// clang --target <triple> -std=c23 plus caller-supplied options.
func (backend *Backend) CompileOne(target string, options []string, source, object string) (Result, error) {
	return backend.CompileOneDialect(target, "c23", options, source, object)
}

// CompileOneDialect compiles one translation unit using the source dialect
// required by that unit. Generated Hexal sources use C23; foreign sources
// retain the dialect selected by -c-standard.
func (backend *Backend) CompileOneDialect(target, dialect string, options []string, source, object string) (Result, error) {
	args := append([]string{"--target=" + target, "-std=" + dialect}, options...)
	args = append(args, "-c", source, "-o", object)
	return backend.Run(args...)
}

// LinkObjects links objects into an executable through the same selected
// Clang executable. Extra options append after the objects.
func (backend *Backend) LinkObjects(target string, objects []string, executable string, options []string) (Result, error) {
	args := append([]string{"--target=" + target, "-std=c23"}, objects...)
	args = append(args, options...)
	args = append(args, "-o", executable)
	return backend.Run(args...)
}

// SystemLibraryArgument translates one already-validated logical
// system-library name to its native linker argument. The driver owns
// validation; the backend owns the spelling, so the CLI never synthesizes a
// platform filename.
func SystemLibraryArgument(name string) string {
	return "-l" + name
}
