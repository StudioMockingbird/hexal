package diagnostics

import "strconv"

type LogicalKeyProblem uint8

const (
	LogicalKeyInvalid LogicalKeyProblem = iota + 1
	LogicalKeyReservedStd
	LogicalKeyReservedCBindings
)

type ImportPathProblem uint8

const (
	ImportPathNotRelative ImportPathProblem = iota + 1
	ImportPathAboveRoot
	ImportPathInvalidComponent
)

func MissingEntrypoint(key string) Message {
	return message("module.entrypoint-not-found", CategoryModule, StageCompile, "entrypoint "+key+" was not found in the supplied sources")
}

func InvalidLogicalKey(key string, problem LogicalKeyProblem) Message {
	switch problem {
	case LogicalKeyReservedStd:
		return message("module.logical-key-reserved-stdlib", CategoryModule, StageCompile, "logical key \""+key+"\" is invalid: the \"std\" path prefix is reserved for the standard library")
	case LogicalKeyReservedCBindings:
		return message("module.logical-key-reserved-c-bindings", CategoryModule, StageCompile, "logical key \""+key+"\" is invalid: the \"hexalc\" path prefix is reserved for prepared C bindings")
	default:
		return message("module.logical-key-invalid", CategoryModule, StageCompile, "logical key \""+key+"\" is invalid: a logical key must be relative, use \"/\" as its only separator, end in exactly one \".hex\" extension, and have every path component be a Hexal identifier")
	}
}

func ImportPathError(problem ImportPathProblem, rawPath, component string) Message {
	switch problem {
	case ImportPathNotRelative:
		return message("module.import-path-not-relative", CategoryModule, StageCompile, "import path "+rawPath+" is not relative")
	case ImportPathAboveRoot:
		return message("module.import-path-above-root", CategoryModule, StageCompile, "import resolves above the logical source-map root")
	default:
		return message("module.import-path-invalid-component", CategoryModule, StageCompile, "invalid component "+component+" in import path "+rawPath)
	}
}

func PreparedBindingMissing(header string) Message {
	return message("configuration.prepared-c-binding-missing", CategoryConfiguration, StageCompile, "prepared C binding missing for "+header)
}

func CInteropNeedsTarget() Message {
	return message("configuration.c-interop-target-required", CategoryConfiguration, StageCompile, "C interoperability requires a qualified target")
}

func CheckerCInteropNeedsTarget() Message {
	return message("configuration.checker-c-interop-target-required", CategoryConfiguration, StageChecker, "C interoperability requires a qualified target")
}

func LoopMustYield() Message {
	return message("semantic.loop-must-yield", CategorySemantic, StageChecker, "while true loop must execute Task.yield() on every repeating path")
}

func UnknownStdlibModule(display string) Message {
	return message("module.unknown-stdlib-module", CategoryModule, StageCompile, "unknown stdlib module "+display)
}

func ImportedModuleNotFound(display string) Message {
	return message("module.imported-module-not-found", CategoryModule, StageCompile, "imported module "+display+" was not found")
}

func DuplicateImport(target string) Message {
	return message("module.duplicate-import", CategoryModule, StageCompile, "duplicate import of canonical module "+target)
}

func ImportCycle(cycle string) Message {
	return message("module.import-cycle", CategoryModule, StageCompile, "import cycle: "+cycle)
}

func DuplicateSourceKeys(first, second, module string) Message {
	return message("module.duplicate-source-key", CategoryModule, StageCompile, "sources contain both "+first+" and "+second+" for module "+module)
}

func UnknownImportReference() Message {
	return message("internal.unknown-import-reference", CategoryUnknown, StageCompile, "internal compiler error")
}

func ProjectTaskStackCommitExceeds(commit, reserve uint64) Message {
	return message("configuration.task-stack-commit-exceeds-reserve", CategoryConfiguration, StageCompile, "TaskStackCommit "+strconv.FormatUint(commit, 10)+" exceeds TaskStackReserve "+strconv.FormatUint(reserve, 10))
}

func ProjectTaskStackReserveAlignment(reserve, pageSize uint64) Message {
	return message("configuration.task-stack-reserve-alignment", CategoryConfiguration, StageCompile, "TaskStackReserve "+strconv.FormatUint(reserve, 10)+" is not a multiple of "+strconv.FormatUint(pageSize, 10))
}

func ProjectTaskStackCommitAlignment(commit, pageSize uint64) Message {
	return message("configuration.task-stack-commit-alignment", CategoryConfiguration, StageCompile, "TaskStackCommit "+strconv.FormatUint(commit, 10)+" is not a multiple of "+strconv.FormatUint(pageSize, 10))
}

func UnknownTargetProfile(target string) Message {
	return message("configuration.unknown-target-profile", CategoryConfiguration, StageCompile, "unknown target profile "+target)
}

func MissingTargetRecord() Message {
	return message("internal.target-registry-record-missing", CategoryUnknown, StageCompile, "internal compiler error")
}
