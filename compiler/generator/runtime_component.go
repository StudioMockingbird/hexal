package generator

// runtimeSourceModel is the render model for packages/runtime.c: the program
// always gets the diagnostic trap, and Native adds the libuv bootstrap.
type runtimeSourceModel struct {
	Native bool
}

// runtimeComponents returns the generated hexal/runtime.c artifact when any
// selected path can trap. The trap body is the one program-wide definition;
// Native additionally bootstraps libuv onto the shared allocator.
func runtimeComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.requirements == nil || !merged.requirements.trap {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/runtime.c",
		template: "runtime.c",
		model:    runtimeSourceModel{Native: merged.requirements.native},
	}}, nil
}

// utf8procSelected reports whether a generated runtime component uses the
// utf8proc adapter. The callers are the UTF-8 validator's two owners: a module
// that constructs text from bytes or concatenates text, and the program
// component, which validates argv and the executable path as text. A
// literal-only or byte-iteration program selects nothing.
func utf8procSelected(merged *programEmission) bool {
	if merged == nil {
		return false
	}
	if merged.validatorNeed {
		return true
	}
	return merged.corelibState != nil && (merged.corelibState.paths || merged.corelibState.arguments || merged.corelibState.executable)
}

// libuvSelected reports whether any reachable operation links libuv. The
// scheduler substrate, the monotonic clock, File, Terminal, and the
// core-library path and entropy queries link it. std/program.arguments alone
// does not: it reads the process invocation through the C runtime.
func libuvSelected(merged *programEmission) bool {
	if merged.timeState != nil && merged.timeState.instant || merged.fileState != nil && merged.fileState.used {
		return true
	}
	if merged.networkState != nil && merged.networkState.used {
		return true
	}
	if merged.terminalState != nil && merged.terminalState.used {
		return true
	}
	if merged.corelibState != nil && (merged.corelibState.paths || merged.corelibState.entropy) {
		return true
	}
	return merged.concurrencyState != nil && merged.concurrencyState.used
}
