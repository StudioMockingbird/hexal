package generator

// runtimeSourceModel is the render model for packages/runtime.c: the program
// always gets the diagnostic trap, and Native adds the libuv bootstrap.
type runtimeSourceModel struct {
	Native bool
}

// libuvSelected reports whether any reachable operation links libuv. The
// scheduler substrate, the monotonic clock, File, and the core-library path
// and entropy queries link it. std/program.arguments alone does not: it reads
// the process invocation through the C runtime.
func libuvSelected(merged *programEmission) bool {
	if merged.timeState != nil && merged.timeState.instant || merged.fileState != nil && merged.fileState.used {
		return true
	}
	if merged.networkState != nil && merged.networkState.used {
		return true
	}
	if merged.corelibState != nil && (merged.corelibState.paths || merged.corelibState.entropy) {
		return true
	}
	return merged.concurrencyState != nil && merged.concurrencyState.used
}
