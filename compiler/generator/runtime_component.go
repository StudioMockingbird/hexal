package generator

// runtimeSourceModel is the render model for packages/runtime.c: the program
// always gets the diagnostic trap, and Native adds the libuv bootstrap.
type runtimeSourceModel struct {
	Native bool
}

// libuvSelected reports whether any reachable operation links libuv. The
// scheduler substrate, the monotonic clock, and File link it.
func libuvSelected(merged *programEmission) bool {
	if merged.timeState != nil && merged.timeState.instant || merged.fileState != nil && merged.fileState.used {
		return true
	}
	return merged.concurrencyState != nil && merged.concurrencyState.used
}
