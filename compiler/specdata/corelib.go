package specdata

import "fmt"

// Core-library contracts: the modules reached through an `import ... from
// std.<module>` alias that publish no Hexal source and emit no module artifact
// of their own. A record names a module's exported types and functions.
//
// A type export carries an identifier, never a Type. This package imports no
// compiler package, and compiler/corelib already imports compiler/types and
// stores live Type values, so a Type here would close a cycle. An adapter on
// the corelib side resolves the identifier to its canonical identity; the
// registry stays the single owner of which names a module exports.

// CoreModuleID is one core-library module's canonical import path, the
// collection-path spelling (`std/io`) rather than the dotted alias a program
// writes.
type CoreModuleID string

// CoreTypeID identifies one canonical compiler-owned type a module exports. It
// is an identifier, never a Type: the value crosses the package boundary in
// place of the Type value. One canonical type has one identifier, which may be
// exported under one name by several modules.
type CoreTypeID string

// CoreParam is one function parameter's runtime shape, the only level a
// core-library declaration expresses without a checking module's type
// Environment.
type CoreParam uint8

const (
	CoreParamHeap CoreParam = iota
	CoreParamMutByteSlice
)

// CoreResult is one function's success shape. The Error union a fallible
// function adds at the call site is classified separately by ErrorBehavior.
type CoreResult uint8

const (
	CoreResultString CoreResult = iota
	CoreResultStringSlice
	CoreResultNil
	CoreResultSize
)

// ErrorBehavior classifies whether a function's result unions with the built-in
// Error type at the call site. It restates the error half of CoreResult as its
// own fact; a builtin function's error behavior belongs to the checker
// operation it routes to, so its record leaves this zero and validation checks
// the pair only for a runtime function.
type ErrorBehavior uint8

const (
	// ErrorNever yields its result directly, never an Error.
	ErrorNever ErrorBehavior = iota
	// ErrorFallible unions its result with the built-in Error type.
	ErrorFallible
)

// CoreFunction is one exported module function. A moved capability's static
// operation carries Builtin, the checker operation that resolves it. A
// core-library runtime function carries Runtime, the emitted C entry point
// (the RuntimeSymbol fact), and its Params and Result. Exactly one
// of Builtin and Runtime is set. The runtime template owns the function's
// stable ErrorKind and fixed message, so neither crosses this boundary.
type CoreFunction struct {
	Name          string
	Builtin       string
	Params        []CoreParam
	Result        CoreResult
	ErrorBehavior ErrorBehavior
	Runtime       string
	Components    []ComponentID
}

// CoreTypeExport is one exported type name and the identifier of the canonical
// type it names.
type CoreTypeExport struct {
	Name   string
	TypeID CoreTypeID
}

// CoreModule is one core-library module: its canonical path and its exports.
type CoreModule struct {
	ID        CoreModuleID
	Types     []CoreTypeExport
	Functions []CoreFunction
}

// The canonical type identifiers the export table references. Values name the
// canonical type so a record reads against the identity it resolves to.
const (
	CoreTypeIO                  CoreTypeID = "IO"
	CoreTypeBytes               CoreTypeID = "Bytes"
	CoreTypeSeek                CoreTypeID = "Seek"
	CoreTypeFile                CoreTypeID = "File"
	CoreTypeFileMode            CoreTypeID = "FileMode"
	CoreTypeDuration            CoreTypeID = "Duration"
	CoreTypeInstant             CoreTypeID = "Instant"
	CoreTypeWallTime            CoreTypeID = "WallTime"
	CoreTypeAddress             CoreTypeID = "Address"
	CoreTypeTcpConnection       CoreTypeID = "TcpConnection"
	CoreTypeTcpListener         CoreTypeID = "TcpListener"
	CoreTypeProcess             CoreTypeID = "Process"
	CoreTypePipe                CoreTypeID = "Pipe"
	CoreTypeProcessOptions      CoreTypeID = "ProcessOptions"
	CoreTypeStartedProcess      CoreTypeID = "StartedProcess"
	CoreTypeEnvironment         CoreTypeID = "Environment"
	CoreTypeEnvironmentVariable CoreTypeID = "EnvironmentVariable"
	CoreTypeProcessStream       CoreTypeID = "ProcessStream"
	CoreTypeExitStatus          CoreTypeID = "ExitStatus"
	CoreTypeSignal              CoreTypeID = "Signal"
	CoreTypeSignals             CoreTypeID = "Signals"
	CoreTypeTerminalSize        CoreTypeID = "TerminalSize"
)

// coreModules is the registry. It is unexported so no importer can rewrite a
// record, and every query clones the slices it returns, so a caller can never
// reach the registry's own backing arrays.
var coreModules = []CoreModule{
	{
		ID: "std/io",
		Types: []CoreTypeExport{
			{Name: "IO", TypeID: CoreTypeIO},
			{Name: "Bytes", TypeID: CoreTypeBytes},
			{Name: "Seek", TypeID: CoreTypeSeek},
		},
		Functions: []CoreFunction{
			{Name: "stdin", Builtin: "io_stdin"},
			{Name: "stdout", Builtin: "io_stdout"},
			{Name: "stderr", Builtin: "io_stderr"},
			{Name: "bytes_over", Builtin: "io_bytes_over"},
		},
	},
	{
		ID: "std/fs",
		Types: []CoreTypeExport{
			{Name: "File", TypeID: CoreTypeFile},
			{Name: "FileMode", TypeID: CoreTypeFileMode},
		},
		Functions: []CoreFunction{
			{Name: "open", Builtin: "file_open"},
		},
	},
	{
		ID: "std/time",
		Types: []CoreTypeExport{
			{Name: "Duration", TypeID: CoreTypeDuration},
			{Name: "Instant", TypeID: CoreTypeInstant},
			{Name: "WallTime", TypeID: CoreTypeWallTime},
		},
		Functions: []CoreFunction{
			{Name: "nanoseconds", Builtin: "time_nanoseconds"},
			{Name: "microseconds", Builtin: "time_microseconds"},
			{Name: "milliseconds", Builtin: "time_milliseconds"},
			{Name: "seconds", Builtin: "time_seconds"},
			{Name: "now", Builtin: "time_now"},
			{Name: "wall_time", Builtin: "time_wall_time"},
			{Name: "sleep", Builtin: "time_sleep"},
		},
	},
	{
		ID: "std/net",
		Types: []CoreTypeExport{
			{Name: "Address", TypeID: CoreTypeAddress},
			{Name: "TcpConnection", TypeID: CoreTypeTcpConnection},
			{Name: "TcpListener", TypeID: CoreTypeTcpListener},
		},
		Functions: []CoreFunction{
			{Name: "parse_address", Builtin: "address_parse"},
			{Name: "resolve", Builtin: "dns_resolve"},
			{Name: "connect", Builtin: "tcp_connect"},
			{Name: "listen", Builtin: "tcp_listen"},
		},
	},
	{
		ID: "std/process",
		Types: []CoreTypeExport{
			{Name: "Process", TypeID: CoreTypeProcess},
			{Name: "Pipe", TypeID: CoreTypePipe},
			{Name: "ProcessOptions", TypeID: CoreTypeProcessOptions},
			{Name: "StartedProcess", TypeID: CoreTypeStartedProcess},
			{Name: "Environment", TypeID: CoreTypeEnvironment},
			{Name: "EnvironmentVariable", TypeID: CoreTypeEnvironmentVariable},
			{Name: "ProcessStream", TypeID: CoreTypeProcessStream},
			{Name: "ExitStatus", TypeID: CoreTypeExitStatus},
		},
		Functions: []CoreFunction{
			{Name: "start", Builtin: "process_start"},
		},
	},
	{
		ID: "std/signal",
		Types: []CoreTypeExport{
			{Name: "Signal", TypeID: CoreTypeSignal},
			{Name: "Signals", TypeID: CoreTypeSignals},
		},
		Functions: []CoreFunction{
			{Name: "subscribe", Builtin: "signals_subscribe"},
		},
	},
	{
		ID: "std/terminal",
		Types: []CoreTypeExport{
			{Name: "TerminalSize", TypeID: CoreTypeTerminalSize},
		},
		Functions: []CoreFunction{
			{Name: "is_attached", Builtin: "terminal_is_attached"},
			{Name: "size", Builtin: "terminal_size"},
		},
	},
	{
		ID: "std/program",
		Functions: []CoreFunction{
			{Name: "arguments", Result: CoreResultStringSlice, ErrorBehavior: ErrorFallible, Runtime: "hex_program_arguments", Components: []ComponentID{ComponentProgram}},
			{Name: "current_directory", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_current_directory", Components: []ComponentID{ComponentProgram}},
			{Name: "home_directory", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_home_directory", Components: []ComponentID{ComponentProgram}},
			{Name: "temporary_directory", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_temporary_directory", Components: []ComponentID{ComponentProgram}},
			{Name: "executable_path", Params: []CoreParam{CoreParamHeap}, Result: CoreResultString, ErrorBehavior: ErrorFallible, Runtime: "hex_program_executable_path", Components: []ComponentID{ComponentProgram}},
			{Name: "available_parallelism", Result: CoreResultSize, ErrorBehavior: ErrorNever, Runtime: "hex_program_available_parallelism", Components: []ComponentID{ComponentProgram}},
		},
	},
	{
		ID: "std/entropy",
		Functions: []CoreFunction{
			{Name: "fill", Params: []CoreParam{CoreParamMutByteSlice}, Result: CoreResultNil, ErrorBehavior: ErrorFallible, Runtime: "hex_entropy_fill", Components: []ComponentID{ComponentEntropy}},
		},
	},
}

// CoreModuleKnown reports whether id names a registered module, without
// allocating a record copy.
func CoreModuleKnown(id CoreModuleID) bool {
	for _, module := range coreModules {
		if module.ID == id {
			return true
		}
	}
	return false
}

// CoreModuleByID resolves one core-library module by canonical path.
func CoreModuleByID(id CoreModuleID) (CoreModule, bool) {
	for _, module := range coreModules {
		if module.ID == id {
			return cloneCoreModule(module), true
		}
	}
	return CoreModule{}, false
}

// CoreModules returns every module record in registration order as a copy.
func CoreModules() []CoreModule {
	modules := make([]CoreModule, len(coreModules))
	for index, module := range coreModules {
		modules[index] = cloneCoreModule(module)
	}
	return modules
}

// CoreFunctionByModuleName resolves one exported function of a known module.
func CoreFunctionByModuleName(id CoreModuleID, name string) (CoreFunction, bool) {
	for _, module := range coreModules {
		if module.ID != id {
			continue
		}
		for _, function := range module.Functions {
			if function.Name == name {
				return cloneCoreFunction(function), true
			}
		}
	}
	return CoreFunction{}, false
}

// CoreFunctionByRuntime resolves one emitted C entry point back to its owning
// module and declaration. validateCorelib enforces that runtime entry points
// are unique across every core-library function, so the first match is the only
// match.
func CoreFunctionByRuntime(runtime string) (CoreModuleID, CoreFunction, bool) {
	for _, module := range coreModules {
		for _, function := range module.Functions {
			if function.Runtime == runtime {
				return module.ID, cloneCoreFunction(function), true
			}
		}
	}
	return "", CoreFunction{}, false
}

// cloneCoreModule deep-copies the slices a record owns, so a query result
// shares no backing array with the registry.
func cloneCoreModule(module CoreModule) CoreModule {
	module.Types = append([]CoreTypeExport(nil), module.Types...)
	module.Functions = make([]CoreFunction, len(module.Functions))
	for index, function := range module.Functions {
		module.Functions[index] = cloneCoreFunction(function)
	}
	return module
}

// cloneCoreFunction deep-copies one function record's parameter and component
// slices.
func cloneCoreFunction(function CoreFunction) CoreFunction {
	function.Params = append([]CoreParam(nil), function.Params...)
	function.Components = append([]ComponentID(nil), function.Components...)
	return function
}

// validateCorelib checks one core-library registry for internal consistency:
// unique module paths; unique export names inside a module; exactly one of a
// builtin and a runtime entry point per function; runtime entry points unique
// across every module; and, for each runtime function, a valid error behavior
// consistent with its result and at least one known component demand. It
// cannot check that a type identifier resolves, because resolution crosses the
// import boundary; the corelib adapter's own test covers that half. It takes
// the slice so a test can validate a crafted registry without mutating the
// package's own.
func validateCorelib(modules []CoreModule) error {
	moduleIDs := make(map[CoreModuleID]bool, len(modules))
	runtimes := make(map[string]CoreModuleID)
	for _, module := range modules {
		if module.ID == "" {
			return fmt.Errorf("specdata/corelib: module has an empty id")
		}
		if moduleIDs[module.ID] {
			return fmt.Errorf("specdata/corelib: module %q is declared twice", module.ID)
		}
		moduleIDs[module.ID] = true
		exports := make(map[string]bool, len(module.Types)+len(module.Functions))
		for _, export := range module.Types {
			if export.Name == "" {
				return fmt.Errorf("specdata/corelib: module %q exports a type with an empty name", module.ID)
			}
			if export.TypeID == "" {
				return fmt.Errorf("specdata/corelib: module %q type %q has an empty identifier", module.ID, export.Name)
			}
			if exports[export.Name] {
				return fmt.Errorf("specdata/corelib: module %q exports %q twice", module.ID, export.Name)
			}
			exports[export.Name] = true
		}
		for _, function := range module.Functions {
			if function.Name == "" {
				return fmt.Errorf("specdata/corelib: module %q exports a function with an empty name", module.ID)
			}
			if exports[function.Name] {
				return fmt.Errorf("specdata/corelib: module %q exports %q twice", module.ID, function.Name)
			}
			exports[function.Name] = true
			if function.ErrorBehavior != ErrorNever && function.ErrorBehavior != ErrorFallible {
				return fmt.Errorf("specdata/corelib: function %q.%q has unknown error behavior", module.ID, function.Name)
			}
			if (function.Builtin == "") == (function.Runtime == "") {
				return fmt.Errorf("specdata/corelib: function %q.%q must name exactly one of a builtin or a runtime entry point", module.ID, function.Name)
			}
			// A builtin routes to a checker operation that owns its error
			// behavior and component demand; only a runtime function states
			// both here.
			if function.Builtin != "" {
				continue
			}
			if owner, ok := runtimes[function.Runtime]; ok {
				return fmt.Errorf("specdata/corelib: runtime %q is declared by %q and %q", function.Runtime, owner, module.ID)
			}
			runtimes[function.Runtime] = module.ID
			fallible := function.Result != CoreResultSize
			if (function.ErrorBehavior == ErrorFallible) != fallible {
				return fmt.Errorf("specdata/corelib: runtime %q.%q result and error behavior disagree", module.ID, function.Name)
			}
			if len(function.Components) == 0 {
				return fmt.Errorf("specdata/corelib: runtime %q.%q names no component", module.ID, function.Name)
			}
			seenComponents := make(map[ComponentID]bool, len(function.Components))
			for _, id := range function.Components {
				if _, known := Component(id); !known {
					return fmt.Errorf("specdata/corelib: runtime %q.%q names unknown component %q", module.ID, function.Name, id)
				}
				if seenComponents[id] {
					return fmt.Errorf("specdata/corelib: runtime %q.%q repeats component %q", module.ID, function.Name, id)
				}
				seenComponents[id] = true
			}
		}
	}
	return nil
}
