// Package corelib is the compiler-owned declaration table for core-library
// modules: modules reached only through an `import ... from std.<module>`
// alias that emit no module artifact of their own. It imports only
// compiler/types, never the checker or generator, so the dependency graph
// stays one-way: compiler/types <- compiler/corelib <- compiler/checker,
// compiler/generator.
package corelib

import compilerTypes "hexal/compiler/types"

// Param is one runtime function parameter's shape, at the level corelib can
// express without a checking module's own type Environment: a fixed scalar,
// or a Slice of a fixed element with a fixed writability.
type Param uint8

const (
	ParamHeap Param = iota
	ParamMutByteSlice
)

// Result is one runtime function's success shape. Every Result except
// ResultSize unions with the built-in Error type at the call site.
type Result uint8

const (
	ResultString Result = iota
	ResultStringSlice
	ResultNil
	ResultSize
)

// Function is one exported module function. A moved capability's static
// operation carries Builtin, the name of the checker operation that resolves
// it; its parameters, result, and errors are the existing checker contract,
// so no runtime shape is restated here. A core-library runtime function
// carries Runtime, the emitted C entry point, and its Params/Result; its
// stable ErrorKind and fixed message are populated by the runtime template
// itself, so they never cross this boundary.
type Function struct {
	Builtin string
	Params  []Param
	Result  Result
	Runtime string
}

// Module is one core-library module's exports: canonical types plus module
// functions.
type Module struct {
	Types     map[string]compilerTypes.Type
	Functions map[string]Function
}

// Modules is the core-library module table, keyed by canonical module path
// (the collection-path import target). A core library emits no module
// artifact; a moved capability's types keep their canonical compiler/types
// identity and existing component, and a runtime function is a fixed entry
// point selected on demand.
var modules = map[string]Module{
	"std/io": {
		Types: map[string]compilerTypes.Type{
			"IO":    compilerTypes.IOType,
			"Bytes": compilerTypes.BytesType,
			"Seek":  compilerTypes.SeekType,
		},
		Functions: map[string]Function{
			"stdin":      {Builtin: "io_stdin"},
			"stdout":     {Builtin: "io_stdout"},
			"stderr":     {Builtin: "io_stderr"},
			"bytes_over": {Builtin: "io_bytes_over"},
		},
	},
	"std/fs": {
		Types: map[string]compilerTypes.Type{
			"File":     compilerTypes.FileType,
			"FileMode": compilerTypes.FileModeType,
		},
		Functions: map[string]Function{
			"open": {Builtin: "file_open"},
		},
	},
	"std/time": {
		Types: map[string]compilerTypes.Type{
			"Duration": compilerTypes.DurationType,
			"Instant":  compilerTypes.InstantType,
			"WallTime": compilerTypes.WallTimeType,
		},
		Functions: map[string]Function{
			"nanoseconds":  {Builtin: "time_nanoseconds"},
			"microseconds": {Builtin: "time_microseconds"},
			"milliseconds": {Builtin: "time_milliseconds"},
			"seconds":      {Builtin: "time_seconds"},
			"now":          {Builtin: "time_now"},
			"wall_time":    {Builtin: "time_wall_time"},
			"sleep":        {Builtin: "time_sleep"},
		},
	},
	"std/net": {
		Types: map[string]compilerTypes.Type{
			"Address":       compilerTypes.AddressType,
			"TcpConnection": compilerTypes.TcpConnectionType,
			"TcpListener":   compilerTypes.TcpListenerType,
		},
		Functions: map[string]Function{
			"parse_address": {Builtin: "address_parse"},
			"resolve":       {Builtin: "dns_resolve"},
			"connect":       {Builtin: "tcp_connect"},
			"listen":        {Builtin: "tcp_listen"},
		},
	},
	"std/process": {
		Types: map[string]compilerTypes.Type{
			"Process":             compilerTypes.ProcessType,
			"Pipe":                compilerTypes.PipeType,
			"ProcessOptions":      compilerTypes.ProcessOptionsType,
			"StartedProcess":      compilerTypes.StartedProcessType,
			"Environment":         compilerTypes.EnvironmentType,
			"EnvironmentVariable": compilerTypes.EnvironmentVariableType,
			"ProcessStream":       compilerTypes.ProcessStreamType,
			"ExitStatus":          compilerTypes.ExitStatusType,
		},
		Functions: map[string]Function{
			"start": {Builtin: "process_start"},
		},
	},
	"std/signal": {
		Types: map[string]compilerTypes.Type{
			"Signal":  compilerTypes.SignalType,
			"Signals": compilerTypes.SignalsType,
		},
		Functions: map[string]Function{
			"subscribe": {Builtin: "signals_subscribe"},
		},
	},
	"std/terminal": {
		Types: map[string]compilerTypes.Type{
			"TerminalSize": compilerTypes.TerminalSizeType,
		},
		Functions: map[string]Function{
			"is_attached": {Builtin: "terminal_is_attached"},
			"size":        {Builtin: "terminal_size"},
		},
	},
	"std/program": {
		Functions: map[string]Function{
			"arguments":             {Result: ResultStringSlice, Runtime: "hex_program_arguments"},
			"current_directory":     {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_current_directory"},
			"home_directory":        {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_home_directory"},
			"temporary_directory":   {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_temporary_directory"},
			"executable_path":       {Params: []Param{ParamHeap}, Result: ResultString, Runtime: "hex_program_executable_path"},
			"available_parallelism": {Result: ResultSize, Runtime: "hex_program_available_parallelism"},
		},
	},
	"std/entropy": {
		Functions: map[string]Function{
			"fill": {Params: []Param{ParamMutByteSlice}, Result: ResultNil, Runtime: "hex_entropy_fill"},
		},
	},
}

// IsModule reports whether path names a known core-library module.
func IsModule(path string) bool {
	_, ok := modules[path]
	return ok
}

// LookupType resolves one exported type of a known core-library module to its
// canonical compiler/types identity.
func LookupType(path, name string) (compilerTypes.Type, bool) {
	module, ok := modules[path]
	if !ok {
		return compilerTypes.Type{}, false
	}
	typ, ok := module.Types[name]
	return typ, ok
}

// Lookup resolves one exported function of a known core-library module.
func Lookup(path, name string) (Function, bool) {
	module, ok := modules[path]
	if !ok {
		return Function{}, false
	}
	function, ok := module.Functions[name]
	return function, ok
}

// FunctionByRuntime resolves one emitted C runtime entry point back to its
// owning module and declaration. Runtime entry points are unique across every
// core-library function, so a checked call carrying only the runtime name
// still resolves its result shape and parameter list.
func FunctionByRuntime(runtime string) (string, Function, bool) {
	for path, module := range modules {
		for _, function := range module.Functions {
			if function.Runtime == runtime {
				return path, function, true
			}
		}
	}
	return "", Function{}, false
}
